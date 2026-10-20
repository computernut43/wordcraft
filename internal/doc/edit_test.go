package doc

import (
	"slices"
	"strings"
	"testing"
)

// Ports of crates/doc/src/edit.rs's tests.

func lines(ls ...string) *Document { return FromText(strings.Join(ls, "\n")) }

func at(story StoryRef, path Path, off int) Pos { return Pos{Story: story, Path: path, Off: off} }

func TestPathsBetweenMatchesEveryPathFiltered(t *testing.T) {
	d := lines("a", "b", "c", "d", "e")
	inner := NewTable(2, 2, 100)
	inner.Rows[1].Cells[0].Blocks = append(inner.Rows[1].Cells[0].Blocks, Block{Para: NewParagraph()})
	tb := NewTable(3, 2, 200)
	tb.Rows[1].Cells[1].Blocks = slices.Insert(tb.Rows[1].Cells[1].Blocks, 0, Block{Table: inner})
	must(t, d.InsertBlock(Body, TopPath(2), Block{Table: tb}))
	must(t, d.InsertBlock(Body, TopPath(4), Block{Table: NewTable(1, 3, 200)}))
	all := d.ParaPaths(Body)
	for _, a := range all {
		for _, b := range all {
			lo, hi := a, b
			if slices.Compare(a, b) > 0 {
				lo, hi = b, a
			}
			var want []Path
			for _, p := range all {
				if slices.Compare(p, lo) >= 0 && slices.Compare(p, hi) <= 0 {
					want = append(want, p)
				}
			}
			if got := d.PathsBetween(at(Body, a, 0), at(Body, b, 0)); !slices.EqualFunc(got, want, Path.Equal) {
				t.Fatalf("%v %v: %v, want %v", a, b, got, want)
			}
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSplitAndJoin(t *testing.T) {
	d := lines("Hello world")
	p, err := d.SplitParagraph(BodyPos(0, 5))
	must(t, err)
	if !p.Equal(BodyPos(1, 0)) || d.PlainText(Body) != "Hello\n world" {
		t.Fatal(p, d.PlainText(Body))
	}
	a, err := d.DeleteRange(BodyPos(0, 5), BodyPos(1, 0))
	must(t, err)
	if !a.Equal(BodyPos(0, 5)) || d.PlainText(Body) != "Hello world" {
		t.Fatal(a, d.PlainText(Body))
	}
}

func TestDeleteRangeWithBadEndLeavesDocumentUnchanged(t *testing.T) {
	d := lines("one", "two", "three")
	before := d.PlainText(Body)
	for _, b := range []Pos{BodyPos(9, 0), BodyPos(1, 99)} {
		if _, err := d.DeleteRange(BodyPos(0, 1), b); err == nil || d.PlainText(Body) != before {
			t.Fatal(b)
		}
	}
	d = lines("abc", "éx")
	if _, err := d.DeleteRange(BodyPos(0, 1), BodyPos(1, 1)); err == nil || d.PlainText(Body) != "abc\néx" {
		t.Fatal("mid-character")
	}
	d = lines("before", "after")
	must(t, d.InsertBlock(Body, TopPath(1), Block{Table: NewTable(2, 1, 200)}))
	_, err := d.InsertText(at(Body, Path{1, 0, 0, 0}, 0), "x", CharProps{})
	must(t, err)
	before = d.PlainText(Body)
	if _, err := d.DeleteRange(BodyPos(0, 99), at(Body, Path{1, 1, 0, 0}, 0)); err == nil || d.PlainText(Body) != before {
		t.Fatal("bad start entering a table")
	}
}

func TestDeleteMulti(t *testing.T) {
	d := lines("one", "two", "three")
	_, err := d.DeleteRange(BodyPos(2, 2), BodyPos(0, 1))
	must(t, err)
	if d.PlainText(Body) != "oree" || len(d.Body) != 1 {
		t.Fatal(d.PlainText(Body))
	}
}

// #2127: a range over two section breaks joins three sections into the last; the headers and
// footers it showed by linking to earlier ones stay, each from the nearest section that named it.
func TestDeleteOverTwoSectionBreaksKeepsTheLinkedHeaders(t *testing.T) {
	d := lines("A", "B", "C")
	set := func(v uint32) HeaderSet { return HeaderSet{Default: &v} }
	s1, s2, s3 := NewSectionProps(), NewSectionProps(), NewSectionProps()
	s1.Headers, s1.Footers = set(1), set(5)
	s2.Headers = set(2)
	s3.MarginLeft = 99
	for i, s := range []SectionProps{s1, s2, s3} {
		p, err := d.ParaMutAt(Body, TopPath(i))
		must(t, err)
		p.Section = &s
	}
	if len(d.Sections()) != 4 {
		t.Fatal("setup")
	}
	_, err := d.DeleteRange(BodyPos(0, 1), BodyPos(2, 0))
	must(t, err)
	secs := d.Sections()
	want := s3
	want.Headers, want.Footers = set(2), set(5)
	if d.PlainText(Body) != "AC" || len(secs) != 2 || !equal(*secs[0].Props, want) {
		t.Fatalf("%q %d %+v", d.PlainText(Body), len(secs), secs[0].Props)
	}
}

func TestFormatAcross(t *testing.T) {
	d := lines("abc", "def")
	must(t, d.FormatRange(BodyPos(0, 1), BodyPos(1, 2), func(c *CharProps) { c.Bold = ptr(true) }))
	p0, p1 := d.Para(Body, TopPath(0)), d.Para(Body, TopPath(1))
	if p0.PropsOfChar(0).Bold != nil || p0.PropsOfChar(1).Bold == nil || p1.PropsOfChar(1).Bold == nil || p1.PropsOfChar(2).Bold != nil {
		t.Fatal("bold over 0:1..1:2")
	}
}

func TestCopyPasteRoundTrip(t *testing.T) {
	d := lines("alpha", "beta", "gamma")
	f := d.CopyRange(BodyPos(0, 2), BodyPos(2, 3))
	if f.PlainText() != "pha\nbeta\ngam" {
		t.Fatal(f.PlainText())
	}
	end, err := d.InsertFragment(BodyPos(2, 5), f)
	must(t, err)
	if d.PlainText(Body) != "alpha\nbeta\ngammapha\nbeta\ngam" || !end.Equal(BodyPos(4, 3)) {
		t.Fatal(d.PlainText(Body), end)
	}
	e, err := d.InsertFragment(BodyPos(0, 0), FragmentFromText("XY"))
	must(t, err)
	if !e.Equal(BodyPos(0, 2)) || !strings.HasPrefix(d.PlainText(Body), "XYalpha") {
		t.Fatal(e)
	}
}

func boxShape(story *uint32) Object {
	return Object{Shape{Kind: ShapeTextBox, W: 144, H: 72, StrokeWidth: 0.75, Story: story}}
}

func textBox(t *testing.T, d *Document, pos Pos, text string) uint32 {
	id := d.AddPart(PartTextBox, Vec[Block]{{Para: ParagraphWithText(text, CharProps{})}})
	_, err := d.InsertObject(pos, boxShape(&id), CharProps{})
	must(t, err)
	return id
}

// boxesIn are the story ids of the boxes in a body paragraph, in order (0 for none).
func boxesIn(d *Document, path Path) []uint32 {
	var out []uint32
	for _, o := range d.Para(Body, path).Objects {
		if s, ok := o.InlineObject.(Shape); ok {
			id := uint32(0)
			if s.Story != nil {
				id = *s.Story
			}
			out = append(out, id)
		}
	}
	return out
}

func TestPastedTextBoxGetsItsOwnStory(t *testing.T) {
	d := lines("ab", "cd")
	id := textBox(t, d, BodyPos(0, 1), "Inside")
	f := d.CopyRange(BodyPos(0, 0), BodyPos(0, 5))
	if ids := sortedPartIDs(f.Parts); !slices.Equal(ids, []uint32{id}) {
		t.Fatal(ids)
	}
	// Edit the original after copying: the paste keeps the copied text.
	_, err := d.InsertText(at(PartStory(id), TopPath(0), 6), " later", CharProps{})
	must(t, err)
	_, err = d.InsertFragment(BodyPos(1, 2), f)
	must(t, err)
	pasted := boxesIn(d, TopPath(1))
	if len(pasted) != 1 || pasted[0] == 0 || pasted[0] == id || d.Parts[pasted[0]].Kind != PartTextBox {
		t.Fatal(pasted)
	}
	n := pasted[0]
	if d.PlainText(PartStory(n)) != "Inside" || d.PlainText(PartStory(id)) != "Inside later" {
		t.Fatal("texts")
	}
	_, err = d.InsertText(at(PartStory(n), TopPath(0), 0), "Copy: ", CharProps{})
	must(t, err)
	if d.PlainText(PartStory(id)) != "Inside later" {
		t.Fatal("editing the copy changed the original")
	}
	_, err = d.InsertFragment(BodyPos(1, 0), f)
	must(t, err)
	again := boxesIn(d, TopPath(1))
	if len(again) != 2 || again[0] == again[1] || slices.Contains(again, id) || slices.Contains(again, 0) {
		t.Fatal(again)
	}
	f2 := d.CopyRange(BodyPos(0, 0), BodyPos(1, 1))
	before := len(d.Parts)
	_, err = d.InsertFragment(BodyPos(1, 0), f2)
	must(t, err)
	if len(d.Parts) != before+1 {
		t.Fatal(len(d.Parts), before)
	}
}

func TestTextBoxPastedIntoAnotherDocumentBringsItsText(t *testing.T) {
	src := lines("x")
	id := textBox(t, src, BodyPos(0, 1), "From the source")
	f := src.CopyRange(BodyPos(0, 0), BodyPos(0, 1+3))
	dst := lines("target")
	hdr := dst.AddPart(PartHeader, Vec[Block]{{Para: ParagraphWithText("Header text", CharProps{})}})
	if hdr != id {
		t.Fatal("same id")
	}
	_, err := dst.InsertFragment(BodyPos(0, 6), f)
	must(t, err)
	got := boxesIn(dst, TopPath(0))
	if len(got) != 1 || got[0] == hdr || dst.PlainText(PartStory(got[0])) != "From the source" || dst.PlainText(PartStory(hdr)) != "Header text" {
		t.Fatal(got)
	}
	dst2 := lines("t")
	_, err = dst2.InsertFragment(BodyPos(0, 1), src.BodyFragment())
	must(t, err)
	if b := boxesIn(dst2, TopPath(0)); len(b) != 1 || dst2.PlainText(PartStory(b[0])) != "From the source" {
		t.Fatal(b)
	}
}

func TestFragmentWithoutPartsNeverShowsAnUnrelatedStory(t *testing.T) {
	d := lines("x")
	hdr := d.AddPart(PartHeader, nil)
	own := d.AddPart(PartTextBox, Vec[Block]{{Para: ParagraphWithText("Mine", CharProps{})}})
	p := NewParagraph()
	missing := uint32(999)
	for _, id := range []*uint32{&hdr, &own, &missing} {
		must(t, p.InsertObject(0, boxShape(id), CharProps{}))
	}
	_, err := d.InsertFragment(BodyPos(0, 1), Fragment{Blocks: Vec[Block]{{Para: p}}})
	must(t, err)
	got := boxesIn(d, TopPath(0))
	if len(got) != 3 || got[0] != 0 || got[1] == 0 || got[1] == own || d.PlainText(PartStory(got[1])) != "Mine" || got[2] != 0 {
		t.Fatal(got)
	}
}

func TestSelfNestedTextBoxPasteIsBounded(t *testing.T) {
	d := lines("x")
	id := textBox(t, d, BodyPos(0, 1), "loop")
	_, err := d.InsertObject(at(PartStory(id), TopPath(0), 0), boxShape(&id), CharProps{})
	must(t, err)
	f := d.CopyRange(BodyPos(0, 0), BodyPos(0, 1+3))
	before := len(d.Parts)
	_, err = d.InsertFragment(BodyPos(0, 0), f)
	must(t, err)
	if added := len(d.Parts) - before; added < 1 || added > maxBoxDepth {
		t.Fatal(added)
	}
}

func TestTablesInPaths(t *testing.T) {
	d := lines("before", "after")
	must(t, d.InsertBlock(Body, TopPath(1), Block{Table: NewTable(2, 2, 200)}))
	paths := d.ParaPaths(Body)
	if len(paths) != 6 || !paths[1].Equal(Path{1, 0, 0, 0}) {
		t.Fatal(paths)
	}
	cell := at(Body, Path{1, 0, 1, 0}, 0)
	e, err := d.InsertText(cell, "x", CharProps{})
	must(t, err)
	next, _ := d.NextPara(Body, Path{1, 0, 1, 0})
	if e.Off != 1 || !next.Equal(Path{1, 1, 0, 0}) || !strings.Contains(d.PlainText(Body), "\tx") {
		t.Fatal(e, next)
	}
	_, err = d.DeleteRange(BodyPos(0, 2), cell)
	must(t, err)
	if d.Para(Body, TopPath(0)).Text != "be" {
		t.Fatal(d.Para(Body, TopPath(0)).Text)
	}
	if _, ok := d.Container(Body, Path{1, 0, 0, 0}); !ok {
		t.Fatal("the table stays")
	}
	if tp, r, c, ok := (Path{1, 0, 1, 0}).Cell(); !ok || !tp.Equal(TopPath(1)) || r != 0 || c != 1 {
		t.Fatal("cell")
	}
}

func paraBlock(text string) Block { return Block{Para: ParagraphWithText(text, CharProps{})} }

func TestAppendedPiecesAreEachFollowedByOneParagraphMark(t *testing.T) {
	var spike Fragment
	spike.AppendPiece(Fragment{Blocks: Vec[Block]{paraBlock("a")}})
	spike.AppendPiece(Fragment{Blocks: Vec[Block]{paraBlock("b")}})
	if spike.PlainText() != "a\nb\n" {
		t.Fatalf("%q", spike.PlainText())
	}
	spike = Fragment{}
	spike.AppendPiece(Fragment{Blocks: Vec[Block]{paraBlock("a"), paraBlock("")}})
	if spike.PlainText() != "a\n" {
		t.Fatalf("%q", spike.PlainText())
	}
	spike.AppendPiece(Fragment{Blocks: Vec[Block]{paraBlock("b"), paraBlock("c")}})
	if spike.PlainText() != "a\nb\nc\n" {
		t.Fatalf("%q", spike.PlainText())
	}
}

func TestAppendedPiecesKeepTheirOwnTextBoxes(t *testing.T) {
	one, two := lines("x"), lines("y")
	id1 := textBox(t, one, BodyPos(0, 1), "One")
	id2 := textBox(t, two, BodyPos(0, 1), "Two")
	if id1 != id2 {
		t.Fatal("same ids")
	}
	var spike Fragment
	spike.AppendPiece(one.CopyRange(BodyPos(0, 0), BodyPos(0, 1+3)))
	spike.AppendPiece(two.CopyRange(BodyPos(0, 0), BodyPos(0, 1+3)))
	dst := lines("")
	_, err := dst.InsertFragment(BodyPos(0, 0), spike)
	must(t, err)
	var texts []string
	for _, i := range []int{0, 1} {
		for _, b := range boxesIn(dst, TopPath(i)) {
			texts = append(texts, dst.PlainText(PartStory(b)))
		}
	}
	if !slices.Equal(texts, []string{"One", "Two"}) {
		t.Fatal(texts)
	}
}

func TestBadPositionsError(t *testing.T) {
	d := lines("é")
	if _, err := d.InsertText(BodyPos(0, 1), "x", CharProps{}); err == nil {
		t.Fatal("mid-character")
	}
	if _, err := d.InsertText(BodyPos(5, 0), "x", CharProps{}); err == nil {
		t.Fatal("no paragraph")
	}
	if _, err := d.SplitParagraph(BodyPos(0, 99)); err == nil {
		t.Fatal("past the end")
	}
	if c := d.Clamp(BodyPos(9, 9)); !c.Equal(BodyPos(0, 2)) {
		t.Fatal(c)
	}
	if _, err := d.DeleteRange(at(PartStory(3), TopPath(0), 0), BodyPos(0, 0)); err == nil {
		t.Fatal("across stories")
	}
}

// A story (the body, a header or a footnote) holding lines.
func storyDoc(t *testing.T, kind *PartKind, ls ...string) (*Document, StoryRef) {
	if kind == nil {
		return lines(ls...), Body
	}
	d := lines("body")
	var bl Vec[Block]
	for _, l := range ls {
		bl = append(bl, paraBlock(l))
	}
	return d, PartStory(d.AddPart(*kind, bl))
}

var storyKinds = []*PartKind{nil, ptr(PartHeader), ptr(PartFootnote)}

// putTable inserts a rows×cols table at path whose cells hold texts, row by row.
func putTable(t *testing.T, d *Document, s StoryRef, path Path, rows, cols int, texts ...string) {
	must(t, d.InsertBlock(s, path, Block{Table: NewTable(rows, cols, 200)}))
	for k, text := range texts {
		p := append(slices.Clone(path), uint32(k/cols), uint32(k%cols), 0)
		_, err := d.InsertText(at(s, p, 0), text, CharProps{})
		must(t, err)
	}
}

func shapeOf(d *Document, s StoryRef) []string {
	bl, _ := d.Story(s)
	var out []string
	for _, b := range bl {
		if b.Para != nil {
			out = append(out, b.Para.Text)
		} else {
			out = append(out, "[table]")
		}
	}
	return out
}

func cellText(d *Document, s StoryRef, table Path, r, c uint32) string {
	if p := d.Para(s, append(slices.Clone(table), r, c, 0)); p != nil {
		return p.Text
	}
	return "<none>"
}

func TestARangeEndingAtTheEndOfACellThatIsntTheLastKeepsTheTable(t *testing.T) {
	for _, kind := range storyKinds {
		for _, rc := range [][2]uint32{{0, 1}, {1, 0}} {
			d, st := storyDoc(t, kind, "before", "after")
			putTable(t, d, st, TopPath(1), 2, 2, "a", "b", "c", "d")
			_, err := d.DeleteRange(at(st, TopPath(0), 0), at(st, Path{1, rc[0], rc[1], 0}, 1))
			must(t, err)
			if got := shapeOf(d, st); !slices.Equal(got, []string{"", "[table]", "after"}) || cellText(d, st, TopPath(1), 1, 1) != "d" {
				t.Fatal(kind, rc, got)
			}
		}
	}
}

func TestARangeFromTheStartOfOneTableIntoTheMiddleOfTheNextRemovesOnlyTheFirst(t *testing.T) {
	for _, kind := range storyKinds {
		d, st := storyDoc(t, kind, "x", "mid", "y")
		putTable(t, d, st, TopPath(2), 2, 2, "e", "f", "g", "h")
		putTable(t, d, st, TopPath(1), 2, 2, "a", "b", "c", "d")
		to, err := d.DeleteRange(at(st, Path{1, 0, 0, 0}, 0), at(st, Path{3, 0, 1, 0}, 1))
		must(t, err)
		if got := shapeOf(d, st); !slices.Equal(got, []string{"x", "", "[table]", "y"}) {
			t.Fatal(kind, got)
		}
		cells := []string{cellText(d, st, TopPath(2), 0, 0), cellText(d, st, TopPath(2), 0, 1), cellText(d, st, TopPath(2), 1, 0)}
		if !slices.Equal(cells, []string{"", "", "g"}) || d.Para(st, to.Path) == nil {
			t.Fatal(kind, cells, to)
		}
	}
}

func TestARangeFromACellParagraphToTheEndOfANestedTableRemovesOnlyTheNestedTable(t *testing.T) {
	for _, kind := range storyKinds {
		d, st := storyDoc(t, kind, "before", "after")
		putTable(t, d, st, TopPath(1), 1, 1, "p")
		putTable(t, d, st, Path{1, 0, 0, 1}, 1, 1, "n")
		must(t, d.InsertBlock(st, Path{1, 0, 0, 2}, paraBlock("q")))
		to, err := d.DeleteRange(at(st, Path{1, 0, 0, 0}, 1), at(st, Path{1, 0, 0, 1, 0, 0, 0}, 1))
		must(t, err)
		cell, _ := d.Container(st, Path{1, 0, 0, 0})
		var texts []string
		for _, b := range cell {
			if b.Para != nil {
				texts = append(texts, b.Para.Text)
			} else {
				texts = append(texts, "[table]")
			}
		}
		if !slices.Equal(shapeOf(d, st), []string{"before", "[table]", "after"}) || !slices.Equal(texts, []string{"pq"}) ||
			!to.Equal(at(st, Path{1, 0, 0, 0}, 1)) {
			t.Fatal(kind, shapeOf(d, st), texts, to)
		}
	}
}

func TestTwoWholeTablesAtTheEndOfAStoryLeaveOneEmptyParagraph(t *testing.T) {
	for _, kind := range storyKinds {
		for _, trailing := range []bool{true, false} {
			d, st := storyDoc(t, kind, "")
			putTable(t, d, st, TopPath(0), 1, 2, "a", "b")
			putTable(t, d, st, TopPath(1), 2, 1, "c", "d")
			if !trailing {
				// A story that ends in a table (a hostile file).
				must(t, d.withStory(st, func(bl *Vec[Block]) error { *bl = (*bl)[:len(*bl)-1]; return nil }))
			}
			to, err := d.DeleteRange(at(st, Path{0, 0, 0, 0}, 0), at(st, Path{1, 1, 0, 0}, 1))
			must(t, err)
			if got := shapeOf(d, st); !slices.Equal(got, []string{""}) || !to.Equal(at(st, TopPath(0), 0)) {
				t.Fatal(kind, trailing, got, to)
			}
		}
	}
}

// Content controls pair their markers within one block list, so a control inside a table removed
// whole goes with it, and one the range only enters keeps both markers (the control is emptied).
func TestRemovingAWholeTableKeepsAControlTheRangeOnlyPartlyCovers(t *testing.T) {
	start := Object{ControlStart{}}
	end := Object{ControlEnd{}}
	for _, kind := range storyKinds {
		d, st := storyDoc(t, kind, "before", "after")
		putTable(t, d, st, TopPath(1), 1, 1, "c")
		_, err := d.InsertObject(at(st, TopPath(0), 4), end, CharProps{})
		must(t, err)
		inBefore, err := d.InsertObject(at(st, TopPath(0), 1), start, CharProps{})
		must(t, err)
		_, err = d.InsertObject(at(st, Path{1, 0, 0, 0}, 1), end, CharProps{})
		must(t, err)
		_, err = d.InsertObject(at(st, Path{1, 0, 0, 0}, 0), start, CharProps{})
		must(t, err)
		if len(d.ControlRanges(st)) != 2 {
			t.Fatal("setup")
		}
		n := d.Para(st, Path{1, 0, 0, 0}).Len()
		_, err = d.DeleteRange(Pos{Story: st, Path: inBefore.Path, Off: inBefore.Off + 1}, at(st, Path{1, 0, 0, 0}, n))
		must(t, err)
		if got := shapeOf(d, st); !slices.Equal(got, []string{"b￼e￼after"}) {
			t.Fatal(kind, got)
		}
		ranges := d.ControlRanges(st)
		if len(ranges) != 1 || !ranges[0].Start.Path.Equal(TopPath(0)) || !ranges[0].End.Path.Equal(TopPath(0)) ||
			len(d.Para(st, TopPath(0)).Objects) != 2 {
			t.Fatal(kind, ranges)
		}
	}
}

func TestATableNestedDeeperThanTheParagraphWalkIsNeverRemovedWhole(t *testing.T) {
	tb := NewTable(1, 1, 200)
	tb.Rows[0].Cells[0].Blocks = Vec[Block]{paraBlock("deep")}
	for range 20 {
		outer := NewTable(1, 1, 200)
		outer.Rows[0].Cells[0].Blocks = slices.Insert(outer.Rows[0].Cells[0].Blocks, 0, Block{Table: tb})
		tb = outer
	}
	d := lines("before", "after")
	must(t, d.InsertBlock(Body, TopPath(1), Block{Table: tb}))
	var first Path
	for _, p := range d.ParaPaths(Body) {
		if p[0] == 1 {
			first = p
			break
		}
	}
	_, err := d.DeleteRange(at(Body, first, 0), BodyPos(2, 0))
	must(t, err)
	if d.Body[1].Table == nil {
		t.Fatal("a table with paragraphs the walk never saw was removed")
	}
}

// Edits never change a document they were cloned from (copy on write).
func TestEditsLeaveSnapshotsAlone(t *testing.T) {
	d := lines("one", "two")
	putTable(t, d, Body, TopPath(1), 2, 2, "a", "b", "c", "d")
	id := d.AddPart(PartFootnote, Vec[Block]{paraBlock("note")})
	snap := d.Clone()
	want, _ := snap.Marshal()
	_, err := d.InsertText(at(Body, Path{1, 1, 1, 0}, 0), "x", CharProps{})
	must(t, err)
	_, err = d.InsertText(at(PartStory(id), TopPath(0), 0), "y", CharProps{})
	must(t, err)
	_, err = d.DeleteRange(BodyPos(0, 1), at(Body, Path{1, 0, 1, 0}, 1))
	must(t, err)
	must(t, d.FormatRange(d.StartOf(Body), d.EndOf(Body), func(c *CharProps) { c.Bold = ptr(true) }))
	d.ConvertNotes(true, true)
	d.Restyle("TableGrid", nil)
	if got, _ := snap.Marshal(); string(got) != string(want) {
		t.Fatal("a snapshot changed")
	}
}

// FuzzEdits runs edits at hostile positions (any path, story and offset) on any document that
// reads: they never panic.
func FuzzEdits(f *testing.F) {
	f.Add([]byte(`{"body":[{"kind":"para","text":"ab￼c","runs":[{"len":6}],"objects":[{"type":"controlStart","control":{}}]},{"kind":"table","grid":[1],"rows":[{"cells":[{"blocks":[{"kind":"para","text":"x","runs":[]}]}]}]}],"parts":{"1":{"kind":"textBox","blocks":[]}}}`),
		[]byte{0, 1, 0, 0, 3, 5, 2, 9, 1, 0, 0, 4})
	f.Fuzz(func(t *testing.T, js, ops []byte) {
		d, err := Unmarshal(js)
		if err != nil {
			return
		}
		arg := func(i int) int {
			if i < len(ops) {
				return int(int8(ops[i]))
			}
			return 0
		}
		pos := func(i int) Pos {
			s := Body
			if arg(i)%2 != 0 {
				s = PartStory(uint32(arg(i) & 3))
			}
			path := Path{uint32(arg(i+1) & 7)}
			if arg(i+2)%3 == 0 {
				path = append(path, uint32(arg(i+2)&1), uint32(arg(i+3)&1), 0)
			}
			return Pos{Story: s, Path: path, Off: arg(i + 3)}
		}
		var frag Fragment
		for i := 0; i+8 < len(ops) && i < 400; i += 9 {
			a, b := pos(i+1), pos(i+5)
			switch ops[i] % 16 {
			case 0:
				_, _ = d.InsertText(a, "x\n", CharProps{})
			case 1:
				_, _ = d.SplitParagraph(a)
			case 2:
				_, _ = d.DeleteRange(a, b)
			case 3:
				_ = d.FormatRange(a, b, func(c *CharProps) { c.Bold = ptr(true) })
			case 4:
				frag = d.CopyRange(a, b)
			case 5:
				_, _ = d.InsertFragment(a, frag)
			case 6:
				_, _ = d.RemoveBlock(a.Story, a.Path)
			case 7:
				_ = d.InsertBlock(a.Story, a.Path, Block{Table: NewTable(1, 1, 10)})
			case 8:
				d.BalanceControls()
				d.BalanceFieldRanges()
			case 9:
				d.ControlAt(a)
				d.TypingPos(a)
				d.PastFieldEnds(a)
			case 10:
				d.RemoveSectionBreak(arg(i + 1))
				d.SectionMut(arg(i + 2))
			case 11:
				d.ConvertNotes(true, true)
				d.Restyle("Normal", nil)
			case 12:
				_, _ = d.InsertObject(a, Object{ControlEnd{}}, CharProps{})
			case 13:
				if p := d.ParaAt(a); p != nil {
					p.WordAt(a.Off)
					p.SentenceAt(a.Off)
					p.WordStart(a.Off)
					p.WordEnd(a.Off)
				}
			case 14:
				d.Clamp(a)
				d.PathsBetween(a, b)
				d.ParaPathsIn(a, b)
			default:
				_ = d.FormatParagraphs(a, b, func(p *ParaProps) { p.Align = ptr(AlignCenter) })
			}
		}
	})
}
