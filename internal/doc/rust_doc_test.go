package doc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/computernut43/wordcraft/internal/rustref"
)

// TestRustDocumentsRoundTrip: whole documents the Rust app writes as JSON (its built-in sample,
// and after a DOCX save and reopen) read into this model and write back unchanged, and the Rust
// app reads the Go output as the same document.
func TestRustDocumentsRoundTrip(t *testing.T) {
	cli := rustref.Require(t)
	dir := t.TempDir()
	ctx := context.Background()
	sample := filepath.Join(dir, "sample.json")
	docx := filepath.Join(dir, "sample.docx")
	if _, err := cli.Run(ctx, dir, "run", "--template", "sample", "--save", sample); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Run(ctx, dir, "run", "--template", "sample", "--save", docx); err != nil {
		t.Fatal(err)
	}
	fromDocx, err := cli.ConvertToJSON(ctx, docx)
	if err != nil {
		t.Fatal(err)
	}
	sampleJSON, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	for name, rustJSON := range map[string][]byte{"sample": sampleJSON, "sample via docx": fromDocx} {
		d, err := Unmarshal(rustJSON)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		paras := 0
		for _, b := range d.Body {
			if b.Para != nil {
				paras++
			}
		}
		if paras < 10 {
			t.Fatalf("%s: only %d paragraphs read", name, paras)
		}
		goJSON, err := d.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		// Byte for byte: serde_json::to_vec_pretty's layout, field order and number spelling.
		if !bytes.Equal(goJSON, rustJSON) {
			t.Fatalf("%s: Go's JSON differs from Rust's:\n%s", name, firstDiff(goJSON, rustJSON))
		}
		// The Rust app reads what Go wrote as it reads its own file. (Not as the file itself:
		// every save bumps the document's revision counter, core.revision.)
		goFile, rustFile := filepath.Join(dir, "go.json"), filepath.Join(dir, "rust.json")
		if err := os.WriteFile(goFile, goJSON, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rustFile, rustJSON, 0o644); err != nil {
			t.Fatal(err)
		}
		fromGo, err1 := cli.ConvertToJSON(ctx, goFile)
		fromRust, err2 := cli.ConvertToJSON(ctx, rustFile)
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		// Each save stamps core.modified with the time, to the second, so two saves may differ.
		a, err1 := rustref.NormalizeJSON(withoutModified(t, fromGo))
		b, err2 := rustref.NormalizeJSON(withoutModified(t, fromRust))
		if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
			t.Fatalf("%s: Rust reads Go's JSON differently from its own", name)
		}
	}
}

// withoutModified is document JSON without its save time (core.modified).
func withoutModified(t *testing.T, data []byte) []byte {
	t.Helper()
	var v map[string]json.RawMessage
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	var core map[string]json.RawMessage
	if err := json.Unmarshal(v["core"], &core); err != nil {
		t.Fatal(err)
	}
	if _, ok := core["modified"]; !ok {
		t.Fatal("no core.modified in the document")
	}
	delete(core, "modified")
	c, err := json.Marshal(core)
	if err != nil {
		t.Fatal(err)
	}
	v["core"] = c
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// firstDiff shows where two outputs part.
func firstDiff(a, b []byte) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	from := max(i-200, 0)
	return "go:   " + string(a[from:min(i+200, len(a))]) + "\nrust: " + string(b[from:min(i+200, len(b))])
}
