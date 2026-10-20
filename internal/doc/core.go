package doc

// CoreProps are the document properties (File › Info; crates/doc/src/lib.rs CoreProps).
type CoreProps struct {
	Title          string `json:"title"`
	Subject        string `json:"subject"`
	Creator        string `json:"creator"`
	Keywords       string `json:"keywords"`
	Description    string `json:"description"`
	Category       string `json:"category"`
	LastModifiedBy string `json:"lastModifiedBy"`
	Created        string `json:"created"`
	Modified       string `json:"modified"`
	Revision       uint32 `json:"revision"`
}

func (CoreProps) SerdeDefault() {}

// NoteSeparators are the document's own footnote or endnote separators (w:separator,
// w:continuationSeparator, w:continuationNotice); nil ones are the defaults: a short line above
// the notes, a line across the column above notes continued from the page before, no notice.
type NoteSeparators struct {
	// Separator is above the notes on a page.
	Separator *Vec[Block] `json:"separator"`
	// ContinuationSeparator is above the rest of a note continued from the page before.
	ContinuationSeparator *Vec[Block] `json:"continuationSeparator"`
	// ContinuationNotice is below the part of a note that continues on the next page.
	ContinuationNotice *Vec[Block] `json:"continuationNotice"`
}

func (NoteSeparators) SerdeDefault() {}

// IsEmpty reports no separator of the document's own: the defaults apply.
func (n NoteSeparators) IsEmpty() bool {
	return n.Separator == nil && n.ContinuationSeparator == nil && n.ContinuationNotice == nil
}

// IsZero lets encoding/json's omitzero leave empty separators out, as Rust does.
func (n NoteSeparators) IsZero() bool { return n.IsEmpty() }
