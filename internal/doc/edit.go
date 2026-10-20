package doc

import (
	"slices"
	"strings"
	"unicode"
)

// Range editing on a Document (crates/doc/src/edit.rs): insert text, split paragraphs, delete
// ranges across paragraphs and tables, apply character and paragraph formatting, copy a range to
// a fragment and paste a fragment.

// Fragment is a copied piece of a document: blocks, where the first and last paragraphs may be
// partial.
type Fragment struct {
	Blocks Vec[Block] `json:"blocks"`
	// Parts are copies of the text box stories its shapes show, by the source document's part id.
	// Pasting gives each box a new story made from these.
	Parts U32Map[Part] `json:"parts,omitempty" serde:"default"`
}

func (Fragment) SerdeDefault() {}

// ToDocument is the fragment as the body of a new document (default styles).
func (f *Fragment) ToDocument() *Document {
	d := New()
	d.Body = slices.Clone(f.Blocks)
	return d
}

// PlainText is the fragment's text.
func (f *Fragment) PlainText() string {
	d := f.ToDocument()
	return d.PlainText(Body)
}

// HasEquation reports an equation in the fragment (in tables too).
func (f *Fragment) HasEquation() bool {
	found := false
	for _, b := range f.Blocks {
		eachPara(b, func(p *Paragraph) {
			for _, o := range p.Objects {
				if _, ok := o.InlineObject.(Equation); ok {
					found = true
				}
			}
		})
	}
	return found
}

// SoleEquation is the equation (its linear format and whether it is a display equation) when
// the fragment is that one equation and nothing else but white space and empty paragraphs.
func (f *Fragment) SoleEquation() (string, bool, bool) {
	var eq *Equation
	for _, b := range f.Blocks {
		if b.Para == nil {
			return "", false, false
		}
		for _, c := range b.Para.Text {
			if unicode.Is(unicode.White_Space, c) {
				continue
			}
			if c != Obj || eq != nil {
				return "", false, false
			}
			// The paragraph's only visible character, so its first object.
			if len(b.Para.Objects) == 0 {
				return "", false, false
			}
			e, ok := b.Para.Objects[0].InlineObject.(Equation)
			if !ok {
				return "", false, false
			}
			eq = &e
		}
	}
	if eq == nil {
		return "", false, false
	}
	return eq.Linear, eq.Display, true
}

// FragmentFromText is a fragment of plain text, one paragraph per line.
func FragmentFromText(s string) Fragment {
	var f Fragment
	for _, l := range strings.Split(s, "\n") {
		f.Blocks = append(f.Blocks, Block{Para: ParagraphWithText(strings.TrimRight(l, "\r"), CharProps{})})
	}
	return f
}

// AppendPiece adds piece at the end, followed by a paragraph mark unless it already ends with
// one: how Word's Spike collects what it cuts. The piece's text box stories get ids after this
// fragment's, so pieces never share one.
func (f *Fragment) AppendPiece(piece Fragment) {
	if len(piece.Blocks) == 0 {
		return
	}
	blocks := slices.Clone(piece.Blocks)
	last := blocks[len(blocks)-1]
	endsWithMark := len(blocks) >= 2 && last.Para != nil && last.Para.IsEmpty()
	if !endsWithMark {
		mark := NewParagraph()
		if last.Para != nil {
			mark.Props = last.Para.Props
		}
		blocks = append(blocks, Block{Para: mark})
	}
	next := uint32(1)
	if ids := sortedPartIDs(f.Parts); len(ids) > 0 {
		next = satAddU32(ids[len(ids)-1], 1)
	}
	newIDs := map[uint32]uint32{}
	for i, old := range sortedPartIDs(piece.Parts) {
		if uint64(next)+uint64(i) > uint64(^uint32(0)) {
			break
		}
		newIDs[old] = next + uint32(i)
	}
	renumber := func(b Block) Block {
		if !blockShowsBox(b) {
			return b
		}
		return mapBlock(b, func(p *Paragraph) {
			p.Objects = renumberBoxes(p.Objects, func(old uint32) (uint32, bool) { n, ok := newIDs[old]; return n, ok })
		})
	}
	for i := range blocks {
		blocks[i] = renumber(blocks[i])
	}
	if len(piece.Parts) > 0 {
		parts := maps(f.Parts)
		for _, old := range sortedPartIDs(piece.Parts) {
			part := piece.Parts[old]
			part.Blocks = slices.Clone(part.Blocks)
			for i := range part.Blocks {
				part.Blocks[i] = renumber(part.Blocks[i])
			}
			if n, ok := newIDs[old]; ok {
				parts[n] = part
			}
		}
		f.Parts = parts
	}
	// The empty paragraph after the last mark is where the next piece starts.
	if n := len(f.Blocks); n >= 2 && f.Blocks[n-1].Para != nil && f.Blocks[n-1].Para.IsEmpty() {
		f.Blocks = f.Blocks[:n-1]
	}
	f.Blocks = append(slices.Clip(f.Blocks), blocks...)
}

// blockShowsBox reports a text box story shown anywhere in b.
func blockShowsBox(b Block) bool {
	any := false
	eachPara(b, func(p *Paragraph) {
		for _, o := range p.Objects {
			if len(o.TextBoxes()) > 0 {
				any = true
			}
		}
	})
	return any
}

// renumberBoxes is objs with each text box story (also in groups) given to f: its new id, or false
// to leave it (renumber) — the objects are copied, never changed in place.
func renumberBoxes(objs Vec[Object], f func(old uint32) (uint32, bool)) Vec[Object] {
	out := slices.Clone(objs)
	for i, o := range out {
		out[i] = o.mapTextBoxes(func(old *uint32) *uint32 {
			if old == nil {
				return nil
			}
			if n, ok := f(*old); ok {
				return &n
			}
			return old
		})
	}
	return out
}

// mapTextBoxes is the object with each text box story slot (a shape's, or its group members')
// replaced by f's.
func (o Object) mapTextBoxes(f func(*uint32) *uint32) Object {
	switch v := o.InlineObject.(type) {
	case Shape:
		v.Story = f(v.Story)
		return Object{v}
	case Group:
		children := slices.Clone(v.Children)
		for i, c := range children {
			if s, ok := c.Obj.InlineObject.(Shape); ok {
				s.Story = f(s.Story)
				children[i].Obj = Object{s}
			}
		}
		v.Children = children
		return Object{v}
	}
	return o
}

// deeperThanWalk reports whether block b, in a block list the paragraph walk reaches at depth,
// has cells the walk skips.
func deeperThanWalk(b Block, depth int) bool {
	if b.Table == nil {
		return false
	}
	for _, r := range b.Table.Rows {
		for _, c := range r.Cells {
			if depth+1 > ParaWalkDepth || slices.ContainsFunc(c.Blocks, func(x Block) bool { return deeperThanWalk(x, depth+1) }) {
				return true
			}
		}
	}
	return false
}

// Limits on text boxes inside text boxes.
const (
	// maxBoxDepth is the nesting limit when following text boxes inside text boxes.
	maxBoxDepth = 8
	// maxPastedBoxes is the most text box stories one paste may create.
	maxPastedBoxes = 4096
)

// InsertText inserts plain text at pos (\n inside s is a manual line break; SplitParagraph makes
// paragraph breaks) and returns the position after it.
func (d *Document) InsertText(pos Pos, s string, props CharProps) (Pos, error) {
	p, err := d.ParaMut(pos)
	if err != nil {
		return Pos{}, err
	}
	n, err := p.InsertText(pos.Off, s, props)
	if err != nil {
		return Pos{}, err
	}
	return Pos{Story: pos.Story, Path: pos.Path, Off: pos.Off + n}, nil
}

// InsertObject inserts an inline object at pos and returns the position after it.
func (d *Document) InsertObject(pos Pos, o Object, props CharProps) (Pos, error) {
	p, err := d.ParaMut(pos)
	if err != nil {
		return Pos{}, err
	}
	if err := p.InsertObject(pos.Off, o, props); err != nil {
		return Pos{}, err
	}
	return Pos{Story: pos.Story, Path: pos.Path, Off: pos.Off + objLen}, nil
}

// SplitParagraph splits the paragraph at pos and returns the start of the new (second) one.
func (d *Document) SplitParagraph(pos Pos) (Pos, error) {
	p, err := d.ParaMut(pos)
	if err != nil {
		return Pos{}, err
	}
	tail, err := p.SplitOff(pos.Off)
	if err != nil {
		return Pos{}, err
	}
	i := pos.Path.Last()
	err = d.WithContainer(pos.Story, pos.Path, func(bl *Vec[Block]) error {
		*bl = slices.Insert(slices.Clip(*bl), min(i+1, len(*bl)), Block{Para: tail})
		return nil
	})
	if err != nil {
		return Pos{}, err
	}
	return Pos{Story: pos.Story, Path: pos.Path.WithLast(i + 1)}, nil
}

// InsertBlock inserts a block before the block at path (same container).
func (d *Document) InsertBlock(story StoryRef, path Path, b Block) error {
	i := path.Last()
	return d.WithContainer(story, path, func(bl *Vec[Block]) error {
		if i > len(*bl) {
			return &Error{Kind: BadPath, Detail: path.String()}
		}
		*bl = slices.Insert(slices.Clip(*bl), i, b)
		return nil
	})
}

// RemoveBlock removes the block at path and returns it (the container keeps a paragraph at its
// end).
func (d *Document) RemoveBlock(story StoryRef, path Path) (Block, error) {
	i := path.Last()
	var gone Block
	err := d.WithContainer(story, path, func(bl *Vec[Block]) error {
		if i >= len(*bl) {
			return &Error{Kind: BadPath, Detail: path.String()}
		}
		gone = (*bl)[i]
		*bl = slices.Delete(slices.Clone(*bl), i, i+1)
		if n := len(*bl); n == 0 || (*bl)[n-1].Para == nil {
			*bl = append(*bl, Block{Para: NewParagraph()})
		}
		return nil
	})
	return gone, err
}

// PathsBetween are the paragraph paths between a and b inclusive (same story), in document order.
func (d *Document) PathsBetween(a, b Pos) []Path {
	a, b = Order(a, b)
	if !a.Story.Equal(b.Story) {
		return nil
	}
	var out []Path
	if bl, ok := d.Story(a.Story); ok {
		walkBetween(bl, nil, &out, 0, a.Path, b.Path)
	}
	return out
}

// DeleteRange deletes a..b and returns the position the deletion collapsed to. On error the
// document is unchanged: both ends are checked before anything is deleted.
func (d *Document) DeleteRange(a, b Pos) (Pos, error) {
	a, b = Order(a, b)
	if a.Equal(b) {
		return a, nil
	}
	if !a.Story.Equal(b.Story) {
		return Pos{}, &Error{Kind: Invalid, Detail: "range spans stories"}
	}
	for _, p := range []Pos{a, b} {
		para := d.Para(p.Story, p.Path)
		if para == nil {
			return Pos{}, &Error{Kind: BadPath, Detail: p.Path.String()}
		}
		if para.Clamp(p.Off) != p.Off {
			return Pos{}, badOffset(p.Off)
		}
	}
	// Content control markers whose partner is outside the range stay (the control is emptied,
	// not broken).
	kept := d.keptControlMarkers(a, b)
	keptIn := func(path Path) []int {
		var out []int
		for _, k := range kept {
			if k.Path.Equal(path) {
				out = append(out, k.Off)
			}
		}
		return out
	}
	if a.Path.Equal(b.Path) {
		p, err := d.ParaMut(a)
		if err != nil {
			return Pos{}, err
		}
		if err := p.DeleteExcept(a.Off, b.Off, keptIn(a.Path)); err != nil {
			return Pos{}, err
		}
		return a, nil
	}
	if slices.Equal(a.Path.Parent(), b.Path.Parent()) {
		return d.deleteInContainer(a, b, kept, keptIn)
	}
	return d.deleteAcross(a, b, keptIn)
}

// deleteInContainer deletes a..b whose ends are in the same block list: the ends are trimmed, the
// blocks between dropped and the ends joined.
func (d *Document) deleteInContainer(a, b Pos, kept []paraMarker, keptIn func(Path) []int) (Pos, error) {
	ia, ib := a.Path.Last(), b.Path.Last()
	// Kept markers of the paragraphs in between move to the join.
	type moved struct {
		o     Object
		props CharProps
	}
	var move []moved
	for _, k := range kept {
		if k.Path.Equal(a.Path) || k.Path.Equal(b.Path) {
			continue
		}
		if p := d.Para(a.Story, k.Path); p != nil {
			if o, ok := p.ObjectAt(k.Off); ok {
				move = append(move, moved{o, p.PropsOfChar(k.Off)})
			}
		}
	}
	pa, err := d.ParaMut(a)
	if err != nil {
		return Pos{}, err
	}
	if err := pa.DeleteExcept(a.Off, pa.Len(), keptIn(a.Path)); err != nil {
		return Pos{}, err
	}
	for _, m := range move {
		if err := pa.InsertObject(pa.Len(), m.o, m.props); err != nil {
			return Pos{}, err
		}
	}
	pb, err := d.ParaMut(b)
	if err != nil {
		return Pos{}, err
	}
	if err := pb.DeleteExcept(0, b.Off, keptIn(b.Path)); err != nil {
		return Pos{}, err
	}
	tail := pb.Clone()
	var between []*SectionProps
	err = d.WithContainer(a.Story, a.Path, func(bl *Vec[Block]) error {
		// Section breaks on the marks in between go with their paragraphs.
		if ia+1 <= ib && ib <= len(*bl) {
			for _, x := range (*bl)[ia+1 : ib] {
				if x.Para != nil && x.Para.Section != nil {
					between = append(between, x.Para.Section)
				}
			}
		}
		if ib < len(*bl) && ia < ib {
			*bl = slices.Delete(slices.Clone(*bl), ia+1, ib+1)
		}
		return nil
	})
	if err != nil {
		return Pos{}, err
	}
	tail.Props = ParaProps{}
	// The deleted marks include a's, so a section break there goes too: the joined paragraph ends
	// where b's mark did, and its text takes the following section's setup, as in Word (#2127).
	sect := tail.Section
	tail.Section = nil
	pa, err = d.ParaMut(a)
	if err != nil {
		return Pos{}, err
	}
	keepProps := pa.Props
	pa.Append(tail)
	pa.Props = keepProps
	before := pa.Section
	pa.Section = sect
	if a.Story.IsBody() && len(a.Path) == 1 {
		// Nearest first, as "link to previous" resolved them before the delete.
		following := d.SectionMut(a.Path.Last())
		for i := len(between) - 1; i >= 0; i-- {
			following.Absorb(between[i])
		}
		if before != nil {
			following.Absorb(before)
		}
	}
	return a, nil
}

// deleteAcross deletes a range that enters or leaves a table: the text of every paragraph inside
// goes, then the tables it covers whole.
func (d *Document) deleteAcross(a, b Pos, keptIn func(Path) []int) (Pos, error) {
	// Whether b is at its paragraph's end, before that paragraph is trimmed.
	bAtEnd := false
	if p := d.Para(b.Story, b.Path); p != nil {
		bAtEnd = p.Len() == b.Off
	}
	paths := d.PathsBetween(a, b)
	for i := len(paths) - 1; i >= 0; i-- {
		path := paths[i]
		para := d.Para(a.Story, path)
		if para == nil {
			continue
		}
		from, to := 0, para.Len()
		if path.Equal(a.Path) {
			from = a.Off
		}
		if path.Equal(b.Path) {
			to = b.Off
		}
		p, err := d.ParaMutAt(a.Story, path)
		if err != nil {
			return Pos{}, err
		}
		if err := p.DeleteExcept(from, to, keptIn(path)); err != nil {
			return Pos{}, err
		}
	}
	// Tables the range covers whole, in the innermost container holding both ends, go: those
	// strictly between the ends, the one a is in when it starts at that table's very start, and the
	// one b is in when it ends at that table's very end. A range that is one table and nothing else
	// only empties it.
	common := 0
	for common < len(a.Path) && common < len(b.Path) && a.Path[common] == b.Path[common] {
		common++
	}
	level := common - common%3
	if level >= len(a.Path) || level >= len(b.Path) {
		return a, nil
	}
	ia, ib := int(a.Path[level]), int(b.Path[level])
	if ia >= ib {
		return a, nil
	}
	prefix := slices.Clone(a.Path[:level])
	blockPath := func(i int) Path { return append(slices.Clip(prefix), uint32(i)) }
	all := d.ParaPaths(a.Story)
	inside := func(i int) []Path {
		pre := blockPath(i)
		var out []Path
		for _, q := range all {
			if len(q) > len(pre) && slices.Equal(q[:len(pre)], pre) {
				out = append(out, q)
			}
		}
		return out
	}
	walked := func(i int) bool {
		bl, ok := d.Container(a.Story, blockPath(i))
		return ok && i < len(bl) && !deeperThanWalk(bl[i], level/3)
	}
	aCovers := len(a.Path) > level+1 && a.Off == 0 && walked(ia)
	if aCovers {
		in := inside(ia)
		aCovers = len(in) > 0 && in[0].Equal(a.Path)
	}
	bCovers := len(b.Path) > level+1 && walked(ib)
	if bCovers {
		in := inside(ib)
		bCovers = len(in) > 0 && in[len(in)-1].Equal(b.Path) && bAtEnd
	}
	first, last := ia+1, ib-1
	if aCovers {
		first = ia
	}
	if bCovers {
		last = ib
	}
	removed := 0
	err := d.WithContainer(a.Story, blockPath(ia), func(bl *Vec[Block]) error {
		out := slices.Clone(*bl)
		for i := min(last, len(out)-1); i >= first; i-- {
			if out[i].Table != nil {
				out = slices.Delete(out, i, i+1)
				removed++
			}
		}
		if removed == 0 {
			return nil
		}
		// A story or cell always ends with a paragraph.
		if n := len(out); n == 0 || out[n-1].Para == nil {
			out = append(out, Block{Para: NewParagraph()})
		}
		*bl = out
		return nil
	})
	if err != nil {
		return Pos{}, err
	}
	if removed == 0 {
		return a, nil
	}
	// The paragraphs left between the ends join, as in a range within one container.
	at := a
	if aCovers {
		at = Pos{Story: a.Story, Path: blockPath(ia)}
	}
	end := Pos{Story: a.Story, Path: blockPath(max(ib-removed, 0))}
	if bCovers {
		end = Pos{Story: a.Story, Path: blockPath(max(ib+1-removed, 0))}
	}
	paras := func(p Pos) bool { return len(p.Path) == level+1 && d.Para(p.Story, p.Path) != nil }
	if paras(at) && paras(end) && slices.Compare(at.Path, end.Path) <= 0 {
		return d.DeleteRange(at, end)
	}
	if d.Para(at.Story, at.Path) != nil {
		return at, nil
	}
	return d.Clamp(at), nil
}

// FormatRange applies f to the character formatting of a..b. A collapsed range changes nothing.
func (d *Document) FormatRange(a, b Pos, f func(*CharProps)) error {
	a, b = Order(a, b)
	for _, path := range d.PathsBetween(a, b) {
		para, err := d.ParaMutAt(a.Story, path)
		if err != nil {
			return err
		}
		from, to := 0, para.Len()
		if path.Equal(a.Path) {
			from = a.Off
		}
		if path.Equal(b.Path) {
			to = b.Off
		}
		if err := para.Format(from, to, f); err != nil {
			return err
		}
		// A fully selected paragraph also gets the mark formatted (Word does), but a hyperlink
		// ends before the mark (#413).
		if to == para.Len() && (from == 0 || !path.Equal(a.Path)) {
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

// ParaPathsIn are the paragraphs paragraph formatting of a..b applies to: PathsBetween without
// the last paragraph when a non-empty range ends at its start. A caret applies to its own
// paragraph.
func (d *Document) ParaPathsIn(a, b Pos) []Path {
	a, b = Order(a, b)
	paths := d.PathsBetween(a, b)
	if b.Off == 0 && !a.Path.Equal(b.Path) && len(paths) > 1 && paths[len(paths)-1].Equal(b.Path) {
		paths = paths[:len(paths)-1]
	}
	return paths
}

// FormatParagraphs applies f to the paragraph properties of every paragraph a..b covers.
func (d *Document) FormatParagraphs(a, b Pos, f func(*ParaProps)) error {
	for _, path := range d.ParaPathsIn(a, b) {
		p, err := d.ParaMutAt(a.Story, path)
		if err != nil {
			return err
		}
		f(&p.Props)
	}
	return nil
}

// CopyRange copies a..b into a fragment (with copies of the text boxes in it).
func (d *Document) CopyRange(a, b Pos) Fragment {
	blocks := BalanceBlocks(d.copyBlocks(a, b))
	return Fragment{Blocks: blocks, Parts: d.textBoxParts(blocks)}
}

// BodyFragment is the whole body as a fragment (with its text boxes).
func (d *Document) BodyFragment() Fragment {
	blocks := slices.Clone(d.Body)
	return Fragment{Blocks: blocks, Parts: d.textBoxParts(blocks)}
}

// textBoxParts are copies of the text box stories shapes in blocks show, nested boxes included.
func (d *Document) textBoxParts(blocks Vec[Block]) U32Map[Part] {
	out := U32Map[Part]{}
	var todo []uint32
	boxIDs := func(p *Paragraph) {
		for _, o := range p.Objects {
			todo = append(todo, o.TextBoxes()...)
		}
	}
	for _, b := range blocks {
		eachPara(b, boxIDs)
	}
	for len(todo) > 0 {
		id := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if _, ok := out[id]; ok {
			continue
		}
		part, ok := d.Parts[id]
		if !ok || part.Kind != PartTextBox {
			continue
		}
		for _, b := range part.Blocks {
			eachPara(b, boxIDs)
		}
		out[id] = part
	}
	return out
}

func (d *Document) copyBlocks(a, b Pos) Vec[Block] {
	a, b = Order(a, b)
	var out Vec[Block]
	if !a.Story.Equal(b.Story) {
		return out
	}
	trim := func(p *Paragraph, from, to int) *Paragraph {
		q := p.Clone()
		_ = q.Delete(min(to, q.Len()), q.Len())
		_ = q.Delete(0, min(from, q.Len()))
		return q
	}
	if slices.Equal(a.Path.Parent(), b.Path.Parent()) {
		bl, ok := d.Container(a.Story, a.Path)
		if !ok {
			return out
		}
		for i := a.Path.Last(); i <= b.Path.Last(); i++ {
			if i >= len(bl) {
				break
			}
			blk := bl[i]
			if blk.Table != nil {
				out = append(out, Block{Table: blk.Table})
				continue
			}
			p := blk.Para
			from, to := 0, p.Len()
			if i == b.Path.Last() {
				to = b.Off
			}
			if i == a.Path.Last() {
				from = a.Off
			}
			q := trim(p, from, to)
			if i == b.Path.Last() {
				if bp := d.Para(a.Story, b.Path); bp != nil && to < bp.Len() {
					q.Section = nil
				}
			}
			out = append(out, Block{Para: q})
		}
		return out
	}
	for _, path := range d.PathsBetween(a, b) {
		p := d.Para(a.Story, path)
		if p == nil {
			continue
		}
		from, to := 0, p.Len()
		if path.Equal(b.Path) {
			to = b.Off
		}
		if path.Equal(a.Path) {
			from = a.Off
		}
		q := trim(p, from, to)
		q.Section = nil
		out = append(out, Block{Para: q})
	}
	return out
}

// adoptTextBoxes gives every text box in blocks a new story of its own, copied from parts (or,
// for a fragment without them, from this document's text box of that id). A box whose story can't
// be found keeps no story rather than showing an unrelated one.
func (d *Document) adoptTextBoxes(blocks Vec[Block], parts U32Map[Part], depth int, budget *int) Vec[Block] {
	if !slices.ContainsFunc(blocks, blockShowsBox) {
		return blocks
	}
	out := slices.Clone(blocks)
	for i := range out {
		out[i] = mapBlock(out[i], func(p *Paragraph) {
			objs := slices.Clone(p.Objects)
			for k, o := range objs {
				objs[k] = o.mapTextBoxes(func(old *uint32) *uint32 {
					if old == nil {
						return nil
					}
					if id, ok := d.adoptTextBox(*old, parts, depth, budget); ok {
						return &id
					}
					return nil
				})
			}
			p.Objects = objs
		})
	}
	return out
}

// adoptTextBox is a new text box story copied from old; its own boxes get new stories too.
func (d *Document) adoptTextBox(old uint32, parts U32Map[Part], depth int, budget *int) (uint32, bool) {
	if depth >= maxBoxDepth || *budget == 0 {
		return 0, false
	}
	src, ok := parts[old]
	if !ok {
		src, ok = d.Parts[old]
		if !ok || src.Kind != PartTextBox {
			return 0, false
		}
	}
	// The copy keeps its text direction and alignment, not its link (a copy stands alone).
	body := src.Body
	body.Next = nil
	*budget--
	blocks := d.adoptTextBoxes(slices.Clone(src.Blocks), parts, depth+1, budget)
	id, err := d.TryAddPart(PartTextBox, blocks)
	if err != nil {
		return 0, false
	}
	part := d.Parts[id]
	part.Body = body
	d.Parts[id] = part
	return id, true
}

// InsertFragment inserts a fragment at pos and returns the position after the pasted content.
func (d *Document) InsertFragment(pos Pos, frag Fragment) (Pos, error) {
	if len(frag.Blocks) == 0 {
		return pos, nil
	}
	if d.ParaAt(pos) == nil {
		return Pos{}, &Error{Kind: BadPath, Detail: pos.Path.String()}
	}
	// Pasted text boxes get stories of their own (never shared with the copy's source).
	budget := maxPastedBoxes
	blocks := d.adoptTextBoxes(slices.Clone(frag.Blocks), frag.Parts, 0, &budget)
	// Single paragraph: inline insert keeping runs.
	if len(blocks) == 1 && blocks[0].Para != nil {
		src := blocks[0].Para.Clone()
		target, err := d.ParaMut(pos)
		if err != nil {
			return Pos{}, err
		}
		tail, err := target.SplitOff(pos.Off)
		if err != nil {
			return Pos{}, err
		}
		added := src.Len()
		keep := target.Props
		sect := tail.Section
		src.Section = nil
		target.Append(src)
		target.Append(tail)
		target.Props = keep
		target.Section = sect
		return Pos{Story: pos.Story, Path: pos.Path, Off: pos.Off + added}, nil
	}
	// Multi-block: split, merge the first into the left part, the last into the right part.
	after, err := d.SplitParagraph(pos)
	if err != nil {
		return Pos{}, err
	}
	left := pos.Path
	first := blocks[0]
	rest := blocks[1:]
	var last *Block
	if len(rest) > 0 {
		last = &rest[len(rest)-1]
		rest = rest[:len(rest)-1]
	}
	idx := left.Last() + 1
	if first.Para != nil {
		l, err := d.ParaMutAt(pos.Story, left)
		if err != nil {
			return Pos{}, err
		}
		keep := l.Props
		p2 := first.Para.Clone()
		pprops := p2.Props
		p2.Section = nil
		l.Append(p2)
		l.Props = keep
		if keep.IsEmpty() {
			l.Props = pprops
		}
	} else {
		if err := d.InsertBlock(pos.Story, left.WithLast(idx), first); err != nil {
			return Pos{}, err
		}
		idx++
	}
	for _, b := range rest {
		if err := d.InsertBlock(pos.Story, left.WithLast(idx), b); err != nil {
			return Pos{}, err
		}
		idx++
	}
	right := after.Path.WithLast(idx)
	switch {
	case last == nil:
		return Pos{Story: pos.Story, Path: right}, nil
	case last.Para != nil:
		r, err := d.ParaMutAt(pos.Story, right)
		if err != nil {
			return Pos{}, err
		}
		tail := r.Clone()
		*r = *last.Para.Clone()
		off := r.Len()
		sect := tail.Section
		r.Append(tail)
		r.Section = sect
		return Pos{Story: pos.Story, Path: right, Off: off}, nil
	default:
		if err := d.InsertBlock(pos.Story, right, *last); err != nil {
			return Pos{}, err
		}
		return Pos{Story: pos.Story, Path: right.WithLast(idx + 1)}, nil
	}
}

// SetStory replaces every block of a story.
func (d *Document) SetStory(story StoryRef, blocks Vec[Block]) error {
	return d.withStory(story, func(bl *Vec[Block]) error {
		*bl = slices.Clone(blocks)
		if len(*bl) == 0 {
			*bl = Vec[Block]{{Para: NewParagraph()}}
		}
		return nil
	})
}

// usesStyle reports a paragraph anywhere in b (also inside tables, 16 levels deep) using
// paragraph or character style id.
func usesStyle(b Block, id string, depth int) bool {
	is := func(s *string) bool { return s != nil && *s == id }
	switch {
	case b.Para != nil:
		p := b.Para
		return is(p.Props.Style) || is(p.Mark.Style) || slices.ContainsFunc(p.Runs, func(r Run) bool { return is(r.Props.Style) })
	case b.Table != nil && depth < 16:
		for _, r := range b.Table.Rows {
			for _, c := range r.Cells {
				if slices.ContainsFunc(c.Blocks, func(x Block) bool { return usesStyle(x, id, depth+1) }) {
					return true
				}
			}
		}
	}
	return false
}

// restyleBlock points every paragraph and run in b that uses style from at to; it returns the
// block and how many paragraphs changed.
func restyleBlock(b Block, from string, to *string, depth int) (Block, int) {
	fix := func(s **string) bool {
		if *s != nil && **s == from {
			*s = to
			return true
		}
		return false
	}
	switch {
	case b.Para != nil:
		p := b.Para.Clone()
		changed := fix(&p.Props.Style)
		changed = fix(&p.Mark.Style) || changed
		runs := slices.Clone(p.Runs)
		for i := range runs {
			changed = fix(&runs[i].Props.Style) || changed
		}
		if !changed {
			return b, 0
		}
		p.Runs = runs
		p.Normalize()
		return Block{Para: p}, 1
	case b.Table != nil:
		if depth >= 16 {
			return b, 0
		}
		t := b.Table.Clone()
		n := 0
		for r := range t.Rows {
			for c := range t.Rows[r].Cells {
				bl := t.Rows[r].Cells[c].Blocks
				for k := range bl {
					if usesStyle(bl[k], from, depth+1) {
						var m int
						bl[k], m = restyleBlock(bl[k], from, to, depth+1)
						n += m
					}
				}
			}
		}
		return Block{Table: t}, n
	}
	return b, 0
}

// Restyle points every paragraph and run that uses paragraph or character style from at to (nil:
// the default style), in every story, tables included. It returns how many paragraphs changed.
func (d *Document) Restyle(from string, to *string) int {
	n := 0
	for _, s := range d.StoryRefs() {
		bl, _ := d.Story(s)
		if !slices.ContainsFunc(bl, func(b Block) bool { return usesStyle(b, from, 0) }) {
			continue
		}
		_ = d.withStory(s, func(bl *Vec[Block]) error {
			for i := range *bl {
				if usesStyle((*bl)[i], from, 0) {
					var m int
					(*bl)[i], m = restyleBlock((*bl)[i], from, to, 0)
					n += m
				}
			}
			return nil
		})
	}
	return n
}

// ConvertNotes converts notes between footnotes and endnotes: footnotes (foot) become endnotes
// and endnotes (end) footnotes, both at once to swap them. Each note's kind changes, as do its
// reference marks wherever they are and the note styles. It returns how many notes changed.
func (d *Document) ConvertNotes(foot, end bool) int {
	flips := map[uint32]bool{}
	for id, p := range d.Parts {
		if (foot && p.Kind == PartFootnote) || (end && p.Kind == PartEndnote) {
			flips[id] = true
		}
	}
	if len(flips) == 0 {
		return 0
	}
	swap := func(s **string) {
		if *s == nil {
			return
		}
		var to string
		switch **s {
		case "FootnoteText":
			to = "EndnoteText"
		case "EndnoteText":
			to = "FootnoteText"
		case "FootnoteReference":
			to = "EndnoteReference"
		case "EndnoteReference":
			to = "FootnoteReference"
		default:
			return
		}
		*s = &to
	}
	refers := func(p *Paragraph) bool {
		return slices.ContainsFunc(p.Objects, func(o Object) bool { r, ok := o.InlineObject.(NoteRef); return ok && flips[r.ID] })
	}
	convert := func(p *Paragraph, inNote bool) {
		if !inNote && !refers(p) {
			return
		}
		offs := p.ObjectOffsets()
		var at []int
		objs := slices.Clone(p.Objects)
		for k, o := range objs {
			if r, ok := o.InlineObject.(NoteRef); ok && flips[r.ID] {
				if r.Kind == Footnote {
					r.Kind = Endnote
				} else {
					r.Kind = Footnote
				}
				objs[k] = Object{r}
				if k < len(offs) {
					at = append(at, offs[k])
				}
			}
		}
		p.Objects = objs
		runs := slices.Clone(p.Runs)
		start := 0
		for i := range runs {
			e := satAdd(start, runs[i].Len)
			if slices.ContainsFunc(at, func(o int) bool { return o >= start && o < e }) {
				swap(&runs[i].Props.Style)
			}
			start = e
		}
		p.Runs = runs
		if inNote {
			swap(&p.Props.Style)
			swap(&p.Mark.Style)
		}
	}
	touches := func(b Block) bool {
		any := false
		eachPara(b, func(p *Paragraph) { any = any || refers(p) })
		return any
	}
	body := slices.Clone(d.Body)
	for i, b := range body {
		if touches(b) {
			body[i] = mapBlock(b, func(p *Paragraph) { convert(p, false) })
		}
	}
	d.Body = body
	parts := maps(d.Parts)
	for id, part := range parts {
		own := flips[id]
		if own {
			if part.Kind == PartFootnote {
				part.Kind = PartEndnote
			} else {
				part.Kind = PartFootnote
			}
		}
		blocks := slices.Clone(part.Blocks)
		for i, b := range blocks {
			if own || touches(b) {
				blocks[i] = mapBlock(b, func(p *Paragraph) { convert(p, own) })
			}
		}
		part.Blocks = blocks
		parts[id] = part
	}
	d.Parts = parts
	return len(flips)
}
