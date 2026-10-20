package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/computernut43/wordcraft/internal/rustref"
)

// EnvScripts sets how many random scripts TestRunMatchesRust compares (default 300).
const EnvScripts = "WORDCRAFT_CLI_SCRIPTS"

// notPortedMsg starts every refusal of the Go port (engine.NotPorted and the CLI's own).
const notPortedMsg = "not available in the Go port yet"

// TestRunMatchesRust runs random command scripts on the Rust sample document with both CLIs
// (`run --file sample.json --cmd … --print --save out.json`) and compares what they print, how
// they fail, and the documents they save. Scripts are cut before the first step the Go port
// refuses as not ported, so every compared step is one Go claims to do as Rust does.
func TestRunMatchesRust(t *testing.T) {
	cli := rustref.Require(t)
	n := 300
	if v := os.Getenv(EnvScripts); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("%s=%q: want a positive count", EnvScripts, v)
		}
	}
	ctx := context.Background()
	dir := t.TempDir()
	sample := filepath.Join(dir, "sample.json")
	if _, err := cli.Run(ctx, dir, "run", "--template", "sample", "--save", sample); err != nil {
		t.Fatal(err)
	}
	words := sampleWords(t, sample)
	rng := rand.New(rand.NewPCG(7, 1))
	compared, steps := 0, 0
	refused := map[string]int{}
	for i := range n {
		script := randomScript(rng, words)
		goOut, goErr, goCode := runGo(dir, sample, script)
		if strings.Contains(goErr, notPortedMsg) {
			refused[errLine(goErr)[strings.Index(errLine(goErr), notPortedMsg):]]++
			// Each successful step printed one line; the refused one is next.
			script = script[:strings.Count(goOut, "\n")]
			goOut, goErr, goCode = runGo(dir, sample, script)
			if strings.Contains(goErr, notPortedMsg) {
				t.Fatalf("script %d: refused again after cutting it: %s", i, goErr)
			}
		}
		rustArgs := append([]string{"run", "--file", sample}, cmdArgs(script)...)
		rustArgs = append(rustArgs, "--print", "--save", filepath.Join(dir, "rust.json"))
		res, err := cli.Exec(ctx, dir, rustArgs...)
		if err != nil {
			t.Fatal(err)
		}
		where := fmt.Sprintf("script %d %q", i, cmdArgs(script))
		if string(res.Stdout) != goOut {
			t.Fatalf("%s: printed\nrust: %s\ngo:   %s", where, res.Stdout, goOut)
		}
		if res.Code != goCode || errLine(string(res.Stderr)) != errLine(goErr) {
			t.Fatalf("%s: exit\nrust %d: %s\ngo   %d: %s", where, res.Code, res.Stderr, goCode, goErr)
		}
		if goCode == 0 {
			a := savedDoc(t, filepath.Join(dir, "rust.json"))
			b := savedDoc(t, filepath.Join(dir, "go.json"))
			if !bytes.Equal(a, b) {
				t.Fatalf("%s: saved documents differ\n%s", where, firstDiff(a, b))
			}
		}
		os.Remove(filepath.Join(dir, "rust.json"))
		os.Remove(filepath.Join(dir, "go.json"))
		compared++
		steps += len(script)
	}
	t.Logf("%d scripts, %d steps compared; cut at refusals:", compared, steps)
	for _, k := range slices.Sorted(maps.Keys(refused)) {
		t.Logf("%5d %s", refused[k], k)
	}
	if steps < n {
		t.Fatalf("only %d steps compared in %d scripts: the generator mostly hits refusals", steps, n)
	}
}

type scriptStep struct{ id, params string }

func cmdArgs(script []scriptStep) []string {
	var out []string
	for _, s := range script {
		spec := s.id
		if s.params != "" {
			spec += "=" + s.params
		}
		out = append(out, "--cmd", spec)
	}
	return out
}

func runGo(dir, sample string, script []scriptStep) (stdout, stderr string, code int) {
	args := append([]string{"run", "--file", sample}, cmdArgs(script)...)
	args = append(args, "--print", "--save", filepath.Join(dir, "go.json"))
	var out, errs bytes.Buffer
	code = run(args, &out, &errs)
	return out.String(), errs.String(), code
}

// errLine is the error a run printed ("wordcraft-cli: …"), without the "wrote" lines.
func errLine(stderr string) string {
	for _, l := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(l, "wordcraft-cli: ") {
			return l
		}
	}
	return ""
}

// savedDoc reads a saved document without its save times, which differ between the two runs.
func savedDoc(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	if core, ok := d["core"].(map[string]any); ok {
		delete(core, "modified")
		delete(core, "created")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	norm, err := rustref.NormalizeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return norm
}

func firstDiff(a, b []byte) string {
	al, bl := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	for i := range min(len(al), len(bl)) {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d\nrust: %s\ngo:   %s", i+1, al[i], bl[i])
		}
	}
	return fmt.Sprintf("lengths %d and %d lines", len(al), len(bl))
}

// sampleWords are the sample document's words, for select.text targets.
func sampleWords(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Body []struct {
			Text string `json:"text"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var words []string
	for _, b := range d.Body {
		for _, w := range strings.Fields(b.Text) {
			w = strings.Trim(w, ".,;:!?\"'()")
			if w != "" && !seen[w] && !strings.ContainsRune(w, '￼') {
				seen[w] = true
				words = append(words, w)
			}
		}
	}
	if len(words) < 20 {
		t.Fatalf("only %d words in the sample", len(words))
	}
	return words
}

// randomScript is up to 12 steps of the ported commands, mixing cases Go handles with ones it
// refuses, misses and bad parameters.
func randomScript(rng *rand.Rand, words []string) []scriptStep {
	quote := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	inserts := []string{"Workshop", "x", "a b", "  ", "ünïcödé", "日本語", "😀 ok", "tab\there", "line\vbreak", "\v", "q", "'", "\"", ",", "Hello, world"}
	var out []scriptStep
	for range 1 + rng.IntN(12) {
		var s scriptStep
		switch k := rng.IntN(24); {
		case k < 6:
			w := words[rng.IntN(len(words))]
			if rng.IntN(4) == 0 && len(w) > 2 {
				w = w[:1+rng.IntN(len(w)-1)]
			}
			p := map[string]any{"text": w}
			if rng.IntN(3) == 0 {
				p["occurrence"] = rng.IntN(4)
			}
			if rng.IntN(30) == 0 {
				p["text"] = "no such text zq"
			}
			s = scriptStep{"select.text", quote(p)}
		case k < 11:
			text := inserts[rng.IntN(len(inserts))]
			p := map[string]any{"text": text}
			if rng.IntN(3) == 0 {
				p["raw"] = true
			}
			s = scriptStep{"text.insert", quote(p)}
		case k < 15:
			s = scriptStep{"format.bold", quote(map[string]any{"value": rng.IntN(2) == 0})}
		case k < 15+2:
			s = scriptStep{"select.range", quote(map[string]any{"anchor": randomPos(rng), "focus": randomPos(rng)})}
		case k < 15+3:
			if rng.IntN(2) == 0 {
				s = scriptStep{"select.collapse", quote(map[string]any{"end": rng.IntN(2) == 0})}
			} else {
				s = scriptStep{"select.collapse", ""}
			}
		case k < 15+5:
			s = scriptStep{"edit.undo", ""}
		case k < 15+7:
			s = scriptStep{"edit.redo", ""}
		default:
			bad := []scriptStep{{"text.insert", "{}"}, {"text.insert", `{"text":5}`}, {"select.text", `{"text":""}`}, {"no.such", ""}}
			s = bad[rng.IntN(len(bad))]
		}
		out = append(out, s)
	}
	return out
}

// randomPos is a position parameter in one of the forms select.range reads, mostly valid ones
// near the sample's first paragraphs, sometimes out of range or malformed.
func randomPos(rng *rand.Rand) any {
	block, off := rng.IntN(12), rng.IntN(40)
	switch rng.IntN(12) {
	case 0:
		return map[string]any{"block": block}
	case 1:
		return map[string]any{"block": block, "off": off}
	case 2:
		return []any{"body", []any{block}, off}
	case 3:
		return map[string]any{"story": map[string]any{"body": nil}, "path": []any{block}, "off": off}
	case 4:
		return map[string]any{"story": "body", "path": []any{block}, "off": 1 << 40}
	case 5:
		return map[string]any{"story": "body", "path": []any{}, "off": off}
	case 6:
		bad := []any{nil, "x", -1, 1.5, map[string]any{"block": -2}, map[string]any{"story": "part", "path": []any{0}, "off": 0},
			map[string]any{"story": "body", "path": []any{0}}, []any{"body", []any{0}}, map[string]any{"block": 4294967296 + 2, "off": 3},
			map[string]any{"block": 1, "off": json.Number("99999999999999999999")}, map[string]any{"block": 2, "off": 1.5},
			map[string]any{"story": map[string]any{"body": nil, "part": 1}, "path": []any{0}, "off": 0},
			map[string]any{"story": "body", "path": []any{1}, "off": json.Number("1e1")}, []any{"body", []any{1}, 2, 3}}
		return bad[rng.IntN(len(bad))]
	}
	return map[string]any{"story": "body", "path": []any{block}, "off": off}
}
