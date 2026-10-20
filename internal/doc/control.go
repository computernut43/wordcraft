package doc

import (
	"unicode/utf8"

	"github.com/computernut43/craft-libs/serdejson"
)

// Content controls (w:sdt, ECMA-376 §17.5.2): fields of a form or template — rich and plain text,
// check boxes, combo boxes and drop-down lists, date pickers, pictures, building block galleries
// and repeating sections (crates/doc/src/control.rs; the walks over a document's controls come
// with the inline objects). A control whose content starts or ends with a table, and controls
// around table rows or cells, are kept on the table, row or cell (ControlWrap).

// Limits on what is kept from a file.
const (
	// MaxControlText is the longest tag, title or list text kept from a file.
	MaxControlText = 1024
	// MaxListItems is the most list entries a combo box or drop-down list keeps.
	MaxListItems = 1000
)

// ControlLock is what may be done to a control (w:lock).
type ControlLock uint8

// Control locks.
const (
	Unlocked ControlLock = iota
	// SdtLocked: the control can't be deleted; its content can be edited.
	SdtLocked
	// ContentLocked: the content can't be edited; the control can be deleted.
	ContentLocked
	// SdtContentLocked: neither.
	SdtContentLocked
)

var controlLockNames = []string{"unlocked", "sdtLocked", "contentLocked", "sdtContentLocked"}

func (l *ControlLock) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, controlLockNames, l) }
func (l ControlLock) MarshalJSON() ([]byte, error)           { return writeEnum(controlLockNames, l) }

// ControlLockFromOOXML reads a w:lock value; anything else is unlocked.
func ControlLockFromOOXML(v string) ControlLock {
	for i, n := range controlLockNames[1:] {
		if n == v {
			return ControlLock(i + 1)
		}
	}
	return Unlocked
}

// OOXML is the w:lock value; false when unlocked.
func (l ControlLock) OOXML() (string, bool) {
	if l == Unlocked || int(l) >= len(controlLockNames) {
		return "", false
	}
	return controlLockNames[l], true
}

// ControlLockFromFlags is the lock that forbids deleting the control and/or editing its content.
func ControlLockFromFlags(noDelete, noEdit bool) ControlLock {
	switch {
	case noDelete && noEdit:
		return SdtContentLocked
	case noDelete:
		return SdtLocked
	case noEdit:
		return ContentLocked
	}
	return Unlocked
}

// CanDelete reports whether the control itself can be deleted.
func (l ControlLock) CanDelete() bool { return l == Unlocked || l == ContentLocked }

// CanEdit reports whether its content can be edited.
func (l ControlLock) CanEdit() bool { return l == Unlocked || l == SdtLocked }

// ListItem is an entry of a combo box or drop-down list.
type ListItem struct {
	// Display is what the control shows.
	Display string `json:"display"`
	// Value is what it stores (w:value); the display text when empty.
	Value string `json:"value"`
}

func (ListItem) SerdeDefault() {}

// Stored is the value stored: Value, or Display when that is empty.
func (i ListItem) Stored() string {
	if i.Value == "" {
		return i.Display
	}
	return i.Value
}

// Char is a Rust char: a string of exactly one character.
type Char rune

// DecodeSerde reads a one-character string.
func (c *Char) DecodeSerde(d serdejson.Decoder) error {
	s, err := d.String()
	if err != nil {
		return err
	}
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || n != len(s) {
		return d.Fail("invalid value: string, expected a character")
	}
	*c = Char(r)
	return nil
}

// MarshalJSON writes the character as a string.
func (c Char) MarshalJSON() ([]byte, error) { return serdejson.MarshalCompact(string(rune(c))) }

// ControlType is the kind of a content control.
type ControlType uint8

// Control types.
const (
	// RichTextControl holds formatted text, paragraphs, anything (also controls of a type
	// WordCraft doesn't model, whose type element is kept in ContentControl.Extra).
	RichTextControl ControlType = iota
	// TextControl is unformatted text (w:text), on one line unless MultiLine.
	TextControl
	// CheckBoxControl is a check box (w14:checkbox): the content is the state's symbol.
	CheckBoxControl
	// ComboBoxControl is a list to choose from, or type into (w:comboBox).
	ComboBoxControl
	// DropDownControl is a list to choose from (w:dropDownList).
	DropDownControl
	// DateControl is a date (w:date): FullDate is ISO 8601, Format a date picture (M/d/yyyy).
	DateControl
	// PictureControl is a picture (w:picture).
	PictureControl
	// GalleryControl is a building block gallery (w:docPartList) or a building block placed from
	// one (w:docPartObj: tables of contents, cover pages, bibliographies).
	GalleryControl
	// RepeatingSectionControl is a repeating section (w15:repeatingSection), whose items are
	// repeated.
	RepeatingSectionControl
	// RepeatingSectionItemControl is one item of a repeating section
	// (w15:repeatingSectionItem).
	RepeatingSectionItemControl
)

var controlTypeNames = []string{"richText", "text", "checkBox", "comboBox", "dropDown", "date", "picture", "gallery", "repeatingSection",
	"repeatingSectionItem"}

var controlCommandNames = []string{"richText", "plainText", "checkBox", "comboBox", "dropDown", "date", "picture", "buildingBlock",
	"repeatingSection", "repeatingSectionItem"}

// ControlKind is the kind of a content control and its settings (Rust's ControlKind enum, written
// {"type": …, fields}). Only the fields of its Type are used; the others stay zero.
type ControlKind struct {
	Type ControlType
	// MultiLine (text).
	MultiLine bool
	// Check box state and symbols.
	Checked       bool
	CheckedChar   rune
	CheckedFont   string
	UncheckedChar rune
	UncheckedFont string
	// Items and LastValue (combo box, drop-down list).
	Items     Vec[ListItem]
	LastValue string
	// Date settings.
	FullDate, Format, Lid, Calendar, StoreAs string
	// Gallery: List is w:docPartList rather than w:docPartObj.
	List              bool
	Gallery, Category string
	Unique            bool
	// Repeating section.
	Title          string
	NoInsertDelete bool
}

// Default check box symbols: ballot box with X, ballot box.
const (
	Checked   = '☒'
	Unchecked = '☐'
)

// CheckBoxKind is an unchecked check box with the default symbols.
func CheckBoxKind() ControlKind {
	return ControlKind{Type: CheckBoxControl, CheckedChar: Checked, UncheckedChar: Unchecked}
}

type textFields struct {
	MultiLine bool `json:"multiLine" serde:"default"`
}

type checkBoxFields struct {
	Checked       bool   `json:"checked"`
	CheckedChar   Char   `json:"checkedChar"`
	CheckedFont   string `json:"checkedFont"`
	UncheckedChar Char   `json:"uncheckedChar"`
	UncheckedFont string `json:"uncheckedFont"`
}

type listFields struct {
	Items     Vec[ListItem] `json:"items"`
	LastValue string        `json:"lastValue" serde:"default"`
}

type dateFields struct {
	FullDate string `json:"fullDate" serde:"default"`
	Format   string `json:"format" serde:"default"`
	Lid      string `json:"lid" serde:"default"`
	Calendar string `json:"calendar" serde:"default"`
	StoreAs  string `json:"storeAs" serde:"default"`
}

type galleryFields struct {
	List     bool   `json:"list" serde:"default"`
	Gallery  string `json:"gallery" serde:"default"`
	Category string `json:"category" serde:"default"`
	Unique   bool   `json:"unique" serde:"default"`
}

type repeatingFields struct {
	Title          string `json:"title" serde:"default"`
	NoInsertDelete bool   `json:"noInsertDelete" serde:"default"`
}

// DecodeSerde reads serde's internally tagged form.
func (k *ControlKind) DecodeSerde(d serdejson.Decoder) error {
	i, rest, err := d.Tagged("type", controlTypeNames)
	if err != nil {
		return err
	}
	v := ControlKind{Type: ControlType(i)}
	switch v.Type {
	case TextControl:
		var f textFields
		err = rest.Decode(&f)
		v.MultiLine = f.MultiLine
	case CheckBoxControl:
		var f checkBoxFields
		err = rest.Decode(&f)
		v.Checked, v.CheckedChar, v.CheckedFont, v.UncheckedChar, v.UncheckedFont = f.Checked, rune(f.CheckedChar), f.CheckedFont,
			rune(f.UncheckedChar), f.UncheckedFont
	case ComboBoxControl, DropDownControl:
		var f listFields
		err = rest.Decode(&f)
		v.Items, v.LastValue = f.Items, f.LastValue
	case DateControl:
		var f dateFields
		err = rest.Decode(&f)
		v.FullDate, v.Format, v.Lid, v.Calendar, v.StoreAs = f.FullDate, f.Format, f.Lid, f.Calendar, f.StoreAs
	case GalleryControl:
		var f galleryFields
		err = rest.Decode(&f)
		v.List, v.Gallery, v.Category, v.Unique = f.List, f.Gallery, f.Category, f.Unique
	case RepeatingSectionControl:
		var f repeatingFields
		err = rest.Decode(&f)
		v.Title, v.NoInsertDelete = f.Title, f.NoInsertDelete
	default:
		err = serdejson.TaggedUnit(rest)
	}
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// MarshalJSON writes {"type": …} followed by the variant's fields.
func (k ControlKind) MarshalJSON() ([]byte, error) {
	var fields any
	switch k.Type {
	case TextControl:
		fields = textFields{k.MultiLine}
	case CheckBoxControl:
		fields = checkBoxFields{k.Checked, Char(k.CheckedChar), k.CheckedFont, Char(k.UncheckedChar), k.UncheckedFont}
	case ComboBoxControl, DropDownControl:
		items := k.Items
		if items == nil {
			items = Vec[ListItem]{}
		}
		fields = listFields{items, k.LastValue}
	case DateControl:
		fields = dateFields{k.FullDate, k.Format, k.Lid, k.Calendar, k.StoreAs}
	case GalleryControl:
		fields = galleryFields{k.List, k.Gallery, k.Category, k.Unique}
	case RepeatingSectionControl:
		fields = repeatingFields{k.Title, k.NoInsertDelete}
	}
	tag, err := writeEnum(controlTypeNames, k.Type)
	if err != nil {
		return nil, err
	}
	out := append([]byte(`{"type":`), tag...)
	if fields == nil {
		return append(out, '}'), nil
	}
	b, err := serdejson.MarshalCompact(fields)
	if err != nil {
		return nil, err
	}
	if len(b) > 2 {
		out = append(out, ',')
		out = append(out, b[1:]...)
		return out, nil
	}
	return append(out, '}'), nil
}

// Name is the command-friendly name (richText, checkBox…).
func (k ControlKind) Name() string { return enumName(controlCommandNames, k.Type) }

// ListItems are the list entries of a combo box or drop-down list; false for other kinds.
func (k ControlKind) ListItems() (Vec[ListItem], bool) {
	if k.Type == ComboBoxControl || k.Type == DropDownControl {
		return k.Items, true
	}
	return nil, false
}

// TakesTyping reports whether typed text may go in (not a check box, drop-down list or picture).
func (k ControlKind) TakesTyping() bool {
	return k.Type != CheckBoxControl && k.Type != DropDownControl && k.Type != PictureControl
}

// DefaultPlaceholder is the text its placeholder shows by default.
func (k ControlKind) DefaultPlaceholder() string {
	switch k.Type {
	case ComboBoxControl, DropDownControl:
		return "Pick an option."
	case DateControl:
		return "Pick a date."
	case CheckBoxControl, PictureControl, RepeatingSectionControl, RepeatingSectionItemControl:
		return ""
	}
	return "Type here."
}

// ContentControl is a content control's properties (w:sdtPr).
type ContentControl struct {
	// ID is w:id (unique in the document).
	ID *int64 `json:"id,omitempty"`
	// Tag is w:tag: a name for programs.
	Tag string `json:"tag"`
	// Title is w:alias: the title shown on the control.
	Title string      `json:"title"`
	Lock  ControlLock `json:"lock"`
	// Placeholder is the building block holding the placeholder text (w:placeholder/w:docPart).
	Placeholder string `json:"placeholder"`
	// ShowingPlaceholder: the content is the placeholder text (w:showingPlcHdr).
	ShowingPlaceholder bool `json:"showingPlaceholder"`
	// Temporary: the control goes away once its content is edited (w:temporary).
	Temporary bool        `json:"temporary"`
	Kind      ControlKind `json:"kind"`
	// PlaceholderText is the text shown while the control is empty.
	PlaceholderText string `json:"placeholderText"`
	// Block: around whole paragraphs rather than inside one.
	Block bool `json:"block"`
	// RPrXML is the run properties of w:sdtPr (w:rPr), kept verbatim (XML).
	RPrXML string `json:"rprXml,omitempty"`
	// RunProps are the same run properties, read: the formatting text typed in place of the
	// placeholder takes (ECMA-376 §17.5.2.27). nil when the control names none.
	RunProps *CharProps `json:"runProps,omitempty"`
	// Extra holds the other w:sdtPr children WordCraft doesn't model, kept verbatim (XML) in file
	// order.
	Extra Vec[string] `json:"extra,omitempty"`
	// EndPrXML is w:sdtEndPr, kept verbatim (XML).
	EndPrXML string `json:"endPrXml,omitempty"`
}

func (ContentControl) SerdeDefault() {}

// NewContentControl is a control of kind with its default placeholder text.
func NewContentControl(kind ControlKind) ContentControl {
	return ContentControl{Kind: kind, PlaceholderText: kind.DefaultPlaceholder()}
}

// PlaceholderShown is the text to show while empty.
func (c *ContentControl) PlaceholderShown() string {
	if c.PlaceholderText == "" {
		return c.Kind.DefaultPlaceholder()
	}
	return c.PlaceholderText
}

// ControlWrap holds the content controls around a table, row or cell: the controls that open
// just before it, outermost first, and how many of the open ones close just after it.
type ControlWrap struct {
	Open  Vec[ContentControl] `json:"open"`
	Close uint32              `json:"close"`
}

func (ControlWrap) SerdeDefault() {}

// IsZero reports no controls (Rust's is_empty, which leaves the wrap out of the JSON).
func (w ControlWrap) IsZero() bool { return len(w.Open) == 0 && w.Close == 0 }
