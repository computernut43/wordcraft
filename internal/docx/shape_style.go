package docx

// How a Word shape (wps:wsp) looks: its preset geometry, and the fill and outline it gets from its
// own properties (wps:spPr) or, failing those, from the theme through its style references
// (wps:style: a:fillRef, a:lnRef, ECMA-376 Part 1 §20.1.4.2) (Rust
// crates/docx/src/read/shape_style.rs; the VML parts aren't ported yet).

import (
	"math"
	"slices"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

// themeLineWidths are the outline widths of the theme's three line styles, points.
var themeLineWidths = [3]float32{0.5, 1.0, 1.5}

// presetKind is the shape kind drawn for DrawingML preset geometry prst (ECMA-376 Part 1
// §20.1.10.56): presets without a shape of their own are drawn as the nearest one.
func presetKind(prst string) doc.ShapeKind {
	switch prst {
	case "roundRect", "round1Rect", "round2SameRect", "round2DiagRect", "snipRoundRect", "wedgeRoundRectCallout",
		"flowChartAlternateProcess", "flowChartTerminator", "plaque":
		return doc.ShapeRoundedRectangle
	case "ellipse", "wedgeEllipseCallout", "cloudCallout", "cloud", "flowChartConnector", "donut", "flowChartOnlineStorage":
		return doc.ShapeEllipse
	case "flowChartOr":
		return doc.ShapeFlowchartOr
	case "triangle", "flowChartExtract", "flowChartMerge":
		return doc.ShapeTriangle
	case "rtTriangle":
		return doc.ShapeRightTriangle
	case "diamond", "flowChartDecision":
		return doc.ShapeDiamond
	case "line", "lineInv":
		return doc.ShapeLine
	case "straightConnector1":
		return doc.ShapeStraightConnector
	case "bentConnector2", "bentConnector3", "bentConnector4", "bentConnector5":
		return doc.ShapeElbowConnector
	case "curvedConnector2", "curvedConnector3", "curvedConnector4", "curvedConnector5":
		return doc.ShapeCurvedConnector
	case "rightArrow", "leftArrow", "upArrow", "downArrow", "leftRightArrow", "upDownArrow", "notchedRightArrow", "stripedRightArrow":
		return doc.ShapeArrow
	case "star4", "star5", "star6", "star7", "star8", "star10", "star12", "star16", "star24", "star32":
		return doc.ShapeStar
	case "heart":
		return doc.ShapeHeart
	}
	return doc.ShapeRectangle
}

// shapePaintOf is a shape's fill and outline.
type shapePaintOf struct {
	fill, stroke *doc.RGB
	strokeWidth  float32
}

func rgbPtr(c doc.RGB, ok bool) *doc.RGB {
	if ok {
		return &c
	}
	return nil
}

// shapePaint is the paint of a shape from its properties sppr, falling back on its style
// (theme colours theme) for what they leave out: an explicit a:noFill is no fill or outline, an
// explicit colour wins, and a style reference with index 0 is none.
func shapePaint(sppr, style *El, theme []doc.RGB) shapePaintOf {
	styleRef := func(name string) (int64, *El) {
		if style == nil {
			return 0, nil
		}
		r := style.Child(name)
		if r == nil {
			return 0, nil
		}
		idx := int64(0)
		if v, ok := r.Attr("idx"); ok {
			if n, ok := intOf(v); ok {
				idx = n
			}
		}
		return idx, r
	}
	hasFill := func(e *El) bool { return slices.ContainsFunc(e.Els(), isFillEl) }
	var p shapePaintOf
	switch {
	case sppr != nil && (sppr.Child("a:noFill") != nil || sppr.Child("a:blipFill") != nil):
	case sppr != nil && hasFill(sppr):
		p.fill = rgbPtr(fillColour(sppr, theme))
	default:
		if idx, r := styleRef("a:fillRef"); r != nil && idx > 0 {
			p.fill = rgbPtr(drawingColor(r, theme))
		}
	}
	var ln *El
	if sppr != nil {
		ln = sppr.Child("a:ln")
	}
	lineIdx, lineRef := styleRef("a:lnRef")
	if lineIdx <= 0 {
		lineRef = nil
	}
	switch {
	case ln != nil && ln.Child("a:noFill") != nil:
	case ln != nil && hasFill(ln):
		p.stroke = rgbPtr(fillColour(ln, theme))
	case lineRef != nil:
		p.stroke = rgbPtr(drawingColor(lineRef, theme))
	}
	width, ok := float32(0), false
	if ln != nil {
		width, ok = attrMeasure(ln, "w", 12_700)
	}
	if !ok && lineRef != nil {
		width, ok = themeLineWidths[min(max(lineIdx, 1), 3)-1], true
	}
	if !ok {
		// Word's default outline when the file names none (no outline: no width).
		if p.stroke != nil {
			width = 0.75
		}
	}
	p.strokeWidth = fclamp(width, 0, 100)
	return p
}

// maxDashStops is Rust line_dash::MAX_DASH_STOPS.
const maxDashStops = 64

// lineDash is an outline's dash: explicit DrawingML dash stops override the solid default.
func lineDash(ln *El) doc.LineDash {
	if ln == nil {
		return doc.LineDash{}
	}
	if custom := ln.Child("a:custDash"); custom != nil {
		var stops doc.Vec[doc.Pair[uint32, uint32]]
		for i, e := range custom.Children("a:ds") {
			if i >= maxDashStops {
				break
			}
			d, ok1 := e.Attr("d")
			sp, ok2 := e.Attr("sp")
			if !ok1 || !ok2 {
				continue
			}
			dv, ok1 := dashRatio(d)
			sv, ok2 := dashRatio(sp)
			if ok1 && ok2 {
				stops = append(stops, doc.Pair[uint32, uint32]{A: dv, B: sv})
			}
		}
		if len(stops) == 0 {
			return doc.LineDash{}
		}
		return doc.LineDash{Kind: doc.DashCustom, Custom: stops}
	}
	if p := ln.Child("a:prstDash"); p != nil {
		if v, ok := p.Attr("val"); ok {
			return doc.LineDashFromOOXML(v)
		}
	}
	return doc.LineDash{}
}

// dashRatio reads a dash stop; strict OOXML may spell percentages with a percent sign instead of
// integer units.
func dashRatio(value string) (uint32, bool) {
	v := rustcompat.Trim(value)
	percent, cut := cutSuffix(v, "%")
	if !cut {
		n, ok := rustcompat.ParseUint(v, 32)
		return uint32(n), ok
	}
	f, ok := rustcompat.ParseF64(percent)
	if !ok {
		return 0, false
	}
	units := f * 1000
	if math.IsInf(units, 0) || math.IsNaN(units) || units < 0 || units > math.MaxUint32 {
		return 0, false
	}
	return uint32(math.Round(units)), true
}

func cutSuffix(s, suffix string) (string, bool) {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}
