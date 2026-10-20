package doc

import (
	"github.com/computernut43/craft-libs/serdejson"
)

// Text effects on runs (crates/doc/src/wordart.rs) and the shadow and glow they share with shapes
// (crates/doc/src/effects.rs): the data and how a patch overlays it. Sanitizing, the WordArt
// styles and text warps aren't ported yet (MIGRATION.md).
//
// Text effects are the run properties of [MS-DOCX] §2.6 (w14:textFill, w14:textOutline,
// w14:shadow, w14:glow, w14:reflection): how a run's letters are filled and outlined and what is
// drawn around them. Lengths are points, angles degrees, transparencies percent (0 = opaque).

// TextEffects are the WordArt effects of a run. nil fields draw the text normally (its colour,
// no outline).
type TextEffects struct {
	Fill       *TextFill    `json:"fill,omitempty"`
	Outline    *TextOutline `json:"outline,omitempty"`
	Shadow     *Shadow      `json:"shadow,omitempty"`
	Glow       *Glow        `json:"glow,omitempty"`
	Reflection *Reflection  `json:"reflection,omitempty"`
}

func (TextEffects) SerdeDefault() {}

// IsEmpty reports whether no effect is set.
func (t TextEffects) IsEmpty() bool { return equal(t, TextEffects{}) }

// Overlay applies every set effect of patch on top of t (style, then direct formatting).
func (t *TextEffects) Overlay(patch *TextEffects) {
	set(&t.Fill, patch.Fill)
	set(&t.Outline, patch.Outline)
	set(&t.Shadow, patch.Shadow)
	set(&t.Glow, patch.Glow)
	set(&t.Reflection, patch.Reflection)
}

// FillKind is how letters are filled.
type FillKind uint8

// Fill kinds.
const (
	// FillNone: not filled (hollow letters: only their outline shows).
	FillNone FillKind = iota
	FillSolid
	// FillGradient: a linear gradient across the run's letters, at Angle degrees (0 = left to
	// right, 90 = top to bottom).
	FillGradient
)

var fillKindNames = []string{"none", "solid", "gradient"}

// TextFill is how letters are filled: written {"type": "none"}, {"type": "solid", "color": …,
// "transparency": …} or {"type": "gradient", "stops": […], "angle": …}.
type TextFill struct {
	Kind FillKind
	// Color and Transparency of a solid fill.
	Color        RGB
	Transparency F32
	// Stops and Angle of a gradient.
	Stops Vec[GradStop]
	Angle F32
}

type solidFill struct {
	Color        RGB `json:"color"`
	Transparency F32 `json:"transparency" serde:"default"`
}

type gradientFill struct {
	Stops Vec[GradStop] `json:"stops"`
	Angle F32           `json:"angle" serde:"default"`
}

// DecodeSerde reads serde's internally tagged form.
func (f *TextFill) DecodeSerde(d serdejson.Decoder) error {
	i, rest, err := d.Tagged("type", fillKindNames)
	if err != nil {
		return err
	}
	switch FillKind(i) {
	case FillSolid:
		var s solidFill
		if err := rest.Decode(&s); err != nil {
			return err
		}
		*f = TextFill{Kind: FillSolid, Color: s.Color, Transparency: s.Transparency}
	case FillGradient:
		var g gradientFill
		if err := rest.Decode(&g); err != nil {
			return err
		}
		*f = TextFill{Kind: FillGradient, Stops: g.Stops, Angle: g.Angle}
	default:
		if err := serdejson.TaggedUnit(rest); err != nil {
			return err
		}
		*f = TextFill{}
	}
	return nil
}

// MarshalJSON writes the tag, then the variant's fields.
func (f TextFill) MarshalJSON() ([]byte, error) {
	switch f.Kind {
	case FillNone:
		return []byte(`{"type":"none"}`), nil
	case FillSolid:
		c, _ := f.Color.MarshalJSON()
		return []byte(`{"type":"solid","color":` + string(c) + `,"transparency":` + serdejson.FormatF32(float32(f.Transparency)) + `}`), nil
	case FillGradient:
		s, err := f.Stops.MarshalJSON()
		if err != nil {
			return nil, err
		}
		return []byte(`{"type":"gradient","stops":` + string(s) + `,"angle":` + serdejson.FormatF32(float32(f.Angle)) + `}`), nil
	}
	return writeEnum(fillKindNames, f.Kind)
}

// GradStop is a gradient stop at Pos percent along the gradient.
type GradStop struct {
	Pos          F32 `json:"pos"`
	Color        RGB `json:"color"`
	Transparency F32 `json:"transparency" serde:"default"`
}

// TextOutline is the line around letters. A nil Color is explicitly no line.
type TextOutline struct {
	Color        *RGB `json:"color"`
	Width        F32  `json:"width"`
	Transparency F32  `json:"transparency"`
}

func (TextOutline) SerdeDefault() {}

// SetDefaults sets the Rust Default: a black line ¾ pt wide.
func (o *TextOutline) SetDefaults() { *o = TextOutline{Color: ptr(Black), Width: 0.75} }

// Reflection is a mirror image of the letters below them that fades out: it starts at
// Transparency and fades to nothing over Size percent of the letters' height, Distance points
// below them.
type Reflection struct {
	Transparency F32 `json:"transparency"`
	Size         F32 `json:"size"`
	Distance     F32 `json:"distance"`
	Blur         F32 `json:"blur"`
}

func (Reflection) SerdeDefault() {}

// SetDefaults sets the Rust Default.
func (r *Reflection) SetDefaults() { *r = Reflection{Transparency: 50, Size: 50, Blur: 0.5} }

// Shadow is an outer shadow: a blurred copy of the silhouette cast Distance points away in
// direction Angle (degrees clockwise from the positive x axis, y pointing down).
//
// RotWithShape (DrawingML rotWithShape, true when absent in a file) says whether that direction
// is the shape's own, turning with it when it is rotated or flipped, or the page's. Word's
// shadow presets write rotWithShape="0": the light stays put as the shape turns.
type Shadow struct {
	Color        RGB  `json:"color"`
	Transparency F32  `json:"transparency"`
	Blur         F32  `json:"blur"`
	Distance     F32  `json:"distance"`
	Angle        F32  `json:"angle"`
	RotWithShape bool `json:"rotWithShape"`
}

func (Shadow) SerdeDefault() {}

// SetDefaults sets the Rust Default: "Offset: Bottom Right".
func (s *Shadow) SetDefaults() {
	*s = Shadow{Color: Black, Transparency: 60, Blur: 4, Distance: 3, Angle: 45}
}

// Glow is a soft band of colour Size points wide around the outline.
type Glow struct {
	Color        RGB `json:"color"`
	Size         F32 `json:"size"`
	Transparency F32 `json:"transparency"`
}

func (Glow) SerdeDefault() {}

// SetDefaults sets the Rust Default.
func (g *Glow) SetDefaults() { *g = Glow{Color: RGB{0x15, 0x60, 0x82}, Size: 5, Transparency: 60} }
