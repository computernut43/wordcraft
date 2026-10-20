package doc

import "slices"

// Range fields (crates/doc/src/fields.rs): a FieldStart … FieldEnd pair whose result is the
// ordinary content between the two markers, possibly spanning paragraphs. Pairs nest like
// brackets and never cross a block list: a pair opened in a table cell closes in that cell.

// maxFieldDepth is the deepest table nesting scanned.
const maxFieldDepth = 16

// FieldRange is a whole range field.
type FieldRange struct {
	Instr  string
	Locked bool
	// Start is where the FieldStart marker is; End where the FieldEnd marker is (the result is
	// everything between the two).
	Start, End Pos
	// Depth is the number of enclosing range fields.
	Depth int
}

type fieldScan struct {
	ranges  []FieldRange
	orphans []paraMarker
}

func scanFields(story StoryRef, bl Vec[Block], prefix Path, depth int, out *fieldScan) {
	if depth > maxFieldDepth {
		return
	}
	type open struct {
		instr  string
		locked bool
		at     Pos
	}
	var opens []open
	for i, b := range bl {
		path := append(slices.Clip(prefix), uint32(i))
		switch {
		case b.Para != nil:
			p := b.Para
			for _, off := range p.ObjectOffsets() {
				o, _ := p.ObjectAt(off)
				switch v := o.InlineObject.(type) {
				case FieldStart:
					opens = append(opens, open{v.Instr, v.Locked, Pos{Story: story, Path: path, Off: off}})
				case FieldEnd:
					if len(opens) == 0 {
						out.orphans = append(out.orphans, paraMarker{path, off})
						continue
					}
					x := opens[len(opens)-1]
					opens = opens[:len(opens)-1]
					out.ranges = append(out.ranges, FieldRange{x.instr, x.locked, x.at, Pos{Story: story, Path: path, Off: off}, len(opens)})
				}
			}
		case b.Table != nil:
			for r, row := range b.Table.Rows {
				for c, cell := range row.Cells {
					scanFields(story, cell.Blocks, append(slices.Clip(path), uint32(r), uint32(c)), depth+1, out)
				}
			}
		}
	}
	for _, x := range opens {
		out.orphans = append(out.orphans, paraMarker{x.at.Path, x.at.Off})
	}
}

func scanStoryFields(story StoryRef, bl Vec[Block]) fieldScan {
	var out fieldScan
	scanFields(story, bl, nil, 0, &out)
	slices.SortStableFunc(out.ranges, func(a, b FieldRange) int { return pathOff(a.Start.Path, a.Start.Off, b.Start.Path, b.Start.Off) })
	return out
}

// FieldRanges are the whole range fields of a story, in document order of their starts.
func (d *Document) FieldRanges(story StoryRef) []FieldRange {
	bl, ok := d.Story(story)
	if !ok {
		return nil
	}
	return scanStoryFields(story, bl).ranges
}

// PastFieldEnds is p moved past the range-field ends right after it: typing or inserting there
// belongs after the field, as in Word, not in its result.
func (d *Document) PastFieldEnds(p Pos) Pos {
	para := d.ParaAt(p)
	if para == nil {
		return p
	}
	off := p.Off
	for {
		o, ok := para.ObjectAt(off)
		if _, end := o.InlineObject.(FieldEnd); !ok || !end {
			break
		}
		off += objLen
	}
	return Pos{Story: p.Story, Path: p.Path, Off: off}
}

// HasUnbalancedFieldRanges reports a story holding a range-field marker without its partner.
func (d *Document) HasUnbalancedFieldRanges() bool {
	for _, s := range d.StoryRefs() {
		bl, _ := d.Story(s)
		if len(scanStoryFields(s, bl).orphans) > 0 {
			return true
		}
	}
	return false
}

// BalanceFieldRanges removes range-field markers that have no partner and returns how many went.
func (d *Document) BalanceFieldRanges() int {
	removed := 0
	for _, s := range d.StoryRefs() {
		bl, _ := d.Story(s)
		orphans := scanStoryFields(s, bl).orphans
		slices.SortFunc(orphans, comparePM)
		for i := len(orphans) - 1; i >= 0; i-- {
			m := orphans[i]
			if d.Para(s, m.Path) == nil {
				continue
			}
			if p, err := d.ParaMutAt(s, m.Path); err == nil && p.Delete(m.Off, m.Off+objLen) == nil {
				removed++
			}
		}
	}
	return removed
}
