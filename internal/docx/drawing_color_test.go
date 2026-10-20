package docx

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// glibcSRGB is the SHA-256 of the 100 001 bytes Rust's (glibc's pow) sRGB conversion gives for
// the linear channels 0, 1, … 100 000 of an a:scrgbClr (computed with the Rust expression from
// crates/docx/src/read/drawing_color.rs, and with C against glibc 2.39, built without FMA
// contraction; both agree).
const glibcSRGB = "9d30ccaf1260346a469a17944e836a3119f21757538af8bf2a4434e2d7b5ea32"

// TestLinearToSRGBMatchesGlibc checks every possible a:scrgbClr channel converts to the byte
// Rust gives, on whichever CPU runs the test (Go may fuse multiply-adds inside math.Pow on some).
func TestLinearToSRGBMatchesGlibc(t *testing.T) {
	out := make([]byte, 0, 100_001)
	for v := range 100_001 {
		out = append(out, srgbByte(min(max(float64(v)/100_000, 0), 1)))
	}
	sum := sha256.Sum256(out)
	if got := hex.EncodeToString(sum[:]); got != glibcSRGB {
		t.Fatalf("sRGB bytes hash %s, want glibc's %s", got, glibcSRGB)
	}
	// Spot values: black, the linear segment, mid grey, white.
	for _, c := range []struct {
		v    int
		want uint8
	}{{0, 0}, {300, 10}, {21_404, 127}, {21_586, 128}, {100_000, 255}} {
		if got := srgbByte(float64(c.v) / 100_000); got != c.want {
			t.Errorf("srgbByte(%d) = %d, want %d", c.v, got, c.want)
		}
	}
}
