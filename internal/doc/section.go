package doc

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/computernut43/craft-libs/serdejson"
)

// Section properties: page setup, columns, headers/footers, page numbering
// (crates/doc/src/section.rs).

// PageVAlign is how a section's text sits on its pages, top to bottom (w:vAlign, ECMA-376
// §17.6.23, ST_VerticalJc). Table cells and text boxes use VAlign, which has no justified.
type PageVAlign uint8

// Page vertical alignments.
const (
	PageTop PageVAlign = iota
	PageCenter
	// PageJustify (both): the room left on each page is shared out equally between the page's
	// paragraphs and tables, so the text runs from the top margin to the bottom one.
	PageJustify
	PageBottom
)

var pageVAlignNames = []string{"top", "center", "justify", "bottom"}

// DecodeSerde reads the name, or one of justify's aliases (both, justified).
func (v *PageVAlign) DecodeSerde(d serdejson.Decoder) error {
	i, err := d.UnitVariant([]string{"top", "center", "justify", "bottom", "both", "justified"})
	if err != nil {
		return err
	}
	*v = []PageVAlign{PageTop, PageCenter, PageJustify, PageBottom, PageJustify, PageJustify}[i]
	return nil
}
func (v PageVAlign) MarshalJSON() ([]byte, error) { return writeEnum(pageVAlignNames, v) }

// ParsePageVAlign reads a command or file value: top|center|justify|bottom (also both,
// justified, middle); anything else is top.
func ParsePageVAlign(v string) PageVAlign {
	switch v {
	case "center", "middle":
		return PageCenter
	case "justify", "justified", "both":
		return PageJustify
	case "bottom":
		return PageBottom
	}
	return PageTop
}

// SectionStart is where a section starts.
type SectionStart uint8

// Section starts.
const (
	StartNextPage SectionStart = iota
	StartContinuous
	StartEvenPage
	StartOddPage
	StartNextColumn
)

var sectionStartNames = []string{"nextPage", "continuous", "evenPage", "oddPage", "nextColumn"}

func (s *SectionStart) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, sectionStartNames, s)
}
func (s SectionStart) MarshalJSON() ([]byte, error) { return writeEnum(sectionStartNames, s) }

// NumFormat is a number format (ECMA-376 §17.18.59 ST_NumberFormat). Every value the format
// defines is kept, so a document saves back the format it was opened with even where WordCraft
// draws it as plain decimal (see Format). Its JSON name is its OOXML name.
type NumFormat uint8

// Number formats, in the Rust enum's order.
const (
	FmtDecimal NumFormat = iota
	FmtUpperRoman
	FmtLowerRoman
	FmtUpperLetter
	FmtLowerLetter
	FmtOrdinal
	FmtCardinalText
	FmtOrdinalText
	FmtDecimalZero
	FmtBullet
	FmtNone
	FmtHex
	FmtChicago
	FmtIdeographDigital
	FmtJapaneseCounting
	FmtAiueo
	FmtIroha
	FmtDecimalFullWidth
	FmtDecimalHalfWidth
	FmtJapaneseLegal
	FmtJapaneseDigitalTenThousand
	FmtDecimalEnclosedCircle
	FmtDecimalFullWidth2
	FmtAiueoFullWidth
	FmtIrohaFullWidth
	FmtGanada
	FmtChosung
	FmtDecimalEnclosedFullstop
	FmtDecimalEnclosedParen
	FmtDecimalEnclosedCircleChinese
	FmtIdeographEnclosedCircle
	FmtIdeographTraditional
	FmtIdeographZodiac
	FmtIdeographZodiacTraditional
	FmtTaiwaneseCounting
	FmtIdeographLegalTraditional
	FmtTaiwaneseCountingThousand
	FmtTaiwaneseDigital
	FmtChineseCounting
	FmtChineseLegalSimplified
	FmtChineseCountingThousand
	FmtKoreanDigital
	FmtKoreanCounting
	FmtKoreanLegal
	FmtKoreanDigital2
	FmtVietnameseCounting
	FmtRussianLower
	FmtRussianUpper
	FmtNumberInDash
	FmtHebrew1
	FmtHebrew2
	FmtArabicAlpha
	FmtArabicAbjad
	FmtHindiVowels
	FmtHindiConsonants
	FmtHindiNumbers
	FmtHindiCounting
	FmtThaiLetters
	FmtThaiNumbers
	FmtThaiCounting
	FmtBahtText
	FmtDollarText
)

var numFormatNames = []string{"decimal", "upperRoman", "lowerRoman", "upperLetter", "lowerLetter", "ordinal", "cardinalText", "ordinalText",
	"decimalZero", "bullet", "none", "hex", "chicago", "ideographDigital", "japaneseCounting", "aiueo", "iroha", "decimalFullWidth",
	"decimalHalfWidth", "japaneseLegal", "japaneseDigitalTenThousand", "decimalEnclosedCircle", "decimalFullWidth2", "aiueoFullWidth",
	"irohaFullWidth", "ganada", "chosung", "decimalEnclosedFullstop", "decimalEnclosedParen", "decimalEnclosedCircleChinese",
	"ideographEnclosedCircle", "ideographTraditional", "ideographZodiac", "ideographZodiacTraditional", "taiwaneseCounting",
	"ideographLegalTraditional", "taiwaneseCountingThousand", "taiwaneseDigital", "chineseCounting", "chineseLegalSimplified",
	"chineseCountingThousand", "koreanDigital", "koreanCounting", "koreanLegal", "koreanDigital2", "vietnameseCounting", "russianLower",
	"russianUpper", "numberInDash", "hebrew1", "hebrew2", "arabicAlpha", "arabicAbjad", "hindiVowels", "hindiConsonants", "hindiNumbers",
	"hindiCounting", "thaiLetters", "thaiNumbers", "thaiCounting", "bahtText", "dollarText"}

// NumFormats lists every format in the order ST_NumberFormat lists them (which is also the order
// of the binary .doc number format codes up to 0x27).
var NumFormats = []NumFormat{FmtDecimal, FmtUpperRoman, FmtLowerRoman, FmtUpperLetter, FmtLowerLetter, FmtOrdinal, FmtCardinalText,
	FmtOrdinalText, FmtHex, FmtChicago, FmtIdeographDigital, FmtJapaneseCounting, FmtAiueo, FmtIroha, FmtDecimalFullWidth,
	FmtDecimalHalfWidth, FmtJapaneseLegal, FmtJapaneseDigitalTenThousand, FmtDecimalEnclosedCircle, FmtDecimalFullWidth2,
	FmtAiueoFullWidth, FmtIrohaFullWidth, FmtDecimalZero, FmtBullet, FmtGanada, FmtChosung, FmtDecimalEnclosedFullstop,
	FmtDecimalEnclosedParen, FmtDecimalEnclosedCircleChinese, FmtIdeographEnclosedCircle, FmtIdeographTraditional, FmtIdeographZodiac,
	FmtIdeographZodiacTraditional, FmtTaiwaneseCounting, FmtIdeographLegalTraditional, FmtTaiwaneseCountingThousand,
	FmtTaiwaneseDigital, FmtChineseCounting, FmtChineseLegalSimplified, FmtChineseCountingThousand, FmtKoreanDigital,
	FmtKoreanCounting, FmtKoreanLegal, FmtKoreanDigital2, FmtVietnameseCounting, FmtRussianLower, FmtRussianUpper, FmtNone,
	FmtNumberInDash, FmtHebrew1, FmtHebrew2, FmtArabicAlpha, FmtArabicAbjad, FmtHindiVowels, FmtHindiConsonants, FmtHindiNumbers,
	FmtHindiCounting, FmtThaiLetters, FmtThaiNumbers, FmtThaiCounting, FmtBahtText, FmtDollarText}

func (f *NumFormat) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, numFormatNames, f) }
func (f NumFormat) MarshalJSON() ([]byte, error)           { return writeEnum(numFormatNames, f) }

// OOXML is the ST_NumberFormat value.
func (f NumFormat) OOXML() string { return enumName(numFormatNames, f) }

// NumFormatFromOOXML reads an ST_NumberFormat value; one the format doesn't define is decimal (as
// Word shows it). So is custom (a Word 2010 format string, always written with a decimal
// fallback).
func NumFormatFromOOXML(s string) NumFormat {
	for i, n := range numFormatNames {
		if n == s {
			return NumFormat(i)
		}
	}
	return FmtDecimal
}

// Format formats n (1-based). Formats WordCraft doesn't draw yet (Hebrew, Arabic, Hindi, Thai,
// Vietnamese, text amounts) show decimal, as do values past a sequence's end (the 21st item of
// ①…⑳).
func (f NumFormat) Format(n uint32) string {
	switch f {
	case FmtDecimalZero:
		return fmt.Sprintf("%02d", n)
	case FmtUpperRoman:
		return strings.ToUpper(Roman(n))
	case FmtLowerRoman:
		return Roman(n)
	case FmtUpperLetter:
		return strings.ToUpper(Letters(n))
	case FmtLowerLetter:
		return Letters(n)
	case FmtRussianUpper:
		return strings.ToUpper(RussianLetters(n))
	case FmtRussianLower:
		return RussianLetters(n)
	case FmtOrdinal:
		return strconv.FormatUint(uint64(n), 10) + ordinalSuffix(n)
	case FmtCardinalText:
		return CardinalText(n)
	case FmtOrdinalText:
		return OrdinalText(n)
	case FmtBullet, FmtNone:
		return ""
	case FmtChicago:
		return chicago(n)
	case FmtNumberInDash:
		return "- " + strconv.FormatUint(uint64(n), 10) + " -"
	case FmtIdeographDigital, FmtKoreanDigital2, FmtTaiwaneseDigital:
		return ideographDigital(n)
	case FmtJapaneseCounting:
		return japaneseCounting(n)
	case FmtJapaneseLegal:
		return japaneseLegal(n)
	case FmtJapaneseDigitalTenThousand:
		return japaneseDigitalTenThousand(n)
	case FmtAiueo:
		return aiueo(n)
	case FmtAiueoFullWidth:
		return aiueoFullWidth(n)
	case FmtIroha:
		return iroha(n)
	case FmtIrohaFullWidth:
		return irohaFullWidth(n)
	case FmtDecimalFullWidth, FmtDecimalFullWidth2:
		return decimalFullWidth(n)
	case FmtDecimalEnclosedCircle:
		return decimalEnclosedCircle(n)
	case FmtDecimalEnclosedCircleChinese:
		return decimalEnclosedCircleChinese(n)
	case FmtDecimalEnclosedFullstop:
		return decimalEnclosedFullstop(n)
	case FmtDecimalEnclosedParen:
		return decimalEnclosedParen(n)
	case FmtIdeographEnclosedCircle:
		return ideographEnclosedCircle(n)
	case FmtIdeographTraditional:
		return ideographTraditional(n)
	case FmtIdeographZodiac:
		return ideographZodiac(n)
	case FmtIdeographZodiacTraditional:
		return ideographZodiacTraditional(n)
	case FmtIdeographLegalTraditional:
		return ideographLegalTraditional(n)
	case FmtTaiwaneseCounting:
		return taiwaneseCounting(n)
	case FmtTaiwaneseCountingThousand:
		return taiwaneseCountingThousand(n)
	case FmtChineseCounting:
		return chineseCounting(n)
	case FmtChineseCountingThousand:
		return chineseCountingThousand(n)
	case FmtChineseLegalSimplified:
		return chineseLegalSimplified(n)
	case FmtKoreanCounting:
		return koreanCounting(n)
	case FmtKoreanDigital:
		return koreanDigital(n)
	case FmtKoreanLegal:
		return koreanLegal(n)
	case FmtGanada:
		return ganada(n)
	case FmtChosung:
		return chosungOf(n)
	}
	return strconv.FormatUint(uint64(n), 10)
}

// chicago is the Chicago Manual of Style note marks: *, †, ‡, §, then doubled (**, ††, …) and so
// on.
func chicago(n uint32) string {
	if n == 0 {
		return ""
	}
	i := n - 1
	k := min(i/4+1, 10)
	return strings.Repeat(string([]rune{'*', '†', '‡', '§'}[i%4]), int(k))
}

func ordinalSuffix(n uint32) string {
	if m := n % 100; m >= 11 && m <= 13 {
		return "th"
	}
	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

var romanTable = []struct {
	v uint32
	r string
}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"},
	{4, "iv"}, {1, "i"}}

// Roman is lowercase roman numerals (0 → empty; values above 3999 use repeated m, up to 50,000).
func Roman(n uint32) string {
	var s strings.Builder
	n = min(n, 50_000)
	for _, t := range romanTable {
		for n >= t.v {
			s.WriteString(t.r)
			n -= t.v
		}
	}
	return s.String()
}

// Letters is Word-style letters: a..z, aa..zz, aaa.. (repeated letter, not base 26).
func Letters(n uint32) string {
	if n == 0 {
		return ""
	}
	n = min(n, 780)
	return strings.Repeat(string(rune('a'+(n-1)%26)), int((n-1)/26+1))
}

// russianLetters are the 29 letters Word counts Russian lists with: а..я without ё, й, ъ and ь
// (ы stays).
var russianLetters = []rune("абвгдежзиклмнопрстуфхцчшщыэюя")

// RussianLetters is Word-style Russian letters: а..я, аа..яя, ааа.. (repeated letter, as Letters).
func RussianLetters(n uint32) string {
	if n == 0 {
		return ""
	}
	l := uint32(len(russianLetters))
	n = min(n, l*30)
	return strings.Repeat(string(russianLetters[(n-1)%l]), int((n-1)/l+1))
}

var (
	ones = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen",
		"fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
)

func words(n uint32) string {
	switch {
	case n <= 19:
		return ones[n]
	case n <= 99:
		if n%10 == 0 {
			return tens[n/10]
		}
		return tens[n/10] + "-" + ones[n%10]
	case n <= 999:
		h := words(n/100) + " hundred"
		if n%100 == 0 {
			return h
		}
		return h + " " + words(n%100)
	}
	th := words(n/1000) + " thousand"
	if n%1000 == 0 {
		return th
	}
	return th + " " + words(n%1000)
}

// CardinalText is English cardinal words, each word capitalised (Twenty-One).
func CardinalText(n uint32) string { return capitalize(words(min(n, 999_999))) }

// OrdinalText is English ordinal words (Twenty-First).
func OrdinalText(n uint32) string {
	c := strings.ToLower(CardinalText(n))
	head, last := "", c
	if i := strings.LastIndexAny(c, "- "); i >= 0 {
		head, last = c[:i+1], c[i+1:]
	}
	switch last {
	case "one":
		last = "first"
	case "two":
		last = "second"
	case "three":
		last = "third"
	case "five":
		last = "fifth"
	case "eight":
		last = "eighth"
	case "nine":
		last = "ninth"
	case "twelve":
		last = "twelfth"
	default:
		if strings.HasSuffix(last, "y") {
			last = strings.TrimRight(last, "y") + "ieth"
		} else {
			last += "th"
		}
	}
	return capitalize(head + last)
}

func capitalize(s string) string {
	var out strings.Builder
	up := true
	for _, c := range s {
		if up {
			out.WriteString(strings.ToUpper(string(c)))
		} else {
			out.WriteRune(c)
		}
		up = c == '-' || c == ' '
	}
	return out.String()
}

// NotePos is where footnotes or endnotes are placed (ECMA-376 §17.18.33/§17.18.20,
// ST_FtnPos/ST_EdnPos).
type NotePos uint8

// Note positions.
const (
	// NotesPageBottom: footnotes at the bottom of the page.
	NotesPageBottom NotePos = iota
	// NotesBeneathText: footnotes right below the page's last line of text.
	NotesBeneathText
	// NotesSectEnd: endnotes at the end of each section.
	NotesSectEnd
	// NotesDocEnd: endnotes at the end of the document.
	NotesDocEnd
)

var notePosNames = []string{"pageBottom", "beneathText", "sectEnd", "docEnd"}

func (p *NotePos) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, notePosNames, p) }
func (p NotePos) MarshalJSON() ([]byte, error)           { return writeEnum(notePosNames, p) }

// OOXML is the ST_FtnPos/ST_EdnPos value.
func (p NotePos) OOXML() string { return enumName(notePosNames, p) }

// NotePosFromOOXML reads an ST_FtnPos/ST_EdnPos value.
func NotePosFromOOXML(s string) (NotePos, bool) { return enumFromName[NotePos](notePosNames, s) }

// NoteRestart is when note numbering starts again (ECMA-376 §17.18.66, ST_RestartNumber).
type NoteRestart uint8

// Note restarts.
const (
	RestartContinuous NoteRestart = iota
	RestartEachSect
	RestartEachPage
)

var noteRestartNames = []string{"continuous", "eachSect", "eachPage"}

func (r *NoteRestart) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, noteRestartNames, r) }
func (r NoteRestart) MarshalJSON() ([]byte, error)           { return writeEnum(noteRestartNames, r) }

// OOXML is the ST_RestartNumber value.
func (r NoteRestart) OOXML() string { return enumName(noteRestartNames, r) }

// NoteRestartFromOOXML reads an ST_RestartNumber value.
func NoteRestartFromOOXML(s string) (NoteRestart, bool) {
	return enumFromName[NoteRestart](noteRestartNames, s)
}

// MaxNoteStart is the highest note number "start at" takes (Word's own limit is lower; this keeps
// hostile files from overflowing the counters).
const MaxNoteStart = 32_767

// NoteProps are footnote or endnote options as a document or a section states them (ECMA-376
// §17.11.11 and §17.11.4, w:footnotePr / w:endnotePr): unset fields come from the level above (a
// section's from the document's, the document's from Word's defaults). The document's number
// format is Settings.FootnoteFormat / EndnoteFormat, so its NumFmt stays unset.
type NoteProps struct {
	Pos        *NotePos     `json:"pos,omitempty"`
	NumFmt     *NumFormat   `json:"numFmt,omitempty"`
	NumStart   *uint32      `json:"numStart,omitempty"`
	NumRestart *NoteRestart `json:"numRestart,omitempty"`
}

func (NoteProps) SerdeDefault() {}

// IsEmpty reports whether nothing is set.
func (n NoteProps) IsEmpty() bool { return equal(n, NoteProps{}) }

// IsZero lets omitzero leave empty note options out, as Rust does.
func (n NoteProps) IsZero() bool { return n.IsEmpty() }

// NoteOptions are the footnote or endnote options in effect for a section.
type NoteOptions struct {
	Pos    NotePos   `json:"pos"`
	NumFmt NumFormat `json:"numFmt"`
	// NumStart is 1..=MaxNoteStart.
	NumStart   uint32      `json:"numStart"`
	NumRestart NoteRestart `json:"numRestart"`
}

// HeaderSet are the header or footer story ids (in Document parts) of a section.
type HeaderSet struct {
	Default *uint32 `json:"default"`
	First   *uint32 `json:"first"`
	Even    *uint32 `json:"even"`
}

func (HeaderSet) SerdeDefault() {}

// Columns are a section's text columns.
type Columns struct {
	Count uint32 `json:"count"`
	// Space between columns, points.
	Space F32 `json:"space"`
	// Separator is a line between columns.
	Separator bool `json:"separator"`
	// Widths are the unequal widths (width, space after) per column; empty = equal.
	Widths Vec[[2]F32] `json:"widths"`
}

func (Columns) SerdeDefault() {}

// SetDefaults sets the Rust Default: one column, 36 pt apart.
func (c *Columns) SetDefaults() { *c = Columns{Count: 1, Space: 36} }

// LineNumbering are a section's line numbers.
type LineNumbering struct {
	CountBy uint32 `json:"countBy"`
	Start   uint32 `json:"start"`
	// Distance from text, points (0 = auto).
	Distance F32 `json:"distance"`
	// Restart each page, section, or continuous.
	Restart LineNumberRestart `json:"restart"`
}

func (LineNumbering) SerdeDefault() {}

// SetDefaults sets the Rust Default.
func (l *LineNumbering) SetDefaults() { *l = LineNumbering{CountBy: 1, Start: 1} }

// LineNumberRestart is when line numbers restart.
type LineNumberRestart uint8

// Line number restarts.
const (
	LinesPage LineNumberRestart = iota
	LinesSection
	LinesContinuous
)

var lineNumberRestartNames = []string{"page", "section", "continuous"}

func (r *LineNumberRestart) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, lineNumberRestartNames, r)
}
func (r LineNumberRestart) MarshalJSON() ([]byte, error) {
	return writeEnum(lineNumberRestartNames, r)
}

// DocGridType is the document grid kind (w:docGrid/@w:type, ECMA-376 §17.6.5, §17.18.14).
type DocGridType uint8

// Document grid kinds.
const (
	// GridDefault: no grid.
	GridDefault DocGridType = iota
	// GridLines: line grid only: line heights snap to the line pitch.
	GridLines
	// GridLinesAndChars: line and character grid: East Asian characters advance by the
	// character pitch.
	GridLinesAndChars
	// GridSnapToChars: line grid, and all text snaps to the character grid.
	GridSnapToChars
)

var docGridTypeNames = []string{"default", "lines", "linesAndChars", "snapToChars"}

func (t *DocGridType) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, docGridTypeNames, t)
}
func (t DocGridType) MarshalJSON() ([]byte, error) { return writeEnum(docGridTypeNames, t) }

// OOXML is the ST_DocGrid value.
func (t DocGridType) OOXML() string { return enumName(docGridTypeNames, t) }

// DocGridTypeFromOOXML reads an ST_DocGrid value.
func DocGridTypeFromOOXML(s string) (DocGridType, bool) {
	return enumFromName[DocGridType](docGridTypeNames, s)
}

// Grid limits.
const (
	// MinGridPitch and MaxGridPitch are the smallest and largest line or character pitch the
	// layout uses, points.
	MinGridPitch = 1.0
	MaxGridPitch = 1584.0
	// MaxCharSpace bounds w:charSpace (4096ths of a point): ±1584 pt.
	MaxCharSpace = 1584 * 4096
)

// DocGrid is the document grid (w:docGrid): lines per page and characters per line, as pitches.
type DocGrid struct {
	Kind DocGridType `json:"type"`
	// LinePitch is the distance between grid lines, points (w:linePitch, twips in the file).
	LinePitch F32 `json:"linePitch"`
	// CharSpace is added to the default font size to give the character pitch, 4096ths of a
	// point (w:charSpace).
	CharSpace int32 `json:"charSpace"`
}

func (DocGrid) SerdeDefault() {}

// SetDefaults sets Word's grid for a new document: no grid, an 18 pt pitch kept for when one is
// turned on.
func (g *DocGrid) SetDefaults() { *g = DocGrid{LinePitch: 18} }

// LinePitchPt is the line pitch, clamped to something layout can use.
func (g DocGrid) LinePitchPt() F32 {
	if finite(g.LinePitch) {
		return fclamp(g.LinePitch, MinGridPitch, MaxGridPitch)
	}
	return 18
}

// CharPitch is the character pitch for a document whose default font is fontSize points.
func (g DocGrid) CharPitch(fontSize F32) F32 {
	size := F32(10.5)
	if finite(fontSize) {
		size = fclamp(fontSize, MinGridPitch, MaxGridPitch)
	}
	space := min(max(g.CharSpace, -MaxCharSpace), MaxCharSpace)
	return fclamp(size+F32(space)/4096, MinGridPitch, MaxGridPitch)
}

// CharSpaceFor is the w:charSpace that gives pitch with the default font at fontSize points.
func CharSpaceFor(pitch, fontSize F32) int32 {
	if finite(pitch) {
		pitch = fclamp(pitch, MinGridPitch, MaxGridPitch)
	} else {
		pitch = fontSize
	}
	size := F32(10.5)
	if finite(fontSize) {
		size = fclamp(fontSize, MinGridPitch, MaxGridPitch)
	}
	return min(max(i32Sat(float64(fround((pitch-size)*4096))), -MaxCharSpace), MaxCharSpace)
}

// SectionProps are page setup and section-level settings.
type SectionProps struct {
	Start SectionStart `json:"start"`
	// PageW and PageH are the page width and height, points (already swapped for landscape).
	PageW        F32  `json:"pageW"`
	PageH        F32  `json:"pageH"`
	Landscape    bool `json:"landscape"`
	MarginTop    F32  `json:"marginTop"`
	MarginBottom F32  `json:"marginBottom"`
	MarginLeft   F32  `json:"marginLeft"`
	MarginRight  F32  `json:"marginRight"`
	// Header and Footer are the distances from the page edge to the header and footer.
	Header  F32       `json:"header"`
	Footer  F32       `json:"footer"`
	Gutter  F32       `json:"gutter"`
	Columns Columns   `json:"columns"`
	Headers HeaderSet `json:"headers"`
	Footers HeaderSet `json:"footers"`
	// TitlePage gives the first page its own header and footer.
	TitlePage     bool      `json:"titlePage"`
	PageNumStart  *uint32   `json:"pageNumStart"`
	PageNumFormat NumFormat `json:"pageNumFormat"`
	// PageNumSet: the file carries a w:pgNumType element that sets nothing else (no start,
	// decimal format), so saving writes it back rather than dropping it.
	PageNumSet  bool           `json:"pageNumSet,omitempty"`
	LineNumbers *LineNumbering `json:"lineNumbers"`
	VAlign      PageVAlign     `json:"valign"`
	// FormProt is the form protection of the section (w:formProt, ECMA-376 §17.6.6): set when
	// the file states it. Kept so saving writes it back; editing does not act on it.
	FormProt    *bool    `json:"formProt,omitempty"`
	PageBorders *Borders `json:"pageBorders"`
	// RTL is a right-to-left (bidi) section.
	RTL bool `json:"rtl"`
	// TextDirection is which way the section's text runs (w:textDirection, ECMA-376 §17.6.20):
	// vertical sections turn the page's text area (headers and footers stay horizontal).
	TextDirection TextDirection `json:"textDirection,omitzero"`
	// TextDirectionSet: the file states the horizontal direction (lrTb) outright; saving writes
	// it back.
	TextDirectionSet bool `json:"textDirectionSet,omitempty"`
	// FmtChange is a tracked change of the section's properties (w:sectPrChange).
	FmtChange *PropChange[SectionProps] `json:"fmtChange,omitempty"`
	// DocGrid is the document grid (w:docGrid); nil when the file has none (no grid).
	DocGrid *DocGrid `json:"docGrid"`
	// FootnotePr and EndnotePr are the section's own footnote and endnote options (unset: the
	// document's).
	FootnotePr NoteProps `json:"footnotePr,omitzero"`
	EndnotePr  NoteProps `json:"endnotePr,omitzero"`
}

func (SectionProps) SerdeDefault() {}

// SetDefaults sets Word's default: US Letter, 1" margins, 0.5" header and footer distance.
func (s *SectionProps) SetDefaults() { *s = NewSectionProps() }

// NewSectionProps is Word's default section.
func NewSectionProps() SectionProps {
	return SectionProps{PageW: 612, PageH: 792, MarginTop: 72, MarginBottom: 72, MarginLeft: 72, MarginRight: 72, Header: 36, Footer: 36,
		Columns: Columns{Count: 1, Space: 36}}
}

// Absorb joins the section before this one into it, as when the break between them is deleted:
// this section's setup stays, as in Word, and the headers and footers it leaves out (linked to
// the previous section) are taken from before, so its text keeps showing them.
func (s *SectionProps) Absorb(before *SectionProps) {
	for _, hs := range []struct{ own, prev *HeaderSet }{{&s.Headers, &before.Headers}, {&s.Footers, &before.Footers}} {
		if hs.own.Default == nil {
			hs.own.Default = hs.prev.Default
		}
		if hs.own.First == nil {
			hs.own.First = hs.prev.First
		}
		if hs.own.Even == nil {
			hs.own.Even = hs.prev.Even
		}
	}
}

// TextWidth is the width of the text area, points.
func (s *SectionProps) TextWidth() F32 {
	return fmax(s.PageW-s.MarginLeft-s.MarginRight-s.Gutter, 18)
}

// TextHeight is the height of the text area, points.
func (s *SectionProps) TextHeight() F32 { return fmax(s.PageH-s.MarginTop-s.MarginBottom, 18) }

// SetLandscape swaps to landscape or portrait, keeping the margins relative to the paper as Word
// does: it rotates them.
func (s *SectionProps) SetLandscape(on bool) {
	if on == s.Landscape {
		return
	}
	s.Landscape = on
	s.PageW, s.PageH = s.PageH, s.PageW
	t, b, l, r := s.MarginTop, s.MarginBottom, s.MarginLeft, s.MarginRight
	if on {
		s.MarginTop, s.MarginBottom, s.MarginLeft, s.MarginRight = l, r, b, t
	} else {
		s.MarginLeft, s.MarginRight, s.MarginBottom, s.MarginTop = t, b, l, r
	}
}

// GridLinePitch is the line grid's pitch, points, when the section has a line grid (any grid
// but "no grid").
func (s *SectionProps) GridLinePitch() (F32, bool) {
	if s.DocGrid == nil || s.DocGrid.Kind == GridDefault {
		return 0, false
	}
	return s.DocGrid.LinePitchPt(), true
}

// GridCharPitch is the character grid's pitch, points, when the section has one
// (linesAndChars, snapToChars); fontSize is the default font size (Normal style).
func (s *SectionProps) GridCharPitch(fontSize F32) (F32, bool) {
	if s.DocGrid == nil || s.DocGrid.Kind != GridLinesAndChars && s.DocGrid.Kind != GridSnapToChars {
		return 0, false
	}
	return s.DocGrid.CharPitch(fontSize), true
}

func (s *SectionProps) docGridOrDefault() DocGrid {
	if s.DocGrid != nil {
		return *s.DocGrid
	}
	return DocGrid{LinePitch: 18}
}

// GridLinesPerPage is the number of lines per page the line pitch gives (at least 1).
func (s *SectionProps) GridLinesPerPage() uint32 {
	pitch := s.docGridOrDefault().LinePitchPt()
	return max(u32Sat(float64(math32Floor(s.TextHeight()/pitch+0.001))), 1)
}

// GridCharsPerLine is the number of characters per line the character pitch gives (at least
// 1).
func (s *SectionProps) GridCharsPerLine(fontSize F32) uint32 {
	pitch := s.docGridOrDefault().CharPitch(fontSize)
	return max(u32Sat(float64(math32Floor(s.TextWidth()/pitch+0.001))), 1)
}

// ColumnBox is a column's x offset from the text area's left, and its width.
type ColumnBox struct{ X, W F32 }

// ColumnBoxes are the columns in reading order: in a right-to-left section (RTL, w:bidi) the
// first column stands at the right edge and the columns (with the space after each) run
// leftwards.
func (s *SectionProps) ColumnBoxes() []ColumnBox {
	tw := s.TextWidth()
	n := int(min(max(s.Columns.Count, 1), 45))
	boxes := make([]ColumnBox, 0, n)
	if len(s.Columns.Widths) != 0 && len(s.Columns.Widths) == n {
		var x F32
		for _, ws := range s.Columns.Widths {
			boxes = append(boxes, ColumnBox{x, fmax(ws[0], 18)})
			x += ws[0] + ws[1]
		}
	} else {
		space := fmax(s.Columns.Space, 0)
		w := fmax((tw-space*(F32(n)-1))/F32(n), 18)
		for i := range n {
			boxes = append(boxes, ColumnBox{F32(i) * (w + space), w})
		}
	}
	if s.RTL {
		for i, b := range boxes {
			boxes[i].X = tw - b.X - b.W
		}
	}
	return boxes
}

// math32Floor is f32::floor (exact through float64).
func math32Floor(v F32) F32 { return F32(math.Floor(float64(v))) }

// enumFromName finds a variant by name.
func enumFromName[T ~uint8](names []string, s string) (T, bool) {
	for i, n := range names {
		if n == s {
			return T(i), true
		}
	}
	return 0, false
}
