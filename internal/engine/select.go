package engine

// Selection commands that set the selection directly (Rust cmd/caret.rs select.range and
// select.collapse) and the position parameter they read (Rust cmd/mod.rs parse_pos).

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/computernut43/wordcraft/internal/doc"
)

func rangeCommands() []CommandSpec {
	return []CommandSpec{
		{ID: "select.range", Label: "Select Range", Location: "Navigation", Params: `{"anchor": Pos, "focus": Pos}`, Run: selectRange},
		{ID: "select.collapse", Label: "Collapse Selection", Location: "Navigation", Shortcut: "Escape", Run: func(s *Session, p Params) (any, error) {
			end, _ := p.Bool("end")
			a, b := s.Sel.Ordered()
			if end {
				a = b
			}
			s.Sel = Caret(a)
			return s.SelResult(), nil
		}},
	}
}

// selectRange selects anchor..focus, each clamped into the document. Covered: positions in
// top-level body paragraphs, and any body position in a document without tables. Clamping into
// a table or another story isn't ported yet.
func selectRange(s *Session, p Params) (any, error) {
	a, ok := parsePos(p["anchor"])
	if !ok {
		return nil, errParams("bad `anchor`")
	}
	b, ok := parsePos(p["focus"])
	if !ok {
		return nil, errParams("bad `focus`")
	}
	ca, err := clampPos(s.Doc, a)
	if err != nil {
		return nil, err
	}
	cb, err := clampPos(s.Doc, b)
	if err != nil {
		return nil, err
	}
	s.Sel = Selection{Anchor: ca, Focus: cb}
	return s.SelResult(), nil
}

// clampPos is Rust Document::clamp for the positions the model can reach.
func clampPos(d *doc.Document, p doc.Pos) (doc.Pos, error) {
	if d.ParaAt(p) != nil {
		return d.Clamp(p), nil
	}
	if _, complete := d.ParaPaths(); !complete {
		return doc.Pos{}, notPorted("positions clamped in a document with tables")
	}
	if !p.Story.IsBody() {
		return doc.Pos{}, notPorted("positions in other stories")
	}
	return d.Clamp(p), nil
}

// parsePos reads a position parameter as Rust's parse_pos: a serialized Pos ({"story", "path",
// "off"}, or the array [story, path, off] serde also accepts), else {"block": n, "off"?: n} in
// the body. Parameters are decoded with json.Decoder.UseNumber.
func parsePos(v any) (doc.Pos, bool) {
	if p, ok := serdePos(v); ok {
		return p, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return doc.Pos{}, false
	}
	block, ok := asU64(m["block"])
	if !ok {
		return doc.Pos{}, false
	}
	off, _ := asU64(m["off"])
	// Rust: Path::top(block as u32) truncates, as usize offsets saturate here.
	return doc.Pos{Story: doc.Body, Path: doc.Path{uint32(block)}, Off: satInt(off)}, true
}

// serdePos is serde's derived Deserialize for Pos from a JSON value: all three fields required
// (unknown keys ignored), or a sequence of exactly three.
func serdePos(v any) (doc.Pos, bool) {
	var story, path, off any
	switch x := v.(type) {
	case map[string]any:
		var ok1, ok2, ok3 bool
		story, ok1 = x["story"]
		path, ok2 = x["path"]
		off, ok3 = x["off"]
		if !ok1 || !ok2 || !ok3 {
			return doc.Pos{}, false
		}
	case []any:
		if len(x) != 3 {
			return doc.Pos{}, false
		}
		story, path, off = x[0], x[1], x[2]
	default:
		return doc.Pos{}, false
	}
	st, ok := serdeStory(story)
	if !ok {
		return doc.Pos{}, false
	}
	elems, ok := path.([]any)
	if !ok {
		return doc.Pos{}, false
	}
	pth := make(doc.Path, 0, min(len(elems), 64))
	for _, e := range elems {
		n, ok := asU64(e)
		if !ok || n > math.MaxUint32 {
			return doc.Pos{}, false
		}
		pth = append(pth, uint32(n))
	}
	o, ok := asU64(off)
	if !ok {
		return doc.Pos{}, false
	}
	return doc.Pos{Story: st, Path: pth, Off: satInt(o)}, true
}

// serdeStory reads StoryRef as serde's externally tagged enum: "body", {"body": null} or
// {"part": u32}.
func serdeStory(v any) (doc.StoryRef, bool) {
	switch x := v.(type) {
	case string:
		return doc.Body, x == "body"
	case map[string]any:
		if len(x) != 1 {
			return doc.StoryRef{}, false
		}
		if b, ok := x["body"]; ok {
			return doc.Body, b == nil
		}
		n, ok := asU64(x["part"])
		if !ok || n > math.MaxUint32 {
			return doc.StoryRef{}, false
		}
		id := uint32(n)
		return doc.StoryRef{Part: &id}, true
	}
	return doc.StoryRef{}, false
}

// asU64 is serde_json's Value::as_u64: a non-negative integer literal only (no fraction or
// exponent).
func asU64(v any) (uint64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	u, err := strconv.ParseUint(string(n), 10, 64)
	if err != nil {
		return 0, false
	}
	return u, true
}

func satInt(u uint64) int {
	if u > math.MaxInt {
		return math.MaxInt
	}
	return int(u)
}
