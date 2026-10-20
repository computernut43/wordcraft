package doc

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func para(text string, runs ...Run) *Paragraph {
	return &Paragraph{Text: text, Runs: runs}
}

func bold() CharProps { return CharProps{Bold: ptr(true)} }

func runsOf(p *Paragraph) string {
	var b strings.Builder
	for _, r := range p.Runs {
		mark := "."
		if r.Props.Bold != nil && *r.Props.Bold {
			mark = "B"
		}
		b.WriteString(strings.Repeat(mark, r.Len))
	}
	return b.String()
}

// Rust: crates/doc/src/tests.rs and para.rs behaviour, as unit cases.
func TestInsertSplitsAndMergesRuns(t *testing.T) {
	p := para("hello", Run{Len: 5})
	if _, err := p.InsertText(2, "XY", bold()); err != nil {
		t.Fatal(err)
	}
	if p.Text != "heXYllo" || runsOf(p) != "..BB..." || len(p.Runs) != 3 {
		t.Fatalf("%q %q %d", p.Text, runsOf(p), len(p.Runs))
	}
	// Same formatting next to it merges.
	if _, err := p.InsertText(4, "Z", bold()); err != nil {
		t.Fatal(err)
	}
	if runsOf(p) != "..BBB..." || len(p.Runs) != 3 {
		t.Fatalf("%q %d", runsOf(p), len(p.Runs))
	}
	// U+FFFC is never inserted as text.
	if n, err := p.InsertText(0, "￼", CharProps{}); err != nil || n != 0 || p.Text != "heXYZllo" {
		t.Fatalf("object char: %d %v %q", n, err, p.Text)
	}
	// Offsets inside a character or past the end are refused, with Rust's message.
	q := para("é", Run{Len: 2})
	for _, off := range []int{1, 3, -1} {
		if _, err := q.InsertText(off, "x", CharProps{}); err == nil {
			t.Errorf("offset %d: want an error", off)
		}
	}
	if _, err := q.InsertText(1, "x", CharProps{}); err == nil || err.Error() != "offset 1 is not a character boundary in the paragraph" {
		t.Errorf("message: %v", err)
	}
}

func TestDeleteKeepsFormattingForTheMark(t *testing.T) {
	p := para("abc", Run{Len: 3, Props: CharProps{Bold: ptr(true), Extra: Extra{"ins": json.RawMessage("7")}}})
	p.Mark = CharProps{Extra: Extra{"del": json.RawMessage("2")}}
	if err := p.Delete(3, 0); err != nil {
		t.Fatal(err)
	}
	// Emptied: the mark takes the deleted text's formatting, but keeps its own revisions.
	if p.Text != "" || len(p.Runs) != 0 || p.Mark.Bold == nil || !*p.Mark.Bold {
		t.Fatalf("%+v", p)
	}
	if _, ok := p.Mark.Extra["ins"]; ok || string(p.Mark.Extra["del"]) != "2" {
		t.Fatalf("mark revisions: %v", p.Mark.Extra)
	}
}

func TestDeleteRemovesObjects(t *testing.T) {
	p := para("a￼b￼c", Run{Len: len("a￼b￼c")})
	p.Extra = Extra{"objects": json.RawMessage(`[{"n":1},{"n":2}]`)}
	if err := p.Delete(1, 1+3+1); err != nil { // the first object and "b"
		t.Fatal(err)
	}
	if p.Text != "a￼c" || string(p.Extra["objects"]) != `[{"n":2}]` {
		t.Fatalf("%q %s", p.Text, p.Extra["objects"])
	}
}

func TestFormatSplitsAtTheRange(t *testing.T) {
	p := para("abcdef", Run{Len: 6})
	if err := p.Format(4, 2, func(c *CharProps) { c.Bold = ptr(true) }); err != nil {
		t.Fatal(err)
	}
	if runsOf(p) != "..BB.." {
		t.Fatal(runsOf(p))
	}
	if err := p.Format(0, 6, func(c *CharProps) { c.Bold = ptr(true) }); err != nil || len(p.Runs) != 1 {
		t.Fatalf("whole paragraph: %v %d", err, len(p.Runs))
	}
}

func TestNormalizeRepairsRuns(t *testing.T) {
	p := para("aé漢", Run{Len: 2}, Run{Len: 0}, Run{Len: 100})
	p.Normalize()
	// "a" + first byte of é → pushed to the boundary after é; the rest is clamped to the text.
	if len(p.Runs) != 1 || p.Runs[0].Len != len("aé漢") {
		t.Fatalf("%+v", p.Runs)
	}
	q := para("abc")
	q.Mark = bold()
	q.Normalize()
	if len(q.Runs) != 1 || q.Runs[0].Len != 3 || !q.Runs[0].Props.Equal(bold()) {
		t.Fatalf("padding takes the mark: %+v", q.Runs)
	}
}

func TestPropsAt(t *testing.T) {
	p := para("ab", Run{Len: 1, Props: bold()}, Run{Len: 1})
	if !p.PropsAt(0).Equal(bold()) || !p.PropsAt(1).Equal(bold()) || !p.PropsAt(2).Equal(CharProps{}) {
		t.Fatal("PropsAt: the run to the left wins at a boundary")
	}
	if !p.PropsOfChar(1).Equal(CharProps{}) || !p.PropsOfChar(5).Equal(p.Mark) {
		t.Fatal("PropsOfChar")
	}
	empty := para("")
	empty.Mark = bold()
	if !empty.PropsAt(0).Equal(bold()) {
		t.Fatal("empty paragraph: the mark")
	}
}

func TestJSONRoundTripKeepsUnknownFields(t *testing.T) {
	in := `{"body":[{"kind":"para","text":"Hi","runs":[{"len":2,"props":{"bold":true,"color":{"rgb":"FF0000"},"lang":"pl-PL"}}],` +
		`"props":{"style":"Title","align":"center"},"mark":{"italic":false}},{"kind":"table","rows":[]}],` +
		`"styles":{"x":1},"settings":{"trackChanges":false}}`
	var d Document
	if err := json.Unmarshal([]byte(in), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Body) != 2 || d.Body[0].Para == nil || d.Body[1].Kind() != "table" || *d.Body[0].Para.Props.Style != "Title" {
		t.Fatalf("%+v", d)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if json.Unmarshal([]byte(in), &a) != nil || json.Unmarshal(out, &b) != nil {
		t.Fatal("json")
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("round trip changed the document:\n%s\n%s", ja, jb)
	}
}

func TestJSONRefusesWhatRustRefuses(t *testing.T) {
	for _, bad := range []string{
		`{}`, `{"body":[{"text":"x","runs":[]}]}`, `{"body":[{"kind":"para","runs":[]}]}`,
		`{"body":[{"kind":"para","text":"x"}]}`, `{"body":[{"kind":"para","text":"x","runs":[{"len":-1}]}]}`, `[]`,
	} {
		var d Document
		if err := json.Unmarshal([]byte(bad), &d); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
}

func TestEmptyPropsAreLeftOut(t *testing.T) {
	b, err := json.Marshal(Block{Para: para("", Run{Len: 0})})
	if err != nil || string(b) != `{"kind":"para","runs":[{"len":0}],"text":""}` {
		t.Fatalf("%s %v", b, err)
	}
}

func FuzzParagraphEdits(f *testing.F) {
	f.Add("héllo￼😀", 3, 1, 2, "x")
	f.Fuzz(func(t *testing.T, text string, a, b, n int, ins string) {
		p := para(text, Run{Len: n})
		p.Normalize()
		_ = p.Delete(a, b)
		_, _ = p.InsertText(a, ins, bold())
		_ = p.Format(b, a, func(c *CharProps) { c.Italic = ptr(true) })
		total := 0
		for _, r := range p.Runs {
			if r.Len <= 0 {
				t.Fatalf("empty run: %+v", p.Runs)
			}
			total += r.Len
		}
		if total != len(p.Text) {
			t.Fatalf("runs cover %d of %d bytes", total, len(p.Text))
		}
	})
}
