// Package doc is the WordCraft document model: the Go port of crates/doc, growing in steps
// (MIGRATION.md §6). Its JSON is the Rust model's serde JSON, read and written with serdejson as
// serde reads and writes it: what the Rust app reads, this reads the same way, and both write it
// back the same, byte for byte. Parts not typed yet are Raw (kept as read).
//
// Text is UTF-8 and offsets are byte offsets on character boundaries, as in Rust; a run's Len
// counts bytes of its paragraph's Text.
package doc

import (
	"errors"
	"fmt"

	"github.com/computernut43/craft-libs/serdejson"
)

// ErrorKind classifies model errors (Rust DocError).
type ErrorKind int

// Error kinds.
const (
	BadOffset ErrorKind = iota + 1
	BadPath
	BadStory
	Invalid
)

// Error is a model error.
type Error struct {
	Kind   ErrorKind
	Detail string
}

// Error is worded as Rust's DocError.
func (e *Error) Error() string {
	switch e.Kind {
	case BadOffset:
		return "offset " + e.Detail + " is not a character boundary in the paragraph"
	case BadPath:
		return "no paragraph at " + e.Detail
	case BadStory:
		return "no story " + e.Detail
	}
	return e.Detail
}

func badOffset(off int) error { return &Error{Kind: BadOffset, Detail: fmt.Sprint(off)} }

// Document is a whole document (crates/doc/src/lib.rs Document).
type Document struct {
	Body        Vec[Block]      `json:"body"`
	LastSection SectionProps    `json:"lastSection"`
	Parts       U32Map[Part]    `json:"parts"`
	Styles      StyleSheet      `json:"styles"`
	Numbering   Numbering       `json:"numbering"`
	Comments    U32Map[Comment] `json:"comments"`
	Revisions   Vec[Revision]   `json:"revisions"`
	Settings    Settings        `json:"settings"`
	Core        CoreProps       `json:"core"`
	Sources     Vec[Source]     `json:"sources"`
	CustomProps Vec[CustomProp] `json:"customProps"`
	// FootnoteSeparators and EndnoteSeparators are the document's own note separators.
	FootnoteSeparators NoteSeparators     `json:"footnoteSeparators,omitzero"`
	EndnoteSeparators  NoteSeparators     `json:"endnoteSeparators,omitzero"`
	Media              StrMap[Vec[uint8]] `json:"media,omitempty"`
	Passthrough        StrMap[Vec[uint8]] `json:"passthrough,omitempty"`
	Glossary           Vec[BuildingBlock] `json:"glossary,omitempty"`
}

// StrMap is a Rust BTreeMap<String, V>.
type StrMap[V any] = serdejson.StrMap[V]

// U32Map is a Rust BTreeMap<u32, V>.
type U32Map[V any] = serdejson.U32Map[V]

func (Document) SerdeDefault() {}

// SetDefaults is Rust's Document::new(): a body of one empty paragraph, the default section and
// settings, the built-in styles and no lists.
func (d *Document) SetDefaults() {
	*d = Document{Body: Vec[Block]{{Para: NewParagraph()}}, LastSection: NewSectionProps(), Parts: U32Map[Part]{},
		Styles: BuiltinStyles(), Comments: U32Map[Comment]{}, Revisions: Vec[Revision]{}, Settings: NewSettings(),
		Sources: Vec[Source]{}, CustomProps: Vec[CustomProp]{}}
	d.Numbering.SetDefaults()
}

// New is a blank document: one empty Normal paragraph, Letter, built-in styles.
func New() *Document {
	var d Document
	d.SetDefaults()
	return &d
}

// Unmarshal reads a document as the Rust app reads its .json files (serde_json::from_slice).
func Unmarshal(data []byte) (*Document, error) {
	var d Document
	if err := serdejson.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Marshal writes a document as the Rust app saves .json (serde_json::to_vec_pretty).
func (d *Document) Marshal() ([]byte, error) { return serdejson.Marshal(d) }

// Block is a paragraph or a table, written with its kind: {"kind": "para", …}. Exactly one is
// set. Blocks are shared between undo snapshots, so the paragraph or table is never changed in
// place: edit a Clone and replace the block.
type Block struct {
	Para  *Paragraph
	Table *Table
}

var blockKinds = []string{"para", "table"}

// Kind is "para" or "table".
func (b Block) Kind() string {
	if b.Para != nil {
		return "para"
	}
	return "table"
}

// DecodeSerde reads serde's internally tagged form.
func (b *Block) DecodeSerde(d serdejson.Decoder) error {
	i, rest, err := d.Tagged("kind", blockKinds)
	if err != nil {
		return err
	}
	if i == 0 {
		p := new(Paragraph)
		if err := rest.Decode(p); err != nil {
			return err
		}
		*b = Block{Para: p}
		return nil
	}
	t := new(Table)
	if err := rest.Decode(t); err != nil {
		return err
	}
	*b = Block{Table: t}
	return nil
}

// MarshalJSON writes the kind, then the block's fields.
func (b Block) MarshalJSON() ([]byte, error) {
	var v any = b.Para
	if b.Para == nil {
		if b.Table == nil {
			return nil, errors.New("doc: empty block")
		}
		v = b.Table
	}
	fields, err := serdejson.MarshalCompact(v)
	if err != nil {
		return nil, err
	}
	if len(fields) < 2 || fields[0] != '{' {
		return nil, errors.New("doc: empty block")
	}
	out := []byte(`{"kind":"` + b.Kind() + `"`)
	if len(fields) > 2 {
		out = append(out, ',')
	}
	return append(out, fields[1:]...), nil
}

// Paragraph is a paragraph: Text and the formatting Runs covering it (crates/doc/src/para.rs).
type Paragraph struct {
	Text string   `json:"text"`
	Runs Vec[Run] `json:"runs"`
	// Props are the paragraph's formatting.
	Props ParaProps `json:"props,omitzero" serde:"default"`
	// Mark is the formatting of the paragraph mark (what typing into an empty paragraph gets).
	Mark CharProps `json:"mark,omitzero" serde:"default"`
	// Objects are the inline objects (pictures, fields, …), one per U+FFFC in Text, in order.
	Objects Vec[Raw] `json:"objects,omitempty" serde:"default"`
	// Section is set when this paragraph ends a section (OOXML keeps w:sectPr on the last
	// paragraph).
	Section *Raw `json:"section,omitempty" serde:"default"`
}

// NewParagraph is an empty paragraph.
func NewParagraph() *Paragraph { return &Paragraph{Runs: Vec[Run]{}} }

// Clone copies the paragraph. Values behind pointers and Raw parts are never changed in place,
// so they're shared.
func (p *Paragraph) Clone() *Paragraph {
	c := *p
	c.Runs = append(Vec[Run](nil), p.Runs...)
	c.Objects = append(Vec[Raw](nil), p.Objects...)
	return &c
}

// Equal compares like Rust's PartialEq for Paragraph.
func (p *Paragraph) Equal(o *Paragraph) bool { return equal(p, o) }

// Run is a formatting run: Len bytes of text sharing Props.
type Run struct {
	Len   int
	Props CharProps
	// wide is the length as read when it doesn't fit an int (Rust's is a usize): Len is then the
	// largest int, which normalising clamps to the text exactly as Rust clamps wide, and a run
	// written back unchanged writes wide.
	wide uint64
}

type runFields struct {
	Len   uint64    `json:"len"`
	Props CharProps `json:"props,omitzero" serde:"default"`
}

// DecodeSerde reads {"len": n, "props": {…}}.
func (r *Run) DecodeSerde(d serdejson.Decoder) error {
	var v runFields
	if err := d.Decode(&v); err != nil {
		return err
	}
	*r = Run{Len: int(min(v.Len, uint64(maxInt))), Props: v.Props}
	if v.Len > uint64(maxInt) {
		r.wide = v.Len
	}
	return nil
}

// MarshalJSON writes {"len": n, "props": {…}}, leaving out empty props.
func (r Run) MarshalJSON() ([]byte, error) {
	n := uint64(max(r.Len, 0))
	if r.wide != 0 && r.Len == maxInt {
		n = r.wide
	}
	return serdejson.MarshalCompact(runFields{n, r.Props})
}

const maxInt = int(^uint(0) >> 1)
