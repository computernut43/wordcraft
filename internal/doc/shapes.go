package doc

// Shape helpers: effect sanitizing (Rust crates/doc/src/effects.rs sanitized), text warps
// (wordart.rs TextWarp::new, set_adj) and text box anchoring (props.rs valign_from_anchor).

import "math"

// Effect limits (Rust MAX_BLUR, MAX_DISTANCE, MAX_GLOW, MAX_SOFT_EDGE).
const (
	MaxBlur     = 100.0
	MaxDistance = 200.0
	MaxGlow     = 150.0
	MaxSoftEdge = 100.0
)

// effectClamp is v clamped, or dflt when not finite.
func effectClamp(v, lo, hi, dflt F32) F32 {
	if finite(v) {
		return fclamp(v, lo, hi)
	}
	return dflt
}

// Sanitized is the shadow finite and in range.
func (s Shadow) Sanitized() Shadow {
	a := float32(effectClamp(s.Angle, -1e6, 1e6, 45))
	// f32::rem_euclid(360): the remainder is exact; a negative one is shifted up.
	r := float32(math.Mod(float64(a), 360))
	if r < 0 {
		r += 360
	}
	angle := F32(0)
	if finite(F32(r)) && r < 360 {
		angle = F32(r)
	}
	return Shadow{
		Color:        s.Color,
		Transparency: effectClamp(s.Transparency, 0, 100, 60),
		Blur:         effectClamp(s.Blur, 0, MaxBlur, 0),
		Distance:     effectClamp(s.Distance, 0, MaxDistance, 0),
		Angle:        angle,
		RotWithShape: s.RotWithShape,
	}
}

// Sanitized is the glow finite and in range.
func (g Glow) Sanitized() Glow {
	return Glow{Color: g.Color, Size: effectClamp(g.Size, 0, MaxGlow, 0), Transparency: effectClamp(g.Transparency, 0, 100, 60)}
}

// Sanitized are the effects finite and in range; zero-size glows and soft edges dropped.
func (e ShapeEffects) Sanitized() ShapeEffects {
	var out ShapeEffects
	if e.Shadow != nil {
		s := e.Shadow.Sanitized()
		out.Shadow = &s
	}
	if e.Glow != nil {
		if g := e.Glow.Sanitized(); g.Size > 0 {
			out.Glow = &g
		}
	}
	if e.SoftEdge != nil {
		if r := effectClamp(*e.SoftEdge, 0, MaxSoftEdge, 0); r > 0 {
			out.SoftEdge = &r
		}
	}
	return out
}

// DefaultGlow is the Rust Glow default's colour.
var DefaultGlow = Glow{Color: RGB{0x15, 0x60, 0x82}, Size: 5, Transparency: 60}

// Text warp limits (Rust MAX_ADJ, MAX_ADJ_VALUE).
const (
	MaxAdj      = 8
	MaxAdjValue = 100_000_000
)

// WarpPresets are the DrawingML text warps (ST_TextShapeType).
var WarpPresets = []string{
	"textNoShape", "textPlain", "textStop", "textTriangle", "textTriangleInverted", "textChevron", "textChevronInverted",
	"textRingInside", "textRingOutside", "textArchUp", "textArchDown", "textCircle", "textButton", "textArchUpPour",
	"textArchDownPour", "textCirclePour", "textButtonPour", "textCurveUp", "textCurveDown", "textCanUp", "textCanDown",
	"textWave1", "textWave2", "textDoubleWave1", "textWave4", "textInflate", "textDeflate", "textInflateBottom",
	"textDeflateBottom", "textInflateTop", "textDeflateTop", "textDeflateInflate", "textDeflateInflateDeflate",
	"textFadeRight", "textFadeLeft", "textFadeUp", "textFadeDown", "textSlantUp", "textSlantDown", "textCascadeUp",
	"textCascadeDown",
}

// NewTextWarp is a warp with preset (one of WarpPresets); false for any other name.
func NewTextWarp(preset string) (TextWarp, bool) {
	for _, p := range WarpPresets {
		if p == preset {
			return TextWarp{Preset: p}, true
		}
	}
	return TextWarp{}, false
}

// SetAdj adds an adjust value (capped: at most MaxAdj, short ASCII names, values within
// ±MaxAdjValue); a repeated name replaces the earlier value.
func (w *TextWarp) SetAdj(name string, val int64) {
	if name == "" || len(name) > 16 {
		return
	}
	for i := range len(name) {
		c := name[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
			return
		}
	}
	val = min(max(val, -MaxAdjValue), MaxAdjValue)
	for i := range w.Adj {
		if w.Adj[i].A == name {
			w.Adj[i].B = val
			return
		}
	}
	if len(w.Adj) < MaxAdj {
		w.Adj = append(w.Adj, Pair[string, int64]{name, val})
	}
}

// ValignFromAnchor is the text box alignment a:bodyPr/@anchor gives.
func ValignFromAnchor(v string) VAlign {
	switch v {
	case "ctr", "dist":
		return VAlignCenter
	case "b":
		return VAlignBottom
	}
	return VAlignTop
}
