package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cli(args ...string) (stdout, stderr string, code int) {
	var out, errs bytes.Buffer
	code = run(args, &out, &errs)
	return out.String(), errs.String(), code
}

func TestRunScript(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.json")
	doc := `{"body":[{"kind":"para","text":"Hello world","runs":[{"len":11}]}]}`
	if err := os.WriteFile(in, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := cli("run", "--file", in, "--cmd", `select.text={"text":"world"}`, "--cmd", `format.bold={"value":true}`, "--print", "--save", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := `select.text: {"anchor":{"off":6,"path":[0],"story":"body"},"focus":{"off":11,"path":[0],"story":"body"}}` + "\n" +
		`format.bold: {"anchor":{"off":6,"path":[0],"story":"body"},"focus":{"off":11,"path":[0],"story":"body"}}` + "\n"
	if stdout != want {
		t.Fatalf("printed\n%s\nwant\n%s", stdout, want)
	}
	if stderr != "wrote "+out+"\n" {
		t.Fatalf("stderr %q", stderr)
	}
	saved, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(saved), `"bold": true`) {
		t.Fatalf("%v\n%s", err, saved)
	}
	if _, stderr, code := cli("convert", out, filepath.Join(dir, "copy.json")); code != 0 {
		t.Fatalf("convert: %s", stderr)
	}
	// A failing step stops the script before saving.
	_, stderr, code = cli("run", "--file", in, "--cmd", `select.text={"text":"zzz"}`, "--save", filepath.Join(dir, "no.json"))
	if code != 1 || stderr != "wordcraft-cli: select.text: `zzz` not found\n" {
		t.Fatalf("exit %d: %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "no.json")); err == nil {
		t.Fatal("saved after a failed step")
	}
}

func TestErrors(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "usage:"},
		{[]string{"mcp"}, `not available in the Go port yet: the "mcp" subcommand`},
		{[]string{"convert", "a.json"}, "missing argument"},
		{[]string{"convert", "--format", "x"}, "not available in the Go port yet: convert option --format"},
		{[]string{"run", "--template", "sample"}, "not available in the Go port yet: run option --template"},
		{[]string{"run", "--cmd", "x={bad"}, "x: bad JSON params"},
		{[]string{"run", "--save"}, "--save needs a value"},
		{[]string{"run"}, "not available in the Go port yet: run without --file"},
		{[]string{"run", "--file", "missing.json"}, "missing.json"},
		{[]string{"convert", "in.docx", "out.json"}, "in.docx"},
	} {
		_, stderr, code := cli(c.args...)
		if code != 1 || !strings.HasPrefix(stderr, "wordcraft-cli: ") || !strings.Contains(stderr, c.want) {
			t.Errorf("%q: exit %d, %q (want %q)", c.args, code, stderr, c.want)
		}
	}
}

func TestCommandsList(t *testing.T) {
	stdout, _, code := cli("commands")
	if code != 0 {
		t.Fatal(code)
	}
	for _, id := range []string{"edit.redo", "edit.undo", "format.bold", "select.collapse", "select.range", "select.text", "text.insert"} {
		if !strings.Contains(stdout, id+" ") {
			t.Errorf("%s missing from\n%s", id, stdout)
		}
	}
}

func TestParseStep(t *testing.T) {
	st, err := parseStep(`select.text={"text":"a=b","occurrence":18446744073709551615}`)
	if err != nil || st.id != "select.text" || st.params["text"] != "a=b" {
		t.Fatalf("%+v %v", st, err)
	}
	if n, ok := st.params.U64("occurrence"); !ok || n != 18446744073709551615 {
		t.Fatalf("occurrence %d %v", n, ok)
	}
	// Non-object JSON counts as no parameters, as in Rust.
	if st, err := parseStep(`edit.undo=[1]`); err != nil || len(st.params) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
}
