package doc

import (
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// The tests of crates/doc/src/section.rs and cjk_numbers.rs.

func TestNumberFormats(t *testing.T) {
	for _, c := range []struct {
		f    NumFormat
		n    uint32
		want string
	}{
		{FmtUpperRoman, 1994, "MCMXCIV"}, {FmtLowerLetter, 27, "aa"}, {FmtLowerLetter, 0, ""}, {FmtOrdinal, 12, "12th"}, {FmtOrdinal, 22, "22nd"},
		{FmtCardinalText, 21, "Twenty-One"}, {FmtOrdinalText, 21, "Twenty-First"}, {FmtOrdinalText, 20, "Twentieth"},
		{FmtOrdinalText, 112, "One Hundred Twelfth"}, {FmtDecimalZero, 3, "03"}, {FmtChicago, 1, "*"}, {FmtChicago, 6, "††"},
		{FmtChineseCounting, 11, "十一"}, {FmtChineseCountingThousand, 101, "一百零一"}, {FmtDecimalEnclosedCircle, 3, "③"},
		{FmtHebrew1, 3, "3"}, {FmtRussianLower, 0, ""},
	} {
		if got := c.f.Format(c.n); got != c.want {
			t.Errorf("%s(%d) = %q, want %q", c.f.OOXML(), c.n, got, c.want)
		}
	}
	if FmtUpperRoman.Format(math.MaxUint32) == "" || len(FmtLowerLetter.Format(math.MaxUint32)) != 30 ||
		utf8.RuneCountInString(FmtRussianUpper.Format(math.MaxUint32)) != 30 {
		t.Error("formats of the largest number")
	}
	for _, f := range NumFormats {
		if NumFormatFromOOXML(f.OOXML()) != f {
			t.Errorf("%s doesn't round-trip", f.OOXML())
		}
		if f != FmtBullet && f != FmtNone && f.Format(math.MaxUint32) == "" {
			t.Errorf("%s of the largest number is empty", f.OOXML())
		}
	}
	if len(NumFormats) != 62 || NumFormatFromOOXML("klingon") != FmtDecimal {
		t.Error("unknown formats are decimal")
	}
}

func TestRussianLettersFollowWord(t *testing.T) {
	// Word's own labels for a russianLower list: no ё, й, ъ or ь, then doubled letters.
	var labels []string
	for n := uint32(1); n <= 31; n++ {
		labels = append(labels, FmtRussianLower.Format(n))
	}
	if got := strings.Join(labels, " "); got != "а б в г д е ж з и к л м н о п р с т у ф х ц ч ш щ ы э ю я аа бб" {
		t.Fatal(got)
	}
	for n, want := range map[uint32]string{59: "ааа", 100: "нннн"} {
		if got := FmtRussianLower.Format(n); got != want {
			t.Errorf("%d: %s", n, got)
		}
	}
	if FmtRussianUpper.Format(26) != "Ы" || FmtRussianUpper.Format(36) != "ЖЖ" {
		t.Error("upper")
	}
}

func TestLandscapeSwaps(t *testing.T) {
	s := NewSectionProps()
	s.MarginLeft = 50
	s.SetLandscape(true)
	if s.PageW != 792 || s.PageH != 612 || s.MarginTop != 50 {
		t.Fatalf("%+v", s)
	}
	s.SetLandscape(false)
	if s.MarginLeft != 50 || s.PageW != 612 || s.PageH != 792 {
		t.Fatalf("%+v", s)
	}
}

func TestGridPitchesAreClamped(t *testing.T) {
	s := NewSectionProps()
	if _, ok := s.GridLinePitch(); ok {
		t.Fatal("no grid")
	}
	s.DocGrid = &DocGrid{Kind: GridLines, LinePitch: 0, CharSpace: math.MinInt32}
	if p, _ := s.GridLinePitch(); p != MinGridPitch {
		t.Fatal(p)
	}
	if _, ok := s.GridCharPitch(10.5); ok {
		t.Fatal("no character grid")
	}
	s.DocGrid = &DocGrid{Kind: GridLinesAndChars, LinePitch: F32(math.NaN()), CharSpace: math.MinInt32}
	if p, _ := s.GridLinePitch(); p != 18 {
		t.Fatal(p)
	}
	if p, _ := s.GridCharPitch(10.5); p != MinGridPitch {
		t.Fatal(p)
	}
	// 648 pt of text height at an 18 pt pitch: 36 lines; 468 pt / 13 pt = 36 characters.
	s.DocGrid = &DocGrid{Kind: GridLinesAndChars, LinePitch: 18, CharSpace: CharSpaceFor(13, 10.5)}
	if s.GridLinesPerPage() != 36 || s.GridCharsPerLine(10.5) != 36 {
		t.Fatal(s.GridLinesPerPage(), s.GridCharsPerLine(10.5))
	}
}

func TestColumnsSplit(t *testing.T) {
	s := NewSectionProps()
	s.Columns = Columns{Count: 2, Space: 36}
	if b := s.ColumnBoxes(); !slices.Equal(b, []ColumnBox{{0, 216}, {252, 216}}) {
		t.Fatal(b)
	}
}

func TestRTLColumnsRunRightToLeft(t *testing.T) {
	s := NewSectionProps()
	s.RTL = true
	s.Columns = Columns{Count: 2, Space: 36}
	if b := s.ColumnBoxes(); !slices.Equal(b, []ColumnBox{{252, 216}, {0, 216}}) {
		t.Fatal(b)
	}
	// Text width 468: the first column hugs the right edge, the second sits 20pt to its left.
	s.Columns = Columns{Count: 2, Widths: Vec[[2]F32]{{100, 20}, {200, 0}}}
	if b := s.ColumnBoxes(); !slices.Equal(b, []ColumnBox{{368, 100}, {148, 200}}) {
		t.Fatal(b)
	}
}

func TestChineseCountingSystems(t *testing.T) {
	check := func(f func(uint32) string, cases map[uint32]string) {
		t.Helper()
		for n, want := range cases {
			if got := f(n); got != want {
				t.Errorf("%d: %s, want %s", n, got, want)
			}
		}
	}
	check(chineseCounting, map[uint32]string{1: "一", 2: "二", 3: "三", 10: "十", 11: "十一", 20: "二十", 21: "二十一", 99: "九十九", 100: "一〇〇",
		101: "一〇一", 1000: "一〇〇〇"})
	check(chineseCountingThousand, map[uint32]string{10: "十", 11: "十一", 20: "二十", 101: "一百零一", 110: "一百一十", 111: "一百一十一", 1000: "一千",
		1010: "一千零一十", 10_000: "一万", 10_001: "一万零一", 100_100: "十万零一百", 1_000_000: "一百万", 100_000_001: "一亿零一",
		math.MaxUint32: "四十二亿九千四百九十六万七千二百九十五"})
	check(chineseLegalSimplified, map[uint32]string{1: "壹", 3: "叁", 10: "壹拾", 11: "壹拾壹", 101: "壹佰零壹", 1000: "壹仟"})
	check(taiwaneseCountingThousand, map[uint32]string{10_001: "一萬零一"})
	check(ideographLegalTraditional, map[uint32]string{20: "貳拾"})
	check(ideographDigital, map[uint32]string{10: "一〇", 101: "一〇一"})
}

func TestJapaneseAndKorean(t *testing.T) {
	check := func(f func(uint32) string, cases map[uint32]string) {
		t.Helper()
		for n, want := range cases {
			if got := f(n); got != want {
				t.Errorf("%d: %s, want %s", n, got, want)
			}
		}
	}
	check(japaneseCounting, map[uint32]string{10: "十", 11: "十一", 20: "二十", 101: "百一", 1000: "千", 10_000: "一万", 10_001: "一万一"})
	check(japaneseLegal, map[uint32]string{10: "壱拾", 1000: "壱阡"})
	check(japaneseDigitalTenThousand, map[uint32]string{12: "一二", 10_000: "一万〇〇〇〇", 12_345: "一万二三四五", 100_000_000: "一億〇〇〇〇万〇〇〇〇"})
	check(koreanCounting, map[uint32]string{11: "십일", 20: "이십", 101: "백일"})
	check(koreanDigital, map[uint32]string{10: "일영"})
	check(koreanLegal, map[uint32]string{1: "하나", 11: "열하나", 20: "스물", 100: "100"})
}

func TestSequencesAndEnclosedNumbers(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{ideographTraditional(1), "甲"}, {ideographTraditional(10), "癸"}, {ideographTraditional(11), "11"},
		{ideographZodiac(1), "子"}, {ideographZodiac(12), "亥"}, {ideographZodiac(13), "13"},
		{ideographZodiacTraditional(1), "甲子"}, {ideographZodiacTraditional(11), "甲戌"}, {ideographZodiacTraditional(60), "癸亥"},
		{decimalEnclosedCircle(1), "①"}, {decimalEnclosedCircle(20), "⑳"}, {decimalEnclosedCircle(21), "21"},
		{decimalEnclosedCircleChinese(10), "⑩"}, {decimalEnclosedCircleChinese(11), "11"},
		{decimalEnclosedFullstop(1), "⒈"}, {decimalEnclosedParen(20), "⒇"}, {ideographEnclosedCircle(1), "㊀"},
		{decimalFullWidth(10), "１０"}, {aiueoFullWidth(1), "ア"}, {aiueoFullWidth(46), "ン"}, {aiueoFullWidth(47), "47"}, {aiueo(2), "ｲ"},
		{irohaFullWidth(1), "イ"}, {irohaFullWidth(47), "ス"}, {iroha(3), "ﾊ"}, {ganada(1), "가"}, {chosungOf(14), "ㅎ"}, {ganada(0), "0"},
	} {
		if c.got != c.want {
			t.Errorf("%s, want %s", c.got, c.want)
		}
	}
	if len(aiueoFull) != 46 || len(aiueoHalf) != 46 || len(irohaFull) != 47 || len(irohaHalf) != 47 {
		t.Error("sequence lengths")
	}
}
