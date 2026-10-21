package docx

// Office Math (OMML): the document-wide equation settings (Rust crates/docx/src/read/math.rs
// read_math_pr, on, prop, val). Equations themselves aren't ported yet.

import (
	"math"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

// readMathPr reads m:mathPr (in the settings part).
func readMathPr(e *El) doc.MathProps {
	var m doc.MathProps
	m.SetDefaults()
	switch v, _ := mathVal(e, "m:brkBin"); v {
	case "after":
		m.BrkBin = doc.BrkAfter
	case "repeat":
		m.BrkBin = doc.BrkRepeat
	default:
		m.BrkBin = doc.BrkBefore
	}
	switch v, _ := mathVal(e, "m:brkBinSub"); v {
	case "-+":
		m.BrkBinSub = doc.MinusPlus
	case "+-":
		m.BrkBinSub = doc.PlusMinus
	default:
		m.BrkBinSub = doc.MinusMinus
	}
	if c := e.Child("m:wrapIndent"); c != nil {
		if v, ok := tw(c, "m:val"); ok && !math.IsInf(float64(v), 0) && v == v && v >= 0 {
			m.WrapIndent = doc.F32(rustcompat.Min(v, 1584))
		}
	}
	m.WrapRight = mathProp(e, "m:wrapRight", false)
	return m
}

// mathOn reads an OMML on/off property: absent m:val is on.
func mathOn(e *El) bool {
	v, ok := e.Attr("m:val")
	if !ok {
		return true
	}
	switch rustcompat.Trim(v) {
	case "0", "false", "off", "False", "FALSE":
		return false
	}
	return true
}

// mathProp is on/off property name of the properties element pr (absent: def).
func mathProp(pr *El, name string, def bool) bool {
	if pr == nil {
		return def
	}
	if c := pr.Child(name); c != nil {
		return mathOn(c)
	}
	return def
}

// mathVal is the m:val of child name of pr.
func mathVal(pr *El, name string) (string, bool) {
	if pr == nil {
		return "", false
	}
	if c := pr.Child(name); c != nil {
		return c.Attr("m:val")
	}
	return "", false
}
