package doc

import "testing"

// Ports of crates/doc/src/numbering.rs's tests.

func label(t *testing.T, c *Counters, n *Numbering, num uint32, level uint8) string {
	t.Helper()
	s, _, ok := c.NextLabel(n, num, level)
	if !ok {
		t.Fatalf("no label for %d/%d", num, level)
	}
	return s
}

func lvl(format NumFormat, text string) Level {
	l := NewLevel()
	l.Format, l.Text = format, text
	return l
}

func TestAListsOwnLevelDefinitionsWin(t *testing.T) {
	// A specification's article numbers: level 1 counts parts with no label of its own, level 2
	// shows "part.article" with a zero-padded article (Word's lvlOverride/lvl).
	var n Numbering
	id := n.AddList(ListKind{Kind: ListNumbered})
	n.Nums[0].LevelOverrides = Vec[Pair[uint8, Level]]{{1, lvl(FmtDecimal, "")}, {2, lvl(FmtDecimalZero, "%2.%3")}}
	var c Counters
	for _, x := range []struct {
		level uint8
		want  string
	}{{1, ""}, {2, "1.01"}, {2, "1.02"}, {1, ""}, {2, "2.01"}, {0, "1."}} {
		if got := label(t, &c, &n, id, x.level); got != x.want {
			t.Errorf("level %d: %q, want %q", x.level, got, x.want)
		}
	}
	if l, _ := n.Level(id, 2); l.Format != FmtDecimalZero {
		t.Error("levels it doesn't define come from the abstract list")
	}
}

func TestLevelsRestartOnlyAfterTheirRestartLevel(t *testing.T) {
	var n Numbering
	id := n.AddList(ListKind{Kind: ListLegal})
	// Level 3 restarts only after level 1; level 2 never restarts.
	n.Abstracts[0].Levels[2].RestartAfter = ptr[uint8](1)
	n.Abstracts[0].Levels[2].Text = "(%3)"
	n.Abstracts[0].Levels[1].Restart = false
	var c Counters
	for i, x := range []struct {
		level uint8
		want  string
	}{{0, "1."}, {1, "1.1."}, {2, "(1)"}, {1, "1.2."}, {2, "(2)"}, {0, "2."}, {1, "2.3."}, {2, "(1)"}} {
		if got := label(t, &c, &n, id, x.level); got != x.want {
			t.Errorf("%d: %q, want %q", i, got, x.want)
		}
	}
}

func TestNumberedLabels(t *testing.T) {
	var n Numbering
	id := n.AddList(ListKind{Kind: ListNumbered})
	var c Counters
	for i, x := range []struct {
		level uint8
		want  string
	}{{0, "1."}, {0, "2."}, {1, "a."}, {1, "b."}, {0, "3."}, {1, "a."}} {
		if got := label(t, &c, &n, id, x.level); got != x.want {
			t.Errorf("%d: %q, want %q", i, got, x.want)
		}
	}
	if _, _, ok := c.NextLabel(&n, 999, 0); ok {
		t.Error("no list 999")
	}
}

func TestEastAsianLabelsAndLegalNumbering(t *testing.T) {
	// A Chinese outline: 第一章, then "一、" items, and a legal level showing 1.1.
	var n Numbering
	id := n.AddList(ListKind{Kind: ListNumbered})
	levels := n.Abstracts[0].Levels
	levels[0] = lvl(FmtChineseCounting, "第%1章")
	levels[1] = lvl(FmtChineseCounting, "%2、")
	levels[2] = lvl(FmtChineseCounting, "%1.%2.%3")
	levels[2].Legal = true
	levels[3] = lvl(FmtChineseCounting, "（%4）")
	var c Counters
	l := func(level uint8) string { return label(t, &c, &n, id, level) }
	for i, x := range []struct {
		level uint8
		want  string
	}{{0, "第一章"}, {1, "一、"}, {1, "二、"}, {1, "三、"}, {2, "1.3.1"}, {3, "（一）"}} {
		if got := l(x.level); got != x.want {
			t.Errorf("%d: %q, want %q", i, got, x.want)
		}
	}
	for range 9 {
		l(3)
	}
	if got := l(3); got != "（十一）" {
		t.Error(got)
	}
	if got := l(0); got != "第二章" {
		t.Error(got)
	}
}

func TestLegalLabels(t *testing.T) {
	var n Numbering
	id := n.AddList(ListKind{Kind: ListLegal})
	var c Counters
	for i, x := range []struct {
		level uint8
		want  string
	}{{0, "1."}, {1, "1.1."}, {2, "1.1.1."}, {1, "1.2."}} {
		if got := label(t, &c, &n, id, x.level); got != x.want {
			t.Errorf("%d: %q, want %q", i, got, x.want)
		}
	}
}

func TestRestartAndBullets(t *testing.T) {
	var n Numbering
	id := n.AddList(ListKind{Kind: ListNumbered})
	var c Counters
	label(t, &c, &n, id, 0)
	label(t, &c, &n, id, 0)
	r, ok := n.Restart(id)
	if !ok || label(t, &c, &n, r, 0) != "1." {
		t.Error("restart")
	}
	b := n.AddList(ListKind{Kind: ListBullet})
	if label(t, &c, &n, b, 0) != "•" {
		t.Error("bullet")
	}
	if f, ok := n.FindKind(ListKind{Kind: ListBullet}); !ok || f != b {
		t.Error("find_kind")
	}
}

func TestRestartKeepsTheListsOwnLevelDefinitions(t *testing.T) {
	var n Numbering
	id := n.AddList(ListKind{Kind: ListNumbered})
	own := lvl(FmtUpperRoman, "Part %1")
	own.Indent, own.Hanging, own.Start = 90, 40, 5
	n.Nums[0].LevelOverrides = append(n.Nums[0].LevelOverrides, Pair[uint8, Level]{0, own})
	r, _ := n.Restart(id)
	num, _ := n.Num(r)
	if !equal(num.LevelOverrides, Vec[Pair[uint8, Level]]{{0, own}}) {
		t.Error("level overrides kept")
	}
	if l, _ := n.Level(r, 0); !equal(l, own) {
		t.Error("level")
	}
	var c Counters
	if got := label(t, &c, &n, r, 0); got != "Part V" {
		t.Error(got)
	}
}

func TestFreeID(t *testing.T) {
	for _, x := range []struct {
		used []uint32
		min  uint32
		want uint32
		ok   bool
	}{
		{nil, 1, 1, true}, {[]uint32{3, 1}, 1, 4, true}, {[]uint32{0}, 5, 5, true}, {[]uint32{^uint32(0)}, 0, 0, true},
		{[]uint32{^uint32(0), 0, 1, 3}, 0, 2, true}, {[]uint32{^uint32(0), 1}, 1, 2, true},
	} {
		if got, ok := FreeID(x.used, x.min); got != x.want || ok != x.ok {
			t.Errorf("FreeID(%v, %d) = %d %v", x.used, x.min, got, ok)
		}
	}
}
