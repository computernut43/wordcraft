package doc

import (
	"math"
	"slices"
)

// Tables: a column grid, rows and cells (cells hold block lists); crates/doc/src/table.rs. Merges
// follow OOXML: a cell spans Span grid columns; vertical merges are VMergeRestart +
// VMergeContinue cells.
//
// Table methods change the table in place. A table reached from a document may be shared with an
// undo snapshot, so edit a Clone and put it back (copy on write).

// Cell is a table cell.
type Cell struct {
	Props  CellProps  `json:"props" serde:"default"`
	Blocks Vec[Block] `json:"blocks"`
	// Controls are the content controls around the cell.
	Controls ControlWrap `json:"controls,omitzero" serde:"default"`
}

// EmptyCell is a cell with one empty paragraph.
func EmptyCell() Cell {
	return Cell{Props: CellProps{Span: 1}, Blocks: Vec[Block]{{Para: NewParagraph()}}}
}

// CellWithText is a cell with one paragraph of text s.
func CellWithText(s string) Cell {
	return Cell{Props: CellProps{Span: 1}, Blocks: Vec[Block]{{Para: ParagraphWithText(s, CharProps{})}}}
}

// Span is the number of grid columns the cell spans, 1 to MaxCols.
func (c *Cell) Span() int { return int(min(max(c.Props.Span, 1), MaxCols)) }

// Row is a table row.
type Row struct {
	Props RowProps  `json:"props" serde:"default"`
	Cells Vec[Cell] `json:"cells"`
	// Controls are the content controls around the row (repeating sections).
	Controls ControlWrap `json:"controls,omitzero" serde:"default"`
}

// Table is a table.
type Table struct {
	Props TableProps `json:"props" serde:"default"`
	// Grid holds the grid column widths, points.
	Grid Vec[F32] `json:"grid"`
	Rows Vec[Row] `json:"rows"`
	// Controls are the content controls whose content starts or ends with this table.
	Controls ControlWrap `json:"controls,omitzero" serde:"default"`
}

// Hard limits that keep hostile input from exhausting memory.
const (
	MaxRows = 32767
	MaxCols = 63
)

// ParagraphWithText is a paragraph of text s (inline object markers dropped) in props.
func ParagraphWithText(s string, props CharProps) *Paragraph {
	p := NewParagraph()
	var clean []byte
	for _, c := range s {
		if c != Obj {
			clean = append(clean, string(c)...)
		}
	}
	if len(clean) > 0 {
		p.Runs = append(p.Runs, Run{Len: len(clean), Props: props})
	}
	p.Text, p.Mark = string(clean), props
	return p
}

// NewTable is a rows × cols table spanning width points, "Table Grid" style.
func NewTable(rows, cols int, width F32) *Table {
	rows, cols = min(max(rows, 1), MaxRows), min(max(cols, 1), MaxCols)
	w := fmax(F32(float32(width)/float32(cols)), 6)
	t := &Table{Props: NewTableProps(), Grid: make(Vec[F32], cols), Rows: make(Vec[Row], rows)}
	t.Props.Style = ptr("TableGrid")
	for i := range t.Grid {
		t.Grid[i] = w
	}
	for r := range t.Rows {
		cells := make(Vec[Cell], cols)
		for c := range cells {
			cells[c] = EmptyCell()
			cells[c].Props.Width = ptr(w)
		}
		t.Rows[r] = Row{Cells: cells}
	}
	return t
}

// Clone copies the table's grid, rows and cells so they can be edited; blocks inside cells stay
// shared (they are replaced, never changed in place).
func (t *Table) Clone() *Table {
	c := *t
	c.Grid = slices.Clone(t.Grid)
	c.Rows = slices.Clone(t.Rows)
	for i := range c.Rows {
		c.Rows[i].Cells = slices.Clone(c.Rows[i].Cells)
		for j := range c.Rows[i].Cells {
			c.Rows[i].Cells[j].Blocks = slices.Clone(c.Rows[i].Cells[j].Blocks)
		}
	}
	return &c
}

// Equal compares like Rust's PartialEq.
func (t *Table) Equal(o *Table) bool { return equal(t, o) }

// Cols is the number of grid columns: the grid's, or the widest row's when that is wider.
func (t *Table) Cols() int {
	n := len(t.Grid)
	for _, r := range t.Rows {
		w := 0
		for i := range r.Cells {
			w += r.Cells[i].Span()
		}
		n = max(n, w)
	}
	return n
}

// GridCol is the grid column where cell c of row r starts.
func (t *Table) GridCol(r, c int) int {
	if r < 0 || r >= len(t.Rows) {
		return 0
	}
	x := 0
	for i := range min(max(c, 0), len(t.Rows[r].Cells)) {
		x += t.Rows[r].Cells[i].Span()
	}
	return x
}

// CellAtGrid is the index of the cell in row r that covers grid column g.
func (t *Table) CellAtGrid(r, g int) (int, bool) {
	if r < 0 || r >= len(t.Rows) {
		return 0, false
	}
	x := 0
	for i := range t.Rows[r].Cells {
		s := t.Rows[r].Cells[i].Span()
		if g < x+s {
			return i, true
		}
		x += s
	}
	return 0, false
}

// cell is cell c of row r, or nil.
func (t *Table) cell(r, c int) *Cell {
	if r < 0 || r >= len(t.Rows) || c < 0 || c >= len(t.Rows[r].Cells) {
		return nil
	}
	return &t.Rows[r].Cells[c]
}

// InsertRow inserts a row copied (formatting, empty text) from row like at index at.
func (t *Table) InsertRow(at, like int) {
	if len(t.Rows) == 0 || len(t.Rows) >= MaxRows {
		return
	}
	src := t.Rows[min(max(like, 0), len(t.Rows)-1)]
	row := Row{Props: src.Props, Cells: make(Vec[Cell], 0, len(src.Cells))}
	row.Props.Header = false
	for _, c := range src.Cells {
		nc := EmptyCell()
		nc.Props = c.Props
		if nc.Props.VMerge == VMergeRestart {
			nc.Props.VMerge = VMergeNone
		}
		row.Cells = append(row.Cells, nc)
	}
	t.Rows = slices.Insert(t.Rows, min(max(at, 0), len(t.Rows)), row)
}

// DeleteRow deletes row r.
func (t *Table) DeleteRow(r int) {
	if r < 0 || r >= len(t.Rows) {
		return
	}
	// A vertical merge that starts in this row now starts in the row below.
	var starts []int
	for c, x := range t.Rows[r].Cells {
		if x.Props.VMerge == VMergeRestart {
			starts = append(starts, t.GridCol(r, c))
		}
	}
	continues := func(row, g int) bool {
		i, ok := t.CellAtGrid(row, g)
		x := t.cell(row, i)
		return ok && x != nil && x.Props.VMerge == VMergeContinue
	}
	for _, g := range starts {
		if !continues(r+1, g) {
			continue
		}
		more := continues(r+2, g)
		if i, ok := t.CellAtGrid(r+1, g); ok {
			if c := t.cell(r+1, i); c != nil {
				c.Props.VMerge = VMergeNone
				if more {
					c.Props.VMerge = VMergeRestart
				}
			}
		}
	}
	t.Rows = slices.Delete(t.Rows, r, r+1)
}

// InsertCol inserts a grid column at grid index g (cells spanning it widen instead).
func (t *Table) InsertCol(g int) {
	if len(t.Grid) >= MaxCols {
		return
	}
	g = min(max(g, 0), len(t.Grid))
	w := F32(72)
	if len(t.Grid) > 0 {
		w = t.Grid[min(g, len(t.Grid)-1)]
	}
	t.Grid = slices.Insert(t.Grid, g, w)
	for r := range t.Rows {
		row := &t.Rows[r]
		x, idx, inside := 0, -1, false
		for i := range row.Cells {
			if g <= x {
				idx = i
				break
			}
			if s := row.Cells[i].Span(); g < x+s {
				idx, inside = i, true
				break
			} else {
				x += s
			}
		}
		switch {
		case inside:
			row.Cells[idx].Props.Span++
		default:
			c := EmptyCell()
			c.Props.Width = ptr(w)
			if idx < 0 {
				row.Cells = append(row.Cells, c)
			} else {
				row.Cells = slices.Insert(row.Cells, idx, c)
			}
		}
	}
}

// DeleteCol deletes grid column g (spanning cells shrink).
func (t *Table) DeleteCol(g int) {
	if g < 0 || g >= len(t.Grid) || len(t.Grid) <= 1 {
		return
	}
	t.Grid = slices.Delete(t.Grid, g, g+1)
	for r := range t.Rows {
		row := &t.Rows[r]
		x, rm := 0, -1
		for i := range row.Cells {
			c := &row.Cells[i]
			if s := c.Span(); g < x+s {
				if s > 1 {
					c.Props.Span--
				} else {
					rm = i
				}
				break
			} else {
				x += s
			}
		}
		if rm >= 0 {
			row.Cells = slices.Delete(row.Cells, rm, rm+1)
		}
	}
	t.Rows = slices.DeleteFunc(t.Rows, func(r Row) bool { return len(r.Cells) == 0 })
}

// nonEmpty is the blocks that aren't empty paragraphs.
func nonEmpty(bl Vec[Block]) Vec[Block] {
	var out Vec[Block]
	for _, b := range bl {
		if b.Para == nil || b.Para.Text != "" {
			out = append(out, b)
		}
	}
	return out
}

// MergeRight merges cells c0..=c1 of row r horizontally (content is concatenated).
func (t *Table) MergeRight(r, c0, c1 int) {
	if r < 0 || r >= len(t.Rows) {
		return
	}
	row := &t.Rows[r]
	if c0 < 0 || c1 <= c0 || c1 >= len(row.Cells) {
		return
	}
	removed := slices.Clone(row.Cells[c0+1 : c1+1])
	row.Cells = slices.Delete(row.Cells, c0+1, c1+1)
	first := &row.Cells[c0]
	blocks := slices.Clone(first.Blocks)
	for _, c := range removed {
		first.Props.Span += max(c.Props.Span, 1)
		blocks = append(blocks, nonEmpty(c.Blocks)...)
	}
	if len(blocks) > 1 && blocks[0].Para != nil && blocks[0].Para.Text == "" {
		blocks = blocks[1:]
	}
	first.Blocks = blocks
}

// Merge merges a rectangle of cells (rows r0..=r1, grid columns g0..=g1).
func (t *Table) Merge(r0, r1, g0, g1 int) {
	last := min(r1, len(t.Rows)-1)
	for r := max(r0, 0); r <= last; r++ {
		a, okA := t.CellAtGrid(r, g0)
		b, okB := t.CellAtGrid(r, g1)
		if !okA || !okB {
			continue
		}
		t.MergeRight(r, a, b)
		if c := t.cell(r, a); c != nil {
			switch {
			case r1 == r0:
				c.Props.VMerge = VMergeNone
			case r == r0:
				c.Props.VMerge = VMergeRestart
			default:
				c.Props.VMerge = VMergeContinue
			}
		}
	}
	// Move the content of continued cells into the restart cell.
	if r1 > r0 {
		a, ok := t.CellAtGrid(r0, g0)
		if !ok {
			return
		}
		var moved Vec[Block]
		for r := r0 + 1; r <= last; r++ {
			ci, ok := t.CellAtGrid(r, g0)
			if c := t.cell(r, ci); ok && c != nil {
				moved = append(moved, nonEmpty(c.Blocks)...)
				c.Blocks = Vec[Block]{{Para: NewParagraph()}}
			}
		}
		if c := t.cell(r0, a); c != nil {
			c.Blocks = append(slices.Clone(c.Blocks), moved...)
		}
	}
}

// SplitCell splits cell c of row r into n cells horizontally. Where the cell's grid lines don't
// fall on the equal parts, the grid is refined inside the cell only: the new borders divide it
// into equal parts, and the cells of other rows widen over the new grid columns, so their extent
// and the table's width stay as they were. It returns the parts' spans, left to right.
func (t *Table) SplitCell(r, c, n int) []int {
	n = min(max(n, 1), MaxCols)
	cl := t.cell(r, c)
	if cl == nil {
		return nil
	}
	span := cl.Span()
	if n <= 1 {
		return []int{span}
	}
	g := t.GridCol(r, c)
	end := satAdd(g, span)
	// A grid narrower than the row (a damaged file) gets the columns the cell needs.
	for len(t.Grid) < end && len(t.Grid) < MaxCols {
		t.Grid = append(t.Grid, 72)
	}
	if len(t.Grid) < end {
		return []int{span}
	}
	for j := g; j < end; j++ {
		if w := t.Grid[j]; !(finite(w) && w > 0) {
			t.Grid[j] = 72
		}
	}
	const eps = F32(0.01)
	total := sumF32(t.Grid[g:end])
	// Grid lines where the cell's parts start (after the first, at g).
	lines := make([]int, 0, n)
	for k := 1; k < n; k++ {
		x := F32(float32(total) * float32(k) / float32(n))
		var acc F32
		hitCol, split, hit := 0, false, false
		var left, right F32
		for j := g; j < end; j++ {
			w := t.Grid[j]
			if j > g && fabs(x-acc) <= eps {
				hitCol, hit = j, true
				break
			}
			if x < acc+w-eps {
				if x > acc+eps {
					hitCol, hit, split = j, true, true
					left, right = x-acc, w-(x-acc)
				}
				break
			}
			acc += w
		}
		switch {
		case hit && !split:
			// An existing grid line is where the border goes.
			lines = append(lines, hitCol)
		case hit && len(t.Grid) < MaxCols:
			// A new grid line inside column hitCol: every cell over it widens over both halves.
			j := hitCol
			t.Grid[j] = left
			t.Grid = slices.Insert(t.Grid, j+1, right)
			for rr := range t.Rows {
				if ci, ok := t.CellAtGrid(rr, j); ok {
					cell := &t.Rows[rr].Cells[ci]
					cell.Props.Span = satAddU32(max(cell.Props.Span, 1), 1)
				}
			}
			end++
			lines = append(lines, j+1)
		}
	}
	lines = slices.Compact(lines)
	// The cell now spans g..end: cut it at the new lines.
	width := t.width
	row := &t.Rows[r]
	a := g
	type part struct{ a, b int }
	parts := make([]part, 0, len(lines)+1)
	for _, b := range append(lines, end) {
		if b > a {
			parts = append(parts, part{a, b})
			a = b
		}
	}
	for k, p := range parts {
		if k == 0 {
			if c < len(row.Cells) {
				row.Cells[c].Props.Span = uint32(p.b - p.a)
				row.Cells[c].Props.Width = width(p.a, p.b)
			}
			continue
		}
		nc := EmptyCell()
		nc.Props.Span = uint32(p.b - p.a)
		nc.Props.Width = width(p.a, p.b)
		row.Cells = slices.Insert(row.Cells, min(c+k, len(row.Cells)), nc)
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i] = p.b - p.a
	}
	return out
}

func satAddU32(a, b uint32) uint32 {
	if a > ^uint32(0)-b {
		return ^uint32(0)
	}
	return a + b
}

func fabs(v F32) F32 {
	if v < 0 {
		return -v
	}
	return v
}

// VerticalExtent is the rows cell c of row r covers: the first row and the count. A vertically
// merged cell runs from its VMergeRestart cell down through the VMergeContinue cells that start at
// the same grid column.
func (t *Table) VerticalExtent(r, c int) (top, count int, ok bool) {
	g := t.GridCol(r, c)
	at := func(rr int) (VMerge, bool) {
		ci, ok := t.CellAtGrid(rr, g)
		if !ok || t.GridCol(rr, ci) != g {
			return 0, false
		}
		return t.Rows[rr].Cells[ci].Props.VMerge, true
	}
	cl := t.cell(r, c)
	if cl == nil {
		return 0, 0, false
	}
	top = r
	if cl.Props.VMerge == VMergeContinue {
		for top > 0 {
			up, ok := at(top - 1)
			if !ok || up == VMergeNone {
				break
			}
			top--
			if up == VMergeRestart {
				break
			}
		}
	}
	k := 1
	if v, ok := at(top); !ok || v != VMergeNone {
		for {
			v, ok := at(top + k)
			if !ok || v != VMergeContinue {
				break
			}
			k++
		}
	}
	return top, k, true
}

// SplitCellGrid is Split Cells: it cuts cell c of row r into cols × rows cells. A cell one row
// high gets rows-1 new rows under it, over which the row's other cells merge down; a vertically
// merged cell is regrouped instead, so rows must divide its height. The content stays in the
// first part. It returns false (with the table unchanged) when the split can't be made.
func (t *Table) SplitCellGrid(r, c, cols, rows int) bool {
	cols, rows = min(max(cols, 1), MaxCols), min(max(rows, 1), MaxRows)
	top, k, ok := t.VerticalExtent(r, c)
	if !ok {
		return false
	}
	g := t.GridCol(r, c)
	ci, ok := t.CellAtGrid(top, g)
	if !ok {
		return false
	}
	if (k > 1 && k%rows != 0) || (k == 1 && satAdd(len(t.Rows), rows-1) > MaxRows) {
		return false
	}
	height := rows
	if k > 1 {
		height = k
	}
	switch {
	case k > 1 && rows > 1:
		q := k / rows
		for i := range k {
			if cj, ok := t.CellAtGrid(top+i, g); ok {
				cell := &t.Rows[top+i].Cells[cj]
				switch {
				case q == 1:
					cell.Props.VMerge = VMergeNone
				case i%q == 0:
					cell.Props.VMerge = VMergeRestart
				default:
					cell.Props.VMerge = VMergeContinue
				}
			}
		}
	case k == 1 && rows > 1:
		if top >= len(t.Rows) {
			return false
		}
		src := &t.Rows[top]
		like := Row{Props: src.Props, Cells: make(Vec[Cell], 0, len(src.Cells))}
		like.Props.Header = false
		for i := range src.Cells {
			cell := &src.Cells[i]
			nc := EmptyCell()
			nc.Props = cell.Props
			if i == ci {
				cell.Props.VMerge = VMergeNone
				nc.Props.VMerge = VMergeNone
			} else {
				// The row's other cells merge down over the new rows.
				if cell.Props.VMerge == VMergeNone {
					cell.Props.VMerge = VMergeRestart
				}
				nc.Props.VMerge = VMergeContinue
			}
			like.Cells = append(like.Cells, nc)
		}
		for range rows - 1 {
			t.Rows = slices.Insert(t.Rows, top+1, Row{Props: like.Props, Cells: slices.Clone(like.Cells), Controls: like.Controls})
		}
	}
	if cols > 1 {
		spans := t.SplitCell(top, ci, cols)
		total := 0
		for _, s := range spans {
			total += s
		}
		var vmerge VMerge
		if cell := t.cell(top, ci); cell != nil {
			vmerge = cell.Props.VMerge
		}
		if top < len(t.Rows) {
			cells := t.Rows[top].Cells
			for i := ci + 1; i < len(cells) && i < ci+1+max(len(spans)-1, 0); i++ {
				cells[i].Props.VMerge = vmerge
			}
		}
		// The cells under it, which the new grid lines widened, are cut the same way.
		width := t.width
		for rr := top + 1; rr < top+height; rr++ {
			cj, ok := t.CellAtGrid(rr, g)
			if !ok || t.GridCol(rr, cj) != g || rr >= len(t.Rows) {
				continue
			}
			row := &t.Rows[rr]
			cell := row.Cells[cj]
			if cell.Span() != total {
				continue
			}
			a := g
			parts := make([]Cell, 0, len(spans))
			for n, span := range spans {
				part := EmptyCell()
				if n == 0 {
					part = cell
				}
				part.Props.VMerge = cell.Props.VMerge
				part.Props.Span = uint32(span)
				part.Props.Width = width(a, a+span)
				a += span
				parts = append(parts, part)
			}
			row.Cells = slices.Replace(row.Cells, cj, cj+1, parts...)
		}
	}
	return true
}

// TotalWidth is the sum of the grid's column widths.
func (t *Table) TotalWidth() F32 { return sumF32(t.Grid) }

// width is the width of grid columns a..b; nil when they aren't in the grid.
func (t *Table) width(a, b int) *F32 {
	if a < 0 || a > b || b > len(t.Grid) {
		return nil
	}
	return ptr(sumF32(t.Grid[a:b]))
}

// sumF32 is Rust's Iterator::sum of f32s, which starts from -0.0 (so an empty sum is -0.0).
func sumF32(v []F32) F32 {
	s := F32(math.Copysign(0, -1))
	for _, w := range v {
		s += w
	}
	return s
}
