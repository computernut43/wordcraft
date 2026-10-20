package geom

// Pixel-budget arithmetic shared by every package that decodes or allocates rasters: sizes are
// checked before anything is allocated, so a hostile header or request cannot exhaust memory.

import (
	"math"
	"math/bits"
)

// MaxDecodedBytes is the most bytes a decoded picture may take as RGBA8.
const MaxDecodedBytes uint64 = 512 * 1024 * 1024

// MaxRasterPixels is the most pixels one page raster may hold (400 MB of RGBA8); larger requests
// are scaled down.
const MaxRasterPixels uint64 = 100_000_000

// mul is a checked multiplication: false on overflow.
func mul(a, b uint64) (uint64, bool) {
	hi, lo := bits.Mul64(a, b)
	return lo, hi == 0
}

// RGBABytes is the size of a w×h RGBA8 raster; false on overflow.
func RGBABytes(w, h uint32) (uint64, bool) {
	px, ok := mul(uint64(w), uint64(h))
	if !ok {
		return 0, false
	}
	return mul(px, 4)
}

// RGBAFits reports whether a w×h RGBA8 raster fits budget bytes.
func RGBAFits(w, h uint32, budget uint64) bool {
	b, ok := RGBABytes(w, h)
	return w > 0 && h > 0 && ok && b <= budget
}

// DecodeFits reports whether decoding a w×h picture fits budget bytes at its peak: the decoder's
// buffer and the RGBA conversion are alive together, so two RGBA buffers.
func DecodeFits(w, h uint32, budget uint64) bool {
	b, ok := RGBABytes(w, h)
	if !ok {
		return false
	}
	b2, ok := mul(b, 2)
	return w > 0 && h > 0 && ok && b2 <= budget
}

// satUint64 converts like Rust's `as u64`: saturating, NaN and negatives to 0.
func satUint64(v float64) uint64 {
	switch {
	case !(v > 0):
		return 0
	case v >= 1.8446744073709552e19:
		return math.MaxUint64
	}
	return uint64(v)
}

func clamp64(v, lo, hi uint64) uint64 { return min(max(v, lo), hi) }

// FitPixels shrinks w×h (aspect kept, each side at least 1) until it holds at most maxPixels.
func FitPixels(w, h uint32, maxPixels uint64) (uint32, uint32) {
	w, h = max(w, 1), max(h, 1)
	maxPixels = max(maxPixels, 1)
	area := uint64(w) * uint64(h)
	if area <= maxPixels {
		return w, h
	}
	f := math.Sqrt(float64(maxPixels) / float64(area))
	nh := clamp64(satUint64(float64(h)*f), 1, uint64(h))
	nw := max(min(clamp64(satUint64(float64(w)*f), 1, uint64(w)), maxPixels/nh), 1)
	nh = max(min(nh, maxPixels/nw), 1)
	return uint32(nw), uint32(nh)
}
