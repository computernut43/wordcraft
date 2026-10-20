package docx

// Freeform shapes (DrawingML custom geometry, a:custGeom) and the ink strokes WordCraft draws with
// them (Rust crates/docx/src/read/freeform.rs cust_geom, ink_tool, ink_order, line_alpha,
// bezier). Legacy VML paths (vml_path) aren't ported yet.

import (
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

const (
	// curveSteps is the straight pieces a Bézier curve is flattened into.
	curveSteps = 8
	// maxSegPoints is the points read from one path segment. A conformant a:lnTo/a:moveTo has
	// one, a:quadBezTo two and a:cubicBezTo three; the cap bounds the de Casteljau control
	// polygon so a segment stuffed with points can't make flattening quadratic in their count.
	maxSegPoints = 8
	// maxEMU is the largest path coordinate kept, EMUs.
	maxEMU = 1.0e12
)

// custGeom is the paths of the custom geometry in sppr (wps:spPr), mapped onto a w × h shape
// (points): each a:path in its own w × h space (EMUs of the shape's size when it gives none).
// Lines and Bézier curves (flattened) are kept; arcs are drawn straight to their end. False when
// there is no a:custGeom or it has no points.
func custGeom(sppr *El, w, h float32) (doc.Freeform, bool) {
	cg := sppr.Child("a:custGeom")
	if cg == nil {
		return doc.Freeform{}, false
	}
	list := cg.Child("a:pathLst")
	if list == nil {
		return doc.Freeform{}, false
	}
	paths := doc.Vec[doc.FreePath]{}
	all := list.Children("a:path")
	for _, p := range all[:min(len(all), doc.MaxFreeformPaths)] {
		// Subpaths count too: stop once the shape has as many paths as it may keep.
		if len(paths) >= doc.MaxFreeformPaths {
			break
		}
		dim := func(n string, shape float32) float64 {
			if v, ok := p.Attr(n); ok {
				if k, ok := intOf(v); ok && k > 0 {
					return float64(k)
				}
			}
			return float64(shape) * 12_700
		}
		pw, ph := dim("w", w), dim("h", h)
		scale := func(pt [2]float64) [2]doc.F32 {
			sx, sy := 1.0/12_700, 1.0/12_700
			if pw > 0 {
				sx = float64(w) / pw
			}
			if ph > 0 {
				sy = float64(h) / ph
			}
			return [2]doc.F32{doc.F32(float32(pt[0] * sx)), doc.F32(float32(pt[1] * sy))}
		}
		point := func(e *El) ([2]float64, bool) {
			c := func(n string) (float64, bool) {
				v, ok := e.Attr(n)
				if !ok {
					return 0, false
				}
				k, ok := intOf(v)
				if !ok {
					return 0, false
				}
				return rustcompat.Clamp(float64(k), -maxEMU, maxEMU), true
			}
			x, okx := c("x")
			if !okx {
				return [2]float64{}, false
			}
			y, oky := c("y")
			return [2]float64{x, y}, oky
		}
		type sub struct {
			pts    [][2]float64
			closed bool
		}
		// Each subpath (from an a:moveTo) becomes a path of its own.
		var cur [][2]float64
		var done []sub
		fillNone := attrIs(p, "fill", "none")
		for _, seg := range p.Els() {
			if len(cur) >= doc.MaxFreeformPoints || len(done) >= doc.MaxFreeformPaths {
				break
			}
			var pts [][2]float64
			for _, e := range seg.Children("a:pt") {
				if len(pts) == maxSegPoints {
					break
				}
				if v, ok := point(e); ok {
					pts = append(pts, v)
				}
			}
			switch seg.Local() {
			case "moveTo":
				if len(cur) > 0 {
					done = append(done, sub{cur, false})
					cur = nil
				}
				if len(pts) > 0 {
					cur = append(cur, pts[0])
				}
			case "lnTo":
				if len(pts) > 0 {
					cur = append(cur, pts[0])
				}
			case "cubicBezTo", "quadBezTo":
				if len(cur) == 0 {
					if len(pts) > 0 {
						cur = append(cur, pts[len(pts)-1])
					}
					continue
				}
				ctrl := append([][2]float64{cur[len(cur)-1]}, pts...)
				for i := 1; i <= curveSteps; i++ {
					cur = append(cur, bezier(ctrl, float64(i)/curveSteps))
				}
			case "close":
				if len(cur) > 0 {
					done = append(done, sub{cur, true})
					cur = nil
				}
			}
		}
		if len(cur) > 0 {
			// A path drawn back to where it started is closed, though it has no a:close (Word's
			// cover pages), so it's filled.
			back := len(cur) > 2 && cur[0] == cur[len(cur)-1]
			done = append(done, sub{cur, back})
		}
		for _, d := range done[:min(len(done), max(doc.MaxFreeformPaths-len(paths), 0))] {
			pts := doc.Vec[[2]doc.F32]{}
			for _, pt := range d.pts[:min(len(d.pts), doc.MaxFreeformPoints)] {
				pts = append(pts, scale(pt))
			}
			paths = append(paths, doc.FreePath{Pts: pts, Closed: d.closed && !fillNone})
		}
	}
	f := doc.Freeform{W: doc.F32(w), H: doc.F32(h), Paths: paths, Alpha: 1}.Sanitized()
	return f, len(f.Paths) > 0
}

// inkTool is the pen of an ink stroke written by WordCraft, from its drawing's name ("Ink Pen 3",
// "Ink Highlighter 4").
func inkTool(name string) (doc.InkTool, bool) {
	words := strings.FieldsFunc(name, rustcompat.IsWhitespace)
	if len(words) < 2 || words[0] != "Ink" {
		return 0, false
	}
	return doc.ParseInkTool(words[1])
}

// inkOrder is the drawing order of an ink stroke written by WordCraft, from its drawing's name
// ("Ink Pen 3 #12": the twelfth stroke drawn), for Ink Replay.
func inkOrder(name string) (uint32, bool) {
	words := strings.FieldsFunc(name, rustcompat.IsWhitespace)
	for _, w := range words[min(len(words), 3):] {
		if n, ok := strings.CutPrefix(w, "#"); ok {
			if v, ok := rustcompat.ParseUint(n, 32); ok {
				return uint32(v), true
			}
		}
	}
	return 0, false
}

// lineAlpha is the outline opacity of ln (a:ln): its colour's a:alpha, 0–1.
func lineAlpha(ln *El) float32 {
	if ln == nil {
		return 1
	}
	f := ln.Child("a:solidFill")
	if f == nil {
		return 1
	}
	els := f.Els()
	if len(els) == 0 {
		return 1
	}
	a := els[0].Child("a:alpha")
	if a == nil {
		return 1
	}
	v, ok := a.Attr("val")
	if !ok {
		return 1
	}
	k, ok := intOf(v)
	if !ok {
		return 1
	}
	return float32(min(max(k, 0), 100_000)) / 100_000
}

// bezier is the point at t on the Bézier curve with control points c (de Casteljau).
func bezier(c [][2]float64, t float64) [2]float64 {
	p := append([][2]float64(nil), c...)
	for len(p) > 1 {
		next := make([][2]float64, 0, len(p)-1)
		for i := 0; i+1 < len(p); i++ {
			a, b := p[i], p[i+1]
			// Each product is rounded on its own (float64(…)): Go may otherwise fuse a multiply
			// and an add, which Rust never does.
			next = append(next, [2]float64{a[0] + float64((b[0]-a[0])*t), a[1] + float64((b[1]-a[1])*t)})
		}
		p = next
	}
	if len(p) == 0 {
		return [2]float64{}
	}
	return p[0]
}
