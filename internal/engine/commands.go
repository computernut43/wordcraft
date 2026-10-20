package engine

// The commands of the first slice (MIGRATION.md §10), ported from crates/engine/src/cmd/
// {text,format,caret,edit}.rs. Each one says which cases it covers; the rest are refused with
// NotPorted.

import (
	"strings"
	"unicode"

	"github.com/computernut43/wordcraft/internal/doc"
)

func textCommands() []CommandSpec {
	return []CommandSpec{
		{ID: "text.insert", Label: "Type Text", Location: "Editing", Params: `{"text": string}`, Mutates: true, Run: textInsert},
	}
}

func formatCommands() []CommandSpec {
	return []CommandSpec{
		{ID: "format.bold", Label: "Bold", Location: "Home › Font", Shortcut: "Mod+B", Params: `{"value"?: bool}`, Mutates: true, Run: formatBold},
	}
}

func selectCommands() []CommandSpec {
	return []CommandSpec{
		{ID: "select.text", Label: "Select Text", Location: "Navigation", Params: `{"text": string, "occurrence"?: n}`, Run: selectText},
	}
}

func editCommands() []CommandSpec {
	return []CommandSpec{
		{ID: "edit.undo", Label: "Undo", Location: "Quick Access Toolbar", Shortcut: "Mod+Z", Run: func(s *Session, _ Params) (any, error) {
			s.undo()
			return s.SelResult(), nil
		}},
		{ID: "edit.redo", Label: "Redo", Location: "Quick Access Toolbar", Shortcut: "Mod+Y", Run: func(s *Session, _ Params) (any, error) {
			s.redoStep()
			return s.SelResult(), nil
		}},
	}
}

// autocorrectTriggers are the single characters after which Rust runs AutoCorrect and
// AutoFormat-as-you-type (not ported yet).
const autocorrectTriggers = " ,.;:!?"

// textInsert types text at the caret, replacing the selection (Rust cmd/text.rs insert).
//
// Covered: text in a top-level body paragraph without inline objects, the selection within that
// paragraph, Track Changes off, default typing options; "\v" becomes a manual line break. With
// "raw": false (the default), single characters that make Rust run AutoCorrect, AutoFormat or
// smart quotes are refused, as are paragraph breaks and right-to-left text.
func textInsert(s *Session, p Params) (any, error) {
	text, err := p.ReqStr("text")
	if err != nil {
		return nil, err
	}
	raw, _ := p.Bool("raw")
	if !raw && len([]rune(text)) == 1 && (strings.ContainsAny(text, autocorrectTriggers) || text == `"` || text == "'") {
		return nil, notPorted("text.insert: AutoCorrect, AutoFormat and smart quotes after %q (use \"raw\": true)", text)
	}
	if strings.ContainsAny(text, "\r\u2029") {
		return nil, notPorted("text.insert: paragraph breaks in the text")
	}
	if hasRTL(text) {
		return nil, notPorted("text.insert: right-to-left text")
	}
	if err := typingPreconditions(s); err != nil {
		return nil, err
	}
	// typing_options: typing replaces the selection, overtype is off (the defaults).
	props := s.typingProps()
	if _, err := deleteSelection(s); err != nil {
		return nil, err
	}
	// Typed text is new: not a formatting change, not deleted; its direction is its own.
	props.FmtChange, props.RTL, props.Ins, props.Del = nil, nil, nil, nil
	at := s.Sel.Focus
	if part := strings.ReplaceAll(text, "\u000B", "\n"); part != "" {
		at, err = s.Doc.InsertText(at, part, props)
		if err != nil {
			return nil, err
		}
	}
	s.Sel = Caret(at)
	return s.SelResult(), nil
}

// typingPreconditions refuses what typing doesn't handle yet: Track Changes, a selection that
// isn't in one top-level body paragraph, paragraphs with inline objects (fields, content
// controls, pictures) or other unmodelled content.
func typingPreconditions(s *Session) error {
	if s.trackChanges() {
		return notPorted("typing with Track Changes on")
	}
	if s.Doc.BodyHasObject("controlStart") {
		return notPorted("typing in a document with content controls")
	}
	a, b := s.Sel.Ordered()
	para := s.Doc.ParaAt(a)
	if para == nil || s.Doc.ParaAt(b) != para {
		return notPorted("typing outside a single top-level body paragraph")
	}
	if strings.ContainsRune(para.Text, doc.Obj) {
		return notPorted("typing in a paragraph with inline objects")
	}
	if len(para.Objects) > 0 {
		return notPorted("typing in a paragraph with inline objects")
	}
	return nil
}

// hasRTL reports characters of right-to-left scripts or direction marks: Rust makes a
// paragraph right-to-left when they come first, which isn't ported yet.
func hasRTL(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hebrew, unicode.Arabic, unicode.Syriac, unicode.Thaana, unicode.Nko, unicode.Samaritan,
			unicode.Mandaic, unicode.Adlam, unicode.Hanifi_Rohingya, unicode.Mende_Kikakui, unicode.Old_Hungarian) ||
			strings.ContainsRune("\u200f\u061c\u202b\u202e\u2067", r) {
			return true
		}
	}
	return false
}

var noteStyles = map[string]bool{"FootnoteReference": true, "EndnoteReference": true}

// typingProps is the formatting typed text gets (Rust Session::typing_props): over a selection
// the first selected character's, otherwise the caret's; never right-to-left by itself.
func (s *Session) typingProps() doc.CharProps {
	a, _ := s.Sel.Ordered()
	var c doc.CharProps
	if para := s.Doc.ParaAt(a); para != nil && s.Pending == nil && !s.Sel.IsCollapsed() && a.Off < len(para.Text) {
		// The selection is replaced, so the new text isn't part of a link or a note number.
		c = para.PropsOfChar(a.Off).Unlinked()
		if c.Style != nil && noteStyles[*c.Style] {
			c.Style = nil
		}
	} else {
		c = s.caretProps()
	}
	c.RTL = nil
	return c
}

// caretProps is the caret's formatting (Rust Session::caret_props).
func (s *Session) caretProps() doc.CharProps {
	if s.Pending != nil {
		return *s.Pending
	}
	f := s.Sel.Focus
	para := s.Doc.ParaAt(f)
	if para == nil {
		return doc.CharProps{}
	}
	out := para.PropsAt(f.Off)
	// Typed at either end of a hyperlink, text isn't part of it; only typing inside extends it.
	inside := false
	if out.Link != nil && f.Off > 0 && f.Off < len(para.Text) {
		other := para.PropsOfChar(f.Off).Link
		inside = other != nil && *other == *out.Link
	}
	if !inside {
		out = out.Unlinked()
	}
	// Text typed next to a symbol is in the text's font, not the symbol's.
	out = out.WithoutSymbol()
	if out.Style != nil && noteStyles[*out.Style] {
		out.Style = nil
	}
	return out
}

// deleteSelection deletes the selection (within one paragraph) and leaves a caret there.
func deleteSelection(s *Session) (doc.Pos, error) {
	a, b := s.Sel.Ordered()
	if a.Equal(b) {
		return a, nil
	}
	at, err := s.Doc.DeleteRange(a, b)
	if err != nil {
		return doc.Pos{}, err
	}
	s.Sel = Caret(at)
	return at, nil
}

// formatBold sets bold, plain and complex-script (Rust cmd/format.rs). Covered: an explicit
// "value"; a selection over top-level body paragraphs, or a caret outside a word (the formatting
// then waits for the next typed text). Toggling without "value" needs style resolution and a
// caret inside a word needs word boundaries: both are refused for now.
func formatBold(s *Session, p Params) (any, error) {
	on, ok := p.Bool("value")
	if !ok {
		return nil, notPorted("format.bold without \"value\" (toggling needs style resolution)")
	}
	set := func(c *doc.CharProps) { c.Bold, c.BoldCS = &on, &on }
	a, b := s.Sel.Ordered()
	if !a.Equal(b) {
		if err := s.Doc.FormatRange(a, b, set); err != nil {
			return nil, err
		}
		return s.SelResult(), nil
	}
	para := s.Doc.ParaAt(a)
	if para == nil {
		return nil, notPorted("formatting outside a top-level body paragraph")
	}
	if insideWord(para.Text, a.Off) {
		return nil, notPorted("format.bold at a caret inside a word")
	}
	pend := s.typingProps()
	set(&pend)
	s.Pending = &pend
	// An empty paragraph's mark takes the formatting too.
	if para.Text == "" {
		mp, err := s.Doc.ParaMut(a)
		if err != nil {
			return nil, err
		}
		set(&mp.Mark)
	}
	return s.SelResult(), nil
}

// insideWord: word characters on both sides of off (Rust format.rs target).
func insideWord(text string, off int) bool {
	isW := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '\'' || r == '_' }
	before, after := []rune(text[:off]), []rune(text[off:])
	return len(before) > 0 && isW(before[len(before)-1]) && len(after) > 0 && isW(after[0])
}

// selectText selects the occurrence-th match of text in the body (Rust cmd/caret.rs).
// Searching other stories, and searching past a table, aren't ported yet.
func selectText(s *Session, p Params) (any, error) {
	needle, err := p.ReqStr("text")
	if err != nil {
		return nil, err
	}
	n, ok := p.U64("occurrence")
	if !ok || n == 0 {
		n = 1
	}
	if needle == "" {
		return nil, errParams("`text` is empty")
	}
	if _, ok := p["story"]; ok {
		return nil, notPorted("select.text in other stories")
	}
	var count uint64
	for i, b := range s.Doc.Body {
		if b.Para == nil {
			return nil, notPorted("select.text searching tables")
		}
		text := b.Para.Text
		for at := 0; ; {
			j := strings.Index(text[at:], needle)
			if j < 0 {
				break
			}
			start := at + j
			count++
			if count == n {
				s.Sel = Selection{Anchor: doc.BodyPos(i, start), Focus: doc.BodyPos(i, start+len(needle))}
				return s.SelResult(), nil
			}
			at = start + len(needle)
		}
	}
	return nil, errFailed("`%s` not found", needle)
}
