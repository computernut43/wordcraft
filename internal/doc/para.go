package doc

// Paragraph edit primitives, ported from crates/doc/src/para.rs (Paragraph::check, clamp,
// run_at, props_at, props_of_char, insert_text, insert_run, normalize, delete, format).

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Obj is U+FFFC, the character standing for an inline object (picture, field, …) in Text.
const Obj = '￼'

// isCharBoundary is Rust's str::is_char_boundary.
func isCharBoundary(s string, i int) bool {
	if i == 0 || i == len(s) {
		return true
	}
	if i < 0 || i > len(s) {
		return false
	}
	return utf8.RuneStart(s[i])
}

// check refuses an offset outside the text or inside a character.
func (p *Paragraph) check(off int) error {
	if off < 0 || off > len(p.Text) || !isCharBoundary(p.Text, off) {
		return badOffset(off)
	}
	return nil
}

// Clamp brings off into the text and back to a character boundary.
func (p *Paragraph) Clamp(off int) int {
	o := min(max(off, 0), len(p.Text))
	for o > 0 && !isCharBoundary(p.Text, o) {
		o--
	}
	return o
}

// runAt is the index of the run containing byte off and where it starts. At a run boundary the
// earlier run wins (typing continues the formatting to the left).
func (p *Paragraph) runAt(off int) (int, int, bool) {
	start := 0
	for i, r := range p.Runs {
		end := start + r.Len
		if off < end || (off == end && off > start) {
			return i, start, true
		}
		start = end
	}
	return 0, 0, false
}

// PropsAt is the formatting in effect at off (what typing there would get): the run to the
// left, or the first run at offset 0, or the paragraph mark when empty.
func (p *Paragraph) PropsAt(off int) CharProps {
	if off == 0 {
		if len(p.Runs) > 0 {
			return p.Runs[0].Props
		}
		return p.Mark
	}
	if i, _, ok := p.runAt(off); ok {
		return p.Runs[i].Props
	}
	return p.Mark
}

// PropsOfChar is the formatting of the character starting at off.
func (p *Paragraph) PropsOfChar(off int) CharProps {
	start := 0
	for _, r := range p.Runs {
		if off < start+r.Len {
			return r.Props
		}
		start += r.Len
	}
	return p.Mark
}

// objectsBefore counts the objects in Text[:off].
func (p *Paragraph) objectsBefore(off int) int {
	if off < 0 || off > len(p.Text) || !isCharBoundary(p.Text, off) {
		return 0
	}
	return strings.Count(p.Text[:off], string(Obj))
}

// objects returns the inline objects (kept as JSON in Extra until they are modelled).
func (p *Paragraph) objects() []json.RawMessage {
	var objs []json.RawMessage
	p.Extra.Get("objects", &objs)
	return objs
}

func (p *Paragraph) setObjects(objs []json.RawMessage) {
	if len(objs) == 0 {
		p.Extra.set("objects", nil)
		return
	}
	raw, err := json.Marshal(objs)
	if err != nil {
		return
	}
	p.Extra.set("objects", raw)
}

// InsertText inserts plain text (U+FFFC is dropped) at off with props and returns the number of
// bytes inserted.
func (p *Paragraph) InsertText(off int, s string, props CharProps) (int, error) {
	if err := p.check(off); err != nil {
		return 0, err
	}
	clean := strings.ReplaceAll(s, string(Obj), "")
	if clean == "" {
		return 0, nil
	}
	p.Text = p.Text[:off] + clean + p.Text[off:]
	p.insertRun(off, len(clean), props.Clone())
	return len(clean), nil
}

// insertRun splits the run at off, inserts a run there and merges neighbours.
func (p *Paragraph) insertRun(off, n int, props CharProps) {
	start, idx := 0, len(p.Runs)
	for i, r := range p.Runs {
		if off <= start {
			idx = i
			break
		}
		if off < start+r.Len {
			left := off - start
			right := r.Len - left
			p.Runs[i].Len = left
			p.Runs = insertAt(p.Runs, i+1, Run{Len: right, Props: r.Props.Clone()})
			idx = i + 1
			break
		}
		start += r.Len
	}
	p.Runs = insertAt(p.Runs, min(idx, len(p.Runs)), Run{Len: n, Props: props})
	p.Normalize()
}

func insertAt[T any](s []T, i int, v T) []T {
	s = append(s, v)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

// Normalize merges adjacent equal runs, drops empty ones, and makes the runs end on character
// boundaries and cover the text exactly (files lie: lengths are clamped and the tail is padded).
func (p *Paragraph) Normalize() {
	n := len(p.Text)
	end := 0
	out := make([]Run, 0, len(p.Runs))
	for _, r := range p.Runs {
		e := min(satAdd(end, r.Len), n)
		for !isCharBoundary(p.Text, e) {
			e++
		}
		if e == end {
			continue
		}
		if l := len(out); l > 0 && out[l-1].Props.Equal(r.Props) {
			out[l-1].Len += e - end
		} else {
			out = append(out, Run{Len: e - end, Props: r.Props})
		}
		end = e
	}
	if end < n {
		if l := len(out); l > 0 {
			out[l-1].Len += n - end
		} else {
			out = append(out, Run{Len: n, Props: p.Mark.Clone()})
		}
	}
	p.Runs = out
}

// satAdd adds non-negative ints without overflowing (Rust: saturating_add).
func satAdd(a, b int) int {
	if b > 0 && a > int(^uint(0)>>1)-b {
		return int(^uint(0) >> 1)
	}
	return a + b
}

// revisionKeys are the mark's own tracked-change fields, which a deletion never takes from the
// deleted text.
var revisionKeys = []string{"ins", "del", "fmtChange"}

// Delete removes Text[a:b] (bytes, in either order), with the objects inside it.
func (p *Paragraph) Delete(a, b int) error {
	a, b = min(a, b), max(a, b)
	if err := p.check(a); err != nil {
		return err
	}
	if err := p.check(b); err != nil {
		return err
	}
	if a == b {
		return nil
	}
	if (len(p.Runs) == 0 || a == 0) && b == len(p.Text) {
		// Emptying: the mark keeps the deleted text's formatting, but stays its own tracked
		// change (or not), whatever revision the deleted text was part of.
		mark := p.PropsOfChar(a).Clone()
		mark.Extra = mark.Extra.Clone()
		for _, k := range revisionKeys {
			mark.Extra.set(k, p.Mark.Extra[k])
		}
		p.Mark = mark
	}
	if k0, k1 := p.objectsBefore(a), p.objectsBefore(b); k1 > k0 {
		if objs := p.objects(); k1 <= len(objs) {
			p.setObjects(append(objs[:k0:k0], objs[k1:]...))
		}
	}
	p.Text = p.Text[:a] + p.Text[b:]
	start := 0
	for i := range p.Runs {
		rs, re := start, start+p.Runs[i].Len
		start = re
		ov := max(min(re, b)-max(rs, a), 0)
		p.Runs[i].Len -= min(ov, p.Runs[i].Len)
	}
	p.Normalize()
	return nil
}

// Format applies f to the formatting of Text[a:b] (bytes, in either order).
func (p *Paragraph) Format(a, b int, f func(*CharProps)) error {
	a, b = min(a, b), max(a, b)
	if err := p.check(a); err != nil {
		return err
	}
	if err := p.check(b); err != nil {
		return err
	}
	if a == b {
		return nil
	}
	out := make([]Run, 0, len(p.Runs)+2)
	start := 0
	for _, r := range p.Runs {
		rs, re := start, start+r.Len
		start = re
		cut := [4]int{rs, clampInt(a, rs, re), clampInt(b, rs, re), re}
		for w := range 3 {
			x, y := cut[w], cut[w+1]
			if y <= x {
				continue
			}
			props := r.Props.Clone()
			if x >= a && y <= b {
				f(&props)
			}
			out = append(out, Run{Len: y - x, Props: props})
		}
	}
	p.Runs = out
	p.Normalize()
	return nil
}

// clampInt is Rust's Ord::clamp for lo <= hi (a run's start and end).
func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }
