// Package doc is the WordCraft document model: the Go port of crates/doc, growing in steps
// (MIGRATION.md §6). Its JSON is the Rust model's serde JSON: a document the Rust app saved as
// .json reads here and writes back unchanged, because fields not modelled yet are kept verbatim
// (Extra).
//
// Text is UTF-8 and offsets are byte offsets on character boundaries, as in Rust; a run's Len
// counts bytes of its paragraph's Text.
package doc

import (
	"encoding/json"
	"errors"
	"fmt"
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

// Document is a whole document. Only the body is modelled so far; every other field (styles,
// numbering, sections, parts, comments, settings, …) is kept in Extra.
type Document struct {
	Body  []Block `json:"body"`
	Extra Extra   `json:"-"`
}

type documentJSON struct {
	Body []Block `json:"body"`
}

// UnmarshalJSON reads the Rust model's JSON.
func (d *Document) UnmarshalJSON(data []byte) error {
	var v documentJSON
	extra, err := unmarshalWithExtra(data, &v)
	if err != nil {
		return err
	}
	if v.Body == nil {
		return errors.New("doc: document has no body")
	}
	*d = Document{Body: v.Body, Extra: extra}
	return nil
}

// MarshalJSON writes the Rust model's JSON.
func (d Document) MarshalJSON() ([]byte, error) {
	v := documentJSON{Body: d.Body}
	if v.Body == nil {
		v.Body = []Block{}
	}
	return marshalWithExtra(&v, d.Extra)
}

// Block is a paragraph or, not modelled yet, a table (kept as raw JSON).
type Block struct {
	Para *Paragraph
	// Raw is the JSON of a block that isn't a paragraph.
	Raw json.RawMessage
}

// Kind is "para" or the raw block's kind ("table").
func (b Block) Kind() string {
	if b.Para != nil {
		return "para"
	}
	var k struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(b.Raw, &k)
	return k.Kind
}

// UnmarshalJSON reads {"kind": "para", …} or another kind.
func (b *Block) UnmarshalJSON(data []byte) error {
	var k struct {
		Kind *string `json:"kind"`
	}
	if err := json.Unmarshal(data, &k); err != nil {
		return err
	}
	if k.Kind == nil {
		return errors.New("doc: block without a kind")
	}
	if *k.Kind == "para" {
		var p Paragraph
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		delete(p.Extra, "kind")
		if len(p.Extra) == 0 {
			p.Extra = nil
		}
		*b = Block{Para: &p}
		return nil
	}
	*b = Block{Raw: append(json.RawMessage(nil), data...)}
	return nil
}

// MarshalJSON writes the block with its kind tag.
func (b Block) MarshalJSON() ([]byte, error) {
	if b.Para == nil {
		if b.Raw == nil {
			return nil, errors.New("doc: empty block")
		}
		return b.Raw, nil
	}
	p := *b.Para
	p.Extra = p.Extra.Clone()
	p.Extra.set("kind", json.RawMessage(`"para"`))
	return json.Marshal(p)
}

// Paragraph is a paragraph: Text and the formatting Runs covering it. Objects, the section
// break and other fields are in Extra.
type Paragraph struct {
	Text  string
	Runs  []Run
	Props ParaProps
	// Mark is the formatting of the paragraph mark (what typing into an empty paragraph gets).
	Mark  CharProps
	Extra Extra
}

type paragraphJSON struct {
	Text  string    `json:"text"`
	Runs  []Run     `json:"runs"`
	Props ParaProps `json:"props,omitzero"`
	Mark  CharProps `json:"mark,omitzero"`
}

// UnmarshalJSON reads the Rust model's JSON (text and runs are required, as in Rust).
func (p *Paragraph) UnmarshalJSON(data []byte) error {
	var req struct {
		Text *string          `json:"text"`
		Runs *json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	if req.Text == nil || req.Runs == nil {
		return errors.New("doc: paragraph needs text and runs")
	}
	var v paragraphJSON
	extra, err := unmarshalWithExtra(data, &v)
	if err != nil {
		return err
	}
	*p = Paragraph{Text: v.Text, Runs: v.Runs, Props: v.Props, Mark: v.Mark, Extra: extra}
	return nil
}

// MarshalJSON writes the Rust model's JSON.
func (p Paragraph) MarshalJSON() ([]byte, error) {
	v := paragraphJSON{Text: p.Text, Runs: p.Runs, Props: p.Props, Mark: p.Mark}
	if v.Runs == nil {
		v.Runs = []Run{}
	}
	return marshalWithExtra(&v, p.Extra)
}

// Clone is a deep copy (Extra values are immutable and shared).
func (p *Paragraph) Clone() *Paragraph {
	c := *p
	c.Runs = make([]Run, len(p.Runs))
	for i, r := range p.Runs {
		c.Runs[i] = Run{Len: r.Len, Props: r.Props.Clone()}
	}
	c.Props = p.Props.Clone()
	c.Mark = p.Mark.Clone()
	c.Extra = p.Extra.Clone()
	return &c
}

// Equal compares like Rust's PartialEq for Paragraph.
func (p *Paragraph) Equal(o *Paragraph) bool {
	if p.Text != o.Text || len(p.Runs) != len(o.Runs) || !p.Props.Equal(o.Props) || !p.Mark.Equal(o.Mark) || !p.Extra.Equal(o.Extra) {
		return false
	}
	for i := range p.Runs {
		if p.Runs[i].Len != o.Runs[i].Len || !p.Runs[i].Props.Equal(o.Runs[i].Props) {
			return false
		}
	}
	return true
}

// Run is a formatting run: Len bytes of text sharing Props.
type Run struct {
	Len   int
	Props CharProps
}

type runJSON struct {
	Len   int       `json:"len"`
	Props CharProps `json:"props,omitzero"`
}

// UnmarshalJSON reads {"len": n, "props": {…}}; a negative length is refused (Rust: usize).
func (r *Run) UnmarshalJSON(data []byte) error {
	var v runJSON
	if _, err := unmarshalWithExtra(data, &v); err != nil {
		return err
	}
	if v.Len < 0 {
		return errors.New("doc: negative run length")
	}
	*r = Run(v)
	return nil
}

// MarshalJSON writes {"len": n, "props": {…}}.
func (r Run) MarshalJSON() ([]byte, error) { return json.Marshal(runJSON(r)) }

// CharProps is character formatting (direct or in a style); nil fields inherit. The fields not
// modelled yet (underline, colour, revisions, links, …) are in Extra.
type CharProps struct {
	Style    *string  // character style id
	Font     *string  // font name
	Size     *float32 // points
	Bold     *bool
	BoldCs   *bool // complex-script bold
	Italic   *bool
	ItalicCs *bool
	Extra    Extra
}

type charPropsJSON struct {
	Style    *string  `json:"style,omitempty"`
	Font     *string  `json:"font,omitempty"`
	Size     *float32 `json:"size,omitempty"`
	Bold     *bool    `json:"bold,omitempty"`
	BoldCs   *bool    `json:"boldCs,omitempty"`
	Italic   *bool    `json:"italic,omitempty"`
	ItalicCs *bool    `json:"italicCs,omitempty"`
}

// UnmarshalJSON reads the Rust model's camelCase fields.
func (c *CharProps) UnmarshalJSON(data []byte) error {
	var v charPropsJSON
	extra, err := unmarshalWithExtra(data, &v)
	if err != nil {
		return err
	}
	*c = CharProps{v.Style, v.Font, v.Size, v.Bold, v.BoldCs, v.Italic, v.ItalicCs, extra}
	return nil
}

// MarshalJSON writes the Rust model's camelCase fields, leaving out unset ones.
func (c CharProps) MarshalJSON() ([]byte, error) {
	v := charPropsJSON{c.Style, c.Font, c.Size, c.Bold, c.BoldCs, c.Italic, c.ItalicCs}
	return marshalWithExtra(&v, c.Extra)
}

// IsZero is Rust's is_empty: nothing set (so JSON leaves it out).
func (c CharProps) IsZero() bool {
	return c.Style == nil && c.Font == nil && c.Size == nil && c.Bold == nil && c.BoldCs == nil &&
		c.Italic == nil && c.ItalicCs == nil && len(c.Extra) == 0
}

func eqPtr[T comparable](a, b *T) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Equal compares like Rust's PartialEq.
func (c CharProps) Equal(o CharProps) bool {
	return eqPtr(c.Style, o.Style) && eqPtr(c.Font, o.Font) && eqPtr(c.Size, o.Size) && eqPtr(c.Bold, o.Bold) &&
		eqPtr(c.BoldCs, o.BoldCs) && eqPtr(c.Italic, o.Italic) && eqPtr(c.ItalicCs, o.ItalicCs) && c.Extra.Equal(o.Extra)
}

// Clone is a deep copy.
func (c CharProps) Clone() CharProps {
	return CharProps{clonePtr(c.Style), clonePtr(c.Font), clonePtr(c.Size), clonePtr(c.Bold), clonePtr(c.BoldCs),
		clonePtr(c.Italic), clonePtr(c.ItalicCs), c.Extra.Clone()}
}

// ParaProps is paragraph formatting. Only the style is modelled so far.
type ParaProps struct {
	Style *string // paragraph style id
	Extra Extra
}

type paraPropsJSON struct {
	Style *string `json:"style,omitempty"`
}

// UnmarshalJSON reads the Rust model's camelCase fields.
func (p *ParaProps) UnmarshalJSON(data []byte) error {
	var v paraPropsJSON
	extra, err := unmarshalWithExtra(data, &v)
	if err != nil {
		return err
	}
	*p = ParaProps{Style: v.Style, Extra: extra}
	return nil
}

// MarshalJSON writes the Rust model's camelCase fields.
func (p ParaProps) MarshalJSON() ([]byte, error) {
	return marshalWithExtra(&paraPropsJSON{Style: p.Style}, p.Extra)
}

// IsZero is Rust's is_empty.
func (p ParaProps) IsZero() bool { return p.Style == nil && len(p.Extra) == 0 }

// Equal compares like Rust's PartialEq.
func (p ParaProps) Equal(o ParaProps) bool { return eqPtr(p.Style, o.Style) && p.Extra.Equal(o.Extra) }

// Clone is a deep copy.
func (p ParaProps) Clone() ParaProps {
	return ParaProps{Style: clonePtr(p.Style), Extra: p.Extra.Clone()}
}
