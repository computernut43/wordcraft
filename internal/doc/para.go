package doc

// Paragraph edit primitives, ported from crates/doc/src/para.rs (Paragraph::check, clamp,
// run_at, props_at, props_of_char, insert_text, insert_run, normalize, delete, format).

import (
	"slices"
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
	p.insertRun(off, len(clean), props)
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
			p.Runs = insertAt(p.Runs, i+1, Run{Len: right, Props: r.Props})
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
			out = append(out, Run{Len: n, Props: p.Mark})
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
		mark := p.PropsOfChar(a)
		mark.Ins, mark.Del, mark.FmtChange = p.Mark.Ins, p.Mark.Del, p.Mark.FmtChange
		p.Mark = mark
	}
	if k0, k1 := p.objectsBefore(a), p.objectsBefore(b); k1 > k0 {
		if k1 <= len(p.Objects) {
			p.Objects = append(p.Objects[:k0:k0], p.Objects[k1:]...)
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
			props := r.Props
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

// Len is the text's length in bytes.
func (p *Paragraph) Len() int { return len(p.Text) }

// IsEmpty reports an empty paragraph.
func (p *Paragraph) IsEmpty() bool { return p.Text == "" }

// PlainText is the text with objects removed or replaced by their text (fields by their result).
func (p *Paragraph) PlainText() string { return p.TextWithout(nil) }

// FinalText is PlainText without tracked deletions: the text as it reads with every change
// accepted.
func (p *Paragraph) FinalText() string { return p.TextWithout(p.DeletedRanges()) }

// ByteRange is a range of byte offsets [Start, End).
type ByteRange struct{ Start, End int }

// RunRange is a run as a byte range and its formatting.
type RunRange struct {
	ByteRange
	Props CharProps
}

// RunRanges are the runs as byte ranges.
func (p *Paragraph) RunRanges() []RunRange {
	out := make([]RunRange, 0, len(p.Runs))
	s := 0
	for _, r := range p.Runs {
		out = append(out, RunRange{ByteRange{s, s + r.Len}, r.Props})
		s += r.Len
	}
	return out
}

// DeletedRanges are the byte ranges of the tracked deletions.
func (p *Paragraph) DeletedRanges() []ByteRange {
	var out []ByteRange
	for _, r := range p.RunRanges() {
		if r.Props.Del != nil {
			out = append(out, r.ByteRange)
		}
	}
	return out
}

// TextWithout is PlainText without the text in the byte ranges dropped (in any order,
// overlapping or not).
func (p *Paragraph) TextWithout(dropped []ByteRange) string {
	sorted := slices.Clone(dropped)
	slices.SortStableFunc(sorted, func(a, b ByteRange) int { return a.Start - b.Start })
	var out strings.Builder
	k, next := 0, 0
	for i, c := range p.Text {
		// By start: once the ranges that end by i are skipped, i is dropped if the next has begun.
		for next < len(sorted) && sorted[next].End <= i {
			next++
		}
		keep := next >= len(sorted) || sorted[next].Start > i
		if c == Obj {
			if keep && k < len(p.Objects) {
				out.WriteString(p.Objects[k].PlainText())
			}
			k++
		} else if keep {
			out.WriteRune(c)
		}
	}
	return out.String()
}

// ObjectAt is the object at byte off (which must hold U+FFFC).
func (p *Paragraph) ObjectAt(off int) (Object, bool) {
	if off < 0 || off > len(p.Text) || !strings.HasPrefix(p.Text[off:], string(Obj)) {
		return Object{}, false
	}
	k := p.objectsBefore(off)
	if k >= len(p.Objects) {
		return Object{}, false
	}
	return p.Objects[k], true
}

// SetObjectAt replaces the object at byte off; false when there is none.
func (p *Paragraph) SetObjectAt(off int, o Object) bool {
	if _, ok := p.ObjectAt(off); !ok {
		return false
	}
	p.Objects = slices.Clone(p.Objects)
	p.Objects[p.objectsBefore(off)] = o
	return true
}

// ObjectOffsets are the byte offsets of all objects, in order.
func (p *Paragraph) ObjectOffsets() []int {
	var out []int
	for i, c := range p.Text {
		if c == Obj {
			out = append(out, i)
		}
	}
	return out
}

// InsertObject inserts an object at off.
func (p *Paragraph) InsertObject(off int, o Object, props CharProps) error {
	if err := p.check(off); err != nil {
		return err
	}
	k := p.objectsBefore(off)
	p.Text = p.Text[:off] + string(Obj) + p.Text[off:]
	p.Objects = slices.Insert(slices.Clip(p.Objects), min(k, len(p.Objects)), o)
	p.insertRun(off, utf8.RuneLen(Obj), props)
	return nil
}

// DeleteExcept deletes a..b except the one-character objects at the offsets in keep (control
// markers a deletion leaves in place).
func (p *Paragraph) DeleteExcept(a, b int, keep []int) error {
	a, b = min(a, b), max(a, b)
	w := utf8.RuneLen(Obj)
	var kept []int
	for _, k := range keep {
		if k >= a && k+w <= b && k <= len(p.Text) && strings.HasPrefix(p.Text[k:], string(Obj)) {
			kept = append(kept, k)
		}
	}
	if len(kept) == 0 {
		return p.Delete(a, b)
	}
	slices.Sort(kept)
	kept = slices.Compact(kept)
	// Pieces between the kept objects, last first so earlier offsets stay valid.
	type piece struct{ x, y int }
	pieces := make([]piece, 0, len(kept)+1)
	from := a
	for _, k := range kept {
		pieces = append(pieces, piece{from, k})
		from = k + w
	}
	pieces = append(pieces, piece{from, b})
	for i := len(pieces) - 1; i >= 0; i-- {
		if pieces[i].y > pieces[i].x {
			if err := p.Delete(pieces[i].x, pieces[i].y); err != nil {
				return err
			}
		}
	}
	return nil
}

// SplitOff splits the paragraph at off: p keeps Text[:off], the returned paragraph gets the rest
// with the same paragraph properties (the section break, if any, moves to the second half).
func (p *Paragraph) SplitOff(off int) (*Paragraph, error) {
	if err := p.check(off); err != nil {
		return nil, err
	}
	k := p.objectsBefore(off)
	tailText := p.Text[off:]
	p.Text = p.Text[:off]
	var tailObjs Vec[Object]
	if k <= len(p.Objects) {
		tailObjs = slices.Clone(p.Objects[k:])
		p.Objects = slices.Clip(p.Objects[:k])
	}
	var keep, tailRuns []Run
	start := 0
	for _, r := range p.Runs {
		rs, re := start, start+r.Len
		start = re
		switch {
		case re <= off:
			keep = append(keep, r)
		case rs >= off:
			tailRuns = append(tailRuns, r)
		default:
			keep = append(keep, Run{Len: off - rs, Props: r.Props})
			tailRuns = append(tailRuns, Run{Len: re - off, Props: r.Props})
		}
	}
	p.Runs = keep
	// The new paragraph's mark takes the formatting at the split, but never a hyperlink.
	carry := p.PropsAt(off).Unlinked()
	tail := &Paragraph{Text: tailText, Runs: tailRuns, Props: p.Props, Objects: tailObjs, Section: p.Section}
	tail.Mark = carry
	if p.Text == "" && off == 0 {
		tail.Mark = p.Mark
	}
	p.Section = nil
	if tail.Text == "" {
		tail.Mark = p.PropsAt(off).Unlinked()
	}
	p.Normalize()
	tail.Normalize()
	if tail.Runs == nil {
		tail.Runs = Vec[Run]{}
	}
	return tail, nil
}

// Append appends other's content (its paragraph properties are dropped; its section break kept).
func (p *Paragraph) Append(other *Paragraph) {
	p.Text += other.Text
	p.Runs = append(slices.Clip(p.Runs), other.Runs...)
	p.Objects = append(slices.Clip(p.Objects), other.Objects...)
	if other.Section != nil {
		p.Section = other.Section
	}
	p.Normalize()
}

// isWordChar is a character that belongs to a word for word selection and Ctrl+arrow movement
// (bidi.rs is_word_char): letters, digits, _ and ', plus what sits inside Persian and Arabic
// words: ZWNJ, ZWJ, tatweel and the Arabic combining marks, and the combining diacriticals.
func isWordChar(c rune) bool {
	if isAlphanumeric(c) || c == '_' || c == '\'' {
		return true
	}
	switch {
	case c == 0x200C, c == 0x200D, c == 0x0640, 0x0610 <= c && c <= 0x061A, 0x064B <= c && c <= 0x065F, c == 0x0670,
		0x06D6 <= c && c <= 0x06DC, 0x06DF <= c && c <= 0x06E8, 0x06EA <= c && c <= 0x06ED, 0x08D3 <= c && c <= 0x08FF,
		0x0300 <= c && c <= 0x036F:
		return true
	}
	return false
}

// WordStart is the start of the word at or before off (Ctrl+Left).
func (p *Paragraph) WordStart(off int) int {
	off = p.Clamp(off)
	before := []rune(p.Text[:off])
	idx, seenWord, at := off, false, off
	for i := len(before) - 1; i >= 0; i-- {
		c := before[i]
		at -= utf8.RuneLen(c)
		if isWordChar(c) {
			seenWord, idx = true, at
		} else if seenWord {
			break
		} else {
			idx = at
		}
	}
	return idx
}

// WordEnd is the end of the word after off, including trailing spaces (Ctrl+Right, like Word).
func (p *Paragraph) WordEnd(off int) int {
	off = p.Clamp(off)
	after := p.Text[off:]
	first, _ := utf8.DecodeRuneInString(after)
	firstWord := after != "" && isWordChar(first) && first != '\''
	inSpace := false
	for i, c := range after {
		w := isWordChar(c)
		if c == ' ' || c == 0xA0 {
			inSpace = true
			continue
		}
		if inSpace || (firstWord && !w) || (!firstWord && i > 0) {
			return off + i
		}
	}
	return len(p.Text)
}

// WordAt is the word around off (double-click): its start and end with the trailing spaces.
func (p *Paragraph) WordAt(off int) (int, int) {
	off = p.Clamp(off)
	before := []rune(p.Text[:off])
	a, at := off, off
	for i := len(before) - 1; i >= 0; i-- {
		at -= utf8.RuneLen(before[i])
		if !isWordChar(before[i]) {
			break
		}
		a = at
	}
	b := off
	for i, c := range p.Text[off:] {
		if !isWordChar(c) {
			if i == 0 && a == off {
				b = off + utf8.RuneLen(c)
			}
			break
		}
		b = off + i + utf8.RuneLen(c)
	}
	// Word also selects the trailing spaces.
	e := b
	for i, c := range p.Text[b:] {
		if c != ' ' {
			break
		}
		e = b + i + 1
	}
	return a, e
}

// SentenceAt is the sentence around off (Ctrl+click): it ends after ., ! or ? and the spaces
// that follow.
func (p *Paragraph) SentenceAt(off int) (int, int) {
	off = p.Clamp(off)
	t := p.Text
	a, prevEnd := 0, false
	for i, c := range t {
		if i > off {
			break
		}
		if prevEnd && c != ' ' {
			a = i
		}
		if c == '.' || c == '!' || c == '?' {
			prevEnd = true
		} else if c != ' ' {
			prevEnd = false
		}
	}
	b := len(t)
	// A caret on the space right after a sentence's end belongs to that sentence; a terminator
	// directly followed by text doesn't end the sentence the caret is in.
	ended := strings.HasSuffix(t[:off], ".") || strings.HasSuffix(t[:off], "!") || strings.HasSuffix(t[:off], "?")
	ended = ended && strings.HasPrefix(t[off:], " ")
	for i, c := range t[off:] {
		if ended && c != ' ' {
			b = off + i
			break
		}
		if c == '.' || c == '!' || c == '?' {
			ended = true
		}
	}
	return a, b
}
