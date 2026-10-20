package main

// Asset attribution, ported from `cargo xtask assets` (xtask/src/assets.rs): every non-code asset
// must be listed as `path` in ATTRIBUTION.md.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var assetExt = map[string]bool{}

func init() {
	for _, e := range []string{
		"png", "jpg", "jpeg", "gif", "webp", "bmp", "tif", "tiff", "ico", "icns", "svg", "pdf", "ai", "eps", "psd", "icc", "icm", "ttf", "otf", "woff",
		"woff2", "ase", "indd", "idml", "aco", "abr", "docx", "odt", "rtf", "mp4", "wav", "mp3",
	} {
		assetExt[e] = true
	}
}

var assetDirs = []string{"assets/", "docs/images/", "examples/"}

func isAsset(path string) bool {
	for _, d := range assetDirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return assetExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
}

// missingAttribution lists the asset paths that ATTRIBUTION.md doesn't mention as `path`.
func missingAttribution(files []string, attribution string) []string {
	var miss []string
	for _, f := range files {
		if isAsset(f) && !strings.Contains(attribution, "`"+f+"`") {
			miss = append(miss, f)
		}
	}
	return miss
}

// gitPaths splits `git ls-files -z` output: NUL-separated and never quoted, so names with
// non-ASCII letters or newlines are still checked.
func gitPaths(stdout []byte) []string {
	var out []string
	for _, p := range bytes.Split(stdout, []byte{0}) {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

func checkAssets(root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A failed listing is no listing: read as empty, it would pass with nothing checked (#1364).
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git ls-files failed (%v): %s", err, strings.TrimSpace(stderr.String()))
	}
	var files []string
	for _, f := range gitPaths(stdout.Bytes()) {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			files = append(files, f)
		}
	}
	md, err := os.ReadFile(filepath.Join(root, "ATTRIBUTION.md"))
	if err != nil {
		return fmt.Errorf("ATTRIBUTION.md: %w", err)
	}
	if miss := missingAttribution(files, string(md)); len(miss) > 0 {
		return fmt.Errorf("%d asset file(s) lack an ATTRIBUTION.md entry (author, source, licence):\n  %s", len(miss), strings.Join(miss, "\n  "))
	}
	count := 0
	for _, f := range files {
		if isAsset(f) {
			count++
		}
	}
	fmt.Printf("assets: all %d asset files attributed in ATTRIBUTION.md\n", count)
	return nil
}
