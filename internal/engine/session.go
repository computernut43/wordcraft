package engine

import (
	"fmt"
	"strings"

	"github.com/computernut43/wordcraft/internal/doc"
)

// Selection is the selection: anchor (where it started) and focus (the caret).
type Selection struct {
	Anchor doc.Pos `json:"anchor"`
	Focus  doc.Pos `json:"focus"`
}

// Caret is a collapsed selection at p.
func Caret(p doc.Pos) Selection { return Selection{Anchor: p, Focus: p} }

// IsCollapsed reports a caret.
func (s Selection) IsCollapsed() bool { return s.Anchor.Equal(s.Focus) }

// Ordered returns (start, end) in document order.
func (s Selection) Ordered() (doc.Pos, doc.Pos) { return doc.Order(s.Anchor, s.Focus) }

func (s Selection) equal(o Selection) bool { return s.Anchor.Equal(o.Anchor) && s.Focus.Equal(o.Focus) }

// maxUndo is how many undo steps are kept (Rust MAX_UNDO).
const maxUndo = 500

type undoStep struct {
	label   string
	doc     *doc.Document
	sel     Selection
	pending *doc.CharProps
}

// Session is an open document with its selection, undo history and command registry.
type Session struct {
	Doc *doc.Document
	Sel Selection
	// Pending is formatting picked with a collapsed caret, applied to the next typed text.
	Pending *doc.CharProps
	// Author names comments and tracked changes and is stamped on save.
	Author string
	Dirty  bool

	history, redo []undoStep
	evicted       uint64
	typingOpen    bool
	registry      *Registry
}

// New opens d in a session with the caret at the start of the body. It refuses documents
// whose body starts with a table: the caret would be in a cell, which the model can't reach yet.
func New(d *doc.Document) (*Session, error) {
	start, ok := d.StartOfBody()
	if !ok {
		return nil, notPorted("documents that start with a table")
	}
	return &Session{Doc: d, Sel: Caret(start), Author: "WordCraft User", registry: defaultRegistry}, nil
}

// SelResult is the standard command result: the selection after the command.
func (s *Session) SelResult() any { return s.Sel }

// CanUndo and CanRedo report whether there are steps.
func (s *Session) CanUndo() bool { return len(s.history) > 0 }
func (s *Session) CanRedo() bool { return len(s.redo) > 0 }

// UndoLabels are the undo steps' labels, newest first.
func (s *Session) UndoLabels() []string {
	out := make([]string, 0, len(s.history))
	for i := len(s.history) - 1; i >= 0; i-- {
		out = append(out, s.history[i].label)
	}
	return out
}

func clonePending(p *doc.CharProps) *doc.CharProps {
	if p == nil {
		return nil
	}
	c := p.Clone()
	return &c
}

// checkpoint snapshots the document before a change. Consecutive typing is one step.
func (s *Session) checkpoint(label string) {
	if label == "Typing" && s.typingOpen {
		return
	}
	s.typingOpen = label == "Typing"
	s.history = append(s.history, undoStep{label: label, doc: s.Doc.Clone(), sel: s.Sel, pending: clonePending(s.Pending)})
	if len(s.history) > maxUndo {
		s.history = append([]undoStep(nil), s.history[1:]...)
		s.evicted++
	}
	s.redo = nil
}

// undo goes back one step; false when there is none.
func (s *Session) undo() bool {
	s.typingOpen = false
	if len(s.history) == 0 {
		return false
	}
	u := s.history[len(s.history)-1]
	s.history = s.history[:len(s.history)-1]
	s.redo = append(s.redo, undoStep{label: u.label, doc: s.Doc, sel: s.Sel, pending: s.Pending})
	s.Doc, s.Sel, s.Pending = u.doc, u.sel, u.pending
	s.Dirty = true
	return true
}

// redoStep goes forward one step; false when there is none.
func (s *Session) redoStep() bool {
	s.typingOpen = false
	if len(s.redo) == 0 {
		return false
	}
	u := s.redo[len(s.redo)-1]
	s.redo = s.redo[:len(s.redo)-1]
	s.history = append(s.history, undoStep{label: u.label, doc: s.Doc, sel: s.Sel, pending: s.Pending})
	s.Doc, s.Sel, s.Pending = u.doc, u.sel, u.pending
	s.Dirty = true
	return true
}

func (s *Session) clampSelection() {
	s.Sel.Anchor = s.Doc.Clamp(s.Sel.Anchor)
	s.Sel.Focus = s.Doc.Clamp(s.Sel.Focus)
}

// protected reports Restrict Editing on the document, which isn't ported yet.
func (s *Session) protected() bool {
	var settings struct {
		Protection *string `json:"protection"`
	}
	return s.Doc.Extra.Get("settings", &settings) && settings.Protection != nil
}

// trackChanges reports Track Changes on.
func (s *Session) trackChanges() bool {
	var settings struct {
		TrackChanges bool `json:"trackChanges"`
	}
	s.Doc.Extra.Get("settings", &settings)
	return settings.TrackChanges
}

type snapshot struct {
	doc        *doc.Document
	sel        Selection
	pending    *doc.CharProps
	history    []undoStep
	redo       []undoStep
	typingOpen bool
	evicted    uint64
}

// Run runs the command id with params: the single entry point for the CLI, MCP and UI.
// Mutating commands are undoable; a failed command leaves the session as it was; a panic inside
// a command becomes an error and changes nothing.
func (s *Session) Run(id string, params Params) (result any, err error) {
	spec, ok := s.registry.Get(id)
	if !ok {
		return nil, &CmdError{Kind: Unknown, Msg: id}
	}
	if params == nil {
		params = Params{}
	}
	if spec.Enabled != nil {
		if why := spec.Enabled(s); why != "" {
			return nil, &CmdError{Kind: Disabled, Msg: id + ": " + why}
		}
	}
	if spec.Mutates && s.protected() {
		return nil, notPorted("%s: documents with editing restrictions", id)
	}
	var before *snapshot
	if spec.Mutates {
		before = &snapshot{s.Doc.Clone(), s.Sel, clonePending(s.Pending), append([]undoStep(nil), s.history...),
			append([]undoStep(nil), s.redo...), s.typingOpen, s.evicted}
	}
	typing := id == "text.insert"
	switch {
	case spec.Mutates:
		if !typing {
			s.typingOpen = false
		}
		label := spec.Label
		if typing {
			label = "Typing"
		}
		s.checkpoint(label)
	case strings.HasPrefix(id, "caret."):
		s.typingOpen = false
	}
	selBefore := s.Sel

	defer func() {
		if p := recover(); p != nil {
			result, err = nil, &CmdError{Kind: Failed, Msg: fmt.Sprintf("%s panicked: %v", id, p)}
		}
		if err != nil {
			if before != nil {
				s.Doc, s.Sel, s.Pending = before.doc, before.sel, before.pending
				s.history, s.redo, s.typingOpen, s.evicted = before.history, before.redo, before.typingOpen, before.evicted
			}
			return
		}
		if spec.Mutates {
			s.Dirty = true
			if !s.Doc.EnsureNonempty() {
				s.Doc, s.Sel, s.Pending = before.doc, before.sel, before.pending
				s.history, s.redo, s.typingOpen, s.evicted = before.history, before.redo, before.typingOpen, before.evicted
				result, err = nil, notPorted("%s: repairing empty table cells or parts", id)
				return
			}
		}
		s.clampSelection()
		// A selection command that moves the insertion point ends the typing group (#419).
		if !spec.Mutates && !s.Sel.equal(selBefore) {
			s.typingOpen = false
		}
	}()
	result, err = spec.Run(s, params)
	return result, asCmdError(err)
}
