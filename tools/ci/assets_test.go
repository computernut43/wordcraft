package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// The cases of xtask/src/assets.rs's tests.
func TestMissingAttribution(t *testing.T) {
	files := []string{"assets/icons/a.svg", "crates/x/src/lib.rs", "docs/images/b.png", "tests/fixture.png"}
	md := "| `assets/icons/a.svg` | me | here | MIT |"
	want := []string{"docs/images/b.png", "tests/fixture.png"}
	if got := missingAttribution(files, md); !reflect.DeepEqual(got, want) {
		t.Fatalf("missingAttribution = %q, want %q", got, want)
	}
	if isAsset("crates/x/src/lib.rs") || isAsset("internal/doc/doc.go") {
		t.Fatal("code is not an asset")
	}
	if !isAsset("assets/fonts/OFL.txt") || !isAsset("X.PNG") {
		t.Fatal("assets/ files and upper-case extensions are assets")
	}
}

func TestGitPaths(t *testing.T) {
	got := gitPaths([]byte("assets/\xc3\xa9 b.png\x00docs/images/x.svg\x00"))
	if want := []string{"assets/é b.png", "docs/images/x.svg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gitPaths = %q, want %q", got, want)
	}
	if len(gitPaths(nil)) != 0 {
		t.Fatal("empty output: want no paths")
	}
}

// #1364: outside a git checkout `git ls-files` fails, and its empty output read as "no files"
// passed the check.
func TestFailedFileListingFailsTheCheck(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ATTRIBUTION.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	if cmd.Run() == nil {
		t.Skip("the temporary directory is inside a git checkout")
	}
	if err := checkAssets(dir); err == nil {
		t.Fatal("checkAssets outside a git checkout passed; want an error")
	}
}

func TestCheckAssetsInARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ATTRIBUTION.md", "| `assets/a.svg` | me | here | MIT |\n")
	write("assets/a.svg", "<svg/>")
	write("main.go", "package main\n")
	if err := checkAssets(dir); err != nil {
		t.Fatalf("attributed tree: %v", err)
	}
	write("docs/images/new.png", "png")
	if err := checkAssets(dir); err == nil {
		t.Fatal("unattributed docs/images/new.png passed")
	}
	write(".gitignore", "docs/images/new.png\n")
	if err := checkAssets(dir); err != nil {
		t.Fatalf("ignored file: %v", err)
	}
}
