package doc

import "github.com/computernut43/craft-libs/serdejson"

// Document settings, stories and lists (crates/doc/src/lib.rs, math.rs MathProps).

// Settings are the document-wide settings.
type Settings struct {
	TrackChanges bool `json:"trackChanges"`
	// TrackMoves records a tracked cut and paste of a sentence or paragraph as a move; off when
	// the file has w:doNotTrackMoves (ECMA-376 §17.15.1.29), and then it is an ordinary deletion
	// and insertion.
	TrackMoves bool `json:"trackMoves"`
	// DefaultTab is the default tab stop interval, points.
	DefaultTab      F32  `json:"defaultTab"`
	EvenOddHeaders  bool `json:"evenOddHeaders"`
	MirrorMargins   bool `json:"mirrorMargins"`
	AutoHyphenation bool `json:"autoHyphenation"`
	// HyphenationZone (w:hyphenationZone), points: a line is hyphenated only when breaking it at
	// a word boundary would leave more than this empty at its end.
	HyphenationZone F32 `json:"hyphenationZone"`
	// HyphenateCaps: words in capitals may be hyphenated (w:doNotHyphenateCaps turns it off).
	HyphenateCaps bool `json:"hyphenateCaps"`
	// ConsecutiveHyphenLimit is the most consecutive lines that may end with a hyphen
	// (w:consecutiveHyphenLimit); 0 = no limit.
	ConsecutiveHyphenLimit uint32 `json:"consecutiveHyphenLimit"`
	PageColor              *RGB   `json:"pageColor"`
	// PagePicture is a picture or texture page background (Design › Page Color › Fill Effects),
	// drawn over the colour.
	PagePicture *PagePicture `json:"pagePicture"`
	Watermark   *Watermark   `json:"watermark"`
	// MajorFont and MinorFont are the theme fonts (major = headings, minor = body).
	MajorFont string `json:"majorFont"`
	MinorFont string `json:"minorFont"`
	// ThemeColors are dk1 lt1 dk2 lt2 accent1..6 hlink folHlink.
	ThemeColors    Vec[RGB]  `json:"themeColors"`
	ThemeName      string    `json:"themeName"`
	FootnoteFormat NumFormat `json:"footnoteFormat"`
	EndnoteFormat  NumFormat `json:"endnoteFormat"`
	// FootnotePr and EndnotePr are the document's footnote and endnote placement, start and
	// restart (w:footnotePr / w:endnotePr in the settings); sections may override them.
	FootnotePr NoteProps `json:"footnotePr,omitzero"`
	EndnotePr  NoteProps `json:"endnotePr,omitzero"`
	// Protection is Restrict Editing, when enforced: readOnly, comments, trackedChanges, forms,
	// or none when only formatting is restricted (w:documentProtection w:edit).
	Protection *string `json:"protection"`
	// ProtectFormatting: Restrict Editing limits formatting to the styles that aren't locked,
	// and forbids direct formatting (w:formatting="1").
	ProtectFormatting bool `json:"protectFormatting,omitempty"`
	// ProtectHash is the protection password's hash, as the file's w:documentProtection
	// attributes (name, value). Empty: no password.
	ProtectHash Vec[[2]string] `json:"protectHash,omitempty"`
	// CompatMode is the Word compatibility mode the document is laid out in
	// (compatibilityMode): 15 for Word 2013 and later.
	CompatMode uint32 `json:"compatMode"`
	// RomanFonts are the fonts the document's font table classes as roman (serif,
	// w:family w:val="roman"): Word draws one that isn't installed in a serif face.
	RomanFonts Vec[string] `json:"romanFonts,omitempty" serde:"default"`
	// SumParagraphSpacing (w:doNotUseHTMLParagraphAutoSpacing): the space between two paragraphs
	// is the first's space after plus the second's space before, not the larger of the two.
	SumParagraphSpacing bool `json:"sumParagraphSpacing,omitempty" serde:"default"`
	// AllowSpaceOfSameStyleInTable keeps contextual paragraph spacing inside legacy table
	// cells.
	AllowSpaceOfSameStyleInTable bool `json:"allowSpaceOfSameStyleInTable"`
	// GridH and GridV are the drawing grid spacing in points, across and down (View ›
	// Gridlines).
	GridH F32 `json:"gridH"`
	GridV F32 `json:"gridV"`
	// Math are the equation options (m:mathPr), when the document has them.
	Math *MathProps `json:"math,omitempty"`
	// BibStyle is the citation and bibliography style (References › Style) by name, when the
	// document records one.
	BibStyle *string `json:"bibStyle,omitempty"`
	// AttachedTemplate is the template the document is attached to: the target of
	// w:attachedTemplate's relationship, a path or file: URL as the file gives it.
	AttachedTemplate *string `json:"attachedTemplate,omitempty"`
	// LinkStyles updates the document's styles from the attached template when it is opened.
	LinkStyles bool `json:"linkStyles"`
	// Zoom is the zoom the document was saved at (w:zoom), restored when it is opened; nil for
	// 100% without a fit mode.
	Zoom *Zoom `json:"zoom,omitempty"`
	// RemovePersonalInfo (w:removePersonalInformation, ECMA-376 §17.15.1.70): the saved file
	// names comment and revision authors "Author" and leaves out the document's creator and
	// last-modified-by. The open document keeps its real values.
	RemovePersonalInfo bool `json:"removePersonalInfo,omitempty"`
	// RemoveDateTime (w:removeDateAndTime, §17.15.1.69): comments and tracked changes are saved
	// without w:date.
	RemoveDateTime bool `json:"removeDateTime,omitempty"`
}

func (Settings) SerdeDefault() {}

// SetDefaults sets the Rust Default.
func (s *Settings) SetDefaults() { *s = NewSettings() }

// Fonts and settings defaults.
const (
	BodyFont               = "Aptos"
	HeadingFont            = "Aptos Display"
	LegacyCompatMode       = 12
	CompatModeCurrent      = 15
	DefaultHyphenationZone = 18.0
	DefaultGrid            = 9.0
)

// ThemeColors are WordCraft's default theme colours.
var ThemeColors = []RGB{{0x00, 0x00, 0x00}, {0xFF, 0xFF, 0xFF}, {0x0E, 0x28, 0x41}, {0xE8, 0xE8, 0xE8}, {0x15, 0x60, 0x82}, {0xE9, 0x71, 0x32},
	{0x19, 0x6B, 0x24}, {0x0F, 0x9E, 0xD5}, {0xA0, 0x2B, 0x93}, {0x4E, 0xA7, 0x2E}, {0x46, 0x78, 0x86}, {0x96, 0x60, 0x7D}}

// NewSettings are the defaults of a new document.
func NewSettings() Settings {
	return Settings{TrackMoves: true, DefaultTab: 36, HyphenationZone: DefaultHyphenationZone, HyphenateCaps: true, MajorFont: HeadingFont,
		MinorFont: BodyFont, ThemeColors: append(Vec[RGB](nil), ThemeColors...), ThemeName: "Craft", FootnoteFormat: FmtDecimal,
		EndnoteFormat: FmtLowerRoman, CompatMode: CompatModeCurrent, GridH: DefaultGrid, GridV: DefaultGrid}
}

// Zoom is a saved zoom: a percentage, or a fit mode (the percentage it last came to).
type Zoom struct {
	// Percent is 10–500.
	Percent uint32   `json:"percent"`
	Fit     *ZoomFit `json:"fit"`
}

// ZoomFit is one of View › Zoom's fit modes a document can be saved in.
type ZoomFit uint8

// Fit modes.
const (
	FitOnePage ZoomFit = iota
	FitPageWidth
	FitTextWidth
)

var zoomFitNames = []string{"onePage", "pageWidth", "textWidth"}

func (z *ZoomFit) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, zoomFitNames, z) }
func (z ZoomFit) MarshalJSON() ([]byte, error)           { return writeEnum(zoomFitNames, z) }

// PagePicture is a picture fill for the page background (v:background/v:fill with an image).
type PagePicture struct {
	// Media is a key into Document media.
	Media string `json:"media"`
	// FillType is v:fill/@type: tiled when tile, stretched to the page otherwise.
	FillType string `json:"fill_type"`
	// Title is o:title of the fill (texture name), kept for round trip.
	Title string `json:"title"`
}

func (PagePicture) SerdeDefault() {}

// Watermark is a text watermark.
type Watermark struct {
	Text            string `json:"text"`
	Font            string `json:"font"`
	Color           RGB    `json:"color"`
	Diagonal        bool   `json:"diagonal"`
	Semitransparent bool   `json:"semitransparent"`
}

func (Watermark) SerdeDefault() {}

// SetDefaults sets the Rust Default: a grey diagonal DRAFT.
func (w *Watermark) SetDefaults() {
	*w = Watermark{Text: "DRAFT", Font: BodyFont, Color: RGB{0xC0, 0xC0, 0xC0}, Diagonal: true, Semitransparent: true}
}

// MathProps are the document-wide equation options (m:mathPr in the settings part) that layout
// uses: how long display equations wrap.
type MathProps struct {
	BrkBin    BrkBin    `json:"brkBin"`
	BrkBinSub BrkBinSub `json:"brkBinSub"`
	// WrapIndent is the indent of a wrapped equation's continuation lines, points
	// (m:wrapIndent).
	WrapIndent F32 `json:"wrapIndent"`
	// WrapRight aligns continuation lines right instead (m:wrapRight).
	WrapRight bool `json:"wrapRight"`
}

func (MathProps) SerdeDefault() {}

// DefaultWrapIndent is m:wrapIndent's default: 1440 twips, one inch.
const DefaultWrapIndent = 72.0

// SetDefaults sets the Rust Default.
func (m *MathProps) SetDefaults() { *m = MathProps{WrapIndent: DefaultWrapIndent} }

// BrkBin is where a display equation too wide for its line breaks at a binary operator
// (m:brkBin).
type BrkBin uint8

// Break positions.
const (
	// BrkBefore: the operator starts the next line.
	BrkBefore BrkBin = iota
	// BrkAfter: the operator ends the line.
	BrkAfter
	// BrkRepeat: the operator ends the line and starts the next one again.
	BrkRepeat
)

var brkBinNames = []string{"before", "after", "repeat"}

func (b *BrkBin) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, brkBinNames, b) }
func (b BrkBin) MarshalJSON() ([]byte, error)           { return writeEnum(brkBinNames, b) }

// BrkBinSub is how a minus repeats across a break when BrkRepeat (m:brkBinSub): the operator
// ending the line, then the one starting the next.
type BrkBinSub uint8

// Repeated minus forms.
const (
	MinusMinus BrkBinSub = iota // --
	MinusPlus                   // -+
	PlusMinus                   // +-
)

var brkBinSubNames = []string{"minusMinus", "minusPlus", "plusMinus"}

func (b *BrkBinSub) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, brkBinSubNames, b) }
func (b BrkBinSub) MarshalJSON() ([]byte, error)           { return writeEnum(brkBinSubNames, b) }

// PartKind is what a secondary story is.
type PartKind uint8

// Part kinds.
const (
	PartHeader PartKind = iota
	PartFooter
	PartFootnote
	PartEndnote
	PartComment
	PartTextBox
)

var partKindNames = []string{"header", "footer", "footnote", "endnote", "comment", "textBox"}

func (k *PartKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, partKindNames, k) }
func (k PartKind) MarshalJSON() ([]byte, error)           { return writeEnum(partKindNames, k) }

// Part is a secondary story.
type Part struct {
	Kind   PartKind   `json:"kind"`
	Blocks Vec[Block] `json:"blocks"`
	// Body is a text box's text direction, alignment and link (unused by other parts).
	Body TextBody `json:"body,omitzero" serde:"default"`
}

// TextBody is how a text box shows its text (Shape Format › Text): which way it runs, where it
// sits between the box's top and bottom, and the box its overflow continues in.
type TextBody struct {
	Vert   TextVert `json:"vert"`
	Anchor VAlign   `json:"anchor"`
	// Next is Create Link: the text box (another TextBox part, shown by its own shape) the text
	// continues in when it doesn't fit this one.
	Next *uint32 `json:"next"`
	// Inset are the internal margins (left, top, right, bottom), EMU, when the file gives its
	// own; nil is Word's defaults.
	Inset *[4]uint32 `json:"inset"`
	// AutoFit resizes the shape to fit its text.
	AutoFit bool `json:"autoFit"`
	// AutoColor is the colour the shape's style gives its text: what automatic text colour
	// means inside this box, instead of black.
	AutoColor *RGB `json:"autoColor,omitempty"`
}

func (TextBody) SerdeDefault() {}

// IsDefault reports the default text body.
func (t TextBody) IsDefault() bool { return equal(t, TextBody{}) }

// IsZero lets omitzero leave a default text body out, as Rust does.
func (t TextBody) IsZero() bool { return t.IsDefault() }

// Text box margins.
const (
	// TextBoxInset are Word's default text box margins (left, top, right, bottom), EMU: 0.1" and
	// 0.05".
	textBoxInsetH = 91_440
	textBoxInsetV = 45_720
	// MaxTextBoxInset is the widest text box margin kept, EMU (22", the largest page side).
	MaxTextBoxInset = 20_116_800
)

// TextBoxInset are Word's default text box margins (left, top, right, bottom), EMU.
var TextBoxInset = [4]uint32{textBoxInsetH, textBoxInsetV, textBoxInsetH, textBoxInsetV}

// InsetsPt are the internal margins (left, top, right, bottom) in points.
func (t TextBody) InsetsPt() [4]F32 {
	in := TextBoxInset
	if t.Inset != nil {
		in = *t.Inset
	}
	var out [4]F32
	for i, v := range in {
		out[i] = F32(float32(min(v, MaxTextBoxInset)) / 12_700)
	}
	return out
}

// Comment is a comment.
type Comment struct {
	Author   string `json:"author"`
	Initials string `json:"initials"`
	// Date is ISO 8601.
	Date string `json:"date"`
	// Parent is the comment this one replies to.
	Parent   *uint32 `json:"parent"`
	Resolved bool    `json:"resolved"`
	// Part is the Document parts id of the comment text.
	Part uint32 `json:"part"`
	// DurableID is Word's durable id for the comment (w16cid:durableId, hex), kept across saves
	// so Word keeps tracking the same comment. nil for comments made here: the writer assigns
	// one.
	DurableID *string `json:"durableId,omitempty"`
	// DateUTC is the comment's UTC timestamp from the file (w16cex:dateUtc); Date is local time
	// there.
	DateUTC *string `json:"dateUtc,omitempty"`
}

func (Comment) SerdeDefault() {}

// RevisionKind is what a tracked change did.
type RevisionKind uint8

// Revision kinds.
const (
	RevInsert RevisionKind = iota
	RevDelete
	RevFormat
)

var revisionKindNames = []string{"insert", "delete", "format"}

func (k *RevisionKind) DecodeSerde(d serdejson.Decoder) error {
	return readEnum(d, revisionKindNames, k)
}
func (k RevisionKind) MarshalJSON() ([]byte, error) { return writeEnum(revisionKindNames, k) }

// Revision is a tracked change's author and date.
type Revision struct {
	Kind   RevisionKind `json:"kind"`
	Author string       `json:"author"`
	Date   string       `json:"date"`
	// MoveName links the two ends of a move (ECMA-376 §17.13.5.21–28): the source (a delete,
	// w:moveFrom) and the destination (an insert, w:moveTo). nil = a plain insertion or
	// deletion.
	MoveName *string `json:"move_name,omitempty"`
}

func (Revision) SerdeDefault() {}

// BuildingBlock is a building block (Insert › Quick Parts, AutoText…) kept in the document's
// glossary (ECMA-376 §17.12: w:docPart).
type BuildingBlock struct {
	// Name is the entry's name (w:docPartPr/w:name).
	Name string `json:"name"`
	// Gallery is its gallery (w:category/w:gallery: docParts for Quick Parts, autoTxt for
	// AutoText…).
	Gallery string `json:"gallery"`
	// Category is its category within the gallery (w:category/w:name, e.g. "General").
	Category    string `json:"category"`
	Description string `json:"description,omitempty"`
	// GUID is the entry's identity (w:guid), kept as read.
	GUID string `json:"guid,omitempty"`
	// Style is the paragraph style it's associated with (w:style), kept as read.
	Style string `json:"style,omitempty"`
	// Types are its types (w:types/w:type: normal, bbPlcHdr for a content control's
	// placeholder…).
	Types Vec[string] `json:"types,omitempty"`
	// Behaviors are how it's inserted (w:behaviors/w:behavior: content, p, pg).
	Behaviors Vec[string] `json:"behaviors,omitempty"`
	// Blocks are the content (w:docPartBody).
	Blocks Vec[Block] `json:"blocks"`
}

func (BuildingBlock) SerdeDefault() {}

// Galleries.
const (
	// QuickPartsGallery is the gallery Quick Parts saves to.
	QuickPartsGallery = "docParts"
	// AutoTextGallery is the AutoText gallery.
	AutoTextGallery = "autoTxt"
)

// Source is a bibliography source.
type Source struct {
	// Tag is the unique tag cited by CITATION fields.
	Tag string `json:"tag"`
	// Kind is book, bookSection, article (journal), periodical, conference, report, website,
	// film, other.
	Kind string `json:"kind"`
	// Author is "Last, First; Last, First".
	Author    string `json:"author"`
	Title     string `json:"title"`
	Year      string `json:"year"`
	Publisher string `json:"publisher"`
	City      string `json:"city"`
	Journal   string `json:"journal"`
	Volume    string `json:"volume"`
	Pages     string `json:"pages"`
	URL       string `json:"url"`
	// Editor and the fields after it are Create Source › Show All Bibliography Fields.
	Editor  string `json:"editor"`
	Edition string `json:"edition"`
	Issue   string `json:"issue"`
	Month   string `json:"month"`
	Day     string `json:"day"`
	// Accessed is the date accessed (websites).
	Accessed string `json:"accessed"`
	DOI      string `json:"doi"`
}

func (Source) SerdeDefault() {}

// CustomProp is a custom document property (File › Info › Properties › Custom).
type CustomProp struct {
	Name string `json:"name"`
	// Kind is the OOXML variant type: lpwstr, i4, r8, bool, filetime…, or raw when Value is the
	// property's XML content kept verbatim (vectors, blobs).
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (CustomProp) SerdeDefault() {}
