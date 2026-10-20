package doc

import (
	"math"

	"github.com/computernut43/craft-libs/rustcompat"
)

// Rust's f32 operations where Go's differ.

// fmax is Rust's f32::max: NaN loses to a number; of 0 and -0, b.
func fmax(a, b F32) F32 { return rustcompat.Max(a, b) }

// fmin is Rust's f32::min: NaN loses to a number; of 0 and -0, b.
func fmin(a, b F32) F32 { return rustcompat.Min(a, b) }

// fclamp is Rust's f32::clamp (lo <= hi): NaN stays NaN, and v is kept when it equals a bound.
func fclamp(v, lo, hi F32) F32 { return rustcompat.Clamp(v, lo, hi) }

// u32Sat is Rust's `v as u32`: NaN is 0, out of range saturates.
func u32Sat(v float64) uint32 {
	switch {
	case v != v || v <= 0:
		return 0
	case v >= math.MaxUint32:
		return math.MaxUint32
	}
	return uint32(v)
}

// i32Sat is Rust's `v as i32`: NaN is 0, out of range saturates.
func i32Sat(v float64) int32 {
	switch {
	case v != v:
		return 0
	case v <= math.MinInt32:
		return math.MinInt32
	case v >= math.MaxInt32:
		return math.MaxInt32
	}
	return int32(v)
}

// fround is Rust's f32::round: halfway cases away from zero.
func fround(v F32) F32 { return F32(math.Round(float64(v))) }
