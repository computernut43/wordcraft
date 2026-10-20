package geom

// Ported from crates/geom/src/{lib,pixels,region}.rs tests.

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

var nan = float32(math.NaN())

func TestUnitRoundTrips(t *testing.T) {
	for _, c := range []struct {
		got, want any
	}{
		{Twips(1440), float32(72)}, {ToTwips(72), int64(1440)}, {EMU(914_400), float32(72)}, {ToEMU(72), int64(914_400)},
		{HalfPoints(24), float32(12)}, {ToHalfPoints(10.5), int64(21)}, {ToTwips(nan), int64(0)},
	} {
		if c.got != c.want {
			t.Errorf("got %v, want %v", c.got, c.want)
		}
	}
	// Go-specific: conversions saturate like Rust's `as` (Go's own conversion is undefined there).
	if ToEMU(math.MaxFloat32) != math.MaxInt64 || ToEMU(-math.MaxFloat32) != math.MinInt64 {
		t.Error("ToEMU must saturate")
	}
	if ToHalfPoints(math.MaxFloat32) != math.MaxInt64 {
		t.Error("ToHalfPoints of an overflowing product must saturate")
	}
	if ToTwips(math.MaxFloat32) != 0 {
		t.Error("ToTwips: an overflowing product is not finite, so 0 (as Rust)")
	}
}

func parse(t *testing.T, s string, def Unit) (float32, bool) {
	t.Helper()
	return ParseMeasure(s, def)
}

func TestParsesMeasurements(t *testing.T) {
	exact := []struct {
		s    string
		def  Unit
		want float32
	}{
		{`1"`, Points, 72}, {"1 in", Points, 72}, {"12pt", Inches, 12}, {"2", Inches, 144}, {"1,5 li", Inches, 18},
		{"1,5", Inches, 108}, {`1.5"`, Centimeters, 108}, {"−1 in", Points, -72},
	}
	for _, c := range exact {
		if got, ok := parse(t, c.s, c.def); !ok || got != c.want {
			t.Errorf("ParseMeasure(%q) = %v, %v; want %v", c.s, got, ok, c.want)
		}
	}
	for _, s := range []string{"2.54 cm", "2,54 cm"} { // both decimal separators (#838)
		if got, ok := parse(t, s, Inches); !ok || abs32(got-72) >= 0.01 {
			t.Errorf("ParseMeasure(%q) = %v, %v", s, got, ok)
		}
	}
	none := []struct {
		s   string
		def Unit
	}{
		{"abc", Inches}, {"", Inches}, {"1 furlong", Inches}, {"1,2,3", Inches},
		// Overflowing values during unit conversion are refused (#1359).
		{"3.4e38 in", Points}, {"-3.4e38 in", Points}, {"3.4e38 cm", Points}, {"3.4e38 mm", Points}, {"3.4e38 pi", Points},
		{"3.4e38 li", Points}, {"3.4e38", Inches}, {"1e39 pt", Points}, {"inf", Points}, {"nan", Points},
		// Go-specific: digits that overflow float32 outright, and multiplications that overflow.
		{strings.Repeat("9", 50), Points}, {"300000000000000000000000000000000000000 in", Points},
	}
	for _, c := range none {
		if got, ok := parse(t, c.s, c.def); ok {
			t.Errorf("ParseMeasure(%q) = %v, want none", c.s, got)
		}
	}
}

func TestFormatsMeasurements(t *testing.T) {
	for _, c := range [][2]string{
		{Inches.Format(72), `1"`}, {Inches.Format(90), `1.25"`}, {Points.Format(12), "12 pt"}, {Centimeters.Format(72), "2.54 cm"},
		{Centimeters.FormatWith(72, ','), "2,54 cm"}, {Inches.FormatWith(108, ','), `1,5"`}, {Points.FormatWith(720, ','), "720 pt"},
		{Inches.FormatWith(nan, ','), `0"`},
	} {
		if c[0] != c[1] {
			t.Errorf("got %q, want %q", c[0], c[1])
		}
	}
}

func TestRectOps(t *testing.T) {
	a, b := NewRect(0, 0, 10, 10), NewRect(5, 5, 10, 10)
	if !a.Intersects(b) {
		t.Error("intersects")
	}
	if a.Union(b) != NewRect(0, 0, 15, 15) {
		t.Errorf("union %v", a.Union(b))
	}
	if !a.Contains(Point{5, 5}) {
		t.Error("contains")
	}
	if NewRect(0, 0, -5, nan).W != 0 || NewRect(0, 0, -5, nan).H != 0 {
		t.Error("negative and NaN sizes are 0")
	}
	if a.Inset(1, 2, 3, 4) != NewRect(1, 2, 6, 4) {
		t.Errorf("inset %v", a.Inset(1, 2, 3, 4))
	}
	// Go-specific: min/max ignore NaN as Rust's f32::min/max do.
	if fmin(nan, 1) != 1 || fmin(1, nan) != 1 || fmax(nan, 1) != 1 || fmax(1, nan) != 1 {
		t.Error("fmin/fmax must ignore NaN")
	}
}

func TestPaperNames(t *testing.T) {
	if n, ok := PaperName(612, 792); !ok || n != "Letter" {
		t.Error("Letter")
	}
	if n, ok := PaperName(792, 612); !ok || n != "Letter" {
		t.Error("Letter landscape")
	}
	if _, ok := PaperName(100, 100); ok {
		t.Error("100x100")
	}
	if p, ok := PaperByName(" a4 "); !ok || p.Name != "A4" {
		t.Error("a4")
	}
	if p, ok := PaperByName("Legal"); !ok || p.W != 612 || p.H != 1008 {
		t.Error("Legal")
	}
	if _, ok := PaperByName("Quarto"); ok {
		t.Error("Quarto")
	}
	// ASCII-only case folding: the Kelvin sign isn't a K.
	if _, ok := PaperByName("Ketter"); ok {
		t.Error("non-ASCII folding")
	}
}

// Scale to paper size (#2108): one factor, centred, paper turned to the page's orientation.
func TestPagesFitOtherPaper(t *testing.T) {
	close := func(a, b float32) bool { return abs32(a-b) < 0.01 }
	f := FitToPaper(612, 792, 595.28, 841.89)
	if !(close(f.PaperW, 595.28) && close(f.PaperH, 841.89) && close(f.K, 595.28/612) && close(f.DX, 0) && close(f.DY, (841.89-792*f.K)/2) && f.DY > 0) {
		t.Errorf("Letter on A4: %+v", f)
	}
	if !close(FitToPaper(595.28, 841.89, 612, 1008).K, 612/595.28) {
		t.Error("A4 on Legal")
	}
	if !close(FitToPaper(419.53, 595.28, 595.28, 841.89).K, 595.28/419.53) {
		t.Error("A5 on A4")
	}
	l := FitToPaper(792, 612, 595.28, 841.89)
	if !(close(l.PaperW, 841.89) && close(l.PaperH, 595.28) && close(l.K, 595.28/612) && l.DX > 0 && close(l.DY, 0)) {
		t.Errorf("landscape: %+v", l)
	}
	same := FitToPaper(612, 792, 612, 792)
	if !(close(same.K, 1) && close(same.DX, 0) && close(same.DY, 0)) {
		t.Errorf("same: %+v", same)
	}
	inf := float32(math.Inf(1))
	for _, c := range [][4]float32{{nan, 792, 595, 842}, {612, 0, 595, 842}, {612, 792, inf, 842}, {612, 792, -5, 842}} {
		f := FitToPaper(c[0], c[1], c[2], c[3])
		if f.K != 1 || f.DX != 0 || f.DY != 0 {
			t.Errorf("hostile %v: %+v", c, f)
		}
	}
	if k := FitToPaper(1, 1, math.MaxFloat32, math.MaxFloat32).K; Finite(k) != k {
		t.Error("huge paper must give a finite scale")
	}
}

func TestRGBABudgetBoundary(t *testing.T) {
	if b, ok := RGBABytes(16, 8); !ok || b != 512 {
		t.Error("16x8")
	}
	if !RGBAFits(16, 8, 512) || RGBAFits(16, 8, 511) || RGBAFits(0, 8, 512) {
		t.Error("RGBAFits")
	}
	if !DecodeFits(16, 8, 1024) || DecodeFits(16, 8, 1023) || DecodeFits(0, 8, math.MaxUint64) {
		t.Error("DecodeFits")
	}
	// (2³²−1)² fits in 64 bits, but 4·(2³²−1)² overflows: refused, as Rust's checked_mul.
	if _, ok := RGBABytes(math.MaxUint32, math.MaxUint32); ok {
		t.Error("4·(2³²−1)² overflows 64 bits")
	}
	if b, ok := RGBABytes(65535, 65535); !ok || b != 4*65535*65535 {
		t.Error("65535²")
	}
}

func TestFitPixelsKeepsNormalSizesAndShrinksHugeOnes(t *testing.T) {
	if w, h := FitPixels(612, 792, MaxRasterPixels); w != 612 || h != 792 {
		t.Error("normal size")
	}
	if w, h := FitPixels(16_000, 16_000, 1_000_000); uint64(w)*uint64(h) > 1_000_000 || w < 900 {
		t.Errorf("%dx%d", w, h)
	}
	if w, h := FitPixels(16_000, 1, 100); uint64(w)*uint64(h) > 100 || h != 1 {
		t.Errorf("%dx%d", w, h)
	}
	if w, _ := FitPixels(0, 5, 10); w != 1 {
		t.Error("zero width")
	}
}

func TestReadsLocaleTags(t *testing.T) {
	if r := RegionFromTag("pl-PL"); r.Language != "pl" || r.Territory != "PL" || r.Letter != nil || r.Inches != nil {
		t.Errorf("pl-PL: %+v", r)
	}
	for tag, want := range map[string]string{"de_AT.UTF-8@euro": "AT", "zh-Hans-CN": "CN", "es-419": "419", "en-u-ms-metric": "US", "fr": ""} {
		if got := RegionFromTag(tag).Territory; got != want {
			t.Errorf("%q: territory %q, want %q", tag, got, want)
		}
	}
	for _, junk := range []string{"", "C", "POSIX", "1234", "x", "--", "ünïcode-ÄÄ", strings.Repeat("a", 10_000), "poſix"} {
		r := RegionFromTag(junk)
		if junk == "poſix" {
			// Not POSIX under ASCII folding, and not a language (5 letters): still en-US.
			r = RegionFromTag(junk)
		}
		if r != DefaultRegion() {
			t.Errorf("%q: %+v, want en-US", junk, r)
		}
	}
	if RegionFromTag("en-US").WithTerritory("de").Territory != "DE" || RegionFromTag("en-US").WithTerritory("Germany").Territory != "US" {
		t.Error("WithTerritory")
	}
}

func str(s string) *string { return &s }

// Paper and units follow the territory, not the language (#838).
func TestPaperAndUnitsFollowTheTerritory(t *testing.T) {
	check := func(r Region, paper string, unit Unit, dec rune) {
		t.Helper()
		if p, _, _ := r.Paper(); p != paper || r.Units() != unit || r.Decimal() != dec {
			t.Errorf("%+v: %s %v %c; want %s %v %c", r, p, r.Units(), r.Decimal(), paper, unit, dec)
		}
	}
	check(RegionFromTag("pl-PL"), "A4", Centimeters, ',')
	check(RegionFromTag("en-US"), "Letter", Inches, '.')
	check(RegionFromTag("fr-CA"), "Letter", Centimeters, ',')
	check(RegionFromTag("en-DE"), "A4", Centimeters, ',')
	check(RegionFromTag("en-GB"), "A4", Centimeters, '.')
	if RegionFromTag("de-CH").Decimal() != '.' {
		t.Error("de-CH")
	}
	if p, _, _ := RegionFromTag("es-MX").Paper(); p != "Letter" {
		t.Error("es-MX")
	}
	if p, _, _ := RegionFromTag("ja-JP").Paper(); p != "A4" {
		t.Error("ja-JP")
	}
	if got := RegionFromTag("pl-PL").Format(Centimeters, 72); got != "2,54 cm" {
		t.Error(got)
	}
	// Unix sets paper and measurement apart from the number locale.
	check(RegionFromTag("pl_PL.UTF-8").WithPaperAndMeasurement(str("en_US.UTF-8"), nil), "Letter", Centimeters, ',')
	check(RegionFromTag("en_US.UTF-8").WithPaperAndMeasurement(str("de_DE.UTF-8"), str("de_DE.UTF-8")), "A4", Centimeters, '.')
	if p, _, _ := RegionFromTag("pl-PL").WithPaperAndMeasurement(str(""), str("junk")).Paper(); p != "Letter" {
		t.Error("junk is en-US")
	}
	if got := RegionFromTag("en-US").Format(Inches, 90); got != `1.25"` {
		t.Error(got)
	}
}

// #1381: the CLI and the app read the system region the same way.
func TestRegionFromLocaleVariables(t *testing.T) {
	env := func(pairs ...string) func(string) (string, bool) {
		return func(n string) (string, bool) {
			for i := 0; i+1 < len(pairs); i += 2 {
				if pairs[i] == n {
					return pairs[i+1], true
				}
			}
			return "", false
		}
	}
	none := func() (string, bool) { return "", false }
	paper := func(r Region) string { p, _, _ := r.Paper(); return p }
	if paper(RegionFromLocaleVars(env("LANG", "it_IT.UTF-8"), none)) != "A4" ||
		paper(RegionFromLocaleVars(env("LANG", "en_US.UTF-8"), none)) != "Letter" ||
		paper(RegionFromLocaleVars(env("LANG", "it_IT.UTF-8", "LC_ALL", "en_US"), none)) != "Letter" ||
		RegionFromLocaleVars(env("LANG", "en_US", "WORDCRAFT_REGION", "de-DE"), none).Territory != "DE" ||
		paper(RegionFromLocaleVars(env("LANG", "en_US", "LC_PAPER", "it_IT"), none)) != "A4" ||
		RegionFromLocaleVars(env("LANG", "C.UTF-8"), func() (string, bool) { return "it-IT", true }).Territory != "IT" ||
		RegionFromLocaleVars(env(), none) != DefaultRegion() {
		t.Error("locale variables")
	}
}

// The JSON shapes match Rust's serde output.
func TestJSONMatchesRust(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{Point{1, 2.5}, `{"x":1,"y":2.5}`},
		{NewRect(1, 2, 3, 4), `{"x":1,"y":2,"w":3,"h":4}`},
		{Centimeters, `"centimeters"`},
		{RegionFromTag("pl-PL"), `{"language":"pl","territory":"PL"}`},
		{RegionFromTag("pl").WithPaperAndMeasurement(str("en-US"), nil), `{"language":"pl","territory":"","letter":true}`},
	} {
		b, err := json.Marshal(c.v)
		if err != nil || string(b) != c.want {
			t.Errorf("%#v: %s, %v; want %s", c.v, b, err, c.want)
		}
	}
	var u Unit
	if err := json.Unmarshal([]byte(`"picas"`), &u); err != nil || u != Picas {
		t.Error("unit round trip")
	}
	if err := json.Unmarshal([]byte(`"furlongs"`), &u); err == nil {
		t.Error("unknown unit must fail")
	}
}

// FuzzParseMeasure: nothing panics, and a result is always finite.
func FuzzParseMeasure(f *testing.F) {
	for _, s := range []string{`1"`, "2,54 cm", "−1 in", "1e39 pt", strings.Repeat("9", 60), "--", ".", "+.5px"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, u := range []Unit{Inches, Centimeters, Millimeters, Points, Picas} {
			if v, ok := ParseMeasure(s, u); ok && Finite(v) != v {
				t.Fatalf("ParseMeasure(%q) = %v", s, v)
			}
		}
		_ = RegionFromTag(s)
	})
}
