package doc

import (
	"slices"
	"strings"
)

// Navigation over a document's stories, tables and paragraphs (crates/doc/src/lib.rs). Edits go
// through ContainerMut, ParaMut and TableMut, which copy what they change on the way down (copy
// on write): blocks are shared with undo snapshots.

// ParaWalkDepth is how deep in nested tables ParaPaths goes: cells below this are skipped.
const ParaWalkDepth = 16

// TopPath is the path of top-level block i.
func TopPath(i int) Path { return Path{uint32(i)} }

// WithLast is the path with its last index replaced by i.
func (p Path) WithLast(i int) Path {
	q := slices.Clone(p)
	if len(q) > 0 {
		q[len(q)-1] = uint32(i)
	}
	return q
}

// Parent is the container prefix (everything but the last index).
func (p Path) Parent() []uint32 {
	if len(p) == 0 {
		return nil
	}
	return p[:len(p)-1]
}

// Depth is the nesting depth (0 = top-level story block).
func (p Path) Depth() int { return max(len(p)-1, 0) / 3 }

// Cell is the enclosing table cell: the table block's path, row and cell; false at the top level.
func (p Path) Cell() (Path, int, int, bool) {
	if len(p) < 4 {
		return nil, 0, 0, false
	}
	n := len(p)
	return slices.Clone(p[:n-3]), int(p[n-3]), int(p[n-2]), true
}

// Equal reports the same path.
func (p Path) Equal(o Path) bool { return slices.Equal(p, o) }

// NewPos is the position off in the paragraph at path of story s.
func NewPos(s StoryRef, path Path, off int) Pos { return Pos{Story: s, Path: path, Off: off} }

// PartStory is the story of part id.
func PartStory(id uint32) StoryRef { return StoryRef{Part: &id} }

// Equal reports the same story.
func (s StoryRef) Equal(o StoryRef) bool { return s.compare(o) == 0 }

// Story is the block list of story s; false when there is no such part.
func (d *Document) Story(s StoryRef) (Vec[Block], bool) {
	if s.Part == nil {
		return d.Body, true
	}
	p, ok := d.Parts[*s.Part]
	return p.Blocks, ok
}

// withStory runs fn on story s's block list, to change; a part is copied out of the map (which
// may be shared with an undo snapshot) and stored back in a copy of it.
func (d *Document) withStory(s StoryRef, fn func(bl *Vec[Block]) error) error {
	if s.Part == nil {
		return fn(&d.Body)
	}
	part, ok := d.Parts[*s.Part]
	if !ok {
		return &Error{Kind: BadStory, Detail: storyName(s)}
	}
	part.Blocks = slices.Clone(part.Blocks)
	err := fn(&part.Blocks)
	parts := maps(d.Parts)
	parts[*s.Part] = part
	d.Parts = parts
	return err
}

func storyName(s StoryRef) string {
	if s.Part == nil {
		return "Body"
	}
	return "Part(" + uitoa(*s.Part) + ")"
}

func uitoa(v uint32) string {
	var b [10]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	return string(b[i:])
}

// Container is the block list that holds the block at path.
func (d *Document) Container(s StoryRef, path Path) (Vec[Block], bool) {
	bl, ok := d.Story(s)
	if !ok {
		return nil, false
	}
	parent := path.Parent()
	if len(parent)%3 != 0 {
		return nil, false
	}
	for i := 0; i < len(parent); i += 3 {
		b, r, c := int(parent[i]), int(parent[i+1]), int(parent[i+2])
		if b >= len(bl) || bl[b].Table == nil {
			return nil, false
		}
		t := bl[b].Table
		if r >= len(t.Rows) || c >= len(t.Rows[r].Cells) {
			return nil, false
		}
		bl = t.Rows[r].Cells[c].Blocks
	}
	return bl, true
}

// WithContainer runs fn on the block list that holds the block at path, to change: the tables
// on the way are copied (copy on write).
func (d *Document) WithContainer(s StoryRef, path Path, fn func(bl *Vec[Block]) error) error {
	bad := &Error{Kind: BadPath, Detail: path.String()}
	parent := path.Parent()
	if len(parent)%3 != 0 {
		return bad
	}
	if _, ok := d.Container(s, path); !ok {
		if _, ok := d.Story(s); !ok {
			return &Error{Kind: BadStory, Detail: storyName(s)}
		}
		return bad
	}
	return d.withStory(s, func(bl *Vec[Block]) error {
		for i := 0; i < len(parent); i += 3 {
			b, r, c := int(parent[i]), int(parent[i+1]), int(parent[i+2])
			t := (*bl)[b].Table.Clone()
			(*bl)[b] = Block{Table: t}
			bl = &t.Rows[r].Cells[c].Blocks
		}
		return fn(bl)
	})
}

// BlockAt is the block at path.
func (d *Document) BlockAt(s StoryRef, path Path) (Block, bool) {
	// An empty path is block 0, as Rust's Path::last says.
	bl, ok := d.Container(s, path)
	if !ok || path.Last() >= len(bl) {
		return Block{}, false
	}
	return bl[path.Last()], true
}

// Para is the paragraph at path (read only: change it through ParaMut).
func (d *Document) Para(s StoryRef, path Path) *Paragraph {
	b, ok := d.BlockAt(s, path)
	if !ok {
		return nil
	}
	return b.Para
}

// ParaAt is the paragraph p is in, or nil (read only).
func (d *Document) ParaAt(p Pos) *Paragraph { return d.Para(p.Story, p.Path) }

// ParaMut is the paragraph at p for editing: a copy now owned by d (copy on write).
func (d *Document) ParaMut(p Pos) (*Paragraph, error) { return d.ParaMutAt(p.Story, p.Path) }

// ParaMutAt is the paragraph at path for editing.
func (d *Document) ParaMutAt(s StoryRef, path Path) (*Paragraph, error) {
	var c *Paragraph
	err := d.WithContainer(s, path, func(bl *Vec[Block]) error {
		i := path.Last()
		if i >= len(*bl) || (*bl)[i].Para == nil {
			return &Error{Kind: BadPath, Detail: path.String()}
		}
		c = (*bl)[i].Para.Clone()
		(*bl)[i] = Block{Para: c}
		return nil
	})
	return c, err
}

// Table is the table at path (read only).
func (d *Document) Table(s StoryRef, path Path) *Table {
	b, ok := d.BlockAt(s, path)
	if !ok {
		return nil
	}
	return b.Table
}

// TableMut is the table at path for editing: a copy now owned by d.
func (d *Document) TableMut(s StoryRef, path Path) (*Table, error) {
	var t *Table
	err := d.WithContainer(s, path, func(bl *Vec[Block]) error {
		i := path.Last()
		if i >= len(*bl) || (*bl)[i].Table == nil {
			return &Error{Kind: BadPath, Detail: path.String()}
		}
		t = (*bl)[i].Table.Clone()
		(*bl)[i] = Block{Table: t}
		return nil
	})
	return t, err
}

// ParaPaths are the paths of every paragraph in a story, in document order (tables descended,
// ParaWalkDepth deep).
func (d *Document) ParaPaths(s StoryRef) []Path {
	var out []Path
	if bl, ok := d.Story(s); ok {
		walk(bl, nil, &out, 0)
	}
	return out
}

func walk(bl Vec[Block], prefix Path, out *[]Path, depth int) {
	if depth > ParaWalkDepth {
		return
	}
	for i, b := range bl {
		at := append(slices.Clip(prefix), uint32(i))
		switch {
		case b.Para != nil:
			*out = append(*out, at)
		case b.Table != nil:
			for r, row := range b.Table.Rows {
				for c, cell := range row.Cells {
					walk(cell.Blocks, append(slices.Clip(at), uint32(r), uint32(c)), out, depth+1)
				}
			}
		}
	}
}

// walkBetween is walk for the paragraphs from path a to b inclusive, skipping the blocks outside.
func walkBetween(bl Vec[Block], prefix Path, out *[]Path, depth int, a, b Path) {
	if depth > ParaWalkDepth {
		return
	}
	// Where the paths starting with q lie against x: all before, all after, or either.
	side := func(q, x Path) int {
		m := min(len(q), len(x))
		return slices.Compare(q[:m], x[:m])
	}
	for i, blk := range bl {
		at := append(slices.Clip(prefix), uint32(i))
		if side(at, b) > 0 {
			break
		}
		if side(at, a) < 0 {
			continue
		}
		switch {
		case blk.Para != nil:
			if slices.Compare(at, a) >= 0 && slices.Compare(at, b) <= 0 {
				*out = append(*out, at)
			}
		case blk.Table != nil:
			for r, row := range blk.Table.Rows {
				for c, cell := range row.Cells {
					in := append(slices.Clip(at), uint32(r), uint32(c))
					if side(in, a) >= 0 && side(in, b) <= 0 {
						walkBetween(cell.Blocks, in, out, depth+1, a, b)
					}
				}
			}
		}
	}
}

// StartOf is the first position of a story.
func (d *Document) StartOf(s StoryRef) Pos {
	paths := d.ParaPaths(s)
	path := TopPath(0)
	if len(paths) > 0 {
		path = paths[0]
	}
	return Pos{Story: s, Path: path}
}

// EndOf is the last position of a story.
func (d *Document) EndOf(s StoryRef) Pos {
	paths := d.ParaPaths(s)
	path := TopPath(0)
	if len(paths) > 0 {
		path = paths[len(paths)-1]
	}
	off := 0
	if p := d.Para(s, path); p != nil {
		off = p.Len()
	}
	return Pos{Story: s, Path: path, Off: off}
}

// NextPara is the paragraph path after path in document order.
func (d *Document) NextPara(s StoryRef, path Path) (Path, bool) {
	all := d.ParaPaths(s)
	i, found := slices.BinarySearchFunc(all, path, slices.Compare)
	if !found || i+1 >= len(all) {
		return nil, false
	}
	return all[i+1], true
}

// PrevPara is the paragraph path before path in document order.
func (d *Document) PrevPara(s StoryRef, path Path) (Path, bool) {
	all := d.ParaPaths(s)
	i, found := slices.BinarySearchFunc(all, path, slices.Compare)
	if !found || i == 0 {
		return nil, false
	}
	return all[i-1], true
}

// Clamp makes p valid in d: a valid path, its offset on a character boundary. A position whose
// paragraph is gone moves to the last paragraph before it (or the first).
func (d *Document) Clamp(p Pos) Pos {
	if para := d.ParaAt(p); para != nil {
		return Pos{Story: p.Story, Path: p.Path, Off: para.Clamp(p.Off)}
	}
	story := p.Story
	if _, ok := d.Story(story); !ok {
		story = Body
	}
	all := d.ParaPaths(story)
	path := TopPath(0)
	if len(all) > 0 {
		path = all[0]
		for i := len(all) - 1; i >= 0; i-- {
			if slices.Compare(all[i], p.Path) <= 0 {
				path = all[i]
				break
			}
		}
	}
	off := 0
	if q := d.Para(story, path); q != nil {
		off = q.Clamp(p.Off)
	}
	return Pos{Story: story, Path: path, Off: off}
}

// textMode is which text of a paragraph storyText collects.
type textMode uint8

const (
	textPlain textMode = iota
	textFinal
)

// PlainText is the whole text of a story, paragraphs separated by \n (tables: cells by \t).
func (d *Document) PlainText(s StoryRef) string { return d.storyText(s, textPlain) }

// FinalText is PlainText without tracked-deleted runs. A deleted paragraph mark still yields \n.
func (d *Document) FinalText(s StoryRef) string { return d.storyText(s, textFinal) }

func (d *Document) storyText(s StoryRef, mode textMode) string {
	var out strings.Builder
	if bl, ok := d.Story(s); ok {
		textOf(bl, &out, 0, mode)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func textOf(bl Vec[Block], out *strings.Builder, depth int, mode textMode) {
	if depth > 16 {
		return
	}
	for _, b := range bl {
		switch {
		case b.Para != nil:
			if mode == textFinal {
				out.WriteString(b.Para.FinalText())
			} else {
				out.WriteString(b.Para.PlainText())
			}
			out.WriteByte('\n')
		case b.Table != nil:
			for _, row := range b.Table.Rows {
				cells := make([]string, 0, len(row.Cells))
				for _, c := range row.Cells {
					var s strings.Builder
					textOf(c.Blocks, &s, depth+1, mode)
					cells = append(cells, strings.ReplaceAll(strings.TrimRight(s.String(), "\n"), "\n", " "))
				}
				out.WriteString(strings.Join(cells, "\t"))
				out.WriteByte('\n')
			}
		}
	}
}

// SectionEnd is a section: the top-level body block it ends with and its properties.
type SectionEnd struct {
	Block int
	Props *SectionProps
}

// Sections are the sections of the body in order: each ends with the paragraph holding its break,
// the last with the body.
func (d *Document) Sections() []SectionEnd {
	var v []SectionEnd
	for i, b := range d.Body {
		if b.Para != nil && b.Para.Section != nil {
			v = append(v, SectionEnd{i, b.Para.Section})
		}
	}
	return append(v, SectionEnd{max(len(d.Body)-1, 0), &d.LastSection})
}

// SectionIndexOf is the section that contains top-level body block block (an index into
// Sections).
func (d *Document) SectionIndexOf(block int) int {
	for i, s := range d.Sections() {
		if block <= s.Block {
			return i
		}
	}
	return 0
}

// SectionMut is the section properties of the section containing body block block, to change.
func (d *Document) SectionMut(block int) *SectionProps {
	secs := d.Sections()
	idx := len(secs) - 1
	for i, s := range secs {
		if block <= s.Block {
			idx = i
			break
		}
	}
	if idx+1 < len(secs) {
		end := secs[idx].Block
		if p, err := d.ParaMutAt(Body, TopPath(end)); err == nil && p.Section != nil {
			s := *p.Section
			p.Section = &s
			return p.Section
		}
	}
	return &d.LastSection
}

// RemoveSectionBreak removes the section break on the mark of top-level body paragraph block:
// its section joins the following one. False when there is no break there.
func (d *Document) RemoveSectionBreak(block int) bool {
	if block < 0 || block >= len(d.Body) || d.Body[block].Para == nil || d.Body[block].Para.Section == nil {
		return false
	}
	p, err := d.ParaMutAt(Body, TopPath(block))
	if err != nil {
		return false
	}
	before := p.Section
	p.Section = nil
	d.SectionMut(block).Absorb(before)
	return true
}

// NoteOptions are the footnote (endnote false) or endnote options in effect in section sect: its
// own, else the document's, else Word's defaults (footnotes at the page bottom in 1, 2, 3;
// endnotes at the end of the document in i, ii, iii; numbered on through the document).
func (d *Document) NoteOptions(sect *SectionProps, endnote bool) NoteOptions {
	own, doc, fmt := sect.FootnotePr, d.Settings.FootnotePr, d.Settings.FootnoteFormat
	if endnote {
		own, doc, fmt = sect.EndnotePr, d.Settings.EndnotePr, d.Settings.EndnoteFormat
	}
	pos := or(own.Pos, doc.Pos)
	var p NotePos
	if endnote {
		p = NotesDocEnd
		if pos != nil && (*pos == NotesSectEnd || *pos == NotesDocEnd) {
			p = *pos
		}
	} else {
		p = NotesPageBottom
		if pos != nil && (*pos == NotesPageBottom || *pos == NotesBeneathText) {
			p = *pos
		}
	}
	numFmt := fmt
	if own.NumFmt != nil {
		numFmt = *own.NumFmt
	}
	start := uint32(1)
	if s := or(own.NumStart, doc.NumStart); s != nil {
		start = *s
	}
	var restart NoteRestart
	if r := or(own.NumRestart, doc.NumRestart); r != nil {
		restart = *r
	}
	return NoteOptions{Pos: p, NumFmt: numFmt, NumStart: min(max(start, 1), MaxNoteStart), NumRestart: restart}
}

// AddPart adds a part with a new id and returns it. Part ids run out only past MaxUint32 parts,
// more than memory holds; then nothing is added and the id is 0, which FreeID never gives a part.
func (d *Document) AddPart(kind PartKind, blocks Vec[Block]) uint32 {
	id, err := d.TryAddPart(kind, blocks)
	if err != nil {
		return 0
	}
	return id
}

// TryAddPart is AddPart, or an error, adding nothing, when every part id is taken.
func (d *Document) TryAddPart(kind PartKind, blocks Vec[Block]) (uint32, error) {
	ids := make([]uint32, 0, len(d.Parts))
	for id := range d.Parts {
		ids = append(ids, id)
	}
	id, ok := FreeID(ids, 1)
	if !ok {
		return 0, &Error{Kind: Invalid, Detail: "no free part id"}
	}
	if len(blocks) == 0 {
		blocks = Vec[Block]{{Para: NewParagraph()}}
	}
	parts := maps(d.Parts)
	parts[id] = Part{Kind: kind, Blocks: blocks}
	d.Parts = parts
	return id, nil
}

// TextBoxAnchor is the position just after the shape that owns text box story part (body first,
// then the other parts), for leaving the text box.
func (d *Document) TextBoxAnchor(part uint32) (Pos, bool) {
	stories := []StoryRef{Body}
	for _, id := range sortedPartIDs(d.Parts) {
		if id != part {
			stories = append(stories, PartStory(id))
		}
	}
	for _, s := range stories {
		for _, path := range d.ParaPaths(s) {
			p := d.Para(s, path)
			for _, off := range p.ObjectOffsets() {
				if o, ok := p.ObjectAt(off); ok && slices.Contains(o.TextBoxes(), part) {
					return Pos{Story: s, Path: path, Off: off + len(string(Obj))}, true
				}
			}
		}
	}
	return Pos{}, false
}

// sortedPartIDs are the part ids in order (Rust's BTreeMap order).
func sortedPartIDs(parts U32Map[Part]) []uint32 {
	ids := make([]uint32, 0, len(parts))
	for id := range parts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// StoryRefs are the body and every part's story, in order.
func (d *Document) StoryRefs() []StoryRef {
	out := []StoryRef{Body}
	for _, id := range sortedPartIDs(d.Parts) {
		out = append(out, PartStory(id))
	}
	return out
}

// maps copies a part map.
func maps(m U32Map[Part]) U32Map[Part] {
	out := make(U32Map[Part], len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// FromText is a document of plain text, one paragraph per line.
func FromText(text string) *Document {
	d := New()
	d.Body = nil
	for _, l := range strings.Split(text, "\n") {
		d.Body = append(d.Body, Block{Para: ParagraphWithText(strings.TrimRight(l, "\r"), CharProps{})})
	}
	d.EnsureNonempty()
	return d
}
