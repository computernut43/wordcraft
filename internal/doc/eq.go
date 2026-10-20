package doc

// The EQ field's overstrike switch (ECMA-376 Part 1 §17.16.5.20, \o): its arguments are drawn on
// top of one another, centred (\ac, the default), left-aligned (\al) or right-aligned (\ar).
// Home › Font › Enclose Characters stores a character in a circle, square, triangle or diamond as
// one: ` eq \o\ac(○,字)` (Rust crates/doc/src/eq.rs).
//
// Which of the two enclosure styles a field has is kept in the document model as a trailing
// `\* enlarge` (WordCraft's own; nothing is written for the default "shrink text" style). In a
// .docx the style is Word's: the font sizes of the field code's runs, so the marker never reaches
// a file.

import (
	"strings"
	"unicode/utf8"

	"github.com/computernut43/craft-libs/rustcompat"
)

// OAlign is how the overstruck arguments line up.
type OAlign uint8

// Overstrike alignments.
const (
	OAlignLeft OAlign = iota
	OAlignCenter
	OAlignRight
)

// Overstrike is an EQ \o(…) field: the arguments' text (nested switches flattened to their text).
type Overstrike struct {
	Align OAlign
	Args  []string
}

// maxEqArgs is the most arguments read from one field (the rest are ignored).
const maxEqArgs = 16

// ParseOverstrike parses an EQ \o field code; false for any other field (or EQ switch).
func ParseOverstrike(instr string) (Overstrike, bool) {
	t := rustcompat.TrimStart(instr)
	nameEnd := strings.IndexFunc(t, func(c rune) bool { return rustcompat.IsWhitespace(c) || c == '\\' })
	if nameEnd < 0 {
		nameEnd = len(t)
	}
	if !asciiEqualFold(t[:nameEnd], "eq") {
		return Overstrike{}, false
	}
	rest := rustcompat.TrimStart(t[nameEnd:])
	// \o, then its alignment options, then the argument list.
	rest, ok := strings.CutPrefix(rest, "\\")
	if !ok || rest == "" || (rest[0] != 'o' && rest[0] != 'O') {
		return Overstrike{}, false
	}
	rest = rest[1:]
	align := OAlignCenter
	for {
		rest = rustcompat.TrimStart(rest)
		if r, ok := strings.CutPrefix(rest, "("); ok {
			rest = r
			break
		}
		r, ok := strings.CutPrefix(rest, "\\")
		if !ok || len(r) < 2 {
			return Overstrike{}, false
		}
		switch toASCIILower(r[:2]) {
		case "al":
			align = OAlignLeft
		case "ac":
			align = OAlignCenter
		case "ar":
			align = OAlignRight
		default:
			return Overstrike{}, false
		}
		rest = r[2:]
	}
	// Arguments up to the matching ), split at top-level , or ; (the list separator).
	var args []string
	var cur strings.Builder
	depth := 0
	closed := false
	runes := []rune(rest)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\\':
			cur.WriteRune(c)
			if i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
			}
		case c == '(':
			depth++
			cur.WriteRune(c)
		case c == ')' && depth == 0:
			closed = true
		case c == ')':
			depth--
			cur.WriteRune(c)
		case (c == ',' || c == ';') && depth == 0:
			if len(args) < maxEqArgs {
				args = append(args, argText(cur.String()))
			}
			cur.Reset()
		default:
			cur.WriteRune(c)
		}
		if closed {
			break
		}
	}
	if !closed {
		return Overstrike{}, false
	}
	if len(args) < maxEqArgs {
		args = append(args, argText(cur.String()))
	}
	return Overstrike{Align: align, Args: args}, true
}

// argText is an argument's text: escapes (\, \( \) \\) become the character, nested switches
// (\s\up4(…)) and their brackets are dropped, leaving what they show.
func argText(s string) string {
	var out strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		switch c := r[i]; c {
		case '\\':
			if i+1 >= len(r) {
				continue
			}
			switch n := r[i+1]; n {
			case ',', '(', ')', '\\', ';':
				out.WriteRune(n)
				i++
			default:
				// A switch: its letters, then an optional (signed) number.
				j := i + 1
				for j < len(r) && r[j] < utf8.RuneSelf && isASCIIAlpha(byte(r[j])) {
					j++
				}
				for j < len(r) && r[j] == ' ' {
					j++
				}
				for j < len(r) && (r[j] >= '0' && r[j] <= '9' || r[j] == '-' || r[j] == '.') {
					j++
				}
				i = j - 1
			}
		case '(', ')':
		default:
			out.WriteRune(c)
		}
	}
	return out.String()
}

func isASCIIAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// escapeEqArg escapes text for an EQ argument.
func escapeEqArg(s string) string {
	var out strings.Builder
	for _, c := range s {
		switch c {
		case ',', '(', ')', '\\', ';':
			out.WriteByte('\\')
		}
		out.WriteRune(c)
	}
	return out.String()
}

// EncloseShape is the enclosure drawn around enclosed characters.
type EncloseShape uint8

// Enclosure shapes.
const (
	EncloseCircle EncloseShape = iota
	EncloseSquare
	EncloseTriangle
	EncloseDiamond
)

// EncloseShapes are all shapes in order.
var EncloseShapes = [4]EncloseShape{EncloseCircle, EncloseSquare, EncloseTriangle, EncloseDiamond}

// Symbol is the shape's symbol in the field code.
func (s EncloseShape) Symbol() rune {
	switch s {
	case EncloseSquare:
		return '□'
	case EncloseTriangle:
		return '△'
	case EncloseDiamond:
		return '◇'
	}
	return '○'
}

// EncloseShapeFromSymbol is the shape a field-code symbol stands for (the usual symbols and their
// large or small variants).
func EncloseShapeFromSymbol(s string) (EncloseShape, bool) {
	t := rustcompat.Trim(s)
	c, n := utf8.DecodeRuneInString(t)
	if t == "" || n != len(t) {
		return 0, false
	}
	switch c {
	case '○', '◯', '⭘', '◦', '〇':
		return EncloseCircle, true
	case '□', '◻', '⬜', '▢', '◽':
		return EncloseSquare, true
	case '△', '▵', '⊿':
		return EncloseTriangle, true
	case '◇', '◊', '⬦', '⋄':
		return EncloseDiamond, true
	}
	return 0, false
}

// Name is the shape's name.
func (s EncloseShape) Name() string {
	return [...]string{"circle", "square", "triangle", "diamond"}[s&3]
}

// ParseEncloseShape reads a shape name (ASCII case-insensitive, surrounding whitespace ignored).
func ParseEncloseShape(s string) (EncloseShape, bool) {
	t := rustcompat.Trim(s)
	for _, k := range EncloseShapes {
		if asciiEqualFold(k.Name(), t) {
			return k, true
		}
	}
	return 0, false
}

// EncloseStyle shrinks the text to fit the shape, or enlarges the shape around the text.
type EncloseStyle uint8

// Enclosure styles.
const (
	EncloseShrink EncloseStyle = iota
	EncloseEnlarge
)

// Name is the style's name.
func (s EncloseStyle) Name() string {
	if s == EncloseEnlarge {
		return "enlarge"
	}
	return "shrink"
}

// ParseEncloseStyle reads a style name.
func ParseEncloseStyle(s string) (EncloseStyle, bool) {
	switch toASCIILower(rustcompat.Trim(s)) {
	case "shrink":
		return EncloseShrink, true
	case "enlarge":
		return EncloseEnlarge, true
	}
	return 0, false
}

// Enclosure is enclosed characters: an EQ \o field with an enclosure symbol and the text.
type Enclosure struct {
	Shape EncloseShape
	Style EncloseStyle
	Text  string
}

// encloseEnlarge is the model's marker for the "enlarge symbol" style.
const encloseEnlarge = "\\* enlarge"

// ParseEnclosure reads enclosed characters from a field code: \o with exactly two arguments, one
// of them an enclosure symbol and the other non-empty text.
func ParseEnclosure(instr string) (Enclosure, bool) {
	o, ok := ParseOverstrike(instr)
	if !ok || len(o.Args) != 2 {
		return Enclosure{}, false
	}
	a, b := o.Args[0], o.Args[1]
	var shape EncloseShape
	var text string
	if s, ok := EncloseShapeFromSymbol(a); ok {
		shape, text = s, b
	} else if s, ok := EncloseShapeFromSymbol(b); ok {
		shape, text = s, a
	} else {
		return Enclosure{}, false
	}
	if rustcompat.Trim(text) == "" {
		return Enclosure{}, false
	}
	closeAt := strings.LastIndexByte(instr, ')')
	if closeAt < 0 {
		return Enclosure{}, false
	}
	tail := rustcompat.Trim(instr[closeAt+1:])
	style := EncloseShrink
	if asciiEqualFold(tail, encloseEnlarge) {
		style = EncloseEnlarge
	}
	return Enclosure{Shape: shape, Style: style, Text: text}, true
}

// Instr is the field code in the model (with the style marker; see WordInstr).
func (e Enclosure) Instr() string {
	if e.Style == EncloseEnlarge {
		return e.WordInstr() + " " + encloseEnlarge
	}
	return e.WordInstr()
}

// WordInstr is the field code as Word writes it: eq \o\ac(○,字).
func (e Enclosure) WordInstr() string {
	return "eq \\o\\ac(" + string(e.Shape.Symbol()) + "," + escapeEqArg(e.Text) + ")"
}

// IsFullWidth reports the characters Word counts as full width (East Asian wide or fullwidth);
// two half-width characters or one full-width character fit in an enclosure.
func IsFullWidth(c rune) bool {
	switch {
	case 0x1100 <= c && c <= 0x115F, 0x2E80 <= c && c <= 0x303E, 0x3041 <= c && c <= 0x33FF,
		0x3400 <= c && c <= 0x4DBF, 0x4E00 <= c && c <= 0x9FFF, 0xA000 <= c && c <= 0xA4CF,
		0xAC00 <= c && c <= 0xD7A3, 0xF900 <= c && c <= 0xFAFF, 0xFE30 <= c && c <= 0xFE4F,
		0xFF00 <= c && c <= 0xFF60, 0xFFE0 <= c && c <= 0xFFE6, 0x20000 <= c && c <= 0x3FFFD:
		return true
	}
	return false
}

// FitsEnclosure reports whether text fits in an enclosure: one full-width character or one or
// two half-width ones.
func FitsEnclosure(text string) bool {
	n, wide := 0, 0
	for _, c := range text {
		if isControl(c) || c == Obj {
			return false
		}
		n++
		if IsFullWidth(c) {
			wide++
		}
	}
	return n == 1 || (n == 2 && wide == 0)
}

// toASCIILower is Rust's to_ascii_lowercase.
func toASCIILower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// isControl is Rust's char::is_control (general category Cc).
func isControl(c rune) bool { return c < 0x20 || 0x7F <= c && c <= 0x9F }
