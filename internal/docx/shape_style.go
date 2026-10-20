package docx

// How a Word shape (wps:wsp) looks: its preset geometry, and the fill and outline it gets from its
// own properties (wps:spPr) or, failing those, from the theme through its style references
// (wps:style: a:fillRef, a:lnRef, ECMA-376 Part 1 §20.1.4.2) (Rust
// crates/docx/src/read/shape_style.rs), and the legacy VML presets and colours.

import (
	"math"
	"slices"
	"strings"

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

// vmlPresetKind is the shape kind drawn for a legacy VML preset shape type spt (o:spt, the MSOSPT
// numbers of [MS-ODRAW] §2.4.24): that of the DrawingML preset of the same shape, through
// presetKind, so a preset WordCraft has no shape for is drawn as the nearest one; false for shapes
// with no preset geometry (freeforms, WordArt text, picture frames) and numbers MSOSPT doesn't
// define.
func vmlPresetKind(spt uint32) (doc.ShapeKind, bool) {
	var prst string
	switch {
	// A text box's shapetype: a text box even without text, as DrawingML's txBox.
	case spt == 202:
		return doc.ShapeTextBox, true
	// Not primitive (freeform), WordArt text shapes, picture frame, host control.
	case spt == 0, spt >= 24 && spt <= 31, spt == 75, spt >= 136 && spt <= 175, spt == 201:
		return 0, false
	default:
		p, ok := vmlPresets[spt]
		switch {
		case ok:
			prst = p
		// Every other preset is drawn as its box, as presetKind does.
		case spt >= 1 && spt <= 201:
			prst = "rect"
		default:
			return 0, false
		}
	}
	return presetKind(prst), true
}

// vmlPresets are the MSOSPT numbers with a DrawingML preset other than a plain box.
var vmlPresets = map[uint32]string{
	// Line and straight connector: corner to corner of the shape's box.
	20: "line", 32: "line",
	2: "roundRect", 3: "ellipse", 4: "diamond", 5: "triangle", 6: "rtTriangle", 12: "star5", 13: "rightArrow",
	21: "plaque", 23: "donut", 33: "bentConnector2", 34: "bentConnector3", 35: "bentConnector4", 36: "bentConnector5",
	37: "curvedConnector2", 38: "curvedConnector3", 39: "curvedConnector4", 40: "curvedConnector5", 58: "star8",
	59: "star16", 60: "star32", 62: "wedgeRoundRectCallout", 63: "wedgeEllipseCallout", 66: "leftArrow",
	67: "downArrow", 68: "upArrow", 69: "leftRightArrow", 70: "upDownArrow", 74: "heart", 92: "star24",
	93: "stripedRightArrow", 94: "notchedRightArrow", 106: "cloudCallout", 110: "flowChartDecision",
	116: "flowChartTerminator", 120: "flowChartConnector", 127: "flowChartExtract", 128: "flowChartMerge",
	130: "flowChartOnlineStorage", 176: "flowChartAlternateProcess", 187: "star4",
}

// vmlColor reads a VML colour (strokecolor, fillcolor): #rrggbb, rrggbb, #rgb or rgb (Word
// follows it with the theme slot, as in "#4472c4 [3204]"), an rgb(...) function, or a colour
// name.
func vmlColor(v string) (doc.RGB, bool) {
	s := rustcompat.Trim(v)
	inner, ok := strings.CutPrefix(s, "rgb(")
	if !ok {
		inner, ok = strings.CutPrefix(s, "rgba(")
	}
	if ok {
		parts := strings.Split(strings.TrimRight(inner, ")"), ",")
		var nums []uint8
		for _, x := range parts[:min(len(parts), 3)] {
			x = rustcompat.Trim(x)
			if p, ok := strings.CutSuffix(x, "%"); ok {
				if f, ok := rustcompat.ParseF32(rustcompat.Trim(p)); ok {
					nums = append(nums, satU8(roundF32(float32(rustcompat.Clamp(f, 0, 100)*2.55))))
				}
			} else if f, ok := rustcompat.ParseF32(x); ok {
				nums = append(nums, satU8(roundF32(rustcompat.Clamp(f, 0, 255))))
			}
		}
		if len(nums) == 3 {
			return doc.RGB{R: nums[0], G: nums[1], B: nums[2]}, true
		}
	}
	toks := strings.FieldsFunc(s, rustcompat.IsWhitespace)
	if len(toks) == 0 {
		return doc.RGB{}, false
	}
	tok := toks[0]
	hex := strings.TrimPrefix(tok, "#")
	isHex := func(s string) bool {
		for i := range len(s) {
			if !isASCIIHex(s[i]) {
				return false
			}
		}
		return true
	}
	if len(hex) == 3 && isHex(hex) {
		return doc.ParseRGB(string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]}))
	}
	if len(hex) == 6 && isHex(hex) {
		return doc.ParseRGB(hex)
	}
	return presetColor(tok)
}

func isASCIIHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// roundF32 is Rust's f32::round (half away from zero).
func roundF32(f float32) float32 { return float32(math.Round(float64(f))) }

// satU8 is Rust's `f as u8`: saturating, NaN to 0.
func satU8(f float32) uint8 {
	switch {
	case f != f || f <= 0:
		return 0
	case f >= 255:
		return 255
	}
	return uint8(f)
}
