package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/computernut43/wordcraft/internal/doc"
)

// testdata/sample.json is the Rust app's sample document (WordCraft's own text, from
// crates/engine/src/sample.rs), saved by the reference CLI at the baseline commit:
// `wordcraft-cli run --template sample --save sample.json`.
func sample(t testing.TB) *Session {
	t.Helper()
	d, err := Open(filepath.Join("testdata", "sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func para(text string) doc.Block {
	r := []doc.Run{}
	if text != "" {
		r = []doc.Run{{Len: len(text)}}
	}
	return doc.Block{Para: &doc.Paragraph{Text: text, Runs: r}}
}

func session(t testing.TB, texts ...string) *Session {
	t.Helper()
	d := &doc.Document{}
	for _, x := range texts {
		d.Body = append(d.Body, para(x))
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func run(t testing.TB, s *Session, id string, params string) any {
	t.Helper()
	p := params
	if p == "" {
		p = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(p))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	r, err := s.Run(id, v)
	if err != nil {
		t.Fatalf("%s %s: %v", id, params, err)
	}
	return r
}

func runErr(t testing.TB, s *Session, id string, params map[string]any) *CmdError {
	t.Helper()
	_, err := s.Run(id, params)
	var ce *CmdError
	if !errors.As(err, &ce) {
		t.Fatalf("%s: want a command error, got %v", id, err)
	}
	return ce
}

func text(s *Session, i int) string { return s.Doc.Body[i].Para.Text }

func bold(s *Session, i, off int) bool {
	c := s.Doc.Body[i].Para.PropsOfChar(off)
	return c.Bold != nil && *c.Bold && c.BoldCS != nil && *c.BoldCS
}

func TestTypingAndUndo(t *testing.T) {
	s := session(t, "Hello world")
	run(t, s, "select.text", `{"text":"world"}`)
	run(t, s, "text.insert", `{"text":"there"}`)
	run(t, s, "text.insert", `{"text":" friend"}`)
	if got := text(s, 0); got != "Hello there friend" {
		t.Fatalf("text %q", got)
	}
	if s.Sel.Focus.Off != len("Hello there friend") || !s.Sel.IsCollapsed() {
		t.Fatalf("caret %+v", s.Sel)
	}
	// Consecutive typing is one undo step.
	if got := s.UndoLabels(); len(got) != 1 || got[0] != "Typing" {
		t.Fatalf("undo labels %q", got)
	}
	run(t, s, "edit.undo", "")
	if got := text(s, 0); got != "Hello world" {
		t.Fatalf("after undo %q", got)
	}
	if a, b := s.Sel.Ordered(); a.Off != 6 || b.Off != 11 {
		t.Fatalf("undo restores the selection, got %+v", s.Sel)
	}
	run(t, s, "edit.redo", "")
	if got := text(s, 0); got != "Hello there friend" {
		t.Fatalf("after redo %q", got)
	}
	// Moving the caret ends the typing group.
	run(t, s, "select.collapse", "")
	run(t, s, "text.insert", `{"text":"Z"}`)
	run(t, s, "select.range", `{"anchor":{"block":0,"off":0},"focus":{"block":0,"off":0}}`)
	run(t, s, "text.insert", `{"text":">"}`)
	if got := len(s.UndoLabels()); got != 3 {
		t.Fatalf("%d undo steps, want 3", got)
	}
	// Undo with nothing to undo is a no-op, not an error.
	fresh := session(t, "x")
	run(t, fresh, "edit.undo", "")
	run(t, fresh, "edit.redo", "")
}

func TestUndoHistoryIsCapped(t *testing.T) {
	s := session(t, "x")
	for i := range maxUndo + 10 {
		on := i%2 == 0
		if _, err := s.Run("format.bold", Params{"value": on}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.history) != maxUndo || s.evicted != 10 {
		t.Fatalf("history %d, evicted %d", len(s.history), s.evicted)
	}
}

func TestPendingBoldAppliesToTyping(t *testing.T) {
	s := session(t, "one two")
	run(t, s, "select.range", `{"anchor":{"block":0,"off":3},"focus":{"block":0,"off":3}}`)
	run(t, s, "format.bold", `{"value":true}`)
	if bold(s, 0, 0) {
		t.Fatal("a caret between words formats nothing yet")
	}
	run(t, s, "text.insert", `{"text":"XY"}`)
	if !bold(s, 0, 3) || !bold(s, 0, 4) || bold(s, 0, 2) || bold(s, 0, 5) {
		t.Fatalf("runs %+v", s.Doc.Body[0].Para.Runs)
	}
	// Inside a word Rust formats the word, which needs word boundaries: refused.
	run(t, s, "select.range", `{"anchor":{"block":0,"off":1},"focus":{"block":0,"off":1}}`)
	if e := runErr(t, s, "format.bold", Params{"value": true}); e.Kind != NotPorted {
		t.Fatalf("got %v", e)
	}
	// An empty paragraph's mark takes the formatting.
	e := session(t, "")
	run(t, e, "format.bold", `{"value":true}`)
	if m := e.Doc.Body[0].Para.Mark; m.Bold == nil || !*m.Bold {
		t.Fatalf("mark %+v", m)
	}
}

func TestBoldAcrossParagraphs(t *testing.T) {
	s := session(t, "alpha", "beta", "", "gamma")
	run(t, s, "select.range", `{"anchor":{"block":0,"off":2},"focus":{"story":"body","path":[3],"off":0}}`)
	run(t, s, "format.bold", `{"value":true}`)
	if bold(s, 0, 1) || !bold(s, 0, 2) || !bold(s, 1, 0) || bold(s, 3, 0) {
		t.Fatal("wrong characters formatted")
	}
	// Fully selected paragraphs format their marks; the partly selected ends don't.
	for i, want := range []bool{false, true, true, false} {
		m := s.Doc.Body[i].Para.Mark
		if got := m.Bold != nil && *m.Bold; got != want {
			t.Errorf("mark %d bold %v, want %v", i, got, want)
		}
	}
	// Typing over a selection that crosses paragraphs isn't ported.
	if e := runErr(t, s, "text.insert", Params{"text": "x"}); e.Kind != NotPorted {
		t.Fatalf("got %v", e)
	}
	if text(s, 0) != "alpha" {
		t.Fatal("a refused command changed the document")
	}
}

func TestSelectText(t *testing.T) {
	s := session(t, "a cat, a cat", "cat")
	for n, want := range map[string][2]int{`1`: {0, 2}, `2`: {0, 9}, `3`: {1, 0}, `0`: {0, 2}} {
		run(t, s, "select.text", `{"text":"cat","occurrence":`+n+`}`)
		if got := [2]int{s.Sel.Anchor.Path.Last(), s.Sel.Anchor.Off}; got != want || s.Sel.Focus.Off != want[1]+3 {
			t.Errorf("occurrence %s: %+v", n, s.Sel)
		}
	}
	if e := runErr(t, s, "select.text", Params{"text": "cat", "occurrence": json.Number("4")}); e.Error() != "`cat` not found" {
		t.Fatalf("got %q", e.Error())
	}
	if e := runErr(t, s, "select.text", Params{"text": ""}); e.Error() != "bad parameters: `text` is empty" {
		t.Fatalf("got %q", e.Error())
	}
	if e := runErr(t, s, "select.text", Params{}); e.Error() != "bad parameters: `text` (string) is required" {
		t.Fatalf("got %q", e.Error())
	}
	// Overlapping matches count once, as Rust's match_indices.
	o := session(t, "aaaa")
	if e := runErr(t, o, "select.text", Params{"text": "aa", "occurrence": json.Number("3")}); e.Kind != Failed {
		t.Fatalf("got %v", e)
	}
}

func TestParsePos(t *testing.T) {
	part := uint32(3)
	cases := []struct {
		in   string
		want *doc.Pos
	}{
		{`{"story":"body","path":[2],"off":5}`, &doc.Pos{Story: doc.Body, Path: doc.Path{2}, Off: 5}},
		{`{"story":{"body":null},"path":[2],"off":5,"extra":1}`, &doc.Pos{Story: doc.Body, Path: doc.Path{2}, Off: 5}},
		{`{"story":{"part":3},"path":[0,1,2,3],"off":0}`, &doc.Pos{Story: doc.StoryRef{Part: &part}, Path: doc.Path{0, 1, 2, 3}}},
		{`["body",[1],2]`, &doc.Pos{Story: doc.Body, Path: doc.Path{1}, Off: 2}},
		{`{"block":4}`, &doc.Pos{Story: doc.Body, Path: doc.Path{4}}},
		{`{"block":4294967298,"off":1.5}`, &doc.Pos{Story: doc.Body, Path: doc.Path{2}}},
		{`{"block":1,"off":99999999999999999999}`, &doc.Pos{Story: doc.Body, Path: doc.Path{1}}},
		// A Pos missing a field falls back to "block".
		{`{"story":"body","path":[1],"block":7}`, &doc.Pos{Story: doc.Body, Path: doc.Path{7}}},
		{`{"story":"body","path":[1]}`, nil},
		{`["body",[1]]`, nil},
		{`["body",[1],2,3]`, nil},
		{`{"story":"part","path":[1],"off":0}`, nil},
		{`{"story":{"part":1,"body":null},"path":[1],"off":0}`, nil},
		{`{"story":{"body":1},"path":[1],"off":0}`, nil},
		{`{"story":"body","path":[4294967296],"off":0}`, nil},
		{`{"story":"body","path":[1],"off":-1}`, nil},
		{`{"story":"body","path":[1],"off":1.0}`, nil},
		{`{"block":-1}`, nil},
		{`{"block":1e2}`, nil},
		{`7`, nil},
		{`null`, nil},
	}
	for _, c := range cases {
		dec := json.NewDecoder(strings.NewReader(c.in))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		got, ok := parsePos(v)
		switch {
		case c.want == nil && ok:
			t.Errorf("%s: parsed %+v, want an error", c.in, got)
		case c.want != nil && !ok:
			t.Errorf("%s: not parsed", c.in)
		case c.want != nil && !got.Equal(*c.want):
			t.Errorf("%s: %+v, want %+v", c.in, got, *c.want)
		}
	}
	// The off of a "block" position saturates instead of wrapping.
	p, _ := parsePos(map[string]any{"block": json.Number("1"), "off": json.Number("18446744073709551615")})
	if p.Off <= 0 {
		t.Fatalf("off %d", p.Off)
	}
}

func TestSelectRangeClamps(t *testing.T) {
	s := session(t, "héllo", "world")
	run(t, s, "select.range", `{"anchor":{"block":0,"off":2},"focus":{"block":9,"off":99}}`)
	// é is two bytes: offset 2 is inside it and moves back to its start; block 9 is past the end.
	if s.Sel.Anchor.Off != 1 || s.Sel.Focus.Path.Last() != 1 || s.Sel.Focus.Off != 5 {
		t.Fatalf("%+v", s.Sel)
	}
	if e := runErr(t, s, "select.range", Params{"anchor": "x", "focus": nil}); e.Error() != "bad parameters: bad `anchor`" {
		t.Fatalf("got %q", e.Error())
	}
	if e := runErr(t, s, "select.range", Params{"anchor": map[string]any{"block": json.Number("0")}}); e.Error() != "bad parameters: bad `focus`" {
		t.Fatalf("got %q", e.Error())
	}
	// Block 13 of the sample is a table: the position moves to the last paragraph before it.
	sm := sample(t)
	run(t, sm, "select.range", `{"anchor":{"block":13},"focus":{"block":0}}`)
	if !sm.Sel.Anchor.Path.Equal(doc.Path{12}) || sm.Doc.Para(doc.Body, doc.Path{13}) != nil {
		t.Fatalf("%+v", sm.Sel)
	}
	run(t, s, "select.range", `{"anchor":{"block":0,"off":0},"focus":{"block":1,"off":3}}`)
	run(t, s, "select.collapse", `{"end":true}`)
	if !s.Sel.IsCollapsed() || s.Sel.Focus.Path.Last() != 1 || s.Sel.Focus.Off != 3 {
		t.Fatalf("%+v", s.Sel)
	}
}

func TestTextInsertRefusals(t *testing.T) {
	cases := []Params{
		{"text": ","}, {"text": " "}, {"text": `"`}, {"text": "'"},
		{"text": "a\rb"}, {"text": "a\u2029b"}, {"text": "שלום"}, {"text": "a\u200fb"},
	}
	for _, p := range cases {
		s := session(t, "abc")
		if e := runErr(t, s, "text.insert", p); e.Kind != NotPorted {
			t.Errorf("%q: got %v", p["text"], e)
		}
		if text(s, 0) != "abc" || s.CanUndo() || s.Dirty {
			t.Errorf("%q: a refused command changed the session", p["text"])
		}
	}
	s := session(t, "abc")
	run(t, s, "text.insert", `{"text":",","raw":true}`)
	run(t, s, "text.insert", `{"text":"line\u000bbreak"}`)
	if got := text(s, 0); got != ",line\nbreakabc" {
		t.Fatalf("%q", got)
	}
	if e := runErr(t, s, "text.insert", Params{"text": json.Number("5")}); e.Kind != BadParams {
		t.Fatalf("got %v", e)
	}
}

func TestTrackChangesAndProtectionRefused(t *testing.T) {
	s := session(t, "abc")
	s.Doc.Settings.TrackChanges = true
	if e := runErr(t, s, "text.insert", Params{"text": "x"}); e.Kind != NotPorted {
		t.Fatalf("got %v", e)
	}
	s.Doc.Settings.TrackChanges = false
	readOnly := "readOnly"
	s.Doc.Settings.Protection = &readOnly
	if e := runErr(t, s, "format.bold", Params{"value": true}); e.Kind != NotPorted {
		t.Fatalf("got %v", e)
	}
	// Non-mutating commands still run.
	run(t, s, "select.text", `{"text":"b"}`)
}

func TestUnknownCommand(t *testing.T) {
	s := session(t, "x")
	if e := runErr(t, s, "no.such", nil); e.Error() != "unknown command `no.such`" {
		t.Fatalf("got %q", e.Error())
	}
}

func TestPanicBecomesErrorAndRestores(t *testing.T) {
	s := session(t, "abc")
	before, _ := json.Marshal(s.Doc)
	s.registry = NewRegistry([]CommandSpec{{ID: "boom", Mutates: true, Run: func(s *Session, _ Params) (any, error) {
		if _, err := s.Doc.InsertText(s.Sel.Focus, "zzz", doc.CharProps{}); err != nil {
			return nil, err
		}
		var p *doc.Paragraph
		return p.Text, nil // a nil dereference panics
	}}})
	e := runErr(t, s, "boom", nil)
	if e.Kind != Failed || !strings.Contains(e.Error(), "boom panicked") {
		t.Fatalf("got %v", e)
	}
	after, _ := json.Marshal(s.Doc)
	if !bytes.Equal(before, after) || s.CanUndo() || s.Dirty {
		t.Fatal("a panicking command changed the session")
	}
}

func TestOpenAndSave(t *testing.T) {
	if _, err := OpenBytes("x.docx", nil); err == nil || !strings.Contains(err.Error(), "not available in the Go port yet") {
		t.Fatalf("got %v", err)
	}
	if _, err := OpenBytes("x.json", []byte(`{"body":`)); err == nil {
		t.Fatal("truncated JSON opened")
	}
	date := `{"body":[{"kind":"para","text":"￼","runs":[{"len":3}],"objects":[{"type":"field","instr":" date \\@ \"d\"","result":"1"}]}]}`
	if _, err := OpenBytes("x.json", []byte(date)); err == nil {
		t.Fatal("a DATE field opened without its refresh")
	}
	locked := strings.Replace(date, `"result"`, `"locked":true,"result"`, 1)
	if _, err := OpenBytes("x.json", []byte(locked)); err != nil {
		t.Fatalf("a locked DATE field: %v", err)
	}
	// An empty body gets a paragraph to type in.
	d, err := OpenBytes("x.JSON", []byte(`{"body":[]}`))
	if err != nil || len(d.Body) != 1 || d.Body[0].Para == nil {
		t.Fatalf("%v %+v", err, d)
	}

	s := sample(t)
	run(t, s, "select.text", `{"text":"Studio"}`)
	run(t, s, "format.bold", `{"value":true}`)
	if !s.Dirty {
		t.Fatal("not dirty after an edit")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	for rev := 2; rev <= 3; rev++ {
		if err := s.Save(out); err != nil {
			t.Fatal(err)
		}
		if s.Dirty {
			t.Fatal("dirty after saving")
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Core     map[string]any `json:"core"`
			Settings map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Core["revision"] != float64(rev) || saved.Core["lastModifiedBy"] != "WordCraft User" || saved.Core["created"] != "2026-10-20T03:00:33Z" {
			t.Fatalf("core %v", saved.Core)
		}
		if _, ok := saved.Settings["zoom"]; ok {
			t.Fatal("zoom saved")
		}
	}
	if err := s.Save(filepath.Join(dir, "out.docx")); err == nil {
		t.Fatal("saved a .docx")
	}
	if err := s.Save(filepath.Join(dir, "missing", "out.json")); err == nil {
		t.Fatal("saved into a missing directory")
	}
}

func TestStampSave(t *testing.T) {
	d := &doc.Document{
		Core:     doc.CoreProps{Revision: 4294967295, Title: "T"},
		Settings: doc.NewSettings(),
	}
	d.Settings.Zoom = &doc.Zoom{Percent: 150}
	if err := stampSave(d, "Ann", mustTime(t)); err != nil {
		t.Fatal(err)
	}
	c := d.Core
	if c.Revision != 4294967295 || c.Created != "2026-01-02T03:04:05Z" || c.Modified != c.Created || c.Title != "T" || c.LastModifiedBy != "Ann" {
		t.Fatalf("core %+v", c)
	}
	if d.Settings.Zoom != nil {
		t.Fatal("zoom kept")
	}
}

func mustTime(t testing.TB) time.Time {
	t.Helper()
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600)).Add(time.Hour)
}

func TestTypedTextIsNotATrackedChange(t *testing.T) {
	s := session(t, "abcd")
	p := s.Doc.Body[0].Para
	on, link := true, "https://example.org"
	p.Runs[0].Props.Ins = new(uint32)
	p.Runs[0].Props.Del = new(uint32)
	p.Runs[0].Props.FmtChange = &doc.PropChange[doc.CharProps]{Rev: 1}
	p.Runs[0].Props.RTL = &on
	p.Runs[0].Props.Link = &link
	run(t, s, "select.range", `{"anchor":{"block":0,"off":2},"focus":{"block":0,"off":2}}`)
	run(t, s, "text.insert", `{"text":"XY"}`)
	runs := s.Doc.Body[0].Para.Runs
	if len(runs) != 3 || runs[1].Len != 2 {
		t.Fatalf("runs %+v", runs)
	}
	typed := runs[1].Props
	if typed.Ins != nil || typed.Del != nil || typed.FmtChange != nil || typed.RTL != nil {
		t.Errorf("typed text is a tracked change or right to left: %+v", typed)
	}
	// Typing inside a hyperlink extends it.
	if typed.Link == nil || *typed.Link != link {
		t.Error("typing inside a link left it")
	}
}
