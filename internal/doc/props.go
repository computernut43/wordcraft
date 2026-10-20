package doc

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/computernut43/craft-libs/serdejson"
)

// Character, paragraph, table and cell properties (crates/doc/src/props.rs).
//
// Every Option field is a pointer: nil means "inherit" (from the style chain, then document
// defaults). CharProps.Overlay and friends apply a patch on top of a base. Values reached through
// a pointer are never changed in place, only replaced, so copies may share them.

// RGB is an sRGB colour, written [r, g, b].
type RGB struct{ R, G, B uint8 }

// Colours.
var (
	Black = RGB{0, 0, 0}
	White = RGB{255, 255, 255}
)

// Hex is RRGGBB (no #).
func (c RGB) Hex() string { return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B) }

// ParseRGB reads RRGGBB or #RRGGBB (surrounding whitespace and any number of leading # are
// ignored; each pair is read as Rust's u8::from_str_radix reads it, so "+F" is 15).
func ParseRGB(s string) (RGB, bool) {
	s = strings.TrimLeft(strings.TrimSpace(s), "#")
	if len(s) != 6 || !isASCII(s) {
		return RGB{}, false
	}
	r, ok1 := hexByte(s[0:2])
	g, ok2 := hexByte(s[2:4])
	b, ok3 := hexByte(s[4:6])
	return RGB{r, g, b}, ok1 && ok2 && ok3
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// hexByte reads two characters as u8::from_str_radix(_, 16) does: an optional leading +, then
// hex digits.
func hexByte(s string) (uint8, bool) {
	if s[0] == '+' {
		s = s[1:]
	}
	var n uint8
	for i := range len(s) {
		var d byte
		switch c := s[i]; {
		case '0' <= c && c <= '9':
			d = c - '0'
		case 'a' <= c && c <= 'f':
			d = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		n = n<<4 | d
	}
	return n, true
}

// Luma is the colour's brightness, 0..255.
func (c RGB) Luma() float32 {
	return 0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)
}

// MarshalJSON writes [r, g, b].
func (c RGB) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "[%d,%d,%d]", c.R, c.G, c.B), nil
}

// DecodeSerde reads what MarshalJSON writes, and also a string ParseRGB reads.
func (c *RGB) DecodeSerde(d serdejson.Decoder) error {
	switch d.Peek() {
	case serdejson.KindString:
		s, err := d.String()
		if err != nil {
			return err
		}
		v, ok := ParseRGB(s)
		if !ok {
			return d.Fail("invalid value: string, expected `[r, g, b]`, `RRGGBB` or `#RRGGBB`")
		}
		*c = v
		return nil
	case serdejson.KindArray:
		var v [3]uint8
		n := 0
		err := d.Array(func(i int) error {
			if i >= 3 {
				return d.Fail("invalid length 4, expected `[r, g, b]`, `RRGGBB` or `#RRGGBB`")
			}
			n = i + 1
			u, err := d.Uint(8)
			v[i] = uint8(u)
			return err
		})
		if err != nil {
			return err
		}
		if n < 3 {
			return d.Fail(fmt.Sprintf("invalid length %d, expected `[r, g, b]`, `RRGGBB` or `#RRGGBB`", n))
		}
		*c = RGB{v[0], v[1], v[2]}
		return nil
	}
	return d.Fail("invalid type, expected `[r, g, b]`, `RRGGBB` or `#RRGGBB`")
}

// TextColor is automatic (the zero value: black on light backgrounds, white on dark) or a fixed
// colour.
type TextColor struct {
	Fixed bool
	RGB   RGB // when Fixed
}

// FixedColor is the text colour c.
func FixedColor(c RGB) TextColor { return TextColor{Fixed: true, RGB: c} }

// ParseTextColor reads auto or automatic (any ASCII case), or a colour ParseRGB reads.
func ParseTextColor(s string) (TextColor, bool) {
	if strings.EqualFold(s, "auto") && isASCII(s) || strings.EqualFold(s, "automatic") && isASCII(s) {
		return TextColor{}, true
	}
	c, ok := ParseRGB(s)
	return FixedColor(c), ok
}

// MarshalJSON writes "Auto" or {"Rgb": [r, g, b]}.
func (t TextColor) MarshalJSON() ([]byte, error) {
	if !t.Fixed {
		return []byte(`"Auto"`), nil
	}
	c, _ := t.RGB.MarshalJSON()
	return append(append([]byte(`{"Rgb":`), c...), '}'), nil
}

// DecodeSerde reads what MarshalJSON writes, and also a string ParseTextColor reads (through a
// serde_json::Value, as the Rust implementation does).
func (t *TextColor) DecodeSerde(d serdejson.Decoder) error {
	v, err := d.Value()
	if err != nil {
		return err
	}
	if v.Kind == serdejson.KindString {
		c, ok := ParseTextColor(v.Str)
		if !ok {
			return d.Fail("invalid value: string, expected `auto`, `RRGGBB` or `#RRGGBB`")
		}
		*t = c
		return nil
	}
	var w writtenTextColor
	if err := serdejson.FromValue(v, &w); err != nil {
		return d.Fail(err.Error())
	}
	*t = TextColor(w)
	return nil
}

type writtenTextColor TextColor

func (w *writtenTextColor) DecodeSerde(d serdejson.Decoder) error {
	var c RGB
	i, err := serdejson.ExternallyTagged(d, []string{"Auto", "Rgb"}, func(i int) bool { return i == 0 }, func(_ int, d serdejson.Decoder) error {
		return d.Decode(&c)
	})
	if err != nil {
		return err
	}
	*w = writtenTextColor{Fixed: i == 1, RGB: c}
	return nil
}

// Underline is an underline style.
type Underline uint8

// Underline styles.
const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineWords
	UnderlineDouble
	UnderlineThick
	UnderlineDotted
	UnderlineDash
	UnderlineDotDash
	UnderlineDotDotDash
	UnderlineWave
	UnderlineDoubleWave
)

var underlineNames = []string{"none", "single", "words", "double", "thick", "dotted", "dash", "dotDash", "dotDotDash", "wave", "doubleWave"}

var underlineOOXML = []string{"none", "single", "words", "double", "thick", "dotted", "dash", "dotDash", "dotDotDash", "wave", "wavyDouble"}

// Underlines lists every style.
func Underlines() []Underline {
	u := make([]Underline, len(underlineNames))
	for i := range u {
		u[i] = Underline(i)
	}
	return u
}

func (u *Underline) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, underlineNames, u) }
func (u Underline) MarshalJSON() ([]byte, error)           { return writeEnum(underlineNames, u) }

// OOXML is the w:u/@w:val name.
func (u Underline) OOXML() string { return enumName(underlineOOXML, u) }

// UnderlineFromOOXML reads a w:u/@w:val name; Word's heavy and long variants map to the nearest
// style, and anything else is single.
func UnderlineFromOOXML(s string) Underline {
	for i, n := range underlineOOXML {
		if n == s {
			return Underline(i)
		}
	}
	switch s {
	case "thickDash", "dashLong", "dashedHeavy", "dashLongHeavy":
		return UnderlineDash
	case "dottedHeavy":
		return UnderlineDotted
	case "wavyHeavy":
		return UnderlineWave
	case "dashDotHeavy":
		return UnderlineDotDash
	case "dashDotDotHeavy":
		return UnderlineDotDotDash
	}
	return UnderlineSingle
}

// Ligatures are the OpenType ligatures a run asks for ([MS-DOCX] w14:ligatures): standard
// (liga), contextual (clig), historical (hlig) and discretional (dlig).
type Ligatures struct {
	Standard     bool `json:"standard"`
	Contextual   bool `json:"contextual"`
	Historical   bool `json:"historical"`
	Discretional bool `json:"discretional"`
}

func (Ligatures) SerdeDefault() {}

// AllLigatures is every combination of the four flags.
func AllLigatures() []Ligatures {
	out := make([]Ligatures, 16)
	for b := range 16 {
		out[b] = Ligatures{b&1 != 0, b&2 != 0, b&4 != 0, b&8 != 0}
	}
	return out
}

// OOXML is the w14:ligatures/@w14:val name: none, all, or the set flags joined in camelCase
// (standardContextual).
func (l Ligatures) OOXML() string {
	var on []string
	for _, f := range []struct {
		on   bool
		name string
	}{{l.Standard, "standard"}, {l.Contextual, "contextual"}, {l.Historical, "historical"}, {l.Discretional, "discretional"}} {
		if f.on {
			on = append(on, f.name)
		}
	}
	switch len(on) {
	case 0:
		return "none"
	case 4:
		return "all"
	}
	var b strings.Builder
	for i, n := range on {
		if i > 0 {
			b.WriteString(strings.ToUpper(n[:1]))
			n = n[1:]
		}
		b.WriteString(n)
	}
	return b.String()
}

// LigaturesFromOOXML reads a name [MS-DOCX] defines.
func LigaturesFromOOXML(s string) (Ligatures, bool) {
	for _, l := range AllLigatures() {
		if l.OOXML() == s {
			return l, true
		}
	}
	return Ligatures{}, false
}

// VertAlign is superscript or subscript.
type VertAlign uint8

// Vertical alignments.
const (
	Baseline VertAlign = iota
	Superscript
	Subscript
)

var vertAlignNames = []string{"baseline", "superscript", "subscript"}

func (v *VertAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, vertAlignNames, v) }
func (v VertAlign) MarshalJSON() ([]byte, error)           { return writeEnum(vertAlignNames, v) }

// Highlight is one of Word's 16 highlighter colours (or none).
type Highlight uint8

// Highlight colours.
const (
	HighlightNone Highlight = iota
	HighlightYellow
	HighlightBrightGreen
	HighlightTurquoise
	HighlightPink
	HighlightBlue
	HighlightRed
	HighlightDarkBlue
	HighlightTeal
	HighlightGreen
	HighlightViolet
	HighlightDarkRed
	HighlightDarkYellow
	HighlightGray50
	HighlightGray25
	HighlightBlack
	HighlightWhite
)

var highlights = []struct {
	name, ooxml, label string
	rgb                RGB
}{
	{"none", "none", "No Color", RGB{}},
	{"yellow", "yellow", "Yellow", RGB{255, 255, 0}},
	{"brightGreen", "green", "Bright Green", RGB{0, 255, 0}},
	{"turquoise", "cyan", "Turquoise", RGB{0, 255, 255}},
	{"pink", "magenta", "Pink", RGB{255, 0, 255}},
	{"blue", "blue", "Blue", RGB{0, 0, 255}},
	{"red", "red", "Red", RGB{255, 0, 0}},
	{"darkBlue", "darkBlue", "Dark Blue", RGB{0, 0, 128}},
	{"teal", "darkCyan", "Teal", RGB{0, 128, 128}},
	{"green", "darkGreen", "Green", RGB{0, 128, 0}},
	{"violet", "darkMagenta", "Violet", RGB{128, 0, 128}},
	{"darkRed", "darkRed", "Dark Red", RGB{128, 0, 0}},
	{"darkYellow", "darkYellow", "Dark Yellow", RGB{128, 128, 0}},
	{"gray50", "darkGray", "Gray-50%", RGB{128, 128, 128}},
	{"gray25", "lightGray", "Gray-25%", RGB{192, 192, 192}},
	{"black", "black", "Black", RGB{0, 0, 0}},
	{"white", "white", "White", RGB{255, 255, 255}},
}

var highlightNames = func() []string {
	n := make([]string, len(highlights))
	for i, h := range highlights {
		n[i] = h.name
	}
	return n
}()

// Highlights lists every colour, none first.
func Highlights() []Highlight {
	h := make([]Highlight, len(highlights))
	for i := range h {
		h[i] = Highlight(i)
	}
	return h
}

func (h *Highlight) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, highlightNames, h) }
func (h Highlight) MarshalJSON() ([]byte, error)           { return writeEnum(highlightNames, h) }

// RGB is the highlighter's colour; false for none.
func (h Highlight) RGB() (RGB, bool) {
	if h == HighlightNone || int(h) >= len(highlights) {
		return RGB{}, false
	}
	return highlights[h].rgb, true
}

// OOXML is the w:highlight/@w:val name.
func (h Highlight) OOXML() string {
	if int(h) >= len(highlights) {
		return ""
	}
	return highlights[h].ooxml
}

// HighlightFromOOXML reads a w:highlight/@w:val name; anything else is none.
func HighlightFromOOXML(s string) Highlight {
	for i, h := range highlights {
		if h.ooxml == s {
			return Highlight(i)
		}
	}
	return HighlightNone
}

// Name is the colour's name in the UI.
func (h Highlight) Name() string {
	if int(h) >= len(highlights) {
		return ""
	}
	return highlights[h].label
}

// CharProps is character formatting (direct or in a style). nil = inherit.
type CharProps struct {
	// Style is the character style id.
	Style *string `json:"style,omitempty"`
	Font  *string `json:"font,omitempty"`
	// SymFont is the symbol's own font (OOXML w:sym/@w:font, ECMA-376 §17.3.3.30), set on a
	// symbol character from a w:sym or Insert › Symbol: it is shown in this font whatever Font
	// is, so changing the text's font keeps the symbol. nil = the run's font.
	SymFont *string `json:"symFont,omitempty"`
	// Size is in points.
	Size           *F32       `json:"size,omitempty"`
	Bold           *bool      `json:"bold,omitempty"`
	Italic         *bool      `json:"italic,omitempty"`
	Underline      *Underline `json:"underline,omitempty"`
	UnderlineColor *RGB       `json:"underlineColor,omitempty"`
	Strike         *bool      `json:"strike,omitempty"`
	DoubleStrike   *bool      `json:"doubleStrike,omitempty"`
	Color          *TextColor `json:"color,omitempty"`
	Highlight      *Highlight `json:"highlight,omitempty"`
	// Shading is the character shading (background fill).
	Shading *RGB `json:"shading,omitempty"`
	// Border is the character border (w:bdr); a BorderNone style is explicitly no border.
	Border    *Border    `json:"border,omitempty"`
	VertAlign *VertAlign `json:"vertAlign,omitempty"`
	Caps      *bool      `json:"caps,omitempty"`
	SmallCaps *bool      `json:"smallCaps,omitempty"`
	Hidden    *bool      `json:"hidden,omitempty"`
	// Spacing is character spacing (expanded/condensed), points.
	Spacing *F32 `json:"spacing,omitempty"`
	// Scale is the horizontal scale, percent (100 = normal).
	Scale *F32 `json:"scale,omitempty"`
	// Position raises (+) or lowers (−) the text, points.
	Position *F32 `json:"position,omitempty"`
	// Kern kerns fonts at or above this size (points); 0 = off.
	Kern *F32 `json:"kern,omitempty"`
	// Ligatures are the OpenType ligatures (w14:ligatures). nil leaves WordCraft's default:
	// standard and contextual ligatures.
	Ligatures *Ligatures `json:"ligatures,omitempty"`
	Outline   *bool      `json:"outline,omitempty"`
	Shadow    *bool      `json:"shadow,omitempty"`
	Emboss    *bool      `json:"emboss,omitempty"`
	Engrave   *bool      `json:"engrave,omitempty"`
	// TextEffects are WordArt text effects: fill, outline, shadow, glow, reflection ([MS-DOCX]
	// w14: run properties). Overlaid effect by effect.
	TextEffects *TextEffects `json:"textEffects,omitempty"`
	// Lang is a BCP 47 language tag (proofing).
	Lang    *string `json:"lang,omitempty"`
	NoProof *bool   `json:"noProof,omitempty"`
	// RTL is a right-to-left run (OOXML w:rtl): its characters are complex script and read right
	// to left.
	RTL *bool `json:"rtl,omitempty"`
	// CS formats the whole run with the complex-script properties below (OOXML w:cs).
	CS *bool `json:"cs,omitempty"`
	// FontCS is the complex-script font (Arabic, Persian, Hebrew…; w:rFonts/@w:cs). nil = Font.
	FontCS *string `json:"fontCs,omitempty"`
	// FontEA is the East Asian font (Chinese, Japanese and Korean text; w:rFonts/@w:eastAsia).
	// nil = inherit, then Font.
	FontEA *string `json:"fontEa,omitempty"`
	// SizeCS is the complex-script size, points (w:szCs). nil leaves the inherited value.
	SizeCS *F32 `json:"sizeCs,omitempty"`
	// BoldCS and ItalicCS are complex-script bold and italic (w:bCs, w:iCs).
	BoldCS   *bool `json:"boldCs,omitempty"`
	ItalicCS *bool `json:"italicCs,omitempty"`
	// LangBidi is the BCP 47 language of complex-script text (w:lang/@w:bidi, e.g. fa-IR).
	LangBidi *string `json:"langBidi,omitempty"`
	// EastAsianLayout is East Asian typography: two lines in one and horizontal in vertical
	// (w:eastAsianLayout). Kept for round trip; layout doesn't apply it yet.
	EastAsianLayout *EastAsianLayout `json:"eastAsianLayout,omitempty"`
	// Link is a hyperlink target: a URL, or #bookmark for an internal link.
	Link *string `json:"link,omitempty"`
	// Ins is a tracked insertion: index into Document revisions.
	Ins *uint32 `json:"ins,omitempty"`
	// Del is a tracked deletion: index into Document revisions.
	Del *uint32 `json:"del,omitempty"`
	// FmtChange is a tracked formatting change (w:rPrChange): the formatting before it.
	FmtChange *PropChange[CharProps] `json:"fmtChange,omitempty"`
}

func (CharProps) SerdeDefault() {}

// EastAsianLayout holds the East Asian typography settings of a run (ISO/IEC 29500-1 §17.3.2.10,
// w:eastAsianLayout): two lines in one sets the run's text on two sub-lines inside one line
// (warichu), horizontal in vertical turns it 90° to read across a vertical line
// (tate-chu-yoko). Each attribute is nil when the file doesn't say.
type EastAsianLayout struct {
	// ID is the East Asian typography run id (w:id).
	ID *int32 `json:"id,omitempty"`
	// Combine is two lines in one (w:combine).
	Combine *bool `json:"combine,omitempty"`
	// CombineBrackets are the brackets around two lines in one (w:combineBrackets).
	CombineBrackets *CombineBrackets `json:"combineBrackets,omitempty"`
	// Vert is horizontal in vertical (w:vert).
	Vert *bool `json:"vert,omitempty"`
	// VertCompress compresses horizontal-in-vertical text to the line height (w:vertCompress).
	VertCompress *bool `json:"vertCompress,omitempty"`
}

func (EastAsianLayout) SerdeDefault() {}

// CombineBrackets are the brackets around two lines in one (ST_CombineBrackets).
type CombineBrackets uint8

// Brackets.
const (
	BracketsNone CombineBrackets = iota
	BracketsRound
	BracketsSquare
	BracketsAngle
	BracketsCurly
)

var combineBracketsNames = []string{"none", "round", "square", "angle", "curly"}

func (b *CombineBrackets) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, combineBracketsNames, b)
}
func (b CombineBrackets) MarshalJSON() ([]byte, error) { return writeEnum(combineBracketsNames, b) }

// OOXML is the w:combineBrackets value.
func (b CombineBrackets) OOXML() string { return enumName(combineBracketsNames, b) }

// CombineBracketsFromOOXML reads a value the schema has.
func CombineBracketsFromOOXML(s string) (CombineBrackets, bool) {
	for i, n := range combineBracketsNames {
		if n == s {
			return CombineBrackets(i), true
		}
	}
	return 0, false
}

// PropChange is a tracked formatting change (ECMA-376 §17.13.5: w:rPrChange, w:pPrChange,
// w:tblPrChange, w:trPrChange, w:tcPrChange, w:sectPrChange): the properties as they were before
// the change, and the revision (author, date) that made it.
type PropChange[T any] struct {
	// Rev is an index into Document revisions.
	Rev uint32 `json:"rev"`
	// Old are the properties before the change (formatting only: no revision or link marks).
	Old T `json:"old"`
}

// NumChange is a tracked change of a paragraph's list numbering (w:numberingChange, kept for
// round trip): Original is the number as it was shown before the change.
type NumChange struct {
	// Rev is an index into Document revisions.
	Rev      uint32 `json:"rev"`
	Original string `json:"original"`
}

func (NumChange) SerdeDefault() {}

// Unlinked is the same formatting outside a hyperlink: no link target, and no "Hyperlink"
// character style that came with it. What a paragraph mark or text typed next to a link gets.
func (c CharProps) Unlinked() CharProps {
	if c.Link != nil {
		c.Link = nil
		if c.Style != nil && *c.Style == "Hyperlink" {
			c.Style = nil
		}
	}
	return c
}

// Minus is the fields of c that differ from base (equal ones unset): what c adds when overlaid on
// base. As in Rust, FontEA, TextEffects and FmtChange are kept whatever base holds.
func (c CharProps) Minus(base *CharProps) CharProps {
	dropSame(&c.Style, base.Style)
	dropSame(&c.Font, base.Font)
	dropSame(&c.SymFont, base.SymFont)
	dropSame(&c.Size, base.Size)
	dropSame(&c.Bold, base.Bold)
	dropSame(&c.Italic, base.Italic)
	dropSame(&c.Underline, base.Underline)
	dropSame(&c.UnderlineColor, base.UnderlineColor)
	dropSame(&c.Strike, base.Strike)
	dropSame(&c.DoubleStrike, base.DoubleStrike)
	dropSame(&c.Color, base.Color)
	dropSame(&c.Highlight, base.Highlight)
	dropSame(&c.Shading, base.Shading)
	dropSame(&c.Border, base.Border)
	dropSame(&c.VertAlign, base.VertAlign)
	dropSame(&c.Caps, base.Caps)
	dropSame(&c.SmallCaps, base.SmallCaps)
	dropSame(&c.Hidden, base.Hidden)
	dropSame(&c.Spacing, base.Spacing)
	dropSame(&c.Scale, base.Scale)
	dropSame(&c.Position, base.Position)
	dropSame(&c.Kern, base.Kern)
	dropSame(&c.Outline, base.Outline)
	dropSame(&c.Shadow, base.Shadow)
	dropSame(&c.Emboss, base.Emboss)
	dropSame(&c.Engrave, base.Engrave)
	dropSame(&c.Lang, base.Lang)
	dropSame(&c.NoProof, base.NoProof)
	dropSame(&c.RTL, base.RTL)
	dropSame(&c.CS, base.CS)
	dropSame(&c.FontCS, base.FontCS)
	dropSame(&c.SizeCS, base.SizeCS)
	dropSame(&c.BoldCS, base.BoldCS)
	dropSame(&c.ItalicCS, base.ItalicCS)
	dropSame(&c.LangBidi, base.LangBidi)
	dropSame(&c.EastAsianLayout, base.EastAsianLayout)
	dropSame(&c.Link, base.Link)
	dropSame(&c.Ins, base.Ins)
	dropSame(&c.Del, base.Del)
	dropSame(&c.Ligatures, base.Ligatures)
	return c
}

// Overlay applies every set field of patch on top of c.
//
// Plain and complex-script properties inherit independently. Commands that intend to format both
// scripts must set both values explicitly.
func (c *CharProps) Overlay(patch *CharProps) {
	set(&c.Style, patch.Style)
	set(&c.Font, patch.Font)
	set(&c.SymFont, patch.SymFont)
	set(&c.Size, patch.Size)
	set(&c.Bold, patch.Bold)
	set(&c.Italic, patch.Italic)
	set(&c.Underline, patch.Underline)
	set(&c.UnderlineColor, patch.UnderlineColor)
	set(&c.Strike, patch.Strike)
	set(&c.DoubleStrike, patch.DoubleStrike)
	set(&c.Color, patch.Color)
	set(&c.Highlight, patch.Highlight)
	set(&c.Shading, patch.Shading)
	set(&c.Border, patch.Border)
	set(&c.VertAlign, patch.VertAlign)
	set(&c.Caps, patch.Caps)
	set(&c.SmallCaps, patch.SmallCaps)
	set(&c.Hidden, patch.Hidden)
	set(&c.Spacing, patch.Spacing)
	set(&c.Scale, patch.Scale)
	set(&c.Position, patch.Position)
	set(&c.Kern, patch.Kern)
	set(&c.Outline, patch.Outline)
	set(&c.Shadow, patch.Shadow)
	set(&c.Emboss, patch.Emboss)
	set(&c.Engrave, patch.Engrave)
	set(&c.Lang, patch.Lang)
	set(&c.NoProof, patch.NoProof)
	set(&c.RTL, patch.RTL)
	set(&c.CS, patch.CS)
	set(&c.FontCS, patch.FontCS)
	set(&c.FontEA, patch.FontEA)
	set(&c.SizeCS, patch.SizeCS)
	set(&c.BoldCS, patch.BoldCS)
	set(&c.ItalicCS, patch.ItalicCS)
	set(&c.LangBidi, patch.LangBidi)
	set(&c.EastAsianLayout, patch.EastAsianLayout)
	set(&c.Link, patch.Link)
	set(&c.Ins, patch.Ins)
	set(&c.Del, patch.Del)
	set(&c.FmtChange, patch.FmtChange)
	set(&c.Ligatures, patch.Ligatures)
	if patch.TextEffects != nil {
		var te TextEffects
		if c.TextEffects != nil {
			te = *c.TextEffects
		}
		te.Overlay(patch.TextEffects)
		c.TextEffects = &te
	}
}

// Overlaid is c with patch applied.
func (c CharProps) Overlaid(patch *CharProps) CharProps {
	c.Overlay(patch)
	return c
}

// Equal compares as Rust's PartialEq does.
func (c CharProps) Equal(o CharProps) bool { return equal(c, o) }

// IsEmpty reports whether nothing is set.
func (c CharProps) IsEmpty() bool { return equal(c, CharProps{}) }

// ShownFont is the font a character of this run is shown in: the symbol's own font when it has
// one (SymFont, unless blank), else the run's font.
func (c CharProps) ShownFont() (string, bool) {
	if c.SymFont != nil && strings.TrimSpace(*c.SymFont) != "" {
		return *c.SymFont, true
	}
	if c.Font != nil {
		return *c.Font, true
	}
	return "", false
}

// WithoutSymbol is the same formatting for text typed or painted next to a symbol: without the
// symbol's own font.
func (c CharProps) WithoutSymbol() CharProps {
	c.SymFont = nil
	return c
}

// Cleared clears direct formatting but keeps what isn't formatting: links, revisions and a
// symbol's own font (the character style is cleared too, as Word's Clear Formatting does).
func (c CharProps) Cleared() CharProps {
	return CharProps{Link: c.Link, Ins: c.Ins, Del: c.Del, FmtChange: c.FmtChange, SymFont: c.SymFont}
}

// Formatting is just the formatting: without the link, revision marks and tracked formatting
// change.
func (c CharProps) Formatting() CharProps {
	c.Link, c.Ins, c.Del, c.FmtChange = nil, nil, nil, nil
	return c
}

// Align is a paragraph alignment. It is logical: Left is the start edge (ISO 29500 start).
type Align uint8

// Alignments.
const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	AlignJustify
	AlignDistribute
)

var alignNames = []string{"left", "center", "right", "justify", "distribute"}

func (a *Align) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, alignNames, a) }
func (a Align) MarshalJSON() ([]byte, error)           { return writeEnum(alignNames, a) }

// Visual is the alignment as seen on the page, or the logical one for a visual choice, in a
// paragraph that reads right to left when rtl (Left and Right swap; the swap is its own inverse).
func (a Align) Visual(rtl bool) Align {
	switch {
	case a == AlignLeft && rtl:
		return AlignRight
	case a == AlignRight && rtl:
		return AlignLeft
	}
	return a
}

// LineRule is how LineSpacing's value is read.
type LineRule uint8

// Line spacing rules.
const (
	// Multiple of single spacing (1.0 = single, 1.15, 1.5, 2.0 = double).
	LineMultiple LineRule = iota
	// AtLeast this many points.
	LineAtLeast
	// Exactly this many points.
	LineExactly
)

var lineRuleNames = []string{"multiple", "atLeast", "exactly"}

// LineSpacing is a paragraph's line spacing, written {"rule": …, "value": …}.
type LineSpacing struct {
	Rule  LineRule
	Value F32
}

// SingleSpacing is the default: Multiple(1.0).
var SingleSpacing = LineSpacing{LineMultiple, 1}

// MarshalJSON writes the rule and value.
func (l LineSpacing) MarshalJSON() ([]byte, error) {
	name, err := writeEnum(lineRuleNames, l.Rule)
	if err != nil {
		return nil, err
	}
	return []byte(`{"rule":` + string(name) + `,"value":` + serdejson.FormatF32(float32(l.Value)) + `}`), nil
}

// DecodeSerde reads serde's adjacently tagged form.
func (l *LineSpacing) DecodeSerde(d serdejson.Decoder) error {
	var v float64
	i, err := serdejson.AdjacentlyTagged(d, "rule", "value", lineRuleNames, func(int) bool { return false }, func(_ int, d serdejson.Decoder) error {
		var err error
		v, err = d.Float(32)
		return err
	})
	if err != nil {
		return err
	}
	*l = LineSpacing{LineRule(i), f32(v)}
	return nil
}

// TabAlign is a tab stop's alignment.
type TabAlign uint8

// Tab alignments.
const (
	TabLeft TabAlign = iota
	TabCenter
	TabRight
	TabDecimal
	TabBar
	// TabClear clears an inherited stop at this position.
	TabClear
)

var tabAlignNames = []string{"left", "center", "right", "decimal", "bar", "clear"}

func (a *TabAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, tabAlignNames, a) }
func (a TabAlign) MarshalJSON() ([]byte, error)           { return writeEnum(tabAlignNames, a) }

// TabLeader is what fills a tab's space.
type TabLeader uint8

// Tab leaders.
const (
	LeaderNone TabLeader = iota
	LeaderDot
	LeaderHyphen
	LeaderUnderscore
	LeaderMiddleDot
)

var tabLeaderNames = []string{"none", "dot", "hyphen", "underscore", "middleDot"}

func (l *TabLeader) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, tabLeaderNames, l) }
func (l TabLeader) MarshalJSON() ([]byte, error)           { return writeEnum(tabLeaderNames, l) }

// Char is the leader character; false for none.
func (l TabLeader) Char() (rune, bool) {
	switch l {
	case LeaderDot:
		return '.', true
	case LeaderHyphen:
		return '-', true
	case LeaderUnderscore:
		return '_', true
	case LeaderMiddleDot:
		return '·', true
	}
	return 0, false
}

// TabPosEps: two tab positions closer than this (points) are the same stop: para.tabs replaces
// at this tolerance, and the Tabs dialog and the ruler merge and detect no-ops with it.
const TabPosEps = 0.5

// TabStop is a tab stop.
type TabStop struct {
	// Pos is in points from the paragraph's left indent origin (the text column's left edge).
	Pos    F32       `json:"pos"`
	Align  TabAlign  `json:"align" serde:"default"`
	Leader TabLeader `json:"leader" serde:"default"`
}

// BorderStyle is a border line's style.
type BorderStyle uint8

// Border styles.
const (
	BorderNone BorderStyle = iota
	BorderSingle
	BorderDouble
	BorderDotted
	BorderDashed
	BorderThick
	BorderTriple
	BorderDotDash
	BorderWave
)

var borderStyleNames = []string{"none", "single", "double", "dotted", "dashed", "thick", "triple", "dotDash", "wave"}

var borderStyleOOXML = []string{"nil", "single", "double", "dotted", "dashed", "thick", "triple", "dotDash", "wave"}

func (b *BorderStyle) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, borderStyleNames, b) }
func (b BorderStyle) MarshalJSON() ([]byte, error)           { return writeEnum(borderStyleNames, b) }

// OOXML is the ST_Border name.
func (b BorderStyle) OOXML() string { return enumName(borderStyleOOXML, b) }

// BorderStyleFromOOXML reads an ST_Border name, mapping the styles WordCraft doesn't draw to the
// nearest one it does; anything else is single.
func BorderStyleFromOOXML(s string) BorderStyle {
	switch s {
	case "nil", "none":
		return BorderNone
	case "double":
		return BorderDouble
	case "dotted":
		return BorderDotted
	case "dashed", "dashSmallGap":
		return BorderDashed
	case "thick":
		return BorderThick
	case "triple":
		return BorderTriple
	case "dotDash", "dotDotDash":
		return BorderDotDash
	case "wave", "doubleWave":
		return BorderWave
	}
	return BorderSingle
}

// Border is one border line.
type Border struct {
	Style BorderStyle `json:"style"`
	// Width is the line width, points.
	Width F32  `json:"width"`
	Color *RGB `json:"color"`
	// Space is the distance from the text, points.
	Space F32 `json:"space"`
}

func (Border) SerdeDefault() {}

// SingleBorder is a single line width points wide.
func SingleBorder(width F32) Border { return Border{Style: BorderSingle, Width: width} }

// IsVisible reports whether the line is drawn.
func (b Border) IsVisible() bool { return b.Style != BorderNone && b.Width > 0 }

// Borders are the borders on the four sides (plus inner borders for tables and runs of bordered
// paragraphs).
type Borders struct {
	Top    *Border `json:"top"`
	Left   *Border `json:"left"`
	Bottom *Border `json:"bottom"`
	Right  *Border `json:"right"`
	// Between is between paragraphs, or inside horizontal (tables).
	Between *Border `json:"between"`
	// InsideV is inside vertical (tables).
	InsideV *Border `json:"inside_v"`
}

func (Borders) SerdeDefault() {}

// AllBorders is b on every side and inside.
func AllBorders(b Border) Borders {
	return Borders{ptr(b), ptr(b), ptr(b), ptr(b), ptr(b), ptr(b)}
}

// BoxBorders is b on the four sides.
func BoxBorders(b Border) Borders {
	return Borders{Top: ptr(b), Left: ptr(b), Bottom: ptr(b), Right: ptr(b)}
}

// AnyVisible reports whether any line is drawn.
func (b Borders) AnyVisible() bool {
	for _, s := range []*Border{b.Top, b.Left, b.Bottom, b.Right, b.Between, b.InsideV} {
		if s != nil && s.IsVisible() {
			return true
		}
	}
	return false
}

// Overlay applies every set side of patch on top of b.
func (b *Borders) Overlay(patch *Borders) {
	set(&b.Top, patch.Top)
	set(&b.Left, patch.Left)
	set(&b.Bottom, patch.Bottom)
	set(&b.Right, patch.Right)
	set(&b.Between, patch.Between)
	set(&b.InsideV, patch.InsideV)
}

// NumRef refers to a numbering definition and level.
type NumRef struct {
	// Num is a Numbering nums id; 0 removes numbering inherited from a style.
	Num   uint32 `json:"num"`
	Level uint8  `json:"level"`
}

// TextFrame is a text frame's placement (w:framePr, ECMA-376 §17.3.1.11), in points. Offsets
// apply when the matching alignment is unset; anchors and the wrap keep OOXML's names.
type TextFrame struct {
	// W is the width; nil fits the text.
	W *F32 `json:"w"`
	// H is the height, exact when HExact, else at least; nil fits the text.
	H      *F32 `json:"h"`
	HExact bool `json:"hExact"`
	X      F32  `json:"x"`
	Y      F32  `json:"y"`
	// XAlign is left, center, right, inside or outside.
	XAlign *string `json:"xAlign"`
	// YAlign is top, center, bottom, inside or outside (inline is no alignment).
	YAlign *string `json:"yAlign"`
	// HAnchor is text (the column), margin or page.
	HAnchor string `json:"hAnchor"`
	// VAnchor is text (the paragraph), margin or page.
	VAnchor string `json:"vAnchor"`
	// Wrap is around, auto, notBeside, none, tight or through.
	Wrap string `json:"wrap"`
	// HSpace and VSpace are the distance from the text left and right, and above and below.
	HSpace F32 `json:"hSpace"`
	VSpace F32 `json:"vSpace"`
}

func (TextFrame) SerdeDefault() {}

// ParaProps is paragraph formatting. nil = inherit.
type ParaProps struct {
	// Style is the paragraph style id.
	Style *string `json:"style,omitempty"`
	Align *Align  `json:"align,omitempty"`
	// IndentLeft is the left indent, points.
	IndentLeft  *F32 `json:"indentLeft,omitempty"`
	IndentRight *F32 `json:"indentRight,omitempty"`
	// IndentFirst is the first-line indent, points; negative = hanging.
	IndentFirst *F32 `json:"indentFirst,omitempty"`
	// IndentLeftChars is the left (start) indent in characters (w:leftChars / w:startChars,
	// stored by Word in hundredths). A non-zero value wins over IndentLeft; one character is the
	// size of the paragraph mark's font. 0 cancels an inherited value. Common in East Asian
	// documents.
	IndentLeftChars *F32 `json:"indentLeftChars,omitempty"`
	// IndentRightChars is the right (end) indent in characters (w:rightChars / w:endChars).
	IndentRightChars *F32 `json:"indentRightChars,omitempty"`
	// IndentFirstChars is the first-line indent in characters (w:firstLineChars; negative =
	// w:hangingChars).
	IndentFirstChars *F32 `json:"indentFirstChars,omitempty"`
	SpaceBefore      *F32 `json:"spaceBefore,omitempty"`
	SpaceAfter       *F32 `json:"spaceAfter,omitempty"`
	// SpaceBeforeAuto is HTML-style automatic space before (w:beforeAutospacing): 14 pt, none at
	// the top of the document or of a table cell, and collapsing with the space after the
	// paragraph above.
	SpaceBeforeAuto *bool `json:"spaceBeforeAuto,omitempty"`
	// SpaceAfterAuto is HTML-style automatic space after (w:afterAutospacing): 14 pt, none at the
	// end of a cell.
	SpaceAfterAuto *bool        `json:"spaceAfterAuto,omitempty"`
	LineSpacing    *LineSpacing `json:"lineSpacing,omitempty"`
	// ContextualSpacing ignores space before/after between paragraphs of the same style.
	ContextualSpacing *bool `json:"contextualSpacing,omitempty"`
	// MirrorIndents (w:mirrorIndents, ECMA-376 §17.3.1.18): the left and right indents are the
	// inside and outside ones, so they swap sides on even (left-hand) pages.
	MirrorIndents   *bool `json:"mirrorIndents,omitempty"`
	KeepNext        *bool `json:"keepNext,omitempty"`
	KeepLines       *bool `json:"keepLines,omitempty"`
	PageBreakBefore *bool `json:"pageBreakBefore,omitempty"`
	WidowControl    *bool `json:"widowControl,omitempty"`
	// OutlineLevel: 0..=8 = Level 1..9; 9 = body text.
	OutlineLevel *uint8  `json:"outlineLevel,omitempty"`
	Numbering    *NumRef `json:"numbering,omitempty"`
	// Tabs are the tab stops (they replace the inherited list; TabClear stops remove inherited
	// ones).
	Tabs                *Vec[TabStop] `json:"tabs,omitempty"`
	Shading             *RGB          `json:"shading,omitempty"`
	Borders             *Borders      `json:"borders,omitempty"`
	SuppressHyphens     *bool         `json:"suppressHyphens,omitempty"`
	SuppressLineNumbers *bool         `json:"suppressLineNumbers,omitempty"`
	Bidi                *bool         `json:"bidi,omitempty"`
	// DropCap is the number of lines to drop (0 = none); it applies to the first character(s).
	DropCap *uint8 `json:"dropCap,omitempty"`
	// DropCapMargin is drop cap "In margin" (w:dropCap="margin"): the letter hangs in the left
	// margin and the text keeps its indent. Unset = dropped into the text.
	DropCapMargin *bool `json:"dropCapMargin,omitempty"`
	// DropCapDistance is the drop cap's distance from the text, in points (w:hSpace). Unset = 0.
	DropCapDistance *F32 `json:"dropCapDistance,omitempty"`
	// DropCapFont is the drop cap letter's font family (the cap run's w:rFonts). Unset = the
	// text's font.
	DropCapFont *string `json:"dropCapFont,omitempty"`
	// Frame is a text frame (w:framePr other than a drop cap): the paragraph is placed apart
	// from the text, which may wrap around it — a footer's page number in a frame, for instance.
	Frame *TextFrame `json:"frame,omitempty"`
	// Kinsoku is Asian typography (w:kinsoku): East Asian line-breaking rules — no line starts
	// with closing punctuation such as 、。」 or ends with opening punctuation such as 「（.
	// Unset = on.
	Kinsoku *bool `json:"kinsoku,omitempty"`
	// WordWrap (w:wordWrap): Latin words wrap whole; false lets them break at any character
	// (Word's "Allow Latin text to wrap in the middle of a word"). Unset = on.
	WordWrap *bool `json:"wordWrap,omitempty"`
	// OverflowPunct (w:overflowPunct): punctuation may hang past the line end. Unset = on.
	OverflowPunct *bool `json:"overflowPunct,omitempty"`
	// TopLinePunct (w:topLinePunct): compress punctuation at the start of a line. Unset = off.
	TopLinePunct *bool `json:"topLinePunct,omitempty"`
	// AutoSpaceDE (w:autoSpaceDE): automatic space between Asian and Latin text. Unset = on.
	AutoSpaceDE *bool `json:"autoSpaceDE,omitempty"`
	// AutoSpaceDN (w:autoSpaceDN): automatic space between Asian text and numbers. Unset = on.
	AutoSpaceDN *bool `json:"autoSpaceDN,omitempty"`
	// NumChange is a tracked change of the list numbering (w:numberingChange).
	NumChange *NumChange `json:"numChange,omitempty"`
	// FmtChange is a tracked formatting change (w:pPrChange): the paragraph properties before it.
	FmtChange *PropChange[ParaProps] `json:"fmtChange,omitempty"`
	// SnapToGrid (w:snapToGrid): lines snap to the section's document grid. Unset = on.
	SnapToGrid *bool `json:"snapToGrid,omitempty"`
}

func (ParaProps) SerdeDefault() {}

// Overlay applies every set field of patch on top of p (not the tracked changes).
func (p *ParaProps) Overlay(patch *ParaProps) {
	set(&p.Style, patch.Style)
	set(&p.Align, patch.Align)
	set(&p.IndentLeft, patch.IndentLeft)
	set(&p.IndentRight, patch.IndentRight)
	set(&p.IndentFirst, patch.IndentFirst)
	set(&p.IndentLeftChars, patch.IndentLeftChars)
	set(&p.IndentRightChars, patch.IndentRightChars)
	set(&p.IndentFirstChars, patch.IndentFirstChars)
	set(&p.SpaceBefore, patch.SpaceBefore)
	set(&p.SpaceAfter, patch.SpaceAfter)
	set(&p.SpaceBeforeAuto, patch.SpaceBeforeAuto)
	set(&p.SpaceAfterAuto, patch.SpaceAfterAuto)
	set(&p.LineSpacing, patch.LineSpacing)
	set(&p.ContextualSpacing, patch.ContextualSpacing)
	set(&p.MirrorIndents, patch.MirrorIndents)
	set(&p.KeepNext, patch.KeepNext)
	set(&p.KeepLines, patch.KeepLines)
	set(&p.PageBreakBefore, patch.PageBreakBefore)
	set(&p.WidowControl, patch.WidowControl)
	set(&p.OutlineLevel, patch.OutlineLevel)
	set(&p.Numbering, patch.Numbering)
	set(&p.Tabs, patch.Tabs)
	set(&p.Shading, patch.Shading)
	set(&p.Borders, patch.Borders)
	set(&p.SuppressHyphens, patch.SuppressHyphens)
	set(&p.SuppressLineNumbers, patch.SuppressLineNumbers)
	set(&p.Bidi, patch.Bidi)
	set(&p.DropCap, patch.DropCap)
	set(&p.DropCapMargin, patch.DropCapMargin)
	set(&p.DropCapDistance, patch.DropCapDistance)
	set(&p.DropCapFont, patch.DropCapFont)
	set(&p.Frame, patch.Frame)
	set(&p.Kinsoku, patch.Kinsoku)
	set(&p.WordWrap, patch.WordWrap)
	set(&p.OverflowPunct, patch.OverflowPunct)
	set(&p.TopLinePunct, patch.TopLinePunct)
	set(&p.AutoSpaceDE, patch.AutoSpaceDE)
	set(&p.AutoSpaceDN, patch.AutoSpaceDN)
	set(&p.SnapToGrid, patch.SnapToGrid)
}

// Overlaid is p with patch applied.
func (p ParaProps) Overlaid(patch *ParaProps) ParaProps {
	p.Overlay(patch)
	return p
}

// Equal compares as Rust's PartialEq does.
func (p ParaProps) Equal(o ParaProps) bool { return equal(p, o) }

// IsEmpty reports whether nothing is set.
func (p ParaProps) IsEmpty() bool { return equal(p, ParaProps{}) }

// CancelCharIndents, after setting indents in points, keeps the indents in characters (left,
// right, first line) an inherited style may set from winning over them, as Word does
// (w:firstLineChars="0").
func (p *ParaProps) CancelCharIndents(left, right, first bool) {
	for _, c := range []struct {
		on    bool
		chars **F32
	}{{left, &p.IndentLeftChars}, {right, &p.IndentRightChars}, {first, &p.IndentFirstChars}} {
		if c.on {
			*c.chars = ptr(F32(0))
		}
	}
}

// Formatting is just the formatting: without the tracked changes.
func (p ParaProps) Formatting() ParaProps {
	p.NumChange, p.FmtChange = nil, nil
	return p
}

// VAlign is a vertical alignment.
type VAlign uint8

// Vertical alignments.
const (
	VAlignTop VAlign = iota
	VAlignCenter
	VAlignBottom
)

var vAlignNames = []string{"top", "center", "bottom"}

func (v *VAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, vAlignNames, v) }
func (v VAlign) MarshalJSON() ([]byte, error)           { return writeEnum(vAlignNames, v) }

// VAlignFromAnchor reads where a text box's text sits between its top and bottom (Align Text;
// a:bodyPr/@anchor, ST_TextAnchoringType). Justified and distributed anchoring read as top and
// middle.
func VAlignFromAnchor(v string) VAlign {
	switch v {
	case "ctr", "dist":
		return VAlignCenter
	case "b":
		return VAlignBottom
	}
	return VAlignTop
}

// AnchorOOXML is the ST_TextAnchoringType value for a.
func AnchorOOXML(a VAlign) string {
	switch a {
	case VAlignCenter:
		return "ctr"
	case VAlignBottom:
		return "b"
	}
	return "t"
}

// TextDirection is which way a table cell's text runs (w:textDirection, ECMA-376 §17.4.72).
type TextDirection uint8

// Text directions.
const (
	// Horizontal: left to right, lines stacking downwards.
	Horizontal TextDirection = iota
	// Down: turned 90° clockwise: lines read top to bottom and stack from right to left (tbRl).
	Down
	// Up: turned 90° counter-clockwise: lines read bottom to top and stack from left to right
	// (btLr).
	Up
)

var textDirectionNames = []string{"horizontal", "down", "up"}

func (t *TextDirection) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, textDirectionNames, t)
}
func (t TextDirection) MarshalJSON() ([]byte, error) { return writeEnum(textDirectionNames, t) }

// TextDirectionFromOOXML reads an ST_TextDirection value (transitional and strict names). The
// East Asian vertical layouts turn like Down; unknown values are horizontal.
func TextDirectionFromOOXML(v string) TextDirection {
	switch v {
	case "tbRl", "tbRlV", "tbLrV", "rl", "rlV", "lrV":
		return Down
	case "btLr", "lr":
		return Up
	}
	return Horizontal
}

// OOXML is the transitional OOXML name.
func (t TextDirection) OOXML() string {
	switch t {
	case Down:
		return "tbRl"
	case Up:
		return "btLr"
	}
	return "lrTb"
}

// Next is Word's Text Direction button: horizontal → down → up → horizontal.
func (t TextDirection) Next() TextDirection {
	switch t {
	case Horizontal:
		return Down
	case Down:
		return Up
	}
	return Horizontal
}

// IsTurned reports whether the text isn't horizontal.
func (t TextDirection) IsTurned() bool { return t != Horizontal }

// TextVert is which way a text box's text runs (Shape Format › Text › Text Direction; DrawingML
// a:bodyPr/@vert, ECMA-376 §21.1.10.83 ST_TextVerticalType).
type TextVert uint8

// Text box directions.
const (
	// Horz: upright, lines stacking downwards (horz).
	Horz TextVert = iota
	// Vert: turned 90° clockwise (vert).
	Vert
	// Vert270: turned 270° (vert270).
	Vert270
	// Stacked: upright letters stacked one above the other (wordArtVert).
	Stacked
)

var textVertNames = []string{"horz", "vert", "vert270", "stacked"}

func (t *TextVert) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, textVertNames, t) }
func (t TextVert) MarshalJSON() ([]byte, error)           { return writeEnum(textVertNames, t) }

// TextVertFromOOXML reads an ST_TextVerticalType value; the East Asian and Mongolian vertical
// layouts turn like vert, unknown values are horizontal.
func TextVertFromOOXML(v string) TextVert {
	switch v {
	case "vert", "eaVert", "mongolianVert":
		return Vert
	case "vert270":
		return Vert270
	case "wordArtVert", "wordArtVertRtl":
		return Stacked
	}
	return Horz
}

// OOXML is the ST_TextVerticalType value.
func (t TextVert) OOXML() string {
	switch t {
	case Vert:
		return "vert"
	case Vert270:
		return "vert270"
	case Stacked:
		return "wordArtVert"
	}
	return "horz"
}

// Turn is how its lines turn on the page (stacked text stays upright).
func (t TextVert) Turn() TextDirection {
	switch t {
	case Vert:
		return Down
	case Vert270:
		return Up
	}
	return Horizontal
}

// TableProps are table-wide properties.
type TableProps struct {
	Style *string `json:"style"`
	// Width is the preferred width: points, or nil for automatic.
	Width *F32 `json:"width"`
	// WidthPct is the preferred width as a percentage of the text column (it overrides Width).
	WidthPct *F32   `json:"widthPct"`
	Align    *Align `json:"align"`
	// Indent is the indent from the left margin, points.
	Indent  *F32     `json:"indent"`
	Borders *Borders `json:"borders"`
	// CellMargins are the default cell margins (top, left, bottom, right), points.
	CellMargins *[4]F32 `json:"cellMargins"`
	// Fixed column widths (no auto-fit).
	Fixed bool `json:"fixed"`
	// Look holds the table style options: header row, total row, banded rows, first column,
	// last column, banded columns.
	Look    TableLook `json:"look"`
	Shading *RGB      `json:"shading"`
	// Caption is the alternative text's title (w:tblCaption).
	Caption *string `json:"caption"`
	// Description is the alternative text's description (w:tblDescription).
	Description *string `json:"description,omitempty"`
	// Float is the floating placement (w:tblpPr); nil for a table in the text flow.
	Float *TableFloat `json:"float,omitempty"`
	// FmtChange is a tracked formatting change (w:tblPrChange).
	FmtChange *PropChange[TableProps] `json:"fmtChange,omitempty"`
	// BidiVisual is a right-to-left table (w:bidiVisual): the first logical column stands at the
	// right edge, cell margins and left/right borders mirror, and the alignment and indent are
	// measured from the right margin.
	BidiVisual bool `json:"bidiVisual,omitempty"`
}

func (TableProps) SerdeDefault() {}

// SetDefaults sets the Rust Default: the default table look.
func (t *TableProps) SetDefaults() { *t = TableProps{Look: DefaultTableLook} }

// NewTableProps is the default (Rust's TableProps::default()).
func NewTableProps() TableProps { return TableProps{Look: DefaultTableLook} }

// TableFloat is where a floating table sits; the text after it wraps around it.
type TableFloat struct {
	// HRel is what X is measured from: the column, the margin or the page.
	HRel Anchor `json:"hRel"`
	// VRel is what Y is measured from: the text where the table stands (paragraph), the margin
	// or the page.
	VRel Anchor `json:"vRel"`
	// X and Y are offsets, points (used when the matching alignment is nil).
	X      F32         `json:"x"`
	Y      F32         `json:"y"`
	HAlign *FloatAlign `json:"hAlign"`
	VAlign *FloatAlign `json:"vAlign"`
	// Dist is the distance from surrounding text: left, top, right, bottom (points).
	Dist [4]F32 `json:"dist"`
	// Overlap: it may overlap other floating tables (w:tblOverlap).
	Overlap bool `json:"overlap"`
}

func (TableFloat) SerdeDefault() {}

// DistFromText is the distances from text, finite and clamped (left, top, right, bottom).
func (f TableFloat) DistFromText() [4]F32 {
	var out [4]F32
	for i, v := range f.Dist {
		if finite(v) {
			out[i] = min(max(v, 0), 1584)
		}
	}
	return out
}

// TableLook holds the table style options.
type TableLook struct {
	HeaderRow     bool `json:"headerRow"`
	TotalRow      bool `json:"totalRow"`
	BandedRows    bool `json:"bandedRows"`
	FirstColumn   bool `json:"firstColumn"`
	LastColumn    bool `json:"lastColumn"`
	BandedColumns bool `json:"bandedColumns"`
}

// DefaultTableLook: header row, banded rows and first column.
var DefaultTableLook = TableLook{HeaderRow: true, BandedRows: true, FirstColumn: true}

func (TableLook) SerdeDefault() {}

// SetDefaults sets DefaultTableLook.
func (l *TableLook) SetDefaults() { *l = DefaultTableLook }

// HeightRule is how a row's height is read.
type HeightRule uint8

// Height rules.
const (
	HeightAuto HeightRule = iota
	HeightAtLeast
	HeightExact
)

var heightRuleNames = []string{"auto", "atLeast", "exact"}

func (h *HeightRule) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, heightRuleNames, h) }
func (h HeightRule) MarshalJSON() ([]byte, error)           { return writeEnum(heightRuleNames, h) }

// RowProps are a table row's properties.
type RowProps struct {
	// Height is in points (its meaning depends on HeightRule).
	Height     *F32       `json:"height"`
	HeightRule HeightRule `json:"heightRule"`
	// Header repeats the row at the top of each page.
	Header bool `json:"header"`
	// CantSplit doesn't let the row break across pages.
	CantSplit bool `json:"cantSplit"`
	// Exceptions are the table properties this row overrides (w:tblPrEx, ECMA-376 §17.4.61):
	// width, alignment, indent, borders, shading, layout and cell margins. Set when the file has
	// the element.
	Exceptions *TableProps `json:"exceptions,omitempty"`
	// GridBefore is the number of grid columns skipped before the row's first cell
	// (w:gridBefore), and WBefore the preferred width of that gap in points (w:wBefore).
	GridBefore uint32 `json:"gridBefore,omitempty"`
	WBefore    *F32   `json:"wBefore,omitempty"`
	// GridAfter is the number of grid columns left after the row's last cell (w:gridAfter), and
	// WAfter their width in points (w:wAfter).
	GridAfter uint32 `json:"gridAfter,omitempty"`
	WAfter    *F32   `json:"wAfter,omitempty"`
	// FmtChange is a tracked formatting change (w:trPrChange).
	FmtChange *PropChange[RowProps] `json:"fmtChange,omitempty"`
}

func (RowProps) SerdeDefault() {}

// VMerge is a cell's part in a vertical merge.
type VMerge uint8

// Vertical merges.
const (
	VMergeNone VMerge = iota
	// VMergeRestart is the first cell of a vertically merged range.
	VMergeRestart
	// VMergeContinue continues the merge from the cell above.
	VMergeContinue
)

var vMergeNames = []string{"none", "restart", "continue"}

func (v *VMerge) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, vMergeNames, v) }
func (v VMerge) MarshalJSON() ([]byte, error)           { return writeEnum(vMergeNames, v) }

// CellProps are a table cell's properties.
type CellProps struct {
	// Width is the preferred width, points.
	Width *F32 `json:"width"`
	// WidthPct is the preferred width as a percentage of the table's (w:tcW w:type="pct").
	WidthPct *F32 `json:"widthPct,omitempty"`
	// Span is the number of grid columns spanned (1 = no horizontal merge).
	Span    uint32   `json:"span"`
	VMerge  VMerge   `json:"vmerge"`
	Shading *RGB     `json:"shading"`
	Borders *Borders `json:"borders"`
	// VAlign is the vertical alignment (w:vAlign); nil follows the table style, else top. An
	// explicit top beats the style.
	VAlign *VAlign `json:"valign,omitempty"`
	// Margins override the cell margins (top, left, bottom, right), points.
	Margins *[4]F32 `json:"margins"`
	// PartialMargins are per-side overrides when Margins is nil; omitted sides inherit the table
	// defaults.
	PartialMargins *[4]*F32 `json:"partialMargins,omitempty"`
	// TextDirection is which way the cell's text runs.
	TextDirection TextDirection `json:"textDirection"`
	// TextDirectionSet: the file states the horizontal direction (lrTb) outright; saving writes
	// it back.
	TextDirectionSet bool `json:"textDirectionSet,omitempty"`
	NoWrap           bool `json:"noWrap"`
	// HideMark: the cell's last paragraph takes no room when it is empty (w:hideMark): a row of
	// such empty cells shrinks to its borders.
	HideMark bool `json:"hideMark,omitempty"`
	// FmtChange is a tracked formatting change (w:tcPrChange).
	FmtChange *PropChange[CellProps] `json:"fmtChange,omitempty"`
}

func (CellProps) SerdeDefault() {}

// ResolvedMargins resolves the top, left, bottom and right margins against the table's
// defaults. A complete override takes precedence over per-side overrides.
func (c CellProps) ResolvedMargins(defaults [4]F32) [4]F32 {
	if c.Margins != nil {
		return *c.Margins
	}
	if c.PartialMargins == nil {
		return defaults
	}
	out := defaults
	for i, s := range c.PartialMargins {
		if s != nil {
			out[i] = *s
		}
	}
	return out
}

// Anchor is what a floating object's offset is relative to (para.rs).
type Anchor uint8

// Anchors.
const (
	AnchorColumn Anchor = iota
	AnchorMargin
	AnchorPage
	AnchorParagraph
	// AnchorLeftMargin is the page's left margin area (from the page's left edge to the text).
	AnchorLeftMargin
	// AnchorRightMargin is the page's right margin area (from the text to the page's right
	// edge).
	AnchorRightMargin
	// AnchorTopMargin is the page's top margin area (from the page's top edge to the text).
	AnchorTopMargin
	// AnchorBottomMargin is the page's bottom margin area (from the text to the page's bottom
	// edge).
	AnchorBottomMargin
	// AnchorInsideMargin is the inside margin (left on odd pages, right on even pages when
	// mirrored).
	AnchorInsideMargin
	// AnchorOutsideMargin is the outside margin.
	AnchorOutsideMargin
	// AnchorCharacter is the anchor character (horizontal only).
	AnchorCharacter
	// AnchorLine is the anchor line (vertical only).
	AnchorLine
)

var anchorNames = []string{"column", "margin", "page", "paragraph", "leftMargin", "rightMargin", "topMargin", "bottomMargin",
	"insideMargin", "outsideMargin", "character", "line"}

func (a *Anchor) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, anchorNames, a) }
func (a Anchor) MarshalJSON() ([]byte, error)           { return writeEnum(anchorNames, a) }

// FloatAlign is the alignment of a floating object within its Anchor area, instead of an offset
// (para.rs).
type FloatAlign uint8

// Float alignments.
const (
	// FloatStart is left or top.
	FloatStart FloatAlign = iota
	FloatCenter
	// FloatEnd is right or bottom.
	FloatEnd
	FloatInside
	FloatOutside
)

var floatAlignNames = []string{"start", "center", "end", "inside", "outside"}

func (a *FloatAlign) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, floatAlignNames, a) }
func (a FloatAlign) MarshalJSON() ([]byte, error)           { return writeEnum(floatAlignNames, a) }

// FloatAlignFromOOXML reads an OOXML alignment (wp:align, w:tblpXSpec, w:tblpYSpec).
func FloatAlignFromOOXML(v string) (FloatAlign, bool) {
	switch v {
	case "left", "top":
		return FloatStart, true
	case "center":
		return FloatCenter, true
	case "right", "bottom":
		return FloatEnd, true
	case "inside":
		return FloatInside, true
	case "outside":
		return FloatOutside, true
	}
	return 0, false
}
