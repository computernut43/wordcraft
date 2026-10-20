package docx

// Colour arithmetic for theme tints and shades (Rust crates/docx/src/read/drawing_color.rs; only
// what the ported readers use so far).

import (
	"math"

	"github.com/computernut43/wordcraft/internal/doc"
)

// adjustLum is c with its HSL luminance changed by f.
func adjustLum(c doc.RGB, f func(float64) float64) doc.RGB {
	h, s, l := toHSL(c)
	return fromHSL(h, s, min(max(f(l), 0), 1))
}

func toHSL(c doc.RGB) (h, s, l float64) {
	r, g, b := float64(c.R)/255.0, float64(c.G)/255.0, float64(c.B)/255.0
	mx, mn := max(r, g, b), min(r, g, b)
	l = (mx + mn) / 2.0
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2.0 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch {
	case mx == r:
		h = (g - b) / d
		if g < b {
			h += 6.0
		}
	case mx == g:
		h = (b-r)/d + 2.0
	default:
		h = (r-g)/d + 4.0
	}
	return h / 6.0, s, l
}

func fromHSL(h, s, l float64) doc.RGB {
	byteOf := func(v float64) uint8 { return uint8(min(max(math.Round(v*255.0), 0), 255)) }
	if s == 0 {
		return doc.RGB{R: byteOf(l), G: byteOf(l), B: byteOf(l)}
	}
	// Products are rounded on their own (float64(…)): Rust never fuses a multiply and an add.
	var q float64
	if l < 0.5 {
		q = l * (1.0 + s)
	} else {
		q = l + s - float64(l*s)
	}
	p := 2.0*l - q
	channel := func(t float64) uint8 {
		t = math.Mod(t, 1.0)
		if t < 0 {
			t += 1.0
		}
		var v float64
		switch {
		case t < 1.0/6.0:
			v = p + float64((q-p)*6.0*t)
		case t < 0.5:
			v = q
		case t < 2.0/3.0:
			v = p + float64((q-p)*(2.0/3.0-t)*6.0)
		default:
			v = p
		}
		return byteOf(v)
	}
	return doc.RGB{R: channel(h + 1.0/3.0), G: channel(h), B: channel(h - 1.0/3.0)}
}
