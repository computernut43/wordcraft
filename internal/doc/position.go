package doc

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// StoryRef says which story a position is in: the body, or a part (header, footer, note, text
// box) by id. JSON: "body" or {"part": id}, as Rust's StoryRef.
type StoryRef struct {
	// Part is the part id, or nil for the body.
	Part *uint32
}

// Body is the body story.
var Body = StoryRef{}

// IsBody reports the body story.
func (s StoryRef) IsBody() bool { return s.Part == nil }

func (s StoryRef) compare(o StoryRef) int {
	switch {
	case s.Part == nil && o.Part == nil:
		return 0
	case s.Part == nil:
		return -1
	case o.Part == nil:
		return 1
	}
	return cmp.Compare(*s.Part, *o.Part)
}

// MarshalJSON writes "body" or {"part": id}.
func (s StoryRef) MarshalJSON() ([]byte, error) {
	if s.Part == nil {
		return []byte(`"body"`), nil
	}
	return json.Marshal(map[string]uint32{"part": *s.Part})
}

// UnmarshalJSON reads "body" or {"part": id}.
func (s *StoryRef) UnmarshalJSON(data []byte) error {
	var name string
	if json.Unmarshal(data, &name) == nil {
		if name != "body" {
			return errors.New("doc: unknown story " + strconv.Quote(name))
		}
		*s = Body
		return nil
	}
	var p struct {
		Part *uint32 `json:"part"`
	}
	if err := json.Unmarshal(data, &p); err != nil || p.Part == nil {
		return errors.New("doc: bad story")
	}
	*s = StoryRef{Part: p.Part}
	return nil
}

// Path leads to a paragraph: [block] in a story; [block, row, cell, block] inside a table cell,
// three more indexes per nesting level. Lexicographic order is document order.
type Path []uint32

// String writes the indexes joined by "/", as Rust's Display.
func (p Path) String() string {
	s := make([]string, len(p))
	for i, v := range p {
		s[i] = strconv.FormatUint(uint64(v), 10)
	}
	return strings.Join(s, "/")
}

// Last is the block index within its container.
func (p Path) Last() int {
	if len(p) == 0 {
		return 0
	}
	return int(p[len(p)-1])
}

// MarshalJSON writes an array (never null).
func (p Path) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]uint32(p))
}

// Pos is a position in a document: a byte offset in the paragraph at Path in Story.
type Pos struct {
	Story StoryRef `json:"story"`
	Path  Path     `json:"path"`
	Off   int      `json:"off"`
}

// BodyPos is the position off in the body's top-level block block.
func BodyPos(block, off int) Pos { return Pos{Story: Body, Path: Path{uint32(block)}, Off: off} }

// Compare orders positions as Rust's derived Ord: story, then path, then offset.
func (p Pos) Compare(o Pos) int {
	if c := p.Story.compare(o.Story); c != 0 {
		return c
	}
	if c := slices.Compare(p.Path, o.Path); c != 0 {
		return c
	}
	return cmp.Compare(p.Off, o.Off)
}

// Equal reports the same position.
func (p Pos) Equal(o Pos) bool { return p.Compare(o) == 0 }

// Order returns a and b in document order.
func Order(a, b Pos) (Pos, Pos) {
	if a.Compare(b) <= 0 {
		return a, b
	}
	return b, a
}
