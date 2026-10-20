package docx

// Lenient number parsing for OOXML attribute values (Rust crates/docx/src/units.rs).

import (
	"math"
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"
)

// MaxLenPt is the largest absolute length accepted, points (beyond any real page: 200 inches).
const MaxLenPt float32 = 14_400.0

// measure parses a number, accepting decimals and ISO 29500 universal measures (1in, 2.5cm,
// 12pt, 3mm, 1pc, 1pi). perPt is how many file units make a point when there's no unit suffix
// (20 for twips, 2 for half-points, 8 for eighths, 12700 for EMU). Returns points.
func measure(s string, perPt float32) (float32, bool) {
	t := rustcompat.Trim(s)
	if t == "" || len(t) > 40 {
		return 0, false
	}
	split := strings.IndexFunc(t, func(c rune) bool {
		return !('0' <= c && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E')
	})
	if split < 0 {
		split = len(t)
	}
	v, ok := rustcompat.ParseF64(t[:split])
	if !ok || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	var pt float64
	switch rustcompat.Trim(t[split:]) {
	case "":
		pt = v / float64(perPt)
	case "pt":
		pt = v
	case "in":
		pt = v * 72.0
	case "cm":
		pt = v * 72.0 / 2.54
	case "mm":
		pt = v * 72.0 / 25.4
	case "pc", "pi":
		pt = v * 12.0
	case "px":
		pt = v * 0.75
	case "emu":
		pt = v / 12_700.0
	default:
		return 0, false
	}
	p := float32(pt)
	if math.IsInf(float64(p), 0) || math.IsNaN(float64(p)) {
		return 0, false
	}
	return rustcompat.Clamp(p, -MaxLenPt, MaxLenPt), true
}

// lineMultiple reads w:line under lineRule="auto": the multiple of single line spacing, in 240ths
// (240 = single). Without a unit the value already is that (the transitional form). A universal
// measure is a height (strict OOXML writes w:line="13.90pt", ISO 29500 ST_SignedTwipsMeasure), so
// it goes through twips as Word reads it: × 20 ÷ 240.
func lineMultiple(s string) (float32, bool) {
	t := rustcompat.Trim(s)
	measured := t != "" && isASCIIAlpha(t[len(t)-1])
	v, ok := measure(t, 240.0)
	if ok && measured {
		v = v * 20.0 / 240.0
	}
	return v, ok
}

func isASCIIAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// intOf is a plain integer (accepts decimals, rounds; clamps into the int64 range).
func intOf(s string) (int64, bool) {
	t := rustcompat.Trim(s)
	if t == "" || len(t) > 40 {
		return 0, false
	}
	if v, ok := rustcompat.ParseInt(t, 64); ok {
		return v, true
	}
	v, ok := rustcompat.ParseF64(t)
	if !ok || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return rustcompat.I64(rustcompat.Clamp(math.Round(v), math.MinInt64, math.MaxInt64)), true
}

// u32Of is intOf clamped into the uint32 range.
func u32Of(s string) (uint32, bool) {
	v, ok := intOf(s)
	return uint32(min(max(v, 0), math.MaxUint32)), ok
}

// tw reads a twips attribute as points.
func tw(e *El, attr string) (float32, bool) {
	v, ok := e.Attr(attr)
	if !ok {
		return 0, false
	}
	return measure(v, 20.0)
}

// onOff reads ST_OnOff: an absent w:val is on; 0, false, off are off.
func onOff(e *El) bool {
	v, ok := e.Attr("w:val")
	if !ok {
		return true
	}
	switch rustcompat.Trim(v) {
	case "0", "false", "off", "False", "FALSE":
		return false
	}
	return true
}

// flag is an on/off child of parent, if present.
func flag(parent *El, name string) (bool, bool) {
	c := parent.Child(name)
	if c == nil {
		return false, false
	}
	return onOff(c), true
}

// attrMeasure is measure of attribute name, if present.
func attrMeasure(e *El, name string, perPt float32) (float32, bool) {
	v, ok := e.Attr(name)
	if !ok {
		return 0, false
	}
	return measure(v, perPt)
}
