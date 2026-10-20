package doc

import (
	"fmt"

	"github.com/computernut43/craft-libs/serdejson"
)

// Inline objects (para.rs InlineObject): what a U+FFFC in a paragraph's text stands for. Their
// JSON is serde's internally tagged form, {"type": "image", …}, with the variant's field names
// as Rust spells them (snake case: stroke_width, ch_w, relative_to, base_size).

// InlineObject is one of the object types below (Image, GraphicObject, Shape, Group, Field,
// FieldStart, FieldEnd, ControlStart, ControlEnd, NoteRef, BookmarkStart, BookmarkEnd,
// CommentStart, CommentEnd, PermStart, PermEnd, Equation, Ruby, PositionalTab, Opaque).
type InlineObject interface{ objectType() string }

// Object holds an inline object; its JSON is the object's.
type Object struct{ InlineObject }

// Image is a picture.
type Image struct {
	// Media is the key in Document.Media.
	Media string `json:"media"`
	// W and H are the display size, points.
	W     F32    `json:"w"`
	H     F32    `json:"h"`
	Alt   string `json:"alt" serde:"default"`
	Float Float  `json:"float" serde:"default"`
	// Crop is (left, top, right, bottom) fractions 0..1.
	Crop [4]F32 `json:"crop" serde:"default"`
	// OLE is the OLE object (embedded or linked file) this picture shows, as read from a file:
	// saving writes the object back, not only its picture.
	OLE *Embedded `json:"ole,omitempty" serde:"default"`
}

// GraphicObject is a chart or SmartArt diagram (Rust's InlineObject::Graphic).
type GraphicObject struct {
	W       F32     `json:"w"`
	H       F32     `json:"h"`
	Alt     string  `json:"alt" serde:"default"`
	Float   Float   `json:"float" serde:"default"`
	Graphic Graphic `json:"graphic"`
}

// Shape is a preset shape, text box, freeform or connector.
type Shape struct {
	Kind        ShapeKind `json:"kind"`
	W           F32       `json:"w"`
	H           F32       `json:"h"`
	Fill        *RGB      `json:"fill"`
	Stroke      *RGB      `json:"stroke"`
	StrokeWidth F32       `json:"stroke_width" serde:"default"`
	Float       Float     `json:"float" serde:"default"`
	// Story is a text box's content: a Document.Parts id.
	Story *uint32 `json:"story" serde:"default"`
	// Freeform is a freeform's geometry (ShapeFreeform): its paths, or an ink stroke.
	Freeform *Freeform `json:"freeform,omitempty" serde:"default"`
	// Effects are the shadow, glow and soft edges.
	Effects ShapeEffects `json:"effects,omitzero" serde:"default"`
	// Extra is the drawing id, text warp, connector ends and arrowheads.
	Extra ShapeExtra `json:"extra,omitzero" serde:"default"`
}

// Group is pictures, shapes and text boxes grouped into one object (Layout › Arrange › Group).
// Its members are laid out in the group's own coordinate space, ChW × ChH, which is stretched
// over the group's W × H (DrawingML's a:chExt and a:ext), so resizing the group scales them.
type Group struct {
	W     F32   `json:"w"`
	H     F32   `json:"h"`
	Float Float `json:"float" serde:"default"`
	// ChW and ChH are the size of the members' coordinate space.
	ChW      F32             `json:"ch_w"`
	ChH      F32             `json:"ch_h"`
	Children Vec[GroupChild] `json:"children"`
	// Canvas makes it a Drawing Canvas (wpc:wpc) rather than a group: it has its own background
	// and outline, its frame is independent of its members (resizing it doesn't scale them), and
	// what lies outside the frame is clipped.
	Canvas *CanvasStyle `json:"canvas,omitempty" serde:"default"`
}

// MaxGroupChildren is the most members a group keeps (hostile files).
const MaxGroupChildren = 1000

// GroupChild is one member of a Group: a picture or shape at X, Y in the group's coordinate
// space. Its Float is unused.
type GroupChild struct {
	X   F32    `json:"x"`
	Y   F32    `json:"y"`
	Obj Object `json:"obj"`
}

// Field is a field: Instr is the field code (PAGE, NUMPAGES, DATE \@ "M/d/yyyy", TOC \o "1-3"…),
// Result the cached display text. A Locked field keeps its result when fields update.
type Field struct {
	Instr  string `json:"instr"`
	Result string `json:"result" serde:"default"`
	Locked bool   `json:"locked" serde:"default"`
	// Dirty is w:dirty: the cached result is stale. Kept through saves; cleared when WordCraft
	// computes a result.
	Dirty bool `json:"dirty,omitempty" serde:"default"`
	// Code shows the field's code instead of its result (Shift+F9). A view toggle: never written
	// to files.
	Code bool `json:"code,omitempty" serde:"default"`
	// Form is a legacy form field's definition (FORMCHECKBOX, FORMTEXT, FORMDROPDOWN): the
	// source's w:ffData, kept for round trip. Empty for other fields.
	Form string `json:"form,omitempty" serde:"default"`
}

// FieldStart is the start of a field whose result is ordinary content up to the matching
// FieldEnd (imported TOCs, citation managers' ADDIN fields).
type FieldStart struct {
	Instr  string `json:"instr"`
	Locked bool   `json:"locked" serde:"default"`
	Dirty  bool   `json:"dirty,omitempty" serde:"default"`
}

// FieldEnd ends the innermost open FieldStart.
type FieldEnd struct{}

// ControlStart is the start of a content control whose content is what follows, up to the
// matching ControlEnd.
type ControlStart struct {
	Control ContentControl `json:"control"`
}

// ControlEnd ends the innermost open ControlStart.
type ControlEnd struct{}

// NoteRef is a footnote or endnote reference.
type NoteRef struct {
	Kind NoteKind `json:"kind"`
	// ID is the Document.Parts id of the note's story.
	ID uint32 `json:"id"`
	// Custom is a custom mark (empty = automatic number).
	Custom string `json:"custom" serde:"default"`
}

// BookmarkStart starts bookmark Name.
type BookmarkStart struct {
	Name string `json:"name"`
}

// BookmarkEnd ends bookmark Name.
type BookmarkEnd struct {
	Name string `json:"name"`
}

// CommentStart starts the range comment ID is about.
type CommentStart struct {
	ID uint32 `json:"id"`
}

// CommentEnd ends it.
type CommentEnd struct {
	ID uint32 `json:"id"`
}

// PermStart starts a range that stays editable while the document is protected (w:permStart):
// for everyone (Group "everyone"), another editing group, or one Editor.
type PermStart struct {
	ID     uint32 `json:"id"`
	Group  string `json:"group,omitempty" serde:"default"`
	Editor string `json:"editor,omitempty" serde:"default"`
}

// PermEnd ends the editable range PermStart ID opened.
type PermEnd struct {
	ID uint32 `json:"id"`
}

// Equation is an equation: its linear format (for plain text) and structure. A display equation
// sits on a line of its own.
type Equation struct {
	Linear  string `json:"linear"`
	Display bool   `json:"display" serde:"default"`
	// Math is the structure; empty = parse Linear.
	Math Math `json:"math,omitzero" serde:"default"`
}

// Ruby is a Phonetic Guide: small Ruby text set over Base text. The base text takes the
// formatting of the object's run.
type Ruby struct {
	Base  string    `json:"base"`
	Ruby  string    `json:"ruby"`
	Align RubyAlign `json:"align" serde:"default"`
	// Size is the ruby text size, points (w:hps).
	Size F32 `json:"size"`
	// Raise is from the base text's baseline up to the ruby text's baseline, points (w:hpsRaise).
	Raise F32 `json:"raise"`
	// BaseSize is the base text size the ruby was set for, points (w:hpsBaseText).
	BaseSize F32 `json:"base_size" serde:"default"`
	// Lang is the language of the ruby (w:lid); empty = unspecified.
	Lang string `json:"lang,omitempty" serde:"default"`
	// Props is the formatting of the ruby text over the run's; its size is Size.
	Props CharProps `json:"props,omitzero" serde:"default"`
}

// PositionalTab is Word's alignment tab (w:ptab): it goes to the left edge, centre or right edge
// of the margins or of the indents, whatever the paragraph's tab stops.
type PositionalTab struct {
	Align      PTabAlign `json:"align" serde:"default"`
	RelativeTo PTabBase  `json:"relative_to" serde:"default"`
	Leader     TabLeader `json:"leader" serde:"default"`
}

// Opaque is something WordCraft doesn't model, kept for round trip (raw XML of the source
// format).
type Opaque struct {
	Format string `json:"format"`
	XML    string `json:"xml"`
	Text   string `json:"text" serde:"default"`
}

func (Image) objectType() string         { return "image" }
func (GraphicObject) objectType() string { return "graphic" }
func (Shape) objectType() string         { return "shape" }
func (Group) objectType() string         { return "group" }
func (Field) objectType() string         { return "field" }
func (FieldStart) objectType() string    { return "fieldStart" }
func (FieldEnd) objectType() string      { return "fieldEnd" }
func (ControlStart) objectType() string  { return "controlStart" }
func (ControlEnd) objectType() string    { return "controlEnd" }
func (NoteRef) objectType() string       { return "noteRef" }
func (BookmarkStart) objectType() string { return "bookmarkStart" }
func (BookmarkEnd) objectType() string   { return "bookmarkEnd" }
func (CommentStart) objectType() string  { return "commentStart" }
func (CommentEnd) objectType() string    { return "commentEnd" }
func (PermStart) objectType() string     { return "permStart" }
func (PermEnd) objectType() string       { return "permEnd" }
func (Equation) objectType() string      { return "equation" }
func (Ruby) objectType() string          { return "ruby" }
func (PositionalTab) objectType() string { return "positionalTab" }
func (Opaque) objectType() string        { return "opaque" }

var objectTypes = []string{"image", "graphic", "shape", "group", "field", "fieldStart", "fieldEnd", "controlStart", "controlEnd", "noteRef",
	"bookmarkStart", "bookmarkEnd", "commentStart", "commentEnd", "permStart", "permEnd", "equation", "ruby", "positionalTab", "opaque"}

// Type is the object's type name ("image", "field", …), as its JSON writes it.
func (o Object) Type() string {
	if o.InlineObject == nil {
		return ""
	}
	return o.objectType()
}

// DecodeSerde reads serde's internally tagged form.
func (o *Object) DecodeSerde(d serdejson.Decoder) error {
	i, rest, err := d.Tagged("type", objectTypes)
	if err != nil {
		return err
	}
	var v InlineObject
	switch objectTypes[i] {
	case "image":
		v, err = decodeAs[Image](rest)
	case "graphic":
		v, err = decodeAs[GraphicObject](rest)
	case "shape":
		v, err = decodeAs[Shape](rest)
	case "group":
		v, err = decodeAs[Group](rest)
	case "field":
		v, err = decodeAs[Field](rest)
	case "fieldStart":
		v, err = decodeAs[FieldStart](rest)
	case "fieldEnd":
		v, err = FieldEnd{}, serdejson.TaggedUnit(rest)
	case "controlStart":
		v, err = decodeAs[ControlStart](rest)
	case "controlEnd":
		v, err = ControlEnd{}, serdejson.TaggedUnit(rest)
	case "noteRef":
		v, err = decodeAs[NoteRef](rest)
	case "bookmarkStart":
		v, err = decodeAs[BookmarkStart](rest)
	case "bookmarkEnd":
		v, err = decodeAs[BookmarkEnd](rest)
	case "commentStart":
		v, err = decodeAs[CommentStart](rest)
	case "commentEnd":
		v, err = decodeAs[CommentEnd](rest)
	case "permStart":
		v, err = decodeAs[PermStart](rest)
	case "permEnd":
		v, err = decodeAs[PermEnd](rest)
	case "equation":
		v, err = decodeAs[Equation](rest)
	case "ruby":
		v, err = decodeAs[Ruby](rest)
	case "positionalTab":
		v, err = decodeAs[PositionalTab](rest)
	default:
		v, err = decodeAs[Opaque](rest)
	}
	if err != nil {
		return err
	}
	*o = Object{v}
	return nil
}

// MarshalJSON writes {"type": …} and the object's fields.
func (o Object) MarshalJSON() ([]byte, error) {
	switch v := o.InlineObject.(type) {
	case nil:
		return nil, fmt.Errorf("doc: empty inline object")
	case FieldEnd, ControlEnd:
		return tagged("type", v.objectType(), nil)
	}
	return tagged("type", o.objectType(), o.InlineObject)
}

// Equal compares like Rust's PartialEq.
func (o Object) Equal(p Object) bool { return equal(o, p) }

// Frame is a drawing's size and placement; false for anything else.
func (o Object) Frame() (w, h F32, f Float, ok bool) {
	switch v := o.InlineObject.(type) {
	case Image:
		return v.W, v.H, v.Float, true
	case GraphicObject:
		return v.W, v.H, v.Float, true
	case Shape:
		return v.W, v.H, v.Float, true
	case Group:
		return v.W, v.H, v.Float, true
	}
	return 0, 0, Float{}, false
}

// IsDrawing reports a picture, chart or diagram, shape, text box or group: something drawn with
// a frame, sized and wrapped.
func (o Object) IsDrawing() bool {
	_, _, _, ok := o.Frame()
	return ok
}

// WithFloat is the drawing with its placement replaced (anything else unchanged).
func (o Object) WithFloat(f Float) Object {
	switch v := o.InlineObject.(type) {
	case Image:
		v.Float = f
		return Object{v}
	case GraphicObject:
		v.Float = f
		return Object{v}
	case Shape:
		v.Float = f
		return Object{v}
	case Group:
		v.Float = f
		return Object{v}
	}
	return o
}

// WithSize is the drawing resized (a group's members scale with it; a canvas's keep their size
// and place).
func (o Object) WithSize(w, h F32) Object {
	switch v := o.InlineObject.(type) {
	case Image:
		v.W, v.H = w, h
		return Object{v}
	case GraphicObject:
		v.W, v.H = w, h
		return Object{v}
	case Shape:
		v.W, v.H = w, h
		return Object{v}
	case Group:
		v.W, v.H = w, h
		if v.Canvas != nil {
			v.ChW, v.ChH = w, h
		}
		return Object{v}
	}
	return o
}

// CanvasStyle is a Drawing Canvas's background and outline; false for anything else.
func (o Object) CanvasStyle() (CanvasStyle, bool) {
	if g, ok := o.InlineObject.(Group); ok && g.Canvas != nil {
		return *g.Canvas, true
	}
	return CanvasStyle{}, false
}

// IsCanvas reports a Drawing Canvas.
func (o Object) IsCanvas() bool {
	_, ok := o.CanvasStyle()
	return ok
}

// TextBox is the text box story this object shows; false if it is no text box.
func (o Object) TextBox() (uint32, bool) {
	if s, ok := o.InlineObject.(Shape); ok && s.Story != nil {
		return *s.Story, true
	}
	return 0, false
}

// TextBoxes are the text box stories this object shows: a text box's, or its group members'.
func (o Object) TextBoxes() []uint32 {
	switch v := o.InlineObject.(type) {
	case Shape:
		if v.Story != nil {
			return []uint32{*v.Story}
		}
	case Group:
		var out []uint32
		for _, c := range v.Children {
			if id, ok := c.Obj.TextBox(); ok {
				out = append(out, id)
			}
		}
		return out
	}
	return nil
}

// IsMarker reports a zero-width marker, which takes no part in layout.
func (o Object) IsMarker() bool {
	switch o.InlineObject.(type) {
	case BookmarkStart, BookmarkEnd, CommentStart, CommentEnd, PermStart, PermEnd, FieldStart, FieldEnd, ControlStart, ControlEnd:
		return true
	}
	return false
}

// AltText is a drawing's description (alternative text), wherever it is kept.
func (o Object) AltText() string {
	switch v := o.InlineObject.(type) {
	case Image:
		return v.Alt
	case GraphicObject:
		return v.Alt
	case Shape:
		return v.Float.Alt.Text
	case Group:
		return v.Float.Alt.Text
	}
	return ""
}

// WithAltText is the drawing with its description set (capped at MaxAltText characters); false
// if this is no drawing.
func (o Object) WithAltText(text string) (Object, bool) {
	text = CapAlt(text)
	switch v := o.InlineObject.(type) {
	case Image:
		v.Alt = text
		return Object{v}, true
	case GraphicObject:
		v.Alt = text
		return Object{v}, true
	case Shape:
		v.Float.Alt.Text = text
		return Object{v}, true
	case Group:
		v.Float.Alt.Text = text
		return Object{v}, true
	}
	return o, false
}

// IsDecorative reports a drawing marked decorative.
func (o Object) IsDecorative() bool {
	_, _, f, ok := o.Frame()
	return ok && f.Alt.Decorative
}

// IsFloating reports a drawing that isn't in line with text.
func (o Object) IsFloating() bool {
	_, _, f, ok := o.Frame()
	return ok && f.Wrap != WrapInline
}

// Ink is the geometry of an ink stroke (a freeform shape drawn with a pen); false if this is
// none.
func (o Object) Ink() (*Freeform, bool) {
	if s, ok := o.InlineObject.(Shape); ok && s.Freeform != nil && s.Freeform.IsInk() {
		return s.Freeform, true
	}
	return nil, false
}

// NoteSeparator is the line a footnote or endnote separator draws (ECMA-376 §17.11): a short
// line, or with continuation one across the column. Kept as the run element itself.
func NoteSeparator(continuation bool) Object {
	el := "w:separator"
	if continuation {
		el = "w:continuationSeparator"
	}
	return Object{Opaque{Format: "docx", XML: "<w:r><" + el + "/></w:r>"}}
}

// AsNoteSeparator reports, for a NoteSeparator, whether it is the continuation separator.
func (o Object) AsNoteSeparator() (continuation, ok bool) {
	if v, isOpaque := o.InlineObject.(Opaque); isOpaque && v.Format == "docx" {
		switch v.XML {
		case "<w:r><w:separator/></w:r>":
			return false, true
		case "<w:r><w:continuationSeparator/></w:r>":
			return true, true
		}
	}
	return false, false
}

// PlainText is the text this object contributes to plain-text extraction.
func (o Object) PlainText() string {
	switch v := o.InlineObject.(type) {
	case Field:
		return v.Result
	case Equation:
		return v.Linear
	case Ruby:
		return v.Base
	case PositionalTab:
		return "\t"
	case Opaque:
		return v.Text
	}
	return ""
}
