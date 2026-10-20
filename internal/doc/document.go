package doc

// Document-level edits, ported from crates/doc/src/{lib,edit}.rs for what the model covers so
// far: paragraphs at the top level of the body. Paragraphs inside tables and other stories
// aren't reachable yet; the engine refuses commands that would need them.

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/computernut43/craft-libs/serdejson"
)

// Clone copies the document for an undo snapshot. Paragraphs are shared: every edit goes
// through ParaMut, which replaces the paragraph with a copy first (copy on write), so a snapshot
// never changes. Inline objects and maps are never changed in place.
func (d *Document) Clone() *Document {
	c := *d
	c.Body = append(Vec[Block](nil), d.Body...)
	return &c
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
	return blocksHaveObject(d.Body, typ)
}

func blocksHaveObject(bl Vec[Block], typ string) bool {
	for _, b := range bl {
		if b.Table != nil {
			for _, r := range b.Table.Rows {
				for _, c := range r.Cells {
					if blocksHaveObject(c.Blocks, typ) {
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
			if o.Type() == typ {
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
