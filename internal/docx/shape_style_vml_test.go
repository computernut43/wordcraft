package docx

import (
	"math"
	"testing"

	"github.com/computernut43/wordcraft/internal/doc"
)

// Ported from crates/docx/src/read/shape_style.rs vml_presets_map_like_drawingml_presets.
func TestVMLPresetsMapLikeDrawingMLPresets(t *testing.T) {
	for _, c := range []struct {
		spt  uint32
		want doc.ShapeKind
	}{
		{13, doc.ShapeArrow}, {12, doc.ShapeStar}, {32, doc.ShapeLine}, {202, doc.ShapeTextBox},
		// The nearest shape, as presetKind gives it: left arrow, right triangle, four-point star,
		// rounded callout, elbow connector, a bevel (its box).
		{66, doc.ShapeArrow}, {6, doc.ShapeRightTriangle}, {187, doc.ShapeStar}, {62, doc.ShapeRoundedRectangle},
		{34, doc.ShapeElbowConnector}, {84, doc.ShapeRectangle},
	} {
		if got, ok := vmlPresetKind(c.spt); !ok || got != c.want {
			t.Errorf("vmlPresetKind(%d) = %v, %v; want %v", c.spt, got, ok, c.want)
		}
	}
	// No preset geometry: freeform, WordArt text, picture frame, host control, out of range.
	for _, spt := range []uint32{0, 24, 31, 136, 175, 75, 201, 203, math.MaxUint32} {
		if k, ok := vmlPresetKind(spt); ok {
			t.Errorf("vmlPresetKind(%d) = %v, want none", spt, k)
		}
	}
}

// Ported from crates/docx/src/read/shape_style.rs vml_colours.
func TestVMLColours(t *testing.T) {
	for _, c := range []struct {
		in   string
		want doc.RGB
	}{
		{"#ffc000 [3207]", doc.RGB{R: 0xFF, G: 0xC0}}, {"ffc000 [3207]", doc.RGB{R: 0xFF, G: 0xC0}}, {"#f00", doc.RGB{R: 0xFF}},
		{"f00", doc.RGB{R: 0xFF}}, {"black", doc.RGB{}}, {"Red", doc.RGB{R: 0xFF}}, {"rgb(255, 128, 0)", doc.RGB{R: 255, G: 128}},
		{"rgb(100%, 0%, 0%)", doc.RGB{R: 255}},
	} {
		if got, ok := vmlColor(c.in); !ok || got != c.want {
			t.Errorf("vmlColor(%q) = %v, %v; want %v", c.in, got, ok, c.want)
		}
	}
	for _, in := range []string{"#é", ""} {
		if got, ok := vmlColor(in); ok {
			t.Errorf("vmlColor(%q) = %v, want none", in, got)
		}
	}
}
