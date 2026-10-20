package main

import (
	"bytes"
	"context"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/computernut43/wordcraft/internal/rustref"
)

// TestConvertDocxMatchesRust opens .docx files the Rust CLI writes (each template, and the
// sample after random command scripts) with both CLIs (`convert IN.docx OUT.json`) and compares
// the documents they save. Files using something the Go reader doesn't port yet are refused and
// skipped.
func TestConvertDocxMatchesRust(t *testing.T) {
	cli := rustref.Require(t)
	ctx := context.Background()
	dir := t.TempDir()
	sample := filepath.Join(dir, "sample.json")
	if _, err := cli.Run(ctx, dir, "run", "--template", "sample", "--save", sample); err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, tpl := range []string{"blank", "sample", "letter", "resume", "report"} {
		f := filepath.Join(dir, tpl+".docx")
		if _, err := cli.Run(ctx, dir, "run", "--template", tpl, "--save", f); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	words := sampleWords(t, sample)
	rng := rand.New(rand.NewPCG(3, 9))
	for i := range 40 {
		f := filepath.Join(dir, "script"+string(rune('a'+i%26))+string(rune('a'+i/26))+".docx")
		args := append([]string{"run", "--file", sample}, cmdArgs(randomScript(rng, words))...)
		if res, err := cli.Exec(ctx, dir, append(args, "--save", f)...); err != nil || res.Code != 0 {
			continue
		}
		files = append(files, f)
	}
	compared := 0
	for _, f := range files {
		var out, errs bytes.Buffer
		goJSON, rustJSON := filepath.Join(dir, "go.json"), filepath.Join(dir, "rust.json")
		code := run([]string{"convert", f, goJSON}, &out, &errs)
		if strings.Contains(errs.String(), notPortedMsg) {
			t.Logf("%s: %s", filepath.Base(f), errLine(errs.String()))
			continue
		}
		res, err := cli.Exec(ctx, dir, "convert", f, rustJSON)
		if err != nil {
			t.Fatal(err)
		}
		if res.Code != code {
			t.Fatalf("%s: exit\nrust %d: %s\ngo   %d: %s", filepath.Base(f), res.Code, res.Stderr, code, errs.String())
		}
		if code == 0 {
			a, b := savedDoc(t, rustJSON), savedDoc(t, goJSON)
			if !bytes.Equal(a, b) {
				t.Fatalf("%s: documents differ\n%s", filepath.Base(f), firstDiff(a, b))
			}
		}
		_ = os.Remove(goJSON)
		_ = os.Remove(rustJSON)
		compared++
	}
	t.Logf("%d of %d files compared", compared, len(files))
	if compared < len(files)/2 {
		t.Fatalf("only %d of %d files compared", compared, len(files))
	}
}
