package doc

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/computernut43/craft-libs/serdejson"
)

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
	p := para("abc", Run{Len: 3, Props: CharProps{Bold: ptr(true), Ins: ptr(uint32(7))}})
	p.Mark = CharProps{Del: ptr(uint32(2))}
	if err := p.Delete(3, 0); err != nil {
		t.Fatal(err)
	}
	// Emptied: the mark takes the deleted text's formatting, but keeps its own revisions.
	if p.Text != "" || len(p.Runs) != 0 || p.Mark.Bold == nil || !*p.Mark.Bold {
		t.Fatalf("%+v", p)
	}
	if p.Mark.Ins != nil || p.Mark.Del == nil || *p.Mark.Del != 2 {
		t.Fatalf("mark revisions: %+v", p.Mark)
	}
}

func TestDeleteRemovesObjects(t *testing.T) {
	p := para("a￼b￼c", Run{Len: len("a￼b￼c")})
	p.Objects = Vec[Object]{{BookmarkStart{"one"}}, {BookmarkStart{"two"}}}
	if err := p.Delete(1, 1+3+1); err != nil { // the first object and "b"
		t.Fatal(err)
	}
	if p.Text != "a￼c" || len(p.Objects) != 1 || p.Objects[0] != (Object{BookmarkStart{"two"}}) {
		t.Fatalf("%q %s", p.Text, p.Objects)
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

// TestJSONReadsAsRust: a document reads and writes as serde does: typed fields normalised
// (12 is the f32 12.0, a colour given as hex is written [r, g, b]) and in Rust's field order,
// unknown fields dropped, missing ones defaulted (a style sheet without styles gets the built-in
// ones).
func TestJSONReadsAsRust(t *testing.T) {
	in := `{"styles":{"x":1},"body":[{"text":"Hi","runs":[{"len":2,"props":{"size":12,"bold":true,"color":"#ff0000","lang":"pl-PL","nope":1}}],` +
		`"kind":"para","props":{"style":"Title","align":"center","lineSpacing":{"value":1.5,"rule":"multiple"}},"mark":{"italic":false}},` +
		`{"kind":"table","rows":[], "grid" : [1], "x" : 1}],"settings":{"trackChanges":false},"unknown":[1]}`
	d, err := Unmarshal([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Body) != 2 || d.Body[0].Para == nil || d.Body[1].Kind() != "table" || *d.Body[0].Para.Props.Style != "Title" {
		t.Fatalf("%+v", d)
	}
	out, err := serdejson.MarshalCompact(d)
	if err != nil {
		t.Fatal(err)
	}
	const body = `{"body":[{"kind":"para","text":"Hi","runs":[{"len":2,"props":{"size":12.0,"bold":true,"color":{"Rgb":[255,0,0]},"lang":"pl-PL"}}],` +
		`"props":{"style":"Title","align":"center","lineSpacing":{"rule":"multiple","value":1.5}},"mark":{"italic":false}},` +
		`{"kind":"table","props":{"style":null,"width":null,"widthPct":null,"align":null,"indent":null,"borders":null,"cellMargins":null,` +
		`"fixed":false,"look":{"headerRow":true,"totalRow":false,"bandedRows":true,"firstColumn":true,"lastColumn":false,"bandedColumns":false},` +
		`"shading":null,"caption":null},"grid":[1.0],"rows":[]}],"lastSection":{"start":"nextPage","pageW":612.0,`
	if !strings.HasPrefix(string(out), body) {
		t.Fatalf("got\n%s", out)
	}
	// Missing settings fields take Rust's defaults.
	if d.Settings.TrackChanges || !d.Settings.TrackMoves || d.Settings.DefaultTab != 36 || d.Settings.ThemeName != "Craft" {
		t.Fatalf("settings %+v", d.Settings)
	}
	if len(d.Styles.Styles) == 0 || d.Styles.Styles[0].ID != "Normal" || len(d.Numbering.Nums) != 0 {
		t.Fatal("a style sheet without styles has the built-in ones; no lists")
	}
}

func TestJSONRefusesWhatRustRefuses(t *testing.T) {
	for _, bad := range []string{
		`{"body":[{"text":"x","runs":[]}]}`, `{"body":[{"kind":"para","runs":[]}]}`, `{"body":[{"kind":"para","text":"x"}]}`,
		`{"body":[{"kind":"para","text":"x","runs":[{"len":-1}]}]}`, `{"body":[],"body":[]}`, `{"body":[{"kind":"Para","text":"","runs":[]}]}`,
		`{"body":[{"kind":"para","text":"","runs":[],"mark":{"color":{"rgb":"FF0000"}}}]}`, `{"core":{"revision":4294967296}}`,
	} {
		if _, err := Unmarshal([]byte(bad)); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
	// Rust's Document is #[serde(default)]: these are documents of one empty paragraph.
	for _, ok := range []string{`{}`, `[]`} {
		d, err := Unmarshal([]byte(ok))
		if err != nil || len(d.Body) != 1 || d.Body[0].Para == nil || d.Body[0].Para.Text != "" {
			t.Errorf("%s: %+v %v", ok, d, err)
		}
	}
}

func TestEmptyPropsAreLeftOut(t *testing.T) {
	b, err := json.Marshal(Block{Para: para("", Run{Len: 0})})
	if err != nil || string(b) != `{"kind":"para","text":"","runs":[{"len":0}]}` {
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

// FuzzUnmarshal: any input either is refused or reads, writes back, and reads again to the same
// document (unless an f32 read from Content overflowed to infinity: serde writes that as null,
// which reads back differently in Rust too).
func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte(`{"body":[{"kind":"para","text":"Hi","runs":[{"len":2,"props":{"size":12,"color":"#ff0000"}}],"props":{"lineSpacing":["exactly",3]}}]}`))
	f.Add([]byte(`{"body":[{"kind":"table","rows":[]}],"core":{"revision":3},"media":{"a":[1,2]},"footnoteSeparators":{"separator":[]}}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Unmarshal(data)
		if err != nil || hasNonFinite(reflect.ValueOf(d)) {
			return
		}
		out, err := d.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		again, err := Unmarshal(out)
		if err != nil {
			t.Fatalf("%s doesn't read back: %v", out, err)
		}
		out2, err := again.Marshal()
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("not stable:\n%s\n%s", out, out2)
		}
	})
}

func hasNonFinite(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return math.IsInf(v.Float(), 0) || math.IsNaN(v.Float())
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil() && hasNonFinite(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if hasNonFinite(v.Field(i)) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return false
		}
		for i := range v.Len() {
			if hasNonFinite(v.Index(i)) {
				return true
			}
		}
	}
	return false
}
