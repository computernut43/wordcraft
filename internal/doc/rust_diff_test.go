package doc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/computernut43/craft-libs/serdejson"

	"github.com/computernut43/wordcraft/internal/rustref"
)

// TestMatchesRust runs random paragraph edits through this package and through crates/doc and
// compares the results (JSON compared after normalising key order and number spelling). It runs
// when WORDCRAFT_DOC_RUST names the reference program built from testdata/rust-oracle.
func TestMatchesRust(t *testing.T) {
	bin := os.Getenv("WORDCRAFT_DOC_RUST")
	if bin == "" {
		t.Skip("set WORDCRAFT_DOC_RUST to testdata/rust-oracle's release binary to compare with Rust")
	}
	if !filepath.IsAbs(bin) {
		t.Fatalf("WORDCRAFT_DOC_RUST must be absolute: %q", bin)
	}
	ask := func(reqs []map[string]any) []string {
		var in bytes.Buffer
		for _, r := range reqs {
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			in.Write(append(b, '\n'))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin)
		cmd.Stdin, cmd.Env = &in, []string{}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("reference: %v", err)
		}
		var lines []string
		sc := bufio.NewScanner(bytes.NewReader(out))
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if len(lines) != len(reqs) {
			t.Fatalf("reference answered %d of %d requests", len(lines), len(reqs))
		}
		return lines
	}

	// Inline objects as Rust writes them (it fills in defaults), so they round-trip unchanged.
	probe := ask([]map[string]any{{"op": "roundtrip", "para": json.RawMessage(
		`{"text":"￼","runs":[{"len":3}],"objects":[{"type":"image","media":"m","w":10,"h":20}]}`)}})
	var canon struct {
		Para struct {
			Objects []json.RawMessage `json:"objects"`
		} `json:"para"`
	}
	if err := json.Unmarshal([]byte(probe[0]), &canon); err != nil || len(canon.Para.Objects) != 1 {
		t.Fatalf("object probe: %s %v", probe[0], err)
	}
	object := canon.Para.Objects[0]

	reqs := diffRequests(30000, object)
	answers := ask(reqs)
	bad := 0
	for i, req := range reqs {
		got, want := goAnswer(req), answers[i]
		if strings.Contains(want, `"parse"`) {
			t.Fatalf("the reference couldn't read request %d: %s", i, want)
		}
		g, err1 := rustref.NormalizeJSON([]byte(got))
		w, err2 := rustref.NormalizeJSON([]byte(want))
		if err1 != nil || err2 != nil || !bytes.Equal(g, w) {
			bad++
			if bad <= 10 {
				b, _ := json.Marshal(req)
				t.Errorf("request %s\n  go   %s\n  rust %s", b, got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d requests differ from Rust", bad, len(reqs))
	}
	t.Logf("%d requests identical to Rust", len(reqs))
}

func num(req map[string]any, k string) int {
	v, _ := req[k].(int)
	return v
}

// goAnswer runs one request the way the reference program does.
func goAnswer(req map[string]any) string {
	raw, err := json.Marshal(req["para"])
	if err != nil {
		return `{"parse":"marshal"}`
	}
	var p Paragraph
	if err := serdejson.Unmarshal(raw, &p); err != nil {
		return fmt.Sprintf(`{"parse":%q}`, err.Error())
	}
	props := func(k string) CharProps {
		var c CharProps
		if b, err := json.Marshal(req[k]); err == nil && req[k] != nil {
			_ = serdejson.Unmarshal(b, &c)
		}
		return c
	}
	done := func(err error) string {
		if err != nil {
			b, _ := json.Marshal(map[string]string{"err": err.Error()})
			return string(b)
		}
		b, _ := json.Marshal(map[string]any{"para": p})
		return string(b)
	}
	switch req["op"] {
	case "roundtrip":
		return done(nil)
	case "insert":
		text, _ := req["text"].(string)
		_, err := p.InsertText(num(req, "off"), text, props("props"))
		return done(err)
	case "delete":
		return done(p.Delete(num(req, "a"), num(req, "b")))
	case "format":
		set := props("set")
		return done(p.Format(num(req, "a"), num(req, "b"), func(c *CharProps) {
			if set.Bold != nil {
				c.Bold = set.Bold
			}
			if set.BoldCS != nil {
				c.BoldCS = set.BoldCS
			}
			if set.Italic != nil {
				c.Italic = set.Italic
			}
			if set.Size != nil {
				c.Size = set.Size
			}
			if set.Style != nil {
				c.Style = set.Style
			}
		}))
	case "normalize":
		p.Normalize()
		return done(nil)
	case "propsAt", "propsOfChar":
		var c CharProps
		if req["op"] == "propsAt" {
			c = p.PropsAt(num(req, "off"))
		} else {
			c = p.PropsOfChar(num(req, "off"))
		}
		b, _ := json.Marshal(map[string]any{"props": c})
		return string(b)
	case "clamp":
		return fmt.Sprintf(`{"off":%d}`, p.Clamp(num(req, "off")))
	}
	return `{"parse":"unknown op"}`
}

// diffRequests builds deterministic paragraphs (multi-byte text, objects, runs that don't fit the
// text, real Rust properties) and operations on them, offsets often inside characters.
func diffRequests(n int, object json.RawMessage) []map[string]any {
	rng := rand.New(rand.NewPCG(5, 6))
	pieces := []string{"a", "b", "é", "漢", "😀", " ", "￼", "Hi", "\n"}
	propSets := []string{`{}`, `{"bold":true}`, `{"bold":false}`, `{"italic":true,"size":12}`, `{"style":"Emphasis"}`,
		`{"ins":3}`, `{"del":1,"caps":true}`, `{"bold":true,"boldCs":true}`, `{"lang":"pl-PL"}`, `{"link":"https://example.com"}`,
		`{"size":10.5,"hidden":true}`, `{"ins":3,"del":4}`}
	prop := func() json.RawMessage { return json.RawMessage(propSets[rng.IntN(len(propSets))]) }
	var out []map[string]any
	for len(out) < n {
		var text strings.Builder
		objects := 0
		for k := rng.IntN(8); k > 0; k-- {
			s := pieces[rng.IntN(len(pieces))]
			if s == "￼" {
				objects++
			}
			text.WriteString(s)
		}
		t := text.String()
		var runs []map[string]any
		left := len(t)
		for left > 0 || rng.IntN(5) == 0 {
			l := rng.IntN(len(t) + 2)
			if rng.IntN(3) == 0 {
				l = max(left, 0)
			}
			runs = append(runs, map[string]any{"len": l, "props": prop()})
			left -= l
			if len(runs) > 6 {
				break
			}
		}
		if runs == nil {
			runs = []map[string]any{}
		}
		para := map[string]any{"text": t, "runs": runs, "mark": prop()}
		if objects > 0 {
			objs := make([]json.RawMessage, objects)
			for i := range objs {
				objs[i] = object
			}
			para["objects"] = objs
		}
		if rng.IntN(3) == 0 {
			para["props"] = json.RawMessage(`{"style":"Heading1","keepNext":true}`)
		}
		off := func() int { return rng.IntN(len(t) + 3) }
		switch rng.IntN(8) {
		case 0:
			out = append(out, map[string]any{"op": "roundtrip", "para": para})
		case 1, 2:
			ins := pieces[rng.IntN(len(pieces))] + pieces[rng.IntN(len(pieces))]
			out = append(out, map[string]any{"op": "insert", "para": para, "off": off(), "text": ins, "props": prop()})
		case 3, 4:
			out = append(out, map[string]any{"op": "delete", "para": para, "a": off(), "b": off()})
		case 5:
			out = append(out, map[string]any{"op": "format", "para": para, "a": off(), "b": off(), "set": prop()})
		case 6:
			out = append(out, map[string]any{"op": "normalize", "para": para})
		default:
			op := []string{"propsAt", "propsOfChar", "clamp"}[rng.IntN(3)]
			out = append(out, map[string]any{"op": op, "para": para, "off": off()})
		}
	}
	return out
}
