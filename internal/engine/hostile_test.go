package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/computernut43/wordcraft/internal/doc"
)

// junkParams are Rust's hostile_params_never_panic inputs, plus junk for the position and
// occurrence parameters the ported commands read.
var junkParams = []string{
	`null`,
	`{}`,
	`{"text": 5, "size": "x", "path": [], "pos": {"block": 9999, "off": 99999}}`,
	`{"value": -1e308, "rows": 1e9}`,
	`{"width": 1e-9, "height": 1e30, "top": -1e9, "left": 1e9}`,
	`{"section": {"pageW": -1e308, "pageH": 0, "marginTop": 1e30, "header": -5, "columns": {"count": 1000000, "space": -1e300, "widths": [[0, 0]]}}}`,
	`{"text": "", "occurrence": -1, "anchor": {"block": 18446744073709551615, "off": 18446744073709551615}, "focus": [1, [], 2]}`,
	`{"text": "\u0000퟿￿", "occurrence": 1e300, "raw": "yes", "end": 1, "value": "true"}`,
	`{"anchor": {"story": {"part": 4294967295}, "path": [4294967295, 0, 0, 0], "off": 0}, "focus": {"story": "body", "path": [], "off": 0}}`,
	`{"text": "x", "story": {"part": 1}}`,
}

func decodeParams(t testing.TB, s string) Params {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	p, _ := v.(map[string]any)
	return p
}

// TestHostileParamsNeverPanic is Rust's hostile_params_never_panic for the ported commands: every
// command with junk parameters, on the sample and on a small document, never panics (Run turns a
// panic into an error, which this test rejects) and leaves a valid session.
func TestHostileParamsNeverPanic(t *testing.T) {
	for _, spec := range defaultRegistry.All() {
		for _, j := range junkParams {
			for _, s := range []*Session{sample(t), session(t, "héllo wörld", "", "x")} {
				if _, err := s.Run(spec.ID, decodeParams(t, j)); err != nil && strings.Contains(err.Error(), "panicked") {
					t.Fatalf("%s %s: %v", spec.ID, j, err)
				}
				checkSession(t, s)
			}
		}
	}
}

// checkSession asserts the invariants every command keeps: runs cover each paragraph's text
// exactly, the selection is at character boundaries of real paragraphs, and the document saves.
func checkSession(t testing.TB, s *Session) {
	t.Helper()
	for i, b := range s.Doc.Body {
		if b.Para == nil {
			continue
		}
		n := 0
		for _, r := range b.Para.Runs {
			if r.Len <= 0 {
				t.Fatalf("paragraph %d: empty or negative run %+v", i, b.Para.Runs)
			}
			n += r.Len
		}
		if n != len(b.Para.Text) {
			t.Fatalf("paragraph %d: runs cover %d of %d bytes", i, n, len(b.Para.Text))
		}
	}
	for _, p := range []doc.Pos{s.Sel.Anchor, s.Sel.Focus} {
		para := s.Doc.ParaAt(p)
		if para == nil || para.Clamp(p.Off) != p.Off {
			t.Fatalf("selection %+v is not in a paragraph at a character boundary", s.Sel)
		}
	}
	if _, err := SaveBytes("x.json", s.Doc); err != nil {
		t.Fatal(err)
	}
}

// FuzzCommands runs a fuzzed sequence of commands with fuzzed parameters on a small document and
// checks the invariants after each, and that undoing everything restores the original.
func FuzzCommands(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3}, `{"text":"Hi","value":true,"anchor":{"block":1,"off":2},"focus":{"block":0}}`)
	f.Add([]byte{4, 4, 5, 6, 0}, `{"text":"é","raw":true,"end":true}`)
	f.Fuzz(func(t *testing.T, ops []byte, params string) {
		if len(ops) > 64 {
			ops = ops[:64]
		}
		dec := json.NewDecoder(strings.NewReader(params))
		dec.UseNumber()
		var p map[string]any
		if dec.Decode(&p) != nil {
			p = nil
		}
		s := session(t, "héllo wörld", "", "a, b")
		orig, err := json.Marshal(s.Doc)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{"text.insert", "format.bold", "select.text", "select.range", "select.collapse", "edit.undo", "edit.redo"}
		for _, op := range ops {
			id := ids[int(op)%len(ids)]
			if _, err := s.Run(id, p); err != nil && strings.Contains(err.Error(), "panicked") {
				t.Fatalf("%s %s: %v", id, params, err)
			}
			checkSession(t, s)
		}
		for s.CanUndo() {
			if _, err := s.Run("edit.undo", nil); err != nil {
				t.Fatal(err)
			}
		}
		got, err := json.Marshal(s.Doc)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(orig) {
			t.Fatalf("undoing everything gives\n%s\nwant\n%s", got, orig)
		}
	})
}
