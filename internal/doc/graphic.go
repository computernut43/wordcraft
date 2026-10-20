package doc

import (
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/craft-libs/serdejson"
)

// Charts, SmartArt diagrams and native ink: drawings kept as neutral items ready to draw
// (graphic.rs), with the model of the charts and SmartArt WordCraft can edit (chart.rs,
// smart_art.rs).

// Graphic is a chart, SmartArt diagram or native ink preview, as items ready to draw (and, for an
// editable chart or SmartArt graphic, its model).
type Graphic struct {
	Kind GraphicKind `json:"kind"`
	// Items are what to draw, in points from the object's top-left corner.
	Items Vec[GraphicItem] `json:"items" serde:"default"`
	// W and H are the size the items were built for; 0 means the object's own size.
	W F32 `json:"w" serde:"default"`
	H F32 `json:"h" serde:"default"`
	// Source is where the graphic was read from, so saving writes the original drawing back.
	Source *Embedded `json:"source,omitempty" serde:"default"`
	// SmartArt is a SmartArt graphic WordCraft can edit.
	SmartArt *SmartArtSpec `json:"smartArt,omitempty" serde:"default"`
	// Chart is a chart WordCraft can edit.
	Chart *ChartSpec `json:"chart,omitempty" serde:"default"`
}

// Embedded is what an object read from a file needs to be written back as it was: its own markup
// and the package parts that markup refers to (kept in Document.Passthrough).
type Embedded struct {
	// XML is the object's markup as a standalone element with its namespace declarations.
	XML string `json:"xml"`
	// Refs are the relationships it uses, by the ids in XML.
	Refs Vec[EmbeddedRef] `json:"refs"`
}

// EmbeddedRef is one relationship an Embedded object uses.
type EmbeddedRef struct {
	// ID is the relationship id the markup uses.
	ID string `json:"id"`
	// Kind is the relationship type (a URI).
	Kind string `json:"kind"`
	// Target is the part name (a Document.Passthrough key), or the URL of an external target.
	Target   string `json:"target"`
	External bool   `json:"external"`
}

// GraphicKind is what a graphic is.
type GraphicKind uint8

// Graphic kinds.
const (
	GraphicChart GraphicKind = iota
	GraphicDiagram
	// GraphicInk is Word's native InkML, kept opaque; displayed as its fallback picture.
	GraphicInk
)

var graphicKindNames = []string{"chart", "diagram", "ink"}

func (k *GraphicKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, graphicKindNames, k) }
func (k GraphicKind) MarshalJSON() ([]byte, error)           { return writeEnum(graphicKindNames, k) }

// Noun is what the graphic is, in words: the alt text of one that has none of its own.
func (k GraphicKind) Noun() string { return enumName(graphicKindNames, k) }

// TextAlign is a graphic text item's horizontal alignment.
type TextAlign uint8

// Text alignments.
const (
	TextLeft TextAlign = iota
	TextCenter
	TextRight
)

var textAlignNames = []string{"left", "center", "right"}

func (a *TextAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, textAlignNames, a) }
func (a TextAlign) MarshalJSON() ([]byte, error)           { return writeEnum(textAlignNames, a) }

// SetDefaults is the default: centred.
func (a *TextAlign) SetDefaults() { *a = TextCenter }

// GraphicItemType is the kind of a GraphicItem.
type GraphicItemType uint8

// Graphic item types.
const (
	// ItemShape is a preset shape filling Rect.
	ItemShape GraphicItemType = iota
	// ItemPath is a path: polygons, pie slices, line series.
	ItemPath
	// ItemText is one line of text in Rect, aligned horizontally by Align, centred vertically.
	ItemText
	// ItemImage is a picture (a Document.Media key) stretched into Rect.
	ItemImage
)

var graphicItemNames = []string{"shape", "path", "text", "image"}

// GraphicItem is one thing a graphic draws. Only the fields of its Type are used.
type GraphicItem struct {
	Type GraphicItemType
	// Rect is [x, y, w, h] (shape, text, image).
	Rect [4]F32
	// Shape.
	Kind ShapeKind
	// Fill, Stroke and StrokeWidth (shape, path).
	Fill, Stroke *RGB
	StrokeWidth  F32
	// Segs (path).
	Segs Vec[PathSeg]
	// Text, Size, Color, Bold, Align and Font (text).
	Text  string
	Size  F32
	Color RGB
	Bold  bool
	Align TextAlign
	Font  *string
	// Media (image).
	Media string
}

type shapeItem struct {
	Rect        [4]F32    `json:"rect"`
	Kind        ShapeKind `json:"kind"`
	Fill        *RGB      `json:"fill"`
	Stroke      *RGB      `json:"stroke"`
	StrokeWidth F32       `json:"stroke_width" serde:"default"`
}

type pathItem struct {
	Segs        Vec[PathSeg] `json:"segs"`
	Fill        *RGB         `json:"fill"`
	Stroke      *RGB         `json:"stroke"`
	StrokeWidth F32          `json:"stroke_width" serde:"default"`
}

type textItem struct {
	Rect  [4]F32    `json:"rect"`
	Text  string    `json:"text"`
	Size  F32       `json:"size"`
	Color RGB       `json:"color"`
	Bold  bool      `json:"bold" serde:"default"`
	Align TextAlign `json:"align" serde:"default"`
	Font  *string   `json:"font" serde:"default"`
}

type imageItem struct {
	Rect  [4]F32 `json:"rect"`
	Media string `json:"media"`
}

// DecodeSerde reads serde's internally tagged form ({"type": "shape", …}).
func (g *GraphicItem) DecodeSerde(d serdejson.Decoder) error {
	i, rest, err := d.Tagged("type", graphicItemNames)
	if err != nil {
		return err
	}
	v := GraphicItem{Type: GraphicItemType(i)}
	switch v.Type {
	case ItemShape:
		var f shapeItem
		err = rest.Decode(&f)
		v.Rect, v.Kind, v.Fill, v.Stroke, v.StrokeWidth = f.Rect, f.Kind, f.Fill, f.Stroke, f.StrokeWidth
	case ItemPath:
		var f pathItem
		err = rest.Decode(&f)
		v.Segs, v.Fill, v.Stroke, v.StrokeWidth = f.Segs, f.Fill, f.Stroke, f.StrokeWidth
	case ItemText:
		var f textItem
		err = rest.Decode(&f)
		v.Rect, v.Text, v.Size, v.Color, v.Bold, v.Align, v.Font = f.Rect, f.Text, f.Size, f.Color, f.Bold, f.Align, f.Font
	default:
		var f imageItem
		err = rest.Decode(&f)
		v.Rect, v.Media = f.Rect, f.Media
	}
	if err != nil {
		return err
	}
	*g = v
	return nil
}

// MarshalJSON writes {"type": …} and the item's fields.
func (g GraphicItem) MarshalJSON() ([]byte, error) {
	var f any
	switch g.Type {
	case ItemShape:
		f = shapeItem{g.Rect, g.Kind, g.Fill, g.Stroke, g.StrokeWidth}
	case ItemPath:
		f = pathItem{g.Segs, g.Fill, g.Stroke, g.StrokeWidth}
	case ItemText:
		f = textItem{g.Rect, g.Text, g.Size, g.Color, g.Bold, g.Align, g.Font}
	default:
		f = imageItem{g.Rect, g.Media}
	}
	name := enumName(graphicItemNames, g.Type)
	return tagged("type", name, f)
}

// PathSegKind is a path segment's command.
type PathSegKind uint8

// Path segment kinds.
const (
	SegMove PathSegKind = iota
	SegLine
	SegCubic
	SegClose
)

var pathSegNames = []string{"move", "line", "cubic", "close"}

// PathSeg is a path segment: move or line to P[0], P[1]; a cubic Bézier through P[0..6]; or
// close.
type PathSeg struct {
	Kind PathSegKind
	P    [6]F32
}

func (k PathSegKind) arity() int { return [4]int{2, 2, 6, 0}[min(int(k), 3)] }

// DecodeSerde reads serde's externally tagged form: {"move": [x, y]}, "close".
func (s *PathSeg) DecodeSerde(d serdejson.Decoder) error {
	var v PathSeg
	i, err := serdejson.ExternallyTagged(d, pathSegNames, func(i int) bool { return PathSegKind(i) == SegClose },
		func(i int, d serdejson.Decoder) error {
			n := PathSegKind(i).arity()
			ptrs := make([]any, n)
			for k := range n {
				ptrs[k] = &v.P[k]
			}
			return readTuple(d, ptrs...)
		})
	if err != nil {
		return err
	}
	v.Kind = PathSegKind(i)
	*s = v
	return nil
}

// MarshalJSON writes {"move": [x, y]} or "close".
func (s PathSeg) MarshalJSON() ([]byte, error) {
	if s.Kind >= SegClose {
		return writeEnum(pathSegNames, s.Kind)
	}
	b, err := serdejson.MarshalCompact(s.P[:s.Kind.arity()])
	if err != nil {
		return nil, err
	}
	return append(append([]byte(`{"`+pathSegNames[s.Kind]+`":`), b...), '}'), nil
}

// Chart limits (chart.rs).
const (
	MaxChartSeries = 255
	MaxChartPoints = 4000
	MaxChartCells  = 100_000
	MaxChartText   = 255
)

// ChartType is what a chart shows its data as.
type ChartType uint8

// Chart types.
const (
	// ChartColumn is clustered columns.
	ChartColumn ChartType = iota
	ChartColumnStacked
	// ChartBar is clustered horizontal bars.
	ChartBar
	ChartBarStacked
	ChartLine
	ChartArea
	ChartPie
	ChartDoughnut
	// ChartScatter is XY scatter with markers: the categories are the x values.
	ChartScatter
)

var chartTypeNames = []string{"column", "columnStacked", "bar", "barStacked", "line", "area", "pie", "doughnut", "scatter"}

var chartTypeLabels = []string{"Clustered Column", "Stacked Column", "Clustered Bar", "Stacked Bar", "Line", "Area", "Pie", "Doughnut",
	"Scatter"}

func (t *ChartType) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, chartTypeNames, t) }
func (t ChartType) MarshalJSON() ([]byte, error)           { return writeEnum(chartTypeNames, t) }

// ID is the id commands use (column, barStacked…).
func (t ChartType) ID() string { return enumName(chartTypeNames, t) }

// Label is the name shown for it.
func (t ChartType) Label() string { return enumName(chartTypeLabels, t) }

// IsRound reports pie and doughnut charts, which show one series, a slice per category.
func (t ChartType) IsRound() bool { return t == ChartPie || t == ChartDoughnut }

// ChartTypeFromID is the type an id names (any ASCII case, surrounding white space ignored).
func ChartTypeFromID(id string) (ChartType, bool) {
	id = rustcompat.Trim(id)
	for i, n := range chartTypeNames {
		if asciiEqualFold(n, id) {
			return ChartType(i), true
		}
	}
	return 0, false
}

// LegendPos is where a chart's legend sits.
type LegendPos uint8

// Legend positions.
const (
	LegendRight LegendPos = iota
	LegendLeft
	LegendTop
	LegendBottom
)

var legendPosNames = []string{"right", "left", "top", "bottom"}

func (p *LegendPos) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, legendPosNames, p) }
func (p LegendPos) MarshalJSON() ([]byte, error)           { return writeEnum(legendPosNames, p) }

// ID is the position's id.
func (p LegendPos) ID() string { return enumName(legendPosNames, p) }

// LegendPosFromID is the position an id names (any ASCII case, surrounding white space ignored).
func LegendPosFromID(id string) (LegendPos, bool) {
	id = rustcompat.Trim(id)
	for _, p := range []LegendPos{LegendRight, LegendTop, LegendLeft, LegendBottom} {
		if asciiEqualFold(p.ID(), id) {
			return p, true
		}
	}
	return 0, false
}

// F64 is a Rust f64 field.
type F64 = serdejson.F64

// ChartSeries is one series: its name and a value per category (nil = no value, a gap).
type ChartSeries struct {
	Name   string    `json:"name"`
	Values Vec[*F64] `json:"values" serde:"default"`
}

// ChartSpec is a chart WordCraft can edit.
type ChartSpec struct {
	Kind ChartType `json:"type"`
	// Categories are the category labels (scatter charts: the x values).
	Categories Vec[string]      `json:"categories" serde:"default"`
	Series     Vec[ChartSeries] `json:"series" serde:"default"`
	Title      *string          `json:"title" serde:"default"`
	// Legend is where the legend is; nil: no legend.
	Legend *LegendPos `json:"legend" serde:"default"`
	// DataLabels shows each point's value next to it.
	DataLabels bool `json:"dataLabels" serde:"default"`
}

// SmartArt limits (smart_art.rs).
const (
	MaxSmartArtItems = 100
	MaxSmartArtText  = 255
	MaxSmartArtLevel = 7
)

// SmartArtLayout is how a SmartArt graphic arranges its items.
type SmartArtLayout uint8

// SmartArt layouts.
const (
	// LayoutBasicList is blocks in rows.
	LayoutBasicList SmartArtLayout = iota
	// LayoutProcess is steps left to right with arrows between.
	LayoutProcess
	LayoutCycle
	LayoutHierarchy
	LayoutPyramid
	LayoutRadial
	LayoutMatrix
	LayoutVenn
)

var smartArtLayoutNames = []string{"basicList", "process", "cycle", "hierarchy", "pyramid", "radial", "matrix", "venn"}

var smartArtLayoutLabels = []string{"Basic List", "Basic Process", "Basic Cycle", "Hierarchy", "Basic Pyramid", "Basic Radial", "Basic Matrix",
	"Basic Venn"}

var smartArtLayoutCategories = []string{"List", "Process", "Cycle", "Hierarchy", "Pyramid", "Relationship", "Matrix", "Relationship"}

var smartArtLayoutDescriptions = []string{
	"Shows items of equal weight as blocks in rows.",
	"Shows steps in order, left to right, with arrows between them.",
	"Shows steps that repeat, around a circle.",
	"Shows a tree: each item below the item it belongs to.",
	"Shows parts that build on each other, largest at the bottom.",
	"Shows items around a central idea: the first item is the centre.",
	"Shows parts of a whole as quadrants.",
	"Shows overlapping ideas as overlapping circles.",
}

func (l *SmartArtLayout) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, smartArtLayoutNames, l)
}
func (l SmartArtLayout) MarshalJSON() ([]byte, error) { return writeEnum(smartArtLayoutNames, l) }

// ID is the layout's id.
func (l SmartArtLayout) ID() string { return enumName(smartArtLayoutNames, l) }

// Label is the name shown for it.
func (l SmartArtLayout) Label() string { return enumName(smartArtLayoutLabels, l) }

// Category is the gallery category it's shown under.
func (l SmartArtLayout) Category() string { return enumName(smartArtLayoutCategories, l) }

// Description is what it's for, in a sentence (the picker shows it).
func (l SmartArtLayout) Description() string { return enumName(smartArtLayoutDescriptions, l) }

// normID drops white space, hyphens and underscores, as the SmartArt id lookups do.
func normID(id string) string {
	var b strings.Builder
	for _, c := range id {
		if !rustcompat.IsWhitespace(c) && c != '-' && c != '_' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// SmartArtLayoutFromID is the layout an id names (any ASCII case, ignoring hyphens, underscores
// and white space; list is the basic list; "basic process" or process is process, etc.).
func SmartArtLayoutFromID(id string) (SmartArtLayout, bool) {
	norm := normID(id)
	if asciiEqualFold(norm, "list") {
		return LayoutBasicList, true
	}
	for i := range smartArtLayoutNames {
		l := SmartArtLayout(i)
		if asciiEqualFold(l.ID(), norm) || asciiEqualFold(strings.ReplaceAll(l.Label(), " ", ""), norm) {
			return l, true
		}
	}
	return 0, false
}

// SmartArtColors is the colours a SmartArt graphic takes from the document's theme.
type SmartArtColors uint8

// SmartArt colour variants: every shape in accent n, or the accents in turn.
const (
	Accent1 SmartArtColors = iota
	Accent2
	Accent3
	Accent4
	Accent5
	Accent6
	Colorful
)

var smartArtColorsNames = []string{"accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "colorful"}

var smartArtColorsLabels = []string{"Accent 1", "Accent 2", "Accent 3", "Accent 4", "Accent 5", "Accent 6", "Colorful"}

func (c *SmartArtColors) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, smartArtColorsNames, c)
}
func (c SmartArtColors) MarshalJSON() ([]byte, error) { return writeEnum(smartArtColorsNames, c) }

// ID is the variant's id.
func (c SmartArtColors) ID() string { return enumName(smartArtColorsNames, c) }

// Label is the name shown for it.
func (c SmartArtColors) Label() string { return enumName(smartArtColorsLabels, c) }

// Accent is the theme accent (1–6) of shape i.
func (c SmartArtColors) Accent(i int) int {
	if c == Colorful {
		return i%6 + 1
	}
	return int(c) + 1
}

// SmartArtColorsFromID is the variant an id or label names (any ASCII case, ignoring hyphens,
// underscores and white space).
func SmartArtColorsFromID(id string) (SmartArtColors, bool) {
	norm := normID(id)
	for i := range smartArtColorsNames {
		c := SmartArtColors(i)
		if asciiEqualFold(c.ID(), norm) || asciiEqualFold(strings.ReplaceAll(c.Label(), " ", ""), norm) {
			return c, true
		}
	}
	return 0, false
}

// SmartArtItem is one item of the outline: its text and level (0 is the top).
type SmartArtItem struct {
	Text  string `json:"text"`
	Level uint8  `json:"level" serde:"default"`
}

// SmartArtSpec is a SmartArt graphic WordCraft can edit.
type SmartArtSpec struct {
	Layout SmartArtLayout    `json:"layout"`
	Items  Vec[SmartArtItem] `json:"items" serde:"default"`
	Colors SmartArtColors    `json:"colors" serde:"default"`
}
