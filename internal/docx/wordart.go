package docx

// WordArt text effects in w:rPr ([MS-DOCX] §2.6.1: w14:textFill, w14:textOutline, w14:shadow,
// w14:glow, w14:reflection; Rust crates/docx/src/read/wordart.rs). Lengths are EMUs, angles
// 60000ths of a degree, percentages 1000ths of a percent.

import (
	"math"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

// readTextEffect reads one w14: run property into fx; false when e isn't one.
func readTextEffect(e *El, theme []doc.RGB, fx *doc.TextEffects) bool {
	switch e.Name {
	case "w14:textFill":
		fx.Fill = nil
		if f, ok := w14Fill(e, theme); ok {
			fx.Fill = &f
		}
	case "w14:textOutline":
		var color *doc.RGB
		transparency := float32(0)
		var f *El
		for _, c := range e.Els() {
			if c.Name == "w14:solidFill" || c.Name == "w14:gradFill" || c.Name == "w14:noFill" {
				f = c
				break
			}
		}
		switch {
		case f == nil:
			color = ptr(doc.Black)
		case f.Name == "w14:noFill":
		default:
			if t, ok := w14Fill(f, theme); ok {
				switch t.Kind {
				case doc.FillSolid:
					color, transparency = ptr(t.Color), float32(t.Transparency)
				case doc.FillGradient:
					if len(t.Stops) > 0 {
						color = ptr(t.Stops[len(t.Stops)/2].Color)
					}
				}
			}
		}
		fx.Outline = &doc.TextOutline{Color: color, Width: doc.F32(w14EMU(e, "w14:w")), Transparency: doc.F32(transparency)}
	case "w14:shadow":
		c, transparency, ok := w14Color(e, theme)
		if !ok {
			c, transparency = doc.Black, 60
		}
		s := doc.Shadow{Color: c, Transparency: doc.F32(transparency), Blur: doc.F32(w14EMU(e, "w14:blurRad")),
			Distance: doc.F32(w14EMU(e, "w14:dist")), Angle: doc.F32(w14Angle(e, "w14:dir"))}.Sanitized()
		fx.Shadow = &s
	case "w14:glow":
		c, transparency, ok := w14Color(e, theme)
		if !ok {
			var d doc.Glow
			d.SetDefaults()
			c, transparency = d.Color, 60
		}
		g := doc.Glow{Color: c, Size: doc.F32(w14EMU(e, "w14:rad")), Transparency: doc.F32(transparency)}.Sanitized()
		fx.Glow = nil
		if g.Size > 0 {
			fx.Glow = &g
		}
	case "w14:reflection":
		// Start opacity and the end position of the fade (both 1000ths of a percent).
		pct := func(n string, d float32) float32 {
			if v, ok := e.Attr(n); ok {
				if k, ok := intOf(v); ok {
					return float32(min(max(k, 0), 100_000)) / 1000
				}
			}
			return d
		}
		fx.Reflection = &doc.Reflection{Transparency: doc.F32(100 - pct("w14:stA", 50)), Size: doc.F32(pct("w14:endPos", 50)),
			Distance: doc.F32(w14EMU(e, "w14:dist")), Blur: doc.F32(w14EMU(e, "w14:blurRad"))}
	default:
		return false
	}
	return true
}

func w14EMU(e *El, n string) float32 {
	v := float32(0)
	if s, ok := e.Attr(n); ok {
		if m, ok := measure(s, 12_700); ok && !math.IsInf(float64(m), 0) && m == m {
			v = m
		}
	}
	return fclamp(v, 0, 2000)
}

func w14Angle(e *El, n string) float32 {
	if s, ok := e.Attr(n); ok {
		if k, ok := intOf(s); ok {
			return float32(rustcompat.RemEuclid(k, 21_600_000)) / 60_000
		}
	}
	return 0
}

func isW14Fill(e *El) bool {
	return e.Name == "w14:noFill" || e.Name == "w14:solidFill" || e.Name == "w14:gradFill"
}

// w14Fill reads a fill element (w14:noFill, w14:solidFill, w14:gradFill), or the one inside e.
func w14Fill(e *El, theme []doc.RGB) (doc.TextFill, bool) {
	f := e
	if !isW14Fill(e) {
		f = nil
		for _, c := range e.Els() {
			if isW14Fill(c) {
				f = c
				break
			}
		}
		if f == nil {
			return doc.TextFill{}, false
		}
	}
	switch f.Name {
	case "w14:noFill":
		return doc.TextFill{Kind: doc.FillNone}, true
	case "w14:solidFill":
		c, t, ok := w14Color(f, theme)
		return doc.TextFill{Kind: doc.FillSolid, Color: c, Transparency: doc.F32(t)}, ok
	}
	stops := doc.Vec[doc.GradStop]{}
	if l := f.Child("w14:gsLst"); l != nil {
		gs := l.Children("w14:gs")
		for _, g := range gs[:min(len(gs), doc.MaxStops)] {
			c, t, ok := w14Color(g, theme)
			if !ok {
				continue
			}
			pos := float32(0)
			if v, ok := g.Attr("w14:pos"); ok {
				if k, ok := intOf(v); ok {
					pos = float32(min(max(k, 0), 100_000)) / 1000
				}
			}
			stops = append(stops, doc.GradStop{Pos: doc.F32(pos), Color: c, Transparency: doc.F32(t)})
		}
	}
	angle := float32(90)
	if l := f.Child("w14:lin"); l != nil {
		angle = w14Angle(l, "w14:ang")
	}
	return doc.TextFill{Kind: doc.FillGradient, Stops: stops, Angle: doc.F32(angle)}.Sanitized()
}

// w14Color is the colour element inside e (w14:srgbClr, w14:schemeClr) with its modifiers, and
// its transparency (percent, from w14:alpha).
func w14Color(e *El, theme []doc.RGB) (doc.RGB, float32, bool) {
	var c *El
	for _, k := range e.Els() {
		if k.Name == "w14:srgbClr" || k.Name == "w14:schemeClr" {
			c = k
			break
		}
	}
	if c == nil {
		return doc.RGB{}, 0, false
	}
	val, ok := c.Attr("w14:val")
	if !ok {
		return doc.RGB{}, 0, false
	}
	var rgb doc.RGB
	if c.Name == "w14:srgbClr" {
		if rgb, ok = doc.ParseRGB(val); !ok {
			return doc.RGB{}, 0, false
		}
	} else {
		i, ok := schemeSlot(val)
		if !ok {
			return doc.RGB{}, 0, false
		}
		rgb = themeSlotOr(theme, i)
	}
	transparency := float32(0)
	for _, m := range c.Els() {
		v := 0.0
		if s, ok := m.Attr("w14:val"); ok {
			if k, ok := intOf(s); ok {
				v = rustcompat.Clamp(float64(k)/100_000, -10, 10)
			}
		}
		switch m.Name {
		case "w14:lumMod":
			rgb = adjustLum(rgb, func(l float64) float64 { return l * v })
		case "w14:lumOff":
			rgb = adjustLum(rgb, func(l float64) float64 { return l + v })
		case "w14:alpha":
			// Unlike DrawingML's a:alpha, w14:alpha is the transparency.
			transparency = float32(rustcompat.Clamp(v, 0, 1) * 100)
		}
	}
	return rgb, transparency, true
}

// themeSlotOr is theme slot i, falling back on the default theme's (then black).
func themeSlotOr(theme []doc.RGB, i int) doc.RGB {
	if i >= 0 && i < len(theme) {
		return theme[i]
	}
	if i >= 0 && i < len(doc.ThemeColors) {
		return doc.ThemeColors[i]
	}
	return doc.Black
}
