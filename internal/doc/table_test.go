package doc

import (
	"bytes"
	"testing"
)

// Ports of crates/doc/src/table.rs's tests.

func TestNewTableShape(t *testing.T) {
	tb := NewTable(3, 4, 468)
	if len(tb.Rows) != 3 || tb.Cols() != 4 || tb.TotalWidth() != 468 {
		t.Errorf("%d rows, %d cols, %v wide", len(tb.Rows), tb.Cols(), tb.TotalWidth())
	}
	huge := NewTable(1_000_000, 1000, 100)
	if len(huge.Rows) != MaxRows || huge.Cols() != MaxCols {
		t.Error("limits")
	}
}

func TestRowColOps(t *testing.T) {
	tb := NewTable(2, 2, 200)
	tb.InsertRow(1, 0)
	if len(tb.Rows) != 3 {
		t.Fatal("insert row")
	}
	tb.InsertCol(1)
	if tb.Cols() != 3 {
		t.Fatal("insert col")
	}
	for _, r := range tb.Rows {
		if len(r.Cells) != 3 {
			t.Fatal("every row widened")
		}
	}
	tb.DeleteCol(0)
	if tb.Cols() != 2 {
		t.Fatal("delete col")
	}
	tb.DeleteRow(0)
	if len(tb.Rows) != 2 {
		t.Fatal("delete row")
	}
	tb.DeleteRow(99)
	tb.DeleteCol(99)
	tb.DeleteRow(-1)
	tb.DeleteCol(-1)
}

func TestMergeAndSplit(t *testing.T) {
	tb := NewTable(2, 3, 300)
	tb.MergeRight(0, 0, 1)
	if len(tb.Rows[0].Cells) != 2 || tb.Rows[0].Cells[0].Span() != 2 {
		t.Fatal("merge right")
	}
	if c, ok := tb.CellAtGrid(0, 1); !ok || c != 0 || tb.GridCol(0, 1) != 2 {
		t.Fatal("grid lookups")
	}
	tb.InsertCol(1)
	if tb.Rows[0].Cells[0].Span() != 3 {
		t.Fatal("a spanning cell widens")
	}
	tb.SplitCell(0, 0, 3)
	if len(tb.Rows[0].Cells) != 4 {
		t.Fatal("split")
	}
	v := NewTable(3, 2, 100)
	v.Merge(0, 2, 0, 0)
	if v.Rows[0].Cells[0].Props.VMerge != VMergeRestart || v.Rows[2].Cells[0].Props.VMerge != VMergeContinue {
		t.Fatal("vertical merge")
	}
}

// Edits work on a Clone; the table they were cloned from is unchanged.
func TestTableCloneIsolatesEdits(t *testing.T) {
	tb := NewTable(2, 2, 100)
	before := tb.Clone()
	c := tb.Clone()
	c.MergeRight(0, 0, 1)
	c.SplitCellGrid(1, 0, 2, 2)
	c.InsertCol(0)
	if !tb.Equal(before) {
		t.Fatal("editing a clone changed the original")
	}
}

// FuzzTableEdits runs hostile edits (any indexes, negative or huge) on any table that reads:
// they never panic.
func FuzzTableEdits(f *testing.F) {
	f.Add([]byte(`{"grid":[72,72],"rows":[{"cells":[{"props":{"span":2,"vmerge":"restart"},"blocks":[]}]},{"cells":[]}]}`), []byte{5, 0, 3, 1, 9, 7, 1, 0, 2, 2})
	f.Fuzz(func(t *testing.T, js []byte, ops []byte) {
		d, err := Unmarshal(append(append([]byte(`{"body":[{"kind":"table",`), bytes.TrimPrefix(js, []byte("{"))...), ']', '}'))
		if err != nil || len(d.Body) == 0 || d.Body[0].Table == nil {
			return
		}
		tb := d.Body[0].Table.Clone()
		arg := func(i int) int {
			if i >= len(ops) {
				return 0
			}
			return int(int8(ops[i]))
		}
		for i := 0; i+4 < len(ops) && i < 200; i += 5 {
			a, b, c, e := arg(i+1), arg(i+2), arg(i+3)%8, arg(i+4)%8
			switch ops[i] % 14 {
			case 0:
				tb.InsertRow(a, b)
			case 1:
				tb.DeleteRow(a)
			case 2:
				tb.InsertCol(a)
			case 3:
				tb.DeleteCol(a)
			case 4:
				tb.MergeRight(a, b, c)
			case 5:
				tb.Merge(a, b, c, e)
			case 6:
				tb.SplitCell(a, b, c)
			case 7:
				tb.SplitCellGrid(a, b, c, e)
			case 8:
				tb.VerticalExtent(a, b)
			case 9:
				tb.CellAtGrid(a, b)
			case 10:
				tb.GridCol(a, b)
			case 11:
				tb.Cols()
			case 12:
				tb.TotalWidth()
			default:
				tb = NewTable(a, b, F32(c))
			}
		}
	})
}
