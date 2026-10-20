package doc

// Document-level edits, ported from crates/doc/src/{lib,edit}.rs for what the model covers so
// far: paragraphs at the top level of the body. Paragraphs inside tables and other stories
// aren't reachable yet; the engine refuses commands that would need them.

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/computernut43/craft-libs/serdejson"
)

// Clone copies the document for an undo snapshot. Paragraphs are shared: every edit goes
// through ParaMut, which replaces the paragraph with a copy first (copy on write), so a snapshot
// never changes. Raw parts and maps are never changed in place.
func (d *Document) Clone() *Document {
	c := *d
	c.Body = append(Vec[Block](nil), d.Body...)
	return &c
}

// topIndex is the body block a top-level path points at, or false.
func (d *Document) topIndex(p Pos) (int, bool) {
	if !p.Story.IsBody() || len(p.Path) != 1 || int(p.Path[0]) >= len(d.Body) {
		return 0, false
	}
	return int(p.Path[0]), true
}

// ParaAt is the paragraph at p, or nil (also for paragraphs the model can't reach yet).
func (d *Document) ParaAt(p Pos) *Paragraph {
	if i, ok := d.topIndex(p); ok {
		return d.Body[i].Para
	}
	return nil
}

// ParaMut is the paragraph at p for editing: a copy now owned by d (copy on write).
func (d *Document) ParaMut(p Pos) (*Paragraph, error) {
	i, ok := d.topIndex(p)
	if !ok || d.Body[i].Para == nil {
		return nil, &Error{Kind: BadPath, Detail: p.Path.String()}
	}
	c := d.Body[i].Para.Clone()
	d.Body[i] = Block{Para: c}
	return c, nil
}

// ParaPaths lists the body's top-level paragraph paths in order. complete is false when the body
// has blocks the model can't look into yet (tables), whose paragraphs Rust would list too.
func (d *Document) ParaPaths() (paths []Path, complete bool) {
	complete = true
	for i, b := range d.Body {
		if b.Para != nil {
			paths = append(paths, Path{uint32(i)})
		} else {
			complete = false
		}
	}
	return paths, complete
}

// StartOfBody is where a new session's caret goes: the first paragraph's start. ok is false
// when that paragraph is in a block the model can't reach yet (the body starts with a table).
func (d *Document) StartOfBody() (Pos, bool) {
	if len(d.Body) == 0 {
		return BodyPos(0, 0), true
	}
	return BodyPos(0, 0), d.Body[0].Para != nil
}

// Clamp makes p valid in d: its offset clamped in its paragraph. A position whose paragraph is
// gone moves to the last paragraph before it (or the first).
func (d *Document) Clamp(p Pos) Pos {
	if para := d.ParaAt(p); para != nil {
		return Pos{Story: p.Story, Path: p.Path, Off: para.Clamp(p.Off)}
	}
	paths, _ := d.ParaPaths()
	path := Path{0}
	if len(paths) > 0 {
		path = paths[0]
		for i := len(paths) - 1; i >= 0; i-- {
			if slices.Compare(paths[i], p.Path) <= 0 {
				path = paths[i]
				break
			}
		}
	}
	q := Pos{Story: Body, Path: path}
	if para := d.ParaAt(q); para != nil {
		q.Off = para.Clamp(p.Off)
	}
	return q
}

// InsertText inserts plain text at p and returns the position after it.
func (d *Document) InsertText(p Pos, s string, props CharProps) (Pos, error) {
	para, err := d.ParaMut(p)
	if err != nil {
		return Pos{}, err
	}
	n, err := para.InsertText(p.Off, s, props)
	if err != nil {
		return Pos{}, err
	}
	return Pos{Story: p.Story, Path: p.Path, Off: p.Off + n}, nil
}

// DeleteRange deletes a..b within one paragraph and returns where it collapsed to. (Deleting
// across paragraphs isn't ported yet.)
func (d *Document) DeleteRange(a, b Pos) (Pos, error) {
	a, b = Order(a, b)
	if a.Equal(b) {
		return a, nil
	}
	if a.Story.compare(b.Story) != 0 || !slices.Equal(a.Path, b.Path) {
		return Pos{}, &Error{Kind: Invalid, Detail: "deleting across paragraphs is not ported yet"}
	}
	para, err := d.ParaMut(a)
	if err != nil {
		return Pos{}, err
	}
	if err := para.Delete(a.Off, b.Off); err != nil {
		return Pos{}, err
	}
	return a, nil
}

// FormatRange applies f to the character formatting of a..b over top-level body paragraphs. A
// fully selected paragraph also gets its mark formatted, as in Word; a hyperlink isn't carried
// into the mark (Rust #413).
func (d *Document) FormatRange(a, b Pos, f func(*CharProps)) error {
	a, b = Order(a, b)
	ia, oka := d.topIndex(a)
	ib, okb := d.topIndex(b)
	if !oka || !okb {
		return &Error{Kind: BadPath, Detail: a.Path.String()}
	}
	for i := ia; i <= ib; i++ {
		if d.Body[i].Para == nil {
			return &Error{Kind: Invalid, Detail: "formatting across tables is not ported yet"}
		}
	}
	for i := ia; i <= ib; i++ {
		at := BodyPos(i, 0)
		para, err := d.ParaMut(at)
		if err != nil {
			return err
		}
		from, to := 0, len(para.Text)
		if i == ia {
			from = a.Off
		}
		if i == ib {
			to = b.Off
		}
		if err := para.Format(from, to, f); err != nil {
			return err
		}
		if to == len(para.Text) && (from == 0 || i != ia) {
			style := para.Mark.Style
			f(&para.Mark)
			if para.Mark.Link != nil {
				para.Mark.Link = nil
				para.Mark.Style = nil
				if style != nil && *style != "Hyperlink" {
					para.Mark.Style = style
				}
			}
		}
	}
	return nil
}

// NormalizeRuns repairs every paragraph's runs, in the body and the parts, inside tables too
// (Rust: on opening a JSON document).
func (d *Document) NormalizeRuns() {
	norm := func(p *Paragraph) *Paragraph {
		c := p.Clone()
		c.Normalize()
		return c
	}
	d.Body = mapParas(d.Body, norm)
	if len(d.Parts) > 0 {
		parts := make(U32Map[Part], len(d.Parts))
		for id, p := range d.Parts {
			p.Blocks = mapParas(p.Blocks, norm)
			parts[id] = p
		}
		d.Parts = parts
	}
}

// mapParas is bl with every paragraph, at any depth, replaced by f's; tables are copied on the
// way (copy on write).
func mapParas(bl Vec[Block], f func(*Paragraph) *Paragraph) Vec[Block] {
	out := make(Vec[Block], len(bl))
	for i, b := range bl {
		switch {
		case b.Para != nil:
			out[i] = Block{Para: f(b.Para)}
		case b.Table != nil:
			t := b.Table.Clone()
			for r := range t.Rows {
				for c := range t.Rows[r].Cells {
					t.Rows[r].Cells[c].Blocks = mapParas(t.Rows[r].Cells[c].Blocks, f)
				}
			}
			out[i] = Block{Table: t}
		default:
			out[i] = b
		}
	}
	return out
}

// EnsureNonempty gives every story at least one paragraph, as Rust's ensure_nonempty does after
// every edit: the body one to type in when it has none or ends with a table, every cell of a
// top-level table when it is empty or ends with a table, and every empty part.
func (d *Document) EnsureNonempty() {
	hasPara := false
	for _, b := range d.Body {
		if b.Para != nil {
			hasPara = true
		}
	}
	if !hasPara || (len(d.Body) > 0 && d.Body[len(d.Body)-1].Para == nil) {
		d.Body = append(slices.Clip(d.Body), Block{Para: NewParagraph()})
	}
	needs := func(bl Vec[Block]) bool { return len(bl) == 0 || bl[len(bl)-1].Para == nil }
	for i, b := range d.Body {
		if b.Table == nil || !slices.ContainsFunc(b.Table.Rows, func(r Row) bool {
			return slices.ContainsFunc(r.Cells, func(c Cell) bool { return needs(c.Blocks) })
		}) {
			continue
		}
		t := b.Table.Clone()
		for r := range t.Rows {
			for c := range t.Rows[r].Cells {
				if cell := &t.Rows[r].Cells[c]; needs(cell.Blocks) {
					cell.Blocks = append(cell.Blocks, Block{Para: NewParagraph()})
				}
			}
		}
		d.Body[i] = Block{Table: t}
	}
	for _, p := range d.Parts {
		if len(p.Blocks) == 0 {
			// The map may be shared with an undo snapshot: repair a copy.
			parts := make(U32Map[Part], len(d.Parts))
			for id, p := range d.Parts {
				if len(p.Blocks) == 0 {
					p.Blocks = Vec[Block]{{Para: NewParagraph()}}
				}
				parts[id] = p
			}
			d.Parts = parts
			break
		}
	}
}

// BodyHasObject reports an inline object of the given type ("controlStart", "field", …) in a
// body paragraph, at any depth of tables.
func (d *Document) BodyHasObject(typ string) bool {
	needle := []byte(`"type":"` + typ + `"`)
	return blocksHaveObject(d.Body, needle)
}

func blocksHaveObject(bl Vec[Block], needle []byte) bool {
	for _, b := range bl {
		if b.Table != nil {
			for _, r := range b.Table.Rows {
				for _, c := range r.Cells {
					if blocksHaveObject(c.Blocks, needle) {
						return true
					}
				}
			}
			continue
		}
		if b.Para == nil {
			continue
		}
		for _, o := range b.Para.Objects {
			if bytes.Contains(o, needle) {
				return true
			}
		}
	}
	return false
}

// NeedsDateRefresh reports an unlocked DATE or TIME field anywhere in the document: Rust
// refreshes those on open, which isn't ported yet. (Other fields open as they are.)
func (d *Document) NeedsDateRefresh() bool {
	raw, err := serdejson.MarshalCompact(d)
	if err != nil {
		return true
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return true
	}
	return hasDateField(v, 0)
}

// maxJSONDepth bounds the walk over hostile JSON.
const maxJSONDepth = 10_000

func hasDateField(v any, depth int) bool {
	if depth > maxJSONDepth {
		return true
	}
	switch x := v.(type) {
	case map[string]any:
		if x["type"] == "field" {
			instr, _ := x["instr"].(string)
			locked, _ := x["locked"].(bool)
			if name := fieldName(instr); !locked && (name == "DATE" || name == "TIME") {
				return true
			}
		}
		for _, e := range x {
			if hasDateField(e, depth+1) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasDateField(e, depth+1) {
				return true
			}
		}
	}
	return false
}

// fieldName is a field instruction's name: its first word without a leading "=", upper-cased
// (ASCII), as Rust's layout::fields::field_name.
func fieldName(instr string) string {
	words := strings.Fields(instr)
	if len(words) == 0 {
		return ""
	}
	name := []byte(strings.TrimLeft(words[0], "="))
	for i, c := range name {
		if 'a' <= c && c <= 'z' {
			name[i] = c - ('a' - 'A')
		}
	}
	return string(name)
}
