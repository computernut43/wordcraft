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

// paint is the colour of a fill or line element: a solid colour, the first stop of a gradient,
// or the foreground of a pattern; false for no fill and picture fills.
func paint(e *El, theme []doc.RGB) (doc.RGB, bool) {
	switch e.Name {
	case "a:gradFill":
		if l := e.Child("a:gsLst"); l != nil {
			if g := l.Child("a:gs"); g != nil {
				return drawingColor(g, theme)
			}
		}
		return doc.RGB{}, false
	case "a:pattFill":
		if c := e.Child("a:fgClr"); c != nil {
			return drawingColor(c, theme)
		}
		return doc.RGB{}, false
	case "a:noFill", "a:blipFill":
		return doc.RGB{}, false
	}
	return drawingColor(e, theme)
}

func isFillEl(e *El) bool {
	return e.Name == "a:solidFill" || e.Name == "a:gradFill" || e.Name == "a:pattFill"
}

// fillColour is the fill of a …Pr or a:ln element (its first solid, gradient or pattern fill).
func fillColour(e *El, theme []doc.RGB) (doc.RGB, bool) {
	for _, f := range e.Els() {
		if isFillEl(f) {
			return paint(f, theme)
		}
	}
	return doc.RGB{}, false
}

func isColorEl(e *El) bool {
	switch e.Name {
	case "a:srgbClr", "a:schemeClr", "a:sysClr", "a:prstClr", "a:scrgbClr", "a:hslClr":
		return true
	}
	return false
}

// drawingColor is a colour element (a:srgbClr, a:schemeClr, a:sysClr…), or the one inside e,
// with its lumMod / lumOff / tint / shade applied in document order. a:scrgbClr needs glibc's pow
// to match Rust (not ported): callers refuse files that use it, and here it gives no colour.
func drawingColor(e *El, theme []doc.RGB) (doc.RGB, bool) {
	c := e
	if !isColorEl(e) {
		c = nil
		for _, k := range e.Els() {
			if isColorEl(k) {
				c = k
				break
			}
		}
		if c == nil {
			return doc.RGB{}, false
		}
	}
	var base doc.RGB
	switch c.Name {
	case "a:srgbClr":
		v, ok := c.Attr("val")
		if !ok {
			return doc.RGB{}, false
		}
		if base, ok = doc.ParseRGB(v); !ok {
			return doc.RGB{}, false
		}
	case "a:schemeClr":
		v, ok := c.Attr("val")
		if !ok {
			return doc.RGB{}, false
		}
		i, ok := schemeSlot(v)
		if !ok || i >= len(theme) {
			return doc.RGB{}, false
		}
		base = theme[i]
	case "a:sysClr":
		if rgb, ok := parseRGBAttr(c, "lastClr"); ok {
			base = rgb
		} else {
			v, ok := c.Attr("val")
			if !ok {
				return doc.RGB{}, false
			}
			if base, ok = sysColor(v); !ok {
				return doc.RGB{}, false
			}
		}
	case "a:prstClr":
		v, ok := c.Attr("val")
		if !ok {
			return doc.RGB{}, false
		}
		if base, ok = presetColor(v); !ok {
			return doc.RGB{}, false
		}
	case "a:hslClr":
		v := func(n string) float64 {
			if s, ok := c.Attr(n); ok {
				if k, ok := intOf(s); ok {
					return float64(k)
				}
			}
			return 0
		}
		hue := math.Mod(v("hue")/60_000, 360)
		if hue < 0 {
			hue += 360
		}
		base = fromHSL(hue/360, min(max(v("sat")/100_000, 0), 1), min(max(v("lum")/100_000, 0), 1))
	default:
		return doc.RGB{}, false
	}
	val := func(m *El) float64 {
		if s, ok := m.Attr("val"); ok {
			if k, ok := intOf(s); ok {
				return min(max(float64(k)/100_000, -10), 10)
			}
		}
		return 0
	}
	rgb := base
	for _, m := range c.Els() {
		switch m.Name {
		case "a:lumMod":
			v := val(m)
			rgb = adjustLum(rgb, func(l float64) float64 { return l * v })
		case "a:lumOff":
			v := val(m)
			rgb = adjustLum(rgb, func(l float64) float64 { return l + v })
		case "a:tint":
			rgb = mix(rgb, 255, val(m))
		case "a:shade":
			rgb = mix(rgb, 0, val(m))
		}
	}
	return rgb, true
}

// mix moves c towards to (tint: 255, shade: 0); v is the share of the original colour kept.
func mix(c doc.RGB, to, v float64) doc.RGB {
	f := func(x uint8) uint8 {
		return uint8(min(max(math.Round(float64(float64(x)*v)+float64(to*(1-v))), 0), 255))
	}
	return doc.RGB{R: f(c.R), G: f(c.G), B: f(c.B)}
}

// presetColor is a preset colour (a:prstClr/@val, ECMA-376 Part 1 §20.1.10.48): the common ones.
func presetColor(name string) (doc.RGB, bool) {
	switch toASCIILower(name) {
	case "black":
		return doc.RGB{R: 0, G: 0, B: 0}, true
	case "white":
		return doc.RGB{R: 0xFF, G: 0xFF, B: 0xFF}, true
	case "red":
		return doc.RGB{R: 0xFF}, true
	case "green":
		return doc.RGB{G: 0x80}, true
	case "lime":
		return doc.RGB{G: 0xFF}, true
	case "blue":
		return doc.RGB{B: 0xFF}, true
	case "yellow":
		return doc.RGB{R: 0xFF, G: 0xFF}, true
	case "cyan", "aqua":
		return doc.RGB{G: 0xFF, B: 0xFF}, true
	case "magenta", "fuchsia":
		return doc.RGB{R: 0xFF, B: 0xFF}, true
	case "gray", "grey":
		return doc.RGB{R: 0x80, G: 0x80, B: 0x80}, true
	case "silver":
		return doc.RGB{R: 0xC0, G: 0xC0, B: 0xC0}, true
	case "ltgray", "ltgrey", "lightgray", "lightgrey":
		return doc.RGB{R: 0xD3, G: 0xD3, B: 0xD3}, true
	case "dkgray", "dkgrey", "darkgray", "darkgrey":
		return doc.RGB{R: 0xA9, G: 0xA9, B: 0xA9}, true
	case "maroon":
		return doc.RGB{R: 0x80}, true
	case "navy":
		return doc.RGB{B: 0x80}, true
	case "olive":
		return doc.RGB{R: 0x80, G: 0x80}, true
	case "purple":
		return doc.RGB{R: 0x80, B: 0x80}, true
	case "teal":
		return doc.RGB{G: 0x80, B: 0x80}, true
	case "orange":
		return doc.RGB{R: 0xFF, G: 0xA5}, true
	case "brown":
		return doc.RGB{R: 0xA5, G: 0x2A, B: 0x2A}, true
	case "pink":
		return doc.RGB{R: 0xFF, G: 0xC0, B: 0xCB}, true
	case "gold":
		return doc.RGB{R: 0xFF, G: 0xD7}, true
	case "darkblue", "dkblue":
		return doc.RGB{B: 0x8B}, true
	case "darkred", "dkred":
		return doc.RGB{R: 0x8B}, true
	case "darkgreen", "dkgreen":
		return doc.RGB{G: 0x64}, true
	case "lightblue", "ltblue":
		return doc.RGB{R: 0xAD, G: 0xD8, B: 0xE6}, true
	case "coral":
		return doc.RGB{R: 0xFF, G: 0x7F, B: 0x50}, true
	case "salmon":
		return doc.RGB{R: 0xFA, G: 0x80, B: 0x72}, true
	case "tomato":
		return doc.RGB{R: 0xFF, G: 0x63, B: 0x47}, true
	case "violet":
		return doc.RGB{R: 0xEE, G: 0x82, B: 0xEE}, true
	case "indigo":
		return doc.RGB{R: 0x4B, B: 0x82}, true
	case "beige":
		return doc.RGB{R: 0xF5, G: 0xF5, B: 0xDC}, true
	case "ivory":
		return doc.RGB{R: 0xFF, G: 0xFF, B: 0xF0}, true
	}
	return doc.RGB{}, false
}

// sysColor is a system colour without its lastClr: the usual light-theme values.
func sysColor(name string) (doc.RGB, bool) {
	switch name {
	case "windowText", "btnText", "menuText", "captionText", "infoText":
		return doc.RGB{}, true
	case "window", "menu", "highlightText":
		return doc.RGB{R: 0xFF, G: 0xFF, B: 0xFF}, true
	case "btnFace", "3dLight":
		return doc.RGB{R: 0xF0, G: 0xF0, B: 0xF0}, true
	case "grayText", "btnShadow":
		return doc.RGB{R: 0xA0, G: 0xA0, B: 0xA0}, true
	case "highlight":
		return doc.RGB{R: 0x00, G: 0x78, B: 0xD7}, true
	}
	return doc.RGB{}, false
}

// schemeSlot is the index into the theme colours (dk1, lt1, dk2, lt2, accent1..6, hlink,
// folHlink).
func schemeSlot(name string) (int, bool) {
	switch name {
	case "dk1", "tx1":
		return 0, true
	case "lt1", "bg1":
		return 1, true
	case "dk2", "tx2":
		return 2, true
	case "lt2", "bg2":
		return 3, true
	case "accent1":
		return 4, true
	case "accent2":
		return 5, true
	case "accent3":
		return 6, true
	case "accent4":
		return 7, true
	case "accent5":
		return 8, true
	case "accent6":
		return 9, true
	case "hlink":
		return 10, true
	case "folHlink":
		return 11, true
	}
	return 0, false
}
