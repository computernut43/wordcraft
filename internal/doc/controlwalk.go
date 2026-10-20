package doc

import (
	"cmp"
	"slices"
	"unicode/utf8"
)

// Walks over content controls (crates/doc/src/control.rs): a control around text is a
// ControlStart … ControlEnd marker pair in the paragraph text; pairs nest like brackets and never
// cross a block list. Controls whose content starts or ends with a table are kept on the table
// (Table.Controls).

// maxControlDepth is the deepest table nesting scanned.
const maxControlDepth = 16

var objLen = utf8.RuneLen(Obj)

// controlEvent is a control boundary in a block list (not looking into table cells): a control
// opens or closes at a marker at Off of paragraph Block, or (HasOff false) before / after table
// Block.
type controlEvent struct {
	Open    bool
	Block   int
	Off     int
	HasOff  bool
	Control *ContentControl
}

// controlEvents are the control boundaries of a block list, in order.
func controlEvents(bl Vec[Block]) []controlEvent {
	var out []controlEvent
	for i, b := range bl {
		switch {
		case b.Para != nil:
			p := b.Para
			if !slices.ContainsFunc(p.Objects, isControlMarker) {
				continue
			}
			for _, off := range p.ObjectOffsets() {
				o, _ := p.ObjectAt(off)
				switch v := o.InlineObject.(type) {
				case ControlStart:
					out = append(out, controlEvent{Open: true, Block: i, Off: off, HasOff: true, Control: &v.Control})
				case ControlEnd:
					out = append(out, controlEvent{Block: i, Off: off, HasOff: true})
				}
			}
		case b.Table != nil:
			for k := range b.Table.Controls.Open {
				out = append(out, controlEvent{Open: true, Block: i, Control: &b.Table.Controls.Open[k]})
			}
			for range min(b.Table.Controls.Close, 10_000) {
				out = append(out, controlEvent{Block: i})
			}
		}
	}
	return out
}

func isControlMarker(o Object) bool {
	switch o.InlineObject.(type) {
	case ControlStart, ControlEnd:
		return true
	}
	return false
}

// eventPair is a whole control: the indexes of its open and close events and its depth.
type eventPair struct{ Open, Close, Depth int }

// pairEvents pairs events like brackets: each whole control (in the order they close), and the
// indexes of events without a partner.
func pairEvents(events []controlEvent) ([]eventPair, []int) {
	var open, orphans []int
	var pairs []eventPair
	for i, e := range events {
		if e.Open {
			open = append(open, i)
			continue
		}
		if len(open) == 0 {
			orphans = append(orphans, i)
			continue
		}
		o := open[len(open)-1]
		open = open[:len(open)-1]
		pairs = append(pairs, eventPair{o, i, len(open)})
	}
	orphans = append(orphans, open...)
	slices.Sort(orphans)
	return pairs, orphans
}

// ControlRange is a whole content control around paragraph content.
type ControlRange struct {
	Control ContentControl
	// Start is where the ControlStart marker is; End where the ControlEnd marker is (the content
	// is everything between the two).
	Start, End Pos
	// Depth is the number of enclosing controls in the same block list.
	Depth int
}

// ContentStart is the start of the content (just after the start marker).
func (r ControlRange) ContentStart() Pos {
	return Pos{Story: r.Start.Story, Path: r.Start.Path, Off: r.Start.Off + objLen}
}

// pathOff orders (path, offset) pairs as Rust's tuple comparison.
func pathOff(p Path, off int, q Path, qoff int) int {
	if c := slices.Compare(p, q); c != 0 {
		return c
	}
	return cmp.Compare(off, qoff)
}

// Contains reports whether p is inside the control (between its markers, either edge included).
func (r ControlRange) Contains(p Pos) bool {
	return p.Story.Equal(r.Start.Story) && pathOff(p.Path, p.Off, r.Start.Path, r.Start.Off+objLen) >= 0 &&
		pathOff(p.Path, p.Off, r.End.Path, r.End.Off) <= 0
}

// IsEmpty reports an empty content.
func (r ControlRange) IsEmpty() bool {
	return r.Start.Path.Equal(r.End.Path) && r.End.Off == r.Start.Off+objLen
}

func scanControls(story StoryRef, bl Vec[Block], prefix Path, depth int, out *[]ControlRange) {
	if depth > maxControlDepth {
		return
	}
	events := controlEvents(bl)
	if len(events) > 0 {
		pairs, _ := pairEvents(events)
		for _, pr := range pairs {
			o, c := events[pr.Open], events[pr.Close]
			if !o.Open || !o.HasOff || !c.HasOff {
				continue
			}
			at := func(b, off int) Pos {
				return Pos{Story: story, Path: append(slices.Clip(prefix), uint32(b)), Off: off}
			}
			*out = append(*out, ControlRange{Control: *o.Control, Start: at(o.Block, o.Off), End: at(c.Block, c.Off), Depth: pr.Depth})
		}
	}
	for i, b := range bl {
		if b.Table == nil {
			continue
		}
		for r, row := range b.Table.Rows {
			for c, cell := range row.Cells {
				scanControls(story, cell.Blocks, append(slices.Clip(prefix), uint32(i), uint32(r), uint32(c)), depth+1, out)
			}
		}
	}
}

// ControlRanges are the whole content controls of a story (around paragraph content), in the
// order they close.
func (d *Document) ControlRanges(story StoryRef) []ControlRange {
	var out []ControlRange
	if bl, ok := d.Story(story); ok {
		scanControls(story, bl, nil, 0, &out)
	}
	return out
}

// FillableControls is how many content controls allow form filling across every story.
func (d *Document) FillableControls() int {
	n := 0
	for _, s := range d.StoryRefs() {
		for _, r := range d.ControlRanges(s) {
			if r.Control.Lock.CanEdit() {
				n++
			}
		}
	}
	return n
}

// ControlAt is the innermost content control holding p.
func (d *Document) ControlAt(p Pos) (ControlRange, bool) {
	para := d.ParaAt(p)
	if para == nil {
		return ControlRange{}, false
	}
	if !slices.ContainsFunc(para.Objects, isControlMarker) && !d.storyHasControls(p.Story) {
		return ControlRange{}, false
	}
	var best ControlRange
	found := false
	for _, r := range d.ControlRanges(p.Story) {
		if !r.Contains(p) {
			continue
		}
		// The last of the greatest starts, as Rust's max_by.
		if !found || pathOff(r.Start.Path, r.Start.Off, best.Start.Path, best.Start.Off) >= 0 {
			best, found = r, true
		}
	}
	return best, found
}

func (d *Document) storyHasControls(s StoryRef) bool {
	bl, ok := d.Story(s)
	if !ok {
		return false
	}
	for _, b := range bl {
		any := false
		eachPara(b, func(p *Paragraph) {
			for _, o := range p.Objects {
				if _, ok := o.InlineObject.(ControlStart); ok {
					any = true
				}
			}
		})
		if any {
			return true
		}
	}
	return false
}

// SetControl replaces the control whose start marker is at start; false when there is none.
func (d *Document) SetControl(start Pos, c ContentControl) bool {
	p := d.ParaAt(start)
	if p == nil {
		return false
	}
	if o, ok := p.ObjectAt(start.Off); !ok {
		return false
	} else if _, ok := o.InlineObject.(ControlStart); !ok {
		return false
	}
	mp, err := d.ParaMut(start)
	if err != nil {
		return false
	}
	return mp.SetObjectAt(start.Off, Object{ControlStart{Control: c}})
}

// ControlText is the content of a control as plain text (paragraphs joined by \n).
func (d *Document) ControlText(r ControlRange) string {
	a := r.ContentStart()
	var out []byte
	for _, path := range d.PathsBetween(a, r.End) {
		p := d.Para(a.Story, path)
		if p == nil {
			continue
		}
		from, to := 0, p.Len()
		if path.Equal(a.Path) {
			from = a.Off
		}
		if path.Equal(r.End.Path) {
			to = r.End.Off
		}
		if len(out) > 0 {
			out = append(out, '\n')
		}
		k := 0
		if from <= len(p.Text) && isCharBoundary(p.Text, from) {
			k = p.objectsBefore(from)
		}
		to = max(to, from)
		if from > len(p.Text) || to > len(p.Text) || !isCharBoundary(p.Text, from) || !isCharBoundary(p.Text, to) {
			continue
		}
		for _, c := range p.Text[from:to] {
			if c == Obj {
				if k < len(p.Objects) {
					out = append(out, p.Objects[k].PlainText()...)
				}
				k++
			} else {
				out = utf8.AppendRune(out, c)
			}
		}
	}
	return string(out)
}

type tableMarker struct {
	Path Path
	Nth  int
	Open bool
}

type paraMarker struct {
	Path Path
	Off  int
}

func comparePM(a, b paraMarker) int { return pathOff(a.Path, a.Off, b.Path, b.Off) }

// orphans are the markers without a partner in a block list and the cells inside it.
func orphans(bl Vec[Block], prefix Path, depth int, out *[]paraMarker, tables *[]tableMarker) {
	if depth > maxControlDepth {
		return
	}
	events := controlEvents(bl)
	_, lone := pairEvents(events)
	for _, k := range lone {
		e := events[k]
		path := append(slices.Clip(prefix), uint32(e.Block))
		switch {
		case e.HasOff:
			*out = append(*out, paraMarker{path, e.Off})
		case e.Open:
			// Which of the table's opens: count those before it on the same table.
			nth := 0
			for _, x := range events[:k] {
				if x.Open && !x.HasOff && x.Block == e.Block {
					nth++
				}
			}
			*tables = append(*tables, tableMarker{path, nth, true})
		default:
			*tables = append(*tables, tableMarker{path, 0, false})
		}
	}
	for i, b := range bl {
		if b.Table == nil {
			continue
		}
		for r, row := range b.Table.Rows {
			for c, cell := range row.Cells {
				orphans(cell.Blocks, append(slices.Clip(prefix), uint32(i), uint32(r), uint32(c)), depth+1, out, tables)
			}
		}
	}
}

// BalanceBlocks removes the markers without a partner from a list of blocks (a copied fragment).
func BalanceBlocks(blocks Vec[Block]) Vec[Block] {
	var paras []paraMarker
	var tables []tableMarker
	orphans(blocks, nil, 0, &paras, &tables)
	if len(paras) == 0 && len(tables) == 0 {
		return blocks
	}
	d := New()
	d.Body = slices.Clone(blocks)
	d.removeOrphans(Body, paras, tables)
	return d.Body
}

// HasUnbalancedControls reports a story holding a control marker without its partner.
func (d *Document) HasUnbalancedControls() bool {
	for _, s := range d.StoryRefs() {
		bl, _ := d.Story(s)
		var p []paraMarker
		var t []tableMarker
		orphans(bl, nil, 0, &p, &t)
		if len(p) > 0 || len(t) > 0 {
			return true
		}
	}
	return false
}

// BalanceControls removes control markers (and table wraps' opens and closes) that have no
// partner, and returns how many went.
func (d *Document) BalanceControls() int {
	removed := 0
	for _, s := range d.StoryRefs() {
		bl, _ := d.Story(s)
		var paras []paraMarker
		var tables []tableMarker
		orphans(bl, nil, 0, &paras, &tables)
		removed += d.removeOrphans(s, paras, tables)
	}
	return removed
}

func (d *Document) removeOrphans(s StoryRef, paras []paraMarker, tables []tableMarker) int {
	removed := 0
	// Last first, so earlier offsets in the same paragraph stay valid.
	slices.SortFunc(paras, comparePM)
	for i := len(paras) - 1; i >= 0; i-- {
		m := paras[i]
		if d.Para(s, m.Path) == nil {
			continue
		}
		p, err := d.ParaMutAt(s, m.Path)
		if err == nil && p.Delete(m.Off, m.Off+objLen) == nil {
			removed++
		}
	}
	// Table opens: highest index first per table.
	slices.SortStableFunc(tables, func(a, b tableMarker) int {
		if c := slices.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return cmp.Compare(b.Nth, a.Nth)
	})
	for _, m := range tables {
		if d.Table(s, m.Path) == nil {
			continue
		}
		t, err := d.TableMut(s, m.Path)
		if err != nil {
			continue
		}
		if m.Open {
			if m.Nth < len(t.Controls.Open) {
				t.Controls.Open = slices.Delete(slices.Clone(t.Controls.Open), m.Nth, m.Nth+1)
				removed++
			}
		} else if t.Controls.Close > 0 {
			t.Controls.Close--
			removed++
		}
	}
	return removed
}

// keptControlMarkers are the control markers in a..b whose partner is outside it (a deletion
// keeps them).
func (d *Document) keptControlMarkers(a, b Pos) []paraMarker {
	var out []paraMarker
	paths := d.PathsBetween(a, b)
	if !slices.ContainsFunc(paths, func(p Path) bool {
		q := d.Para(a.Story, p)
		return q != nil && slices.ContainsFunc(q.Objects, isControlMarker)
	}) {
		return out
	}
	inside := func(p Path, off int) bool {
		return pathOff(p, off, a.Path, a.Off) >= 0 && pathOff(p, off+objLen, b.Path, b.Off) <= 0
	}
	ranges := d.ControlRanges(a.Story)
	for _, r := range ranges {
		s, e := inside(r.Start.Path, r.Start.Off), inside(r.End.Path, r.End.Off)
		if s && !e {
			out = append(out, paraMarker{r.Start.Path, r.Start.Off})
		}
		if e && !s {
			out = append(out, paraMarker{r.End.Path, r.End.Off})
		}
	}
	// Markers paired with a table wrap: their partner is never deleted with them.
	for _, path := range paths {
		p := d.Para(a.Story, path)
		if p == nil {
			continue
		}
		for _, off := range p.ObjectOffsets() {
			o, _ := p.ObjectAt(off)
			if !inside(path, off) || !isControlMarker(o) {
				continue
			}
			paired := slices.ContainsFunc(ranges, func(r ControlRange) bool {
				return (r.Start.Path.Equal(path) && r.Start.Off == off) || (r.End.Path.Equal(path) && r.End.Off == off)
			})
			if !paired && d.pairedWithTable(a.Story, path, off) {
				out = append(out, paraMarker{path, off})
			}
		}
	}
	slices.SortFunc(out, comparePM)
	return slices.CompactFunc(out, func(x, y paraMarker) bool { return comparePM(x, y) == 0 })
}

// pairedWithTable reports whether the marker at path/off is paired with a table wrap's open or
// close.
func (d *Document) pairedWithTable(s StoryRef, path Path, off int) bool {
	bl, ok := d.Container(s, path)
	if !ok {
		return false
	}
	events := controlEvents(bl)
	pairs, _ := pairEvents(events)
	me := slices.IndexFunc(events, func(e controlEvent) bool { return e.Block == path.Last() && e.HasOff && e.Off == off })
	if me < 0 {
		return false
	}
	return slices.ContainsFunc(pairs, func(p eventPair) bool {
		return (p.Open == me && !events[p.Close].HasOff) || (p.Close == me && !events[p.Open].HasOff)
	})
}

// TypingPos is where typing at p goes: a caret at the start of a paragraph, before the start of a
// block-level control, goes inside it; one at the end, after a block-level control's end, goes
// inside it too (the control is around the whole paragraph).
func (d *Document) TypingPos(p Pos) Pos {
	para := d.ParaAt(p)
	if para == nil {
		return p
	}
	isStart := func(off int) bool {
		o, ok := para.ObjectAt(off)
		cs, isCS := o.InlineObject.(ControlStart)
		return ok && isCS && cs.Control.Block
	}
	isEnd := func(off int) bool {
		o, ok := para.ObjectAt(off)
		_, isCE := o.InlineObject.(ControlEnd)
		return ok && isCE
	}
	off := p.Off
	// Only block starts before the caret (or none) and block starts right after it.
	beforeOK := off%objLen == 0
	for k := 0; beforeOK && k < off; k += objLen {
		beforeOK = isStart(k)
	}
	if beforeOK && isStart(off) {
		for isStart(off) {
			off += objLen
		}
		return Pos{Story: p.Story, Path: p.Path, Off: off}
	}
	if p.Off == para.Len() && off >= objLen && isEnd(off-objLen) {
		// Step back over the trailing ends that close block-level controls.
		ranges := d.ControlRanges(p.Story)
		for off >= objLen && isEnd(off-objLen) && slices.ContainsFunc(ranges, func(r ControlRange) bool {
			return r.End.Path.Equal(p.Path) && r.End.Off == off-objLen && r.Control.Block
		}) {
			off -= objLen
		}
		return Pos{Story: p.Story, Path: p.Path, Off: off}
	}
	return p
}

// WalkControls calls f with every control in bl (and its tables).
func WalkControls(bl Vec[Block], f func(*ContentControl)) {
	var walk func(bl Vec[Block], depth int)
	walk = func(bl Vec[Block], depth int) {
		if depth > maxControlDepth {
			return
		}
		for _, b := range bl {
			switch {
			case b.Para != nil:
				for _, o := range b.Para.Objects {
					if cs, ok := o.InlineObject.(ControlStart); ok {
						f(&cs.Control)
					}
				}
			case b.Table != nil:
				t := b.Table
				for i := range t.Controls.Open {
					f(&t.Controls.Open[i])
				}
				for _, r := range t.Rows {
					for i := range r.Controls.Open {
						f(&r.Controls.Open[i])
					}
					for _, c := range r.Cells {
						for i := range c.Controls.Open {
							f(&c.Controls.Open[i])
						}
						walk(c.Blocks, depth+1)
					}
				}
			}
		}
	}
	walk(bl, 0)
}

// eachPara calls f on every paragraph of a block in document order, however deep its tables
// nest (read only).
func eachPara(b Block, f func(*Paragraph)) {
	if b.Para != nil {
		f(b.Para)
		return
	}
	todo := []Block{b}
	for len(todo) > 0 {
		x := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		switch {
		case x.Para != nil:
			f(x.Para)
		case x.Table != nil:
			// Last first, so they come off the stack in document order.
			rows := x.Table.Rows
			for r := len(rows) - 1; r >= 0; r-- {
				cells := rows[r].Cells
				for c := len(cells) - 1; c >= 0; c-- {
					bl := cells[c].Blocks
					for k := len(bl) - 1; k >= 0; k-- {
						todo = append(todo, bl[k])
					}
				}
			}
		}
	}
}

// mapBlock is b with f applied to every paragraph (f gets a copy to change), tables copied on
// the way.
func mapBlock(b Block, f func(*Paragraph)) Block {
	switch {
	case b.Para != nil:
		c := b.Para.Clone()
		f(c)
		return Block{Para: c}
	case b.Table != nil:
		t := b.Table.Clone()
		for r := range t.Rows {
			for c := range t.Rows[r].Cells {
				bl := t.Rows[r].Cells[c].Blocks
				for k := range bl {
					bl[k] = mapBlock(bl[k], f)
				}
			}
		}
		return Block{Table: t}
	}
	return b
}
