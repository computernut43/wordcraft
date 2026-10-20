package doc

import (
	"math"
	"slices"

	"github.com/computernut43/craft-libs/serdejson"
)

// Drawings' placement and shape properties: floating placement and wrapping (para.rs), wrap
// polygons (wrap.rs), shape effects (effects.rs), connectors and the other shape extras
// (connector.rs), dashes (line_dash.rs), text warps (wordart.rs) and freeforms (freeform.rs).

// Wrap is how a floating object is placed.
type Wrap uint8

// Wraps.
const (
	// WrapInline is in line with text (not floating).
	WrapInline Wrap = iota
	WrapSquare
	WrapTight
	WrapThrough
	WrapTopAndBottom
	WrapBehindText
	WrapInFrontOfText
)

var wrapNames = []string{"inline", "square", "tight", "through", "topAndBottom", "behindText", "inFrontOfText"}

func (w *Wrap) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, wrapNames, w) }
func (w Wrap) MarshalJSON() ([]byte, error)           { return writeEnum(wrapNames, w) }

// WrapText is which sides of a Square, Tight or Through object text may flow on (ST_WrapText).
type WrapText uint8

// Wrap sides.
const (
	// WrapBothSides: both sides of the object.
	WrapBothSides WrapText = iota
	// WrapLeft: only the left side; the band to the object's right stays empty.
	WrapLeft
	// WrapRight: only the right side.
	WrapRight
	// WrapLargest: only whichever side has more room, decided per line.
	WrapLargest
)

var wrapTextNames = []string{"bothSides", "left", "right", "largest"}

func (w *WrapText) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, wrapTextNames, w) }
func (w WrapText) MarshalJSON() ([]byte, error)           { return writeEnum(wrapTextNames, w) }

// OOXML is the ST_WrapText value (the JSON name).
func (w WrapText) OOXML() string { return enumName(wrapTextNames, w) }

// WrapTextFromOOXML reads an ST_WrapText value.
func WrapTextFromOOXML(v string) (WrapText, bool) {
	i := slices.Index(wrapTextNames, v)
	return WrapText(max(i, 0)), i >= 0
}

// Float is a drawing's floating placement (ignored when Wrap is WrapInline).
type Float struct {
	Wrap Wrap `json:"wrap"`
	// WrapText is the sides text wraps on (Square, Tight and Through).
	WrapText WrapText `json:"wrapText,omitempty"`
	HRel     Anchor   `json:"hRel"`
	VRel     Anchor   `json:"vRel"`
	// X and Y are offsets, points (used when the matching alignment is nil).
	X F32 `json:"x"`
	Y F32 `json:"y"`
	// HAlign and VAlign are alignments within the anchor area; they take precedence over the
	// offsets.
	HAlign *FloatAlign `json:"hAlign,omitempty"`
	VAlign *FloatAlign `json:"vAlign,omitempty"`
	// Dist is the distance from surrounding text at the left and right, DistTop and DistBottom
	// above and below (points).
	Dist       F32 `json:"dist"`
	DistTop    F32 `json:"distTop"`
	DistBottom F32 `json:"distBottom"`
	// Effect is the room around the object for effects such as shadows (left, top, right,
	// bottom, points), inline or floating: lines and surrounding text keep clear of it.
	Effect [4]F32 `json:"effect"`
	// Rot is the rotation clockwise about the object's centre, degrees (a:xfrm/@rot). Offsets and
	// the size stay those of the unrotated frame, as in Word.
	Rot F32 `json:"rot,omitzero"`
	// FlipH and FlipV mirror it left to right / top to bottom (a:xfrm/@flipH, @flipV).
	FlipH bool `json:"flipH,omitempty"`
	FlipV bool `json:"flipV,omitempty"`
	// WrapPolygon is the outline Tight and Through wrapping follow (wp:wrapPolygon), in the
	// object's own (unrotated) frame; nil = derived from the object.
	WrapPolygon *WrapPolygon `json:"wrapPolygon,omitempty"`
	// Alt is what the drawing is, for screen readers and exports (wp:docPr @descr, @title and the
	// decorative mark). See AltText of InlineObject.
	Alt AltText `json:"alt,omitzero"`
	// Hidden is kept and saved, but neither drawn nor laid out, as Word for Windows does.
	Hidden bool `json:"hidden,omitempty"`
	// OutsideCell: anchored in a table cell but placed on the page, not in the cell.
	OutsideCell bool `json:"outsideCell,omitempty"`
}

func (Float) SerdeDefault() {}

// EffectExtent is the effect extents, finite and clamped (left, top, right, bottom).
func (f Float) EffectExtent() [4]F32 {
	var out [4]F32
	for i, v := range f.Effect {
		if finite(v) {
			out[i] = fclamp(v, 0, 1584)
		}
	}
	return out
}

// MaxAltText is the longest alternative text kept (characters): hostile files and commands are
// capped.
const MaxAltText = 10_000

// AltText is a drawing's alternative text (Picture/Shape Format › Accessibility › Alt Text).
type AltText struct {
	// Text is the description of a shape, text box or group. Pictures and charts keep theirs in
	// their own Alt.
	Text string `json:"text,omitempty"`
	// Title is the short title (wp:docPr/@title).
	Title string `json:"title,omitempty"`
	// Decorative: it adds nothing to the content, so screen readers skip it.
	Decorative bool `json:"decorative,omitempty"`
}

func (AltText) SerdeDefault() {}

// IsZero reports no alternative text (Rust's is_empty).
func (a AltText) IsZero() bool { return a.Text == "" && a.Title == "" && !a.Decorative }

// CapAlt is s cut to MaxAltText characters.
func CapAlt(s string) string {
	n := 0
	for i := range s {
		if n == MaxAltText {
			return s[:i]
		}
		n++
	}
	return s
}

// ShapeKind is a preset shape.
type ShapeKind uint8

// Shape kinds.
const (
	ShapeRectangle ShapeKind = iota
	ShapeRoundedRectangle
	ShapeEllipse
	// ShapeFlowchartOr is the Flowchart OR preset: an ellipse with horizontal and vertical spokes.
	ShapeFlowchartOr
	ShapeTriangle
	// ShapeRightTriangle has its right angle at the bottom left (rtTriangle).
	ShapeRightTriangle
	ShapeDiamond
	ShapeLine
	ShapeArrow
	ShapeStar
	ShapeHeart
	// ShapeTextBox is a rectangle with a text story.
	ShapeTextBox
	// ShapeFreeform is drawn point by point, such as ink (its geometry is the shape's Freeform).
	ShapeFreeform
	// Connectors: lines whose ends can be glued to other shapes.
	ShapeStraightConnector
	ShapeElbowConnector
	ShapeCurvedConnector
)

var shapeKindNames = []string{"rectangle", "roundedRectangle", "ellipse", "flowchartOr", "triangle", "rightTriangle", "diamond", "line",
	"arrow", "star", "heart", "textBox", "freeform", "straightConnector", "elbowConnector", "curvedConnector"}

func (k *ShapeKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, shapeKindNames, k) }
func (k ShapeKind) MarshalJSON() ([]byte, error)           { return writeEnum(shapeKindNames, k) }

// IsConnector reports a connector.
func (k ShapeKind) IsConnector() bool {
	return k == ShapeStraightConnector || k == ShapeElbowConnector || k == ShapeCurvedConnector
}

// IsOpen reports an open path (a line or connector).
func (k ShapeKind) IsOpen() bool { return k == ShapeLine || k.IsConnector() }

// NoteKind is a footnote or an endnote.
type NoteKind uint8

// Note kinds.
const (
	Footnote NoteKind = iota
	Endnote
)

var noteKindNames = []string{"footnote", "endnote"}

func (k *NoteKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, noteKindNames, k) }
func (k NoteKind) MarshalJSON() ([]byte, error)           { return writeEnum(noteKindNames, k) }

// PTabAlign is where a positional tab lands on its line, and how the text after it lines up
// there (ST_PTabAlignment).
type PTabAlign uint8

// Positional tab alignments.
const (
	PTabLeft PTabAlign = iota
	PTabCenter
	PTabRight
)

var pTabAlignNames = []string{"left", "center", "right"}

func (a *PTabAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, pTabAlignNames, a) }
func (a PTabAlign) MarshalJSON() ([]byte, error)           { return writeEnum(pTabAlignNames, a) }

// OOXML is the ST_PTabAlignment name (the JSON name).
func (a PTabAlign) OOXML() string { return enumName(pTabAlignNames, a) }

// PTabAlignFromOOXML reads an ST_PTabAlignment name.
func PTabAlignFromOOXML(v string) (PTabAlign, bool) {
	i := slices.Index(pTabAlignNames, v)
	return PTabAlign(max(i, 0)), i >= 0
}

// PTabBase is what a positional tab's place is measured against (ST_PTabRelativeTo).
type PTabBase uint8

// Positional tab bases.
const (
	PTabMargin PTabBase = iota
	PTabIndent
)

var pTabBaseNames = []string{"margin", "indent"}

func (b *PTabBase) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, pTabBaseNames, b) }
func (b PTabBase) MarshalJSON() ([]byte, error)           { return writeEnum(pTabBaseNames, b) }

// OOXML is the ST_PTabRelativeTo name (the JSON name).
func (b PTabBase) OOXML() string { return enumName(pTabBaseNames, b) }

// PTabBaseFromOOXML reads an ST_PTabRelativeTo name.
func PTabBaseFromOOXML(v string) (PTabBase, bool) {
	i := slices.Index(pTabBaseNames, v)
	return PTabBase(max(i, 0)), i >= 0
}

// RubyAlign is how ruby text lines up over its base text (ST_RubyAlign).
type RubyAlign uint8

// Ruby alignments.
const (
	RubyCenter RubyAlign = iota
	// RubyDistributeLetter spreads letters evenly from edge to edge.
	RubyDistributeLetter
	// RubyDistributeSpace spreads letters evenly with half a gap at each edge.
	RubyDistributeSpace
	RubyLeft
	RubyRight
	// RubyRightVertical is right-aligned beside vertical text (laid out like RubyRight in
	// horizontal text).
	RubyRightVertical
)

var rubyAlignNames = []string{"center", "distributeLetter", "distributeSpace", "left", "right", "rightVertical"}

func (a *RubyAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, rubyAlignNames, a) }
func (a RubyAlign) MarshalJSON() ([]byte, error)           { return writeEnum(rubyAlignNames, a) }

// OOXML is the ST_RubyAlign name (the JSON name), also used by commands.
func (a RubyAlign) OOXML() string { return enumName(rubyAlignNames, a) }

// RubyAlignFromOOXML reads an ST_RubyAlign name.
func RubyAlignFromOOXML(v string) (RubyAlign, bool) {
	i := slices.Index(rubyAlignNames, v)
	return RubyAlign(max(i, 0)), i >= 0
}

// RubyOffset is Phonetic Guide's Offset for a ruby raise (baseline to baseline, points): the gap
// from the top of the base text to the bottom of the ruby text, taking letters as rising 0.8 em
// above their baseline and dropping 0.2 em below it.
func RubyOffset(raise, baseSize, size F32) F32 {
	return F32(float32(raise) - float32(0.8)*float32(baseSize) - float32(0.2)*float32(size))
}

// RubyRaise is the ruby raise (baseline to baseline, points) for a Phonetic Guide Offset.
func RubyRaise(offset, baseSize, size F32) F32 {
	return F32(float32(offset) + float32(0.8)*float32(baseSize) + float32(0.2)*float32(size))
}

// CanvasStyle is the background fill and outline of a Drawing Canvas.
type CanvasStyle struct {
	Fill   *RGB `json:"fill"`
	Stroke *RGB `json:"stroke"`
	// StrokeWidth is the outline width, points.
	StrokeWidth F32 `json:"strokeWidth"`
}

func (CanvasStyle) SerdeDefault() {}

// Wrap polygons (wrap.rs).
const (
	// WrapSpace is the wrap polygon coordinate that is the object's width or height.
	WrapSpace = 21_600
	// MaxWrapPoints is the most points a wrap polygon keeps.
	MaxWrapPoints = 1024
	maxWrapCoord  = WrapSpace * 4
)

// WrapPolygon is the outline Tight and Through wrapping follow, in a coordinate space where
// WrapSpace is the object's width or height.
type WrapPolygon struct {
	// Edited: the user edited the points (wp:wrapPolygon/@edited).
	Edited bool          `json:"edited"`
	Points Vec[[2]int32] `json:"points"`
}

func (WrapPolygon) SerdeDefault() {}

// NewWrapPolygon is a polygon of the points (at most MaxWrapPoints, coordinates clamped); false
// for fewer than three.
func NewWrapPolygon(edited bool, points [][2]int64) (WrapPolygon, bool) {
	pts := make(Vec[[2]int32], 0, min(len(points), MaxWrapPoints))
	for _, p := range points[:min(len(points), MaxWrapPoints)] {
		pts = append(pts, [2]int32{int32(min(max(p[0], -maxWrapCoord), maxWrapCoord)), int32(min(max(p[1], -maxWrapCoord), maxWrapCoord))})
	}
	return WrapPolygon{Edited: edited, Points: pts}, len(pts) >= 3
}

// WrapPolygonFromUnit is a polygon of points given as fractions of the object's size (points
// that aren't finite are left out).
func WrapPolygonFromUnit(edited bool, pts [][2]F32) (WrapPolygon, bool) {
	const s = float32(WrapSpace)
	var out [][2]int64
	for _, p := range pts {
		if finite(p[0]) && finite(p[1]) {
			out = append(out, [2]int64{roundI64(float32(p[0]) * s), roundI64(float32(p[1]) * s)})
		}
	}
	return NewWrapPolygon(edited, out)
}

// roundI64 is Rust's `v.round() as i64`: half away from zero, saturating.
func roundI64(v float32) int64 {
	r := math.Round(float64(v))
	switch {
	case r != r:
		return 0
	case r >= math.MaxInt64:
		return math.MaxInt64
	case r <= math.MinInt64:
		return math.MinInt64
	}
	return int64(r)
}

// Unit is the points as fractions of the object's size.
func (p WrapPolygon) Unit() [][2]F32 {
	const s = float32(WrapSpace)
	out := [][2]F32{}
	for _, q := range p.Points[:min(len(p.Points), MaxWrapPoints)] {
		out = append(out, [2]F32{F32(float32(q[0]) / s), F32(float32(q[1]) / s)})
	}
	return out
}

// ShapeEffects are a shape's outer shadow, glow and soft edges (a:effectLst).
type ShapeEffects struct {
	Shadow *Shadow `json:"shadow,omitempty" serde:"default"`
	Glow   *Glow   `json:"glow,omitempty" serde:"default"`
	// SoftEdge is the soft edge radius, points.
	SoftEdge *F32 `json:"softEdge,omitempty" serde:"default"`
}

// IsZero reports no effects (Rust's is_empty).
func (e ShapeEffects) IsZero() bool { return e.Shadow == nil && e.Glow == nil && e.SoftEdge == nil }

// ConnEnd is a connector end glued to connection site Site of the shape with drawing id ID.
type ConnEnd struct {
	ID   uint32 `json:"id"`
	Site uint32 `json:"site"`
}

// Corner rounding of a rounded rectangle (connector.rs).
const (
	// MaxCorner is the largest corner adjust value (half the shorter side).
	MaxCorner = 50_000
	// DefaultCorner is the corner radius as a fraction of the shorter side when unset.
	DefaultCorner = 0.16
	// MaxSite is the most connection sites a shape has.
	MaxSite = 64
)

// CornerRadius is a w × h rounded rectangle's corner radius, points.
func CornerRadius(corner *uint32, w, h F32) F32 {
	side := fmax(fmin(geomFinite(w), geomFinite(h)), 0)
	f := F32(DefaultCorner)
	if corner != nil {
		f = F32(float32(min(*corner, MaxCorner)) / 100_000)
	}
	return side * f
}

// geomFinite is wordcraft_geom::finite: v, or 0 when it isn't finite.
func geomFinite(v F32) F32 {
	if finite(v) {
		return v
	}
	return 0
}

// ShapeExtra holds the shape properties beyond fill and outline: the drawing id, text warp,
// connector ends and arrowheads, a rounded rectangle's corner and the outline dash.
type ShapeExtra struct {
	// ID is the drawing id (wp:docPr/@id), which connectors glue to.
	ID   uint32    `json:"id,omitempty"`
	Warp *TextWarp `json:"warp,omitempty"`
	// Start and End are a connector's glued ends.
	Start      *ConnEnd `json:"start,omitempty"`
	End        *ConnEnd `json:"end,omitempty"`
	ArrowStart bool     `json:"arrowStart,omitempty"`
	ArrowEnd   bool     `json:"arrowEnd,omitempty"`
	// Corner is a rounded rectangle's corner (a:gd adj, 100000ths of the shorter side).
	Corner *uint32  `json:"corner,omitempty"`
	Dash   LineDash `json:"dash,omitzero"`
}

func (ShapeExtra) SerdeDefault() {}

// IsZero reports the default (Rust's is_empty).
func (e ShapeExtra) IsZero() bool { return equal(e, ShapeExtra{}) }

// TextWarp is a shape's text warp: a preset and its adjust values (a:avLst/a:gd, name and val).
type TextWarp struct {
	Preset string                   `json:"preset"`
	Adj    Vec[Pair[string, int64]] `json:"adj,omitempty" serde:"default"`
}

// LineDashKind names an outline dash.
type LineDashKind uint8

// Line dashes.
const (
	DashSolid LineDashKind = iota
	DashDot
	DashDash
	DashLongDash
	DashDashDot
	DashLongDashDot
	DashLongDashDotDot
	DashSystemDash
	DashSystemDot
	DashSystemDashDot
	DashSystemDashDotDot
	// DashCustom is a pattern of (dash, space) pairs in 100000ths of the stroke width.
	DashCustom
)

var lineDashNames = []string{"solid", "dot", "dash", "longDash", "dashDot", "longDashDot", "longDashDotDot", "systemDash", "systemDot",
	"systemDashDot", "systemDashDotDot", "custom"}

var lineDashOOXML = []string{"solid", "dot", "dash", "lgDash", "dashDot", "lgDashDot", "lgDashDotDot", "sysDash", "sysDot", "sysDashDot",
	"sysDashDotDot"}

// LineDash is a DrawingML outline dash, measured in multiples of the stroke width.
type LineDash struct {
	Kind LineDashKind
	// Custom is the pattern of DashCustom.
	Custom Vec[Pair[uint32, uint32]]
}

// DecodeSerde reads serde's externally tagged form: "dot", or {"custom": [[d, s], …]}.
func (l *LineDash) DecodeSerde(d serdejson.Decoder) error {
	var custom Vec[Pair[uint32, uint32]]
	i, err := serdejson.ExternallyTagged(d, lineDashNames, func(i int) bool { return LineDashKind(i) != DashCustom },
		func(_ int, d serdejson.Decoder) error { return d.Decode(&custom) })
	if err != nil {
		return err
	}
	*l = LineDash{Kind: LineDashKind(i), Custom: custom}
	return nil
}

// MarshalJSON writes the dash's name, or {"custom": [...]}.
func (l LineDash) MarshalJSON() ([]byte, error) {
	if l.Kind == DashCustom {
		b, err := serdejson.MarshalCompact(l.Custom)
		if err != nil {
			return nil, err
		}
		return append(append([]byte(`{"custom":`), b...), '}'), nil
	}
	return writeEnum(lineDashNames, l.Kind)
}

// IsZero reports a solid line (Rust's is_solid).
func (l LineDash) IsZero() bool { return l.Kind == DashSolid }

// LineDashFromOOXML reads an ST_PresetLineDashVal; anything else is solid.
func LineDashFromOOXML(v string) LineDash {
	if i := slices.Index(lineDashOOXML, v); i > 0 {
		return LineDash{Kind: LineDashKind(i)}
	}
	return LineDash{}
}

// OOXML is the preset's ST_PresetLineDashVal; false for a custom dash.
func (l LineDash) OOXML() (string, bool) {
	if int(l.Kind) >= len(lineDashOOXML) {
		return "", false
	}
	return lineDashOOXML[l.Kind], true
}

// InkTool is the pen an ink stroke was drawn with.
type InkTool uint8

// Ink tools.
const (
	InkPen InkTool = iota
	InkPencil
	InkHighlighter
)

var inkToolNames = []string{"pen", "pencil", "highlighter"}

func (t *InkTool) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, inkToolNames, t) }
func (t InkTool) MarshalJSON() ([]byte, error)           { return writeEnum(inkToolNames, t) }

// Name is the tool's name (the JSON name).
func (t InkTool) Name() string { return enumName(inkToolNames, t) }

// ParseInkTool reads a tool's name (any ASCII case).
func ParseInkTool(s string) (InkTool, bool) {
	for i, n := range inkToolNames {
		if asciiEqualFold(n, s) {
			return InkTool(i), true
		}
	}
	return 0, false
}

// DefaultColor is the tool's default colour.
func (t InkTool) DefaultColor() RGB {
	switch t {
	case InkPencil:
		return RGB{0x59, 0x59, 0x59}
	case InkHighlighter:
		return RGB{0xFF, 0xFF, 0x00}
	}
	return RGB{}
}

// DefaultWidth is the tool's default width, points.
func (t InkTool) DefaultWidth() F32 {
	switch t {
	case InkPencil:
		return 1
	case InkHighlighter:
		return 12
	}
	return 1.5
}

// Alpha is the tool's opacity.
func (t InkTool) Alpha() F32 {
	switch t {
	case InkPencil:
		return 0.85
	case InkHighlighter:
		return 0.5
	}
	return 1
}

// FreePath is one path of a freeform: its points and whether it is closed.
type FreePath struct {
	Pts    Vec[[2]F32] `json:"pts"`
	Closed bool        `json:"closed"`
}

func (FreePath) SerdeDefault() {}

// Freeform is the geometry of a shape drawn point by point (a:custGeom), such as an ink stroke.
type Freeform struct {
	// W and H are the size of the paths' coordinate space.
	W     F32           `json:"w"`
	H     F32           `json:"h"`
	Paths Vec[FreePath] `json:"paths"`
	// Alpha is the opacity.
	Alpha F32 `json:"alpha"`
	// Ink is the tool of an ink stroke; nil for other freeforms.
	Ink *InkTool `json:"ink,omitempty"`
	// Order is the stroke's drawing order on its page.
	Order *uint32 `json:"order,omitempty"`
}

func (Freeform) SerdeDefault() {}

// SetDefaults is Rust's Freeform::default(): opaque, no paths.
func (f *Freeform) SetDefaults() { *f = Freeform{Paths: Vec[FreePath]{}, Alpha: 1} }

// IsInk reports an ink stroke.
func (f *Freeform) IsInk() bool { return f.Ink != nil }
