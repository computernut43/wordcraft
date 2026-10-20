package doc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/computernut43/craft-libs/serdejson"
)

// TestEditsMatchRust runs random sequences of document edits and queries (navigation over
// tables and stories, insert, split, delete and format ranges, copy and paste, fields, content
// controls, sections, restyling, note conversion, paragraph-level word and sentence queries) on
// random documents, here and in crates/doc, and compares each result and the final document byte
// for byte. It runs when WORDCRAFT_EDITS_RUST names testdata/rust-oracle's edits binary;
// WORDCRAFT_EDITS_CASES sets how many documents (default 3,000).
func TestEditsMatchRust(t *testing.T) {
	bin := os.Getenv("WORDCRAFT_EDITS_RUST")
	if bin == "" {
		t.Skip("set WORDCRAFT_EDITS_RUST to testdata/rust-oracle's release edits binary to compare with Rust")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("WORDCRAFT_EDITS_RUST must be absolute")
	}
	n := 3000
	if v := os.Getenv("WORDCRAFT_EDITS_CASES"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("WORDCRAFT_EDITS_CASES=%q: want a positive count", v)
		}
	}
	rng := rand.New(rand.NewPCG(9, 2))
	inputs := editEdgeInputs()
	for range n {
		inputs = append(inputs, editInput(rng))
	}
	var in bytes.Buffer
	for _, x := range inputs {
		in.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(x))))
		in.Write(x)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin, cmd.Env = &in, []string{}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != len(inputs) {
		t.Fatalf("reference answered %d of %d", len(lines), len(inputs))
	}
	bad, ops := 0, 0
	for i, x := range inputs {
		got := editAnswer(x)
		if got != lines[i] {
			bad++
			if bad <= 3 {
				t.Errorf("input %d %.3000s:\n%s", i, x, firstDiff([]byte(got), []byte(lines[i])))
			}
		}
		if strings.HasPrefix(got, "[") {
			ops++
		}
	}
	t.Logf("%d documents (%d read), %d differ", len(inputs), ops, bad)
	if bad > 0 {
		t.Fatalf("%d of %d differ", bad, len(inputs))
	}
}

// rawJSON keeps a value's JSON text (serdejson would read a json.RawMessage as bytes).
type rawJSON []byte

func (r *rawJSON) DecodeSerde(d serdejson.Decoder) error {
	b, err := d.Raw()
	*r = b
	return err
}

type editOp map[string]rawJSON

func (o editOp) pos(k string) Pos {
	var p Pos
	if r, ok := o[k]; ok {
		_ = serdejson.Unmarshal(r, &p)
	}
	return p
}

func (o editOp) story() StoryRef {
	var s StoryRef
	if r, ok := o["story"]; ok {
		_ = serdejson.Unmarshal(r, &s)
	}
	return s
}

func (o editOp) path() Path {
	var p []uint32
	if r, ok := o["path"]; ok {
		_ = serdejson.Unmarshal(r, &p)
	}
	return p
}

func (o editOp) n(k string) int {
	var v uint64
	if r, ok := o[k]; ok {
		_ = json.Unmarshal(r, &v)
	}
	return int(v)
}

func (o editOp) b(k string) bool {
	var v bool
	if r, ok := o[k]; ok {
		_ = json.Unmarshal(r, &v)
	}
	return v
}

func (o editOp) s(k string) string {
	var v string
	if r, ok := o[k]; ok {
		_ = json.Unmarshal(r, &v)
	}
	return v
}

func ok(v any) string { return `{"ok":` + compact(v) + `}` }

func errOr(v any, err error) string {
	if err != nil {
		return compact(map[string]any{"err": err.Error()})
	}
	return ok(v)
}

func fieldJSON(f FieldRange) any { return []any{f.Instr, f.Locked, f.Start, f.End, f.Depth} }

func controlJSON(r ControlRange) any { return []any{r.Control, r.Start, r.End, r.Depth} }

func optPath(p Path, found bool) any {
	if !found {
		return nil
	}
	return p
}

func editAnswer(x []byte) string {
	var d Document
	var ops []editOp
	if !readSeq(x, &d, &ops) {
		return "ERROR"
	}
	var frag Fragment
	out := []string{}
	for _, op := range ops {
		out = append(out, runEdit(&d, &frag, op))
	}
	return show([]any{out, &d})
}

func runEdit(d *Document, frag *Fragment, op editOp) string {
	props := CharProps{}
	if r, ok := op["props"]; ok {
		_ = serdejson.Unmarshal(r, &props)
	}
	a, b := op.pos("a"), op.pos("b")
	story, path := op.story(), op.path()
	switch op.s("op") {
	case "insertText":
		return errOr(d.InsertText(a, op.s("text"), props))
	case "insertObject":
		var o Object
		if serdejson.Unmarshal(op["obj"], &o) != nil {
			return "BAD"
		}
		return errOr(d.InsertObject(a, o, props))
	case "split":
		return errOr(d.SplitParagraph(a))
	case "insertBlock":
		var blk Block
		if serdejson.Unmarshal(op["block"], &blk) != nil {
			return "BAD"
		}
		return errOr(nil, d.InsertBlock(story, path, blk))
	case "removeBlock":
		return errOr(d.RemoveBlock(story, path))
	case "delete":
		return errOr(d.DeleteRange(a, b))
	case "format":
		bold := op.b("bold")
		return errOr(nil, d.FormatRange(a, b, func(c *CharProps) {
			c.Bold = ptr(bold)
			if bold {
				c.Link = ptr("x")
			}
		}))
	case "formatParas":
		al := AlignRight
		if op.b("center") {
			al = AlignCenter
		}
		return errOr(nil, d.FormatParagraphs(a, b, func(p *ParaProps) { p.Align = ptr(al) }))
	case "copy":
		*frag = d.CopyRange(a, b)
		return compact(*frag)
	case "bodyFragment":
		*frag = d.BodyFragment()
		return compact(*frag)
	case "paste":
		return errOr(d.InsertFragment(a, *frag))
	case "setStory":
		var bl Vec[Block]
		if serdejson.Unmarshal(op["blocks"], &bl) != nil {
			return "BAD"
		}
		return errOr(nil, d.SetStory(story, bl))
	case "restyle":
		var to *string
		if r, ok := op["to"]; ok {
			_ = serdejson.Unmarshal(r, &to)
		}
		return compact(d.Restyle(op.s("from"), to))
	case "convertNotes":
		return compact(d.ConvertNotes(op.b("foot"), op.b("end")))
	case "clamp":
		return compact(d.Clamp(a))
	case "paths":
		next, okN := d.NextPara(story, path)
		prev, okP := d.PrevPara(story, path)
		return compact([]any{orEmpty(d.ParaPaths(story)), orEmpty(d.PathsBetween(a, b)), orEmpty(d.ParaPathsIn(a, b)), d.StartOf(story), d.EndOf(story),
			optPath(next, okN), optPath(prev, okP)})
	case "fields":
		var fs []any
		for _, f := range d.FieldRanges(story) {
			fs = append(fs, fieldJSON(f))
		}
		return compact([]any{orEmptyAny(fs), d.PastFieldEnds(a), d.HasUnbalancedFieldRanges()})
	case "balanceFields":
		return compact(d.BalanceFieldRanges())
	case "controls":
		ranges := d.ControlRanges(story)
		var rs, texts []any
		for _, r := range ranges {
			rs = append(rs, controlJSON(r))
			texts = append(texts, []any{d.ControlText(r), r.IsEmpty(), r.Contains(a)})
		}
		var at any
		if r, ok := d.ControlAt(a); ok {
			at = controlJSON(r)
		}
		return compact([]any{orEmptyAny(rs), at, orEmptyAny(texts), d.TypingPos(a), d.HasUnbalancedControls(), d.FillableControls()})
	case "balanceControls":
		return compact(d.BalanceControls())
	case "sections":
		secs := d.Sections()
		var opts []any
		for _, s := range secs {
			f, e := d.NoteOptions(s.Props, false), d.NoteOptions(s.Props, true)
			opts = append(opts, []any{[]any{f.Pos, f.NumFmt, f.NumStart, f.NumRestart}, []any{e.Pos, e.NumFmt, e.NumStart, e.NumRestart}})
		}
		return compact([]any{len(secs), d.SectionIndexOf(op.n("block")), opts})
	case "removeSectionBreak":
		return compact(d.RemoveSectionBreak(op.n("block")))
	case "sectionMut":
		d.SectionMut(op.n("block")).Landscape = op.b("landscape")
		return compact(nil)
	case "text":
		return compact([]any{d.PlainText(story), d.FinalText(story)})
	case "addPart":
		var kind PartKind
		if r, ok := op["kind"]; ok {
			_ = serdejson.Unmarshal(r, &kind)
		}
		var bl Vec[Block]
		if r, ok := op["blocks"]; ok {
			_ = serdejson.Unmarshal(r, &bl)
		}
		return compact(d.AddPart(kind, bl))
	case "textBoxAnchor":
		p, found := d.TextBoxAnchor(uint32(op.n("part")))
		if !found {
			return compact(nil)
		}
		return compact(p)
	case "para":
		return paraAnswer(d, a, op)
	case "fragment":
		f := cloneFragment(*frag)
		f.AppendPiece(FragmentFromText(op.s("text")))
		g := cloneFragment(*frag)
		g.AppendPiece(cloneFragment(*frag))
		var sole any
		if lin, disp, found := frag.SoleEquation(); found {
			sole = []any{lin, disp}
		}
		return compact([]any{f, g, frag.HasEquation(), sole, frag.PlainText()})
	}
	return "BAD OP"
}

func cloneFragment(f Fragment) Fragment {
	return Fragment{Blocks: append(Vec[Block](nil), f.Blocks...), Parts: maps(f.Parts)}
}

func orEmpty(p []Path) []Path {
	if p == nil {
		return []Path{}
	}
	return p
}

func orEmptyAny(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

type byteRangeJSON struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func paraAnswer(d *Document, a Pos, op editOp) string {
	src := d.ParaAt(a)
	if src == nil {
		return "NOPARA"
	}
	p := src.Clone()
	off := a.Off
	q := p.Clone()
	var split any
	var tail *Paragraph
	if t, err := q.SplitOff(p.Clamp(off)); err == nil {
		tail = t
		split = t
	}
	joined := q.Clone()
	if tail != nil {
		joined.Append(tail.Clone())
	}
	del := p.Clone()
	var r any = map[string]any{"Ok": nil}
	if err := del.DeleteExcept(p.Clamp(op.n("x")), p.Clamp(op.n("y")), p.ObjectOffsets()); err != nil {
		r = map[string]any{"Err": err.Error()}
	}
	ranges := []ByteRange{{p.Clamp(op.n("x")), p.Clamp(op.n("y"))}, {p.Clamp(off), p.Len()}}
	var obj any
	if o, found := p.ObjectAt(p.Clamp(off)); found {
		obj = o
	}
	wa, wb := p.WordAt(off)
	sa, sb := p.SentenceAt(off)
	var deleted []byteRangeJSON
	for _, r := range p.DeletedRanges() {
		deleted = append(deleted, byteRangeJSON(r))
	}
	offs := p.ObjectOffsets()
	if offs == nil {
		offs = []int{}
	}
	if deleted == nil {
		deleted = []byteRangeJSON{}
	}
	return compact([]any{[]int{wa, wb}, p.WordStart(off), p.WordEnd(off), []int{sa, sb}, p.PlainText(), p.FinalText(), p.TextWithout(ranges), offs, obj,
		[]any{split, q, joined}, []any{r, del}, deleted})
}

// Generators of documents and edits.

type egen struct {
	rng   *rand.Rand
	parts []uint32
}

func (g *egen) pick(xs ...string) string { return xs[g.rng.IntN(len(xs))] }
func (g *egen) chance(n int) bool        { return g.rng.IntN(n) == 0 }

func (g *egen) charProps() CharProps {
	var c CharProps
	switch g.rng.IntN(8) {
	case 0:
		c.Bold = ptr(true)
	case 1:
		c.Italic = ptr(true)
	case 2:
		c.Del = ptr[uint32](0)
	case 3:
		c.Link, c.Style = ptr("http://x"), ptr("Hyperlink")
	case 4:
		c.Style = ptr(g.pick("FootnoteReference", "EndnoteReference", "Strong"))
	case 5:
		c.Ins = ptr[uint32](1)
	}
	return c
}

func (g *egen) object() Object {
	part := uint32(1)
	if len(g.parts) > 0 {
		part = g.parts[g.rng.IntN(len(g.parts))]
	}
	switch g.rng.IntN(13) {
	case 0:
		return Object{FieldStart{Instr: g.pick("TOC \\o", "ADDIN ZOTERO", "PAGE")}}
	case 1:
		return Object{FieldEnd{}}
	case 2:
		c := NewContentControl(ControlKind{Type: ControlType(g.rng.IntN(3))})
		c.Block, c.Tag = g.chance(2), g.pick("a", "b")
		c.Lock = ControlLock(g.rng.IntN(4))
		return Object{ControlStart{Control: c}}
	case 3:
		return Object{ControlEnd{}}
	case 4:
		return Object{NoteRef{Kind: NoteKind(g.rng.IntN(2)), ID: part}}
	case 5:
		return Object{Shape{Kind: ShapeTextBox, W: 10, H: 10, Story: ptr(part)}}
	case 6:
		return Object{Group{W: 1, H: 1, ChW: 1, ChH: 1, Children: Vec[GroupChild]{{Obj: Object{Shape{Kind: ShapeTextBox, Story: ptr(part)}}}}}}
	case 7:
		return Object{Field{Instr: "PAGE", Result: g.pick("1", "", "ii")}}
	case 8:
		return Object{Equation{Linear: "x^2", Display: g.chance(2)}}
	case 9:
		return Object{BookmarkStart{Name: "b"}}
	case 10:
		return Object{PositionalTab{}}
	case 11:
		return Object{Ruby{Base: "漢", Ruby: "kan", Size: 6, Raise: 10}}
	}
	return Object{Opaque{Format: "docx", XML: "<x/>", Text: "o"}}
}

func (g *egen) para() *Paragraph {
	pieces := []string{"", "a", "word ", "héllo", "漢字", " ", ". ", "Hello. World", "it's", "x_y", "سلام", "\t", "😀", "a\u0301"}
	p := NewParagraph()
	for range g.rng.IntN(5) {
		if g.chance(3) {
			_ = p.InsertObject(p.Len(), g.object(), g.charProps())
			continue
		}
		_, _ = p.InsertText(p.Len(), pieces[g.rng.IntN(len(pieces))], g.charProps())
	}
	if g.chance(3) {
		p.Props.Style = ptr(g.pick("Heading1", "FootnoteText", "EndnoteText", "TOC1", "TOCHeading"))
	}
	if g.chance(4) {
		p.Mark = g.charProps()
	}
	return p
}

func (g *egen) blocks(depth, n int, story bool) Vec[Block] {
	var bl Vec[Block]
	for range n {
		if depth < 2 && g.chance(4) {
			bl = append(bl, Block{Table: g.table(depth)})
			continue
		}
		p := g.para()
		if story && g.chance(6) {
			s := NewSectionProps()
			s.Landscape = g.chance(2)
			if g.chance(2) {
				s.FootnotePr.Pos = ptr(NotePos(g.rng.IntN(4)))
				s.EndnotePr.NumStart = ptr(uint32(g.rng.IntN(3)))
			}
			p.Section = &s
		}
		bl = append(bl, Block{Para: p})
	}
	if g.chance(8) || len(bl) == 0 || bl[len(bl)-1].Para == nil {
		bl = append(bl, Block{Para: g.para()})
	}
	return bl
}

func (g *egen) table(depth int) *Table {
	t := NewTable(1+g.rng.IntN(2), 1+g.rng.IntN(2), 200)
	for r := range t.Rows {
		for c := range t.Rows[r].Cells {
			t.Rows[r].Cells[c].Blocks = g.blocks(depth+1, 1+g.rng.IntN(2), false)
		}
	}
	if g.chance(4) {
		t.Controls.Open = Vec[ContentControl]{NewContentControl(ControlKind{})}
		if g.chance(2) {
			c := NewContentControl(ControlKind{Type: TextControl})
			c.Tag = "second"
			t.Controls.Open = append(t.Controls.Open, c)
		}
	}
	if g.chance(4) {
		t.Controls.Close = uint32(g.rng.IntN(2) + 1)
	}
	return t
}

func (g *egen) document() *Document {
	d := New()
	g.parts = nil
	for id := uint32(1); id <= uint32(g.rng.IntN(4)); id++ {
		g.parts = append(g.parts, id)
	}
	for _, id := range g.parts {
		kinds := []PartKind{PartFootnote, PartEndnote, PartTextBox, PartHeader}
		part := Part{Kind: kinds[g.rng.IntN(len(kinds))], Blocks: g.blocks(1, 1+g.rng.IntN(2), false)}
		if part.Kind == PartTextBox && g.chance(2) {
			part.Body.Next = ptr(g.parts[g.rng.IntN(len(g.parts))])
			part.Body.AutoFit = true
		}
		d.Parts[id] = part
	}
	d.Body = g.blocks(0, 1+g.rng.IntN(6), true)
	if g.chance(3) {
		d.Settings.EndnotePr.Pos = ptr(NotesPageBottom)
	}
	return d
}

func (g *egen) pos(d *Document, s StoryRef) Pos {
	paths := d.ParaPaths(s)
	if len(paths) == 0 || g.chance(15) {
		return Pos{Story: s, Path: Path{uint32(g.rng.IntN(9)), uint32(g.rng.IntN(3)), uint32(g.rng.IntN(3)), uint32(g.rng.IntN(3))}[:1+g.rng.IntN(4)], Off: g.rng.IntN(5)}
	}
	if g.chance(25) {
		return Pos{Story: s, Path: Path{}, Off: g.rng.IntN(6)}
	}
	path := paths[g.rng.IntN(len(paths))]
	p := d.Para(s, path)
	off := 0
	switch g.rng.IntN(5) {
	case 4:
		if offs := p.ObjectOffsets(); len(offs) > 0 {
			off = offs[g.rng.IntN(len(offs))] + objLen*g.rng.IntN(2)
		}
	case 0:
		off = p.Len()
	case 1:
		off = g.rng.IntN(p.Len() + 3)
	default:
		off = p.Clamp(g.rng.IntN(p.Len() + 1))
	}
	return Pos{Story: s, Path: path, Off: off}
}

func (g *egen) story(d *Document) StoryRef {
	if len(d.Parts) == 0 || g.chance(2) {
		return Body
	}
	if g.chance(10) {
		return PartStory(99)
	}
	ids := sortedPartIDs(d.Parts)
	return PartStory(ids[g.rng.IntN(len(ids))])
}

func mustJSON(v any) json.RawMessage {
	b, err := serdejson.MarshalCompact(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func editInput(rng *rand.Rand) []byte {
	g := &egen{rng: rng}
	d := g.document()
	shadow := d.Clone()
	var ops []map[string]any
	for range 1 + rng.IntN(8) {
		s := g.story(shadow)
		a, b := g.pos(shadow, s), g.pos(shadow, s)
		if g.chance(3) {
			b = g.pos(shadow, g.story(shadow))
		}
		path := a.Path
		names := []string{"insertText", "insertObject", "split", "insertBlock", "removeBlock", "delete", "delete", "delete", "format", "formatParas",
			"copy", "paste", "bodyFragment", "setStory", "restyle", "convertNotes", "clamp", "paths", "fields", "balanceFields", "controls",
			"balanceControls", "sections", "removeSectionBreak", "sectionMut", "text", "addPart", "textBoxAnchor", "para", "para", "fragment"}
		name := names[rng.IntN(len(names))]
		op := map[string]any{"op": name, "a": a, "b": b, "story": s, "path": path, "props": g.charProps(), "text": g.pick("x", "ab\ncd", "￼", "é", ""),
			"bold": g.chance(2), "center": g.chance(2), "from": g.pick("Heading1", "Hyperlink", "FootnoteReference", "TOC1"),
			"to": g.pick("Normal", "Strong"), "foot": g.chance(2), "end": g.chance(2), "block": rng.IntN(8), "landscape": g.chance(2),
			"part": rng.IntN(5), "x": rng.IntN(12), "y": rng.IntN(12)}
		if g.chance(4) {
			op["to"] = nil
		}
		switch name {
		case "insertObject":
			op["obj"] = mustJSON(g.object())
		case "insertBlock":
			if g.chance(2) {
				op["block"] = mustJSON(Block{Table: g.table(1)})
			} else {
				op["block"] = mustJSON(Block{Para: g.para()})
			}
			if g.chance(2) {
				op["path"] = path.WithLast(path.Last() + 1)
			}
		case "setStory", "addPart":
			op["blocks"] = mustJSON(g.blocks(1, rng.IntN(3), false))
			op["kind"] = []string{"footnote", "endnote", "textBox", "header"}[rng.IntN(4)]
		}
		ops = append(ops, op)
		// Replay on the shadow so later positions are likely to exist.
		var raw editOp = map[string]rawJSON{}
		for k, v := range op {
			raw[k] = rawJSON(mustJSON(v))
			if r, ok := v.(json.RawMessage); ok {
				raw[k] = rawJSON(r)
			}
		}
		var f Fragment
		runEdit(shadow, &f, raw)
	}
	body, err := serdejson.MarshalCompact([]any{d, ops})
	if err != nil {
		return []byte("[]")
	}
	return body
}

// editEdgeInputs are documents built for the cases random ones rarely reach: deleting across
// several section breaks, sentences and words at their edges, block-level controls at a
// paragraph's start and end, nested range fields, two unpaired table control opens, and pasting
// linked text boxes.
func editEdgeInputs() [][]byte {
	var out [][]byte
	add := func(d *Document, ops ...map[string]any) {
		b, err := serdejson.MarshalCompact([]any{d, ops})
		if err == nil {
			out = append(out, b)
		}
	}
	para := func(text string, objs ...Object) *Paragraph {
		p := ParagraphWithText(strings.ReplaceAll(text, "#", ""), CharProps{})
		for i, k := 0, 0; i < len(text); i++ {
			if text[i] == '#' {
				_ = p.InsertObject(i+k*(objLen-1), objs[k], CharProps{})
				k++
			}
		}
		return p
	}
	sect := func(f func(*SectionProps)) *SectionProps {
		s := NewSectionProps()
		f(&s)
		return &s
	}
	at := func(block, off int) Pos { return BodyPos(block, off) }
	// Deleting across several section breaks: the nearest is absorbed first.
	d := New()
	p1, p2, p3 := para("x"), para("y"), para("z")
	p1.Section = sect(func(s *SectionProps) {
		s.Landscape = true
		s.FootnotePr.Pos = ptr(NotesBeneathText)
		s.Headers.Default = ptr[uint32](1)
	})
	p2.Section = sect(func(s *SectionProps) {
		s.FootnotePr.Pos = ptr(NotesPageBottom)
		s.TitlePage = true
		s.Headers.Default = ptr[uint32](2)
		s.Footers.First = ptr[uint32](3)
	})
	p3.Section = sect(func(s *SectionProps) { s.EndnotePr.NumStart = ptr[uint32](3); s.FootnotePr.NumStart = ptr[uint32](5) })
	d.Body = Vec[Block]{{Para: para("abc")}, {Para: p1}, {Para: p2}, {Para: p3}, {Para: para("end")}}
	add(d, map[string]any{"op": "delete", "a": at(0, 1), "b": at(4, 1)}, map[string]any{"op": "sections", "block": 0})
	add(d.Clone(), map[string]any{"op": "delete", "a": at(1, 0), "b": at(3, 1)}, map[string]any{"op": "sections", "block": 1})
	// Sentences and words.
	d = New()
	d.Body = Vec[Block]{{Para: para("Hello. World")}, {Para: para("Hello.World")}, {Para: para("'tis it's  word")}}
	var ops []map[string]any
	for b := range 3 {
		for off := range 13 {
			ops = append(ops, map[string]any{"op": "para", "a": at(b, off), "x": off, "y": 12 - off})
		}
	}
	add(d, ops...)
	// Block-level controls at the start and end of a paragraph.
	block := NewContentControl(ControlKind{})
	block.Block = true
	inline := NewContentControl(ControlKind{Type: TextControl})
	d = New()
	d.Body = Vec[Block]{{Para: para("a#b#", Object{ControlStart{Control: block}}, Object{ControlEnd{}})},
		{Para: para("##x##", Object{ControlStart{Control: block}}, Object{ControlStart{Control: inline}}, Object{ControlEnd{}}, Object{ControlEnd{}})}}
	ops = nil
	for b := range 2 {
		for off := 0; off <= 13; off++ {
			ops = append(ops, map[string]any{"op": "controls", "story": Body, "a": at(b, off)})
		}
	}
	add(d, ops...)
	// Nested range fields.
	d = New()
	d.Body = Vec[Block]{{Para: para("#a#b##", Object{FieldStart{Instr: "OUTER"}}, Object{FieldStart{Instr: "INNER"}}, Object{FieldEnd{}}, Object{FieldEnd{}})},
		{Para: para("#c", Object{FieldEnd{}})}}
	add(d, map[string]any{"op": "fields", "story": Body, "a": at(0, 0)}, map[string]any{"op": "balanceFields"},
		map[string]any{"op": "fields", "story": Body, "a": at(1, 0)})
	// Two unpaired control opens on a table.
	d = New()
	t := NewTable(1, 1, 100)
	t.Controls.Open = Vec[ContentControl]{NewContentControl(ControlKind{}), inline}
	t.Controls.Close = 3
	d.Body = Vec[Block]{{Para: para("a")}, {Table: t}, {Para: para("b")}}
	add(d, map[string]any{"op": "balanceControls"}, map[string]any{"op": "controls", "story": Body, "a": at(0, 0)})
	t2 := NewTable(1, 1, 100)
	t2.Controls.Open = Vec[ContentControl]{NewContentControl(ControlKind{}), inline}
	d2 := New()
	d2.Body = Vec[Block]{{Para: para("a")}, {Table: t2}, {Para: para("b")}}
	add(d2, map[string]any{"op": "balanceControls"})
	// Linked text boxes copied and pasted get new, unlinked stories.
	d = New()
	d.Parts = U32Map[Part]{1: {Kind: PartTextBox, Blocks: Vec[Block]{{Para: para("one")}}, Body: TextBody{Next: ptr[uint32](2)}},
		2: {Kind: PartTextBox, Blocks: Vec[Block]{{Para: para("two")}}}}
	d.Body = Vec[Block]{{Para: para("#", Object{Shape{Kind: ShapeTextBox, Story: ptr[uint32](1)}})}, {Para: para("end")}}
	add(d, map[string]any{"op": "bodyFragment"}, map[string]any{"op": "paste", "a": at(1, 3)}, map[string]any{"op": "copy", "a": at(0, 0), "b": at(1, 0)},
		map[string]any{"op": "paste", "a": at(1, 0)})
	return out
}
