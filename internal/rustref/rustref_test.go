package rustref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake reference CLI: invoked with "convert" or "__fake__" as its
// first argument it behaves as below instead of running tests.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "convert" || os.Args[1] == "__fake__") {
		os.Exit(fake(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fake(args []string) int {
	switch {
	case args[0] == "convert" && len(args) == 3:
		if err := os.WriteFile(args[2], []byte(`{"b":1.0,"a":[1,2.5]}`), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case len(args) < 2:
		return 2
	case args[1] == "env":
		for _, e := range os.Environ() {
			fmt.Println(e)
		}
		return 0
	case args[1] == "big":
		fmt.Print(strings.Repeat("x", 1<<20))
		return 0
	case args[1] == "sleep":
		time.Sleep(10 * time.Second)
		return 0
	case args[1] == "fail":
		fmt.Fprintln(os.Stderr, "boom: could not open")
		return 3
	}
	return 2
}

func self(t *testing.T) *CLI {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &CLI{Path: exe}
}

func digest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestFromEnv(t *testing.T) {
	exe := self(t).Path
	t.Setenv("CI", "")
	t.Setenv(EnvSHA256, "")

	t.Setenv(EnvPath, "")
	if _, err := FromEnv(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unset: %v, want ErrNotConfigured", err)
	}
	t.Setenv(EnvPath, "relative/wordcraft-cli")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative path: %v", err)
	}
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "missing"))
	if _, err := FromEnv(); err == nil {
		t.Fatal("missing binary: want an error")
	}
	t.Setenv(EnvPath, t.TempDir())
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory: %v", err)
	}
	t.Setenv(EnvPath, exe)
	if c, err := FromEnv(); err != nil || c.Path != exe {
		t.Fatalf("no digest outside CI: %v", err)
	}
	t.Setenv("CI", "true")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), EnvSHA256) {
		t.Fatalf("CI without digest: %v", err)
	}
	t.Setenv(EnvSHA256, strings.Repeat("0", 64))
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("wrong digest: %v", err)
	}
	t.Setenv(EnvSHA256, strings.ToUpper(digest(t, exe)))
	if _, err := FromEnv(); err != nil {
		t.Fatalf("right digest (upper case): %v", err)
	}
}

func TestRunGivesTheChildNoInheritedEnvironment(t *testing.T) {
	t.Setenv("RUSTREF_CANARY_TOKEN", "secret-value")
	dir := t.TempDir()
	out, err := self(t).Run(context.Background(), dir, "__fake__", "env")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "secret-value") || strings.Contains(s, "RUSTREF_CANARY_TOKEN") {
		t.Fatalf("inherited variable reached the reference:\n%s", s)
	}
	for _, want := range []string{"HOME=" + dir, "TMPDIR=" + dir, "LANG=en_US.UTF-8"} {
		if !strings.Contains(s, want+"\n") {
			t.Errorf("environment lacks %s:\n%s", want, s)
		}
	}
}

func TestRunCapsOutput(t *testing.T) {
	c := self(t)
	c.MaxOutput = 1024
	if _, err := c.Run(context.Background(), t.TempDir(), "__fake__", "big"); err == nil {
		t.Fatal("1 MiB of output under a 1 KiB cap: want an error")
	}
}

func TestRunTimesOut(t *testing.T) {
	c := self(t)
	c.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := c.Run(context.Background(), t.TempDir(), "__fake__", "sleep")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sleeping child: %v, want a deadline error", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
}

func TestRunReportsFailureWithStderr(t *testing.T) {
	_, err := self(t).Run(context.Background(), t.TempDir(), "__fake__", "fail")
	if err == nil || !strings.Contains(err.Error(), "boom: could not open") {
		t.Fatalf("failing child: %v", err)
	}
}

func TestConvertToJSON(t *testing.T) {
	in := filepath.Join(t.TempDir(), "in.docx")
	if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := self(t).ConvertToJSON(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"b":1.0,"a":[1,2.5]}` {
		t.Fatalf("ConvertToJSON = %s", got)
	}
	c := self(t)
	c.MaxOutput = 4
	if _, err := c.ConvertToJSON(context.Background(), in); err == nil {
		t.Fatal("output file over the cap: want an error")
	}
}

func TestNormalizeJSON(t *testing.T) {
	a, err := NormalizeJSON([]byte(`{"b":1.0,"a":[1,2.5,1e2,-0.0]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NormalizeJSON([]byte(`{"a":[1.0,2.50,100,0],"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("not equal after normalizing:\n%s\n%s", a, b)
	}
	if !json.Valid(a) {
		t.Fatalf("invalid output: %s", a)
	}
	for _, bad := range []string{`{`, `{} {}`, `[1e999]`, strings.Repeat("[", 600) + strings.Repeat("]", 600)} {
		if _, err := NormalizeJSON([]byte(bad)); err == nil {
			t.Errorf("NormalizeJSON(%.20q…): want an error", bad)
		}
	}
}

// TestReferenceSample runs the real reference, when configured: the built-in sample through a
// command and a DOCX save, then back to the model JSON.
func TestReferenceSample(t *testing.T) {
	c := Require(t)
	dir := t.TempDir()
	docx := filepath.Join(dir, "sample.docx")
	ctx := context.Background()
	if _, err := c.Run(ctx, dir, "run", "--template", "sample", "--cmd", `select.text={"text":"Studio"}`, "--cmd", "format.bold", "--save", docx); err != nil {
		t.Fatal(err)
	}
	data, err := c.ConvertToJSON(ctx, docx)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Body []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
			Runs []struct {
				Len   int            `json:"len"`
				Props map[string]any `json:"props"`
			} `json:"runs"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Body) == 0 || doc.Body[0].Kind != "para" || doc.Body[0].Text != "The Open Studio Handbook" {
		t.Fatalf("unexpected first block: %+v", doc.Body)
	}
	runs := doc.Body[0].Runs
	if len(runs) != 3 || runs[1].Len != len("Studio") || runs[1].Props["bold"] != true || runs[1].Props["boldCs"] != true {
		t.Fatalf("Studio should be one bold (and boldCs) run: %+v", runs)
	}
}
