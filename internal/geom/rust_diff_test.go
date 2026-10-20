package geom

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMatchesRust compares this package with crates/geom bit for bit on a generated corpus. It
// runs when WORDCRAFT_GEOM_RUST names the reference program built from testdata/rust-oracle
// (`cargo build --release` there). Results that go through sine and cosine (Spin) may differ by
// one unit in the last place in their sine and cosine (Rust calls the platform's sinf/cosf, Go has
// only float64 trigonometry); everything computed from those is then checked exactly.
func TestMatchesRust(t *testing.T) {
	bin := os.Getenv("WORDCRAFT_GEOM_RUST")
	if bin == "" {
		t.Skip("set WORDCRAFT_GEOM_RUST to testdata/rust-oracle's release binary to compare with Rust")
	}
	if !filepath.IsAbs(bin) {
		t.Fatalf("WORDCRAFT_GEOM_RUST must be absolute: %q", bin)
	}
	cases := diffCorpus()
	var in bytes.Buffer
	for _, c := range cases {
		in.WriteString(c + "\n")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin, cmd.Env = &in, []string{}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	exact, bad := 0, 0
	for i := 0; sc.Scan(); i++ {
		if i >= len(cases) {
			t.Fatal("reference printed more lines than inputs")
		}
		got, want := goAnswer(cases[i]), sc.Text()
		switch {
		case sameAnswer(got, want):
			exact++
		default:
			bad++
			if bad <= 20 {
				t.Errorf("%s\n  go   %s\n  rust %s", cases[i], got, want)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d cases differ from Rust", bad, len(cases))
	}
	t.Logf("%d cases: %d identical", len(cases), exact)
}

// sameAnswer compares token by token, treating any two NaN bit patterns as equal: which NaN
// payload an operation on two NaNs returns depends on operand order in the instruction, which
// IEEE 754 leaves open and the Go compiler may swap for commutative operations. A NaN must still
// be a NaN where Rust's is.
func sameAnswer(got, want string) bool {
	g, w := strings.Fields(got), strings.Fields(want)
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if g[i] != w[i] && !(isNaNBits(g[i]) && isNaNBits(w[i])) {
			return false
		}
	}
	return true
}

func isNaNBits(tok string) bool {
	if len(tok) != 8 {
		return false
	}
	u, err := strconv.ParseUint(tok, 16, 32)
	return err == nil && u&0x7f80_0000 == 0x7f80_0000 && u&0x007f_ffff != 0
}

func fbits(v float32) string { return fmt.Sprintf("%08x", math.Float32bits(v)) }

func fromBits(s string) float32 {
	u, _ := strconv.ParseUint(s, 16, 32)
	return math.Float32frombits(uint32(u))
}

func unhex(s string) string {
	b, _ := hex.DecodeString(s)
	return string(b)
}

func hexs(s string) string { return hex.EncodeToString([]byte(s)) }

func optBool(p *bool) string {
	if p == nil {
		return "None"
	}
	return fmt.Sprintf("Some(%v)", *p)
}

var unitDebug = map[Unit]string{Inches: "Inches", Centimeters: "Centimeters", Millimeters: "Millimeters", Points: "Points", Picas: "Picas"}

// goAnswer computes one corpus line the way the Rust reference prints it.
func goAnswer(line string) string {
	a := strings.Split(line, " ")
	g := func(i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	fl := func(i int) float32 { return fromBits(g(i)) }
	unit := func(i int) Unit { n, _ := strconv.Atoi(g(i)); return Unit(n % 5) }
	i64 := func(i int) int64 { n, _ := strconv.ParseInt(g(i), 10, 64); return n }
	flag := func(i int) bool { return g(i) == "1" }
	switch g(0) {
	case "twips":
		return fbits(Twips(i64(1)))
	case "to_twips":
		return strconv.FormatInt(ToTwips(fl(1)), 10)
	case "emu":
		return fbits(EMU(i64(1)))
	case "to_emu":
		return strconv.FormatInt(ToEMU(fl(1)), 10)
	case "half":
		return fbits(HalfPoints(i64(1)))
	case "to_half":
		return strconv.FormatInt(ToHalfPoints(fl(1)), 10)
	case "number":
		dec := []rune(unhex(g(3)))
		return hexs(unit(1).Number(fl(2), dec[0]))
	case "parse":
		if v, ok := ParseMeasure(unhex(g(2)), unit(1)); ok {
			return fbits(v)
		}
		return "none"
	case "norm":
		return fbits(NormalizeDegrees(fl(1)))
	case "spin":
		sp := NewSpin(fl(1), flag(2), flag(3))
		cx, cy, x, y := fl(4), fl(5), fl(6), fl(7)
		r := NewRect(fl(8), fl(9), fl(10), fl(11))
		var v []string
		for _, m := range sp.Matrix(cx, cy) {
			v = append(v, fbits(m))
		}
		ax, ay := sp.Apply(cx, cy, x, y)
		ux, uy := sp.Unapply(cx, cy, x, y)
		bo := sp.Bounds(r)
		ew, eh := sp.Extent(r.W, r.H)
		v = append(v, fbits(ax), fbits(ay), fbits(ux), fbits(uy), fbits(bo.X), fbits(bo.Y), fbits(bo.W), fbits(bo.H), fbits(ew), fbits(eh), fbits(sp.Deg))
		v = append(v, strconv.FormatBool(sp.IsIdentity()))
		return strings.Join(v, " ")
	case "within":
		w := NewSpin(fl(1), flag(2), flag(3)).Within(NewSpin(fl(4), flag(5), flag(6)))
		return fmt.Sprintf("%s %v %v", fbits(w.Deg), w.FlipH, w.FlipV)
	case "rect":
		p, q := NewRect(fl(1), fl(2), fl(3), fl(4)), NewRect(fl(5), fl(6), fl(7), fl(8))
		r4 := func(r Rect) string { return fbits(r.X) + " " + fbits(r.Y) + " " + fbits(r.W) + " " + fbits(r.H) }
		return fmt.Sprintf("%s %s %v %v %s %s %s", r4(p), r4(p.Union(q)), p.Intersects(q), p.Contains(Point{fl(9), fl(10)}),
			r4(p.Inset(fl(11), fl(12), fl(13), fl(14))), r4(p.Expand(fl(15))), fbits(p.Right()))
	case "paper_name":
		if n, ok := PaperName(fl(1), fl(2)); ok {
			return hexs(n)
		}
		return "none"
	case "paper_by":
		if p, ok := PaperByName(unhex(g(1))); ok {
			return hexs(p.Name) + " " + fbits(p.W) + " " + fbits(p.H)
		}
		return "none"
	case "fit":
		p := FitToPaper(fl(1), fl(2), fl(3), fl(4))
		return strings.Join([]string{fbits(p.PaperW), fbits(p.PaperH), fbits(p.K), fbits(p.DX), fbits(p.DY)}, " ")
	case "pix":
		w64, _ := strconv.ParseUint(g(1), 10, 32)
		h64, _ := strconv.ParseUint(g(2), 10, 32)
		budget, _ := strconv.ParseUint(g(3), 10, 64)
		mx, _ := strconv.ParseUint(g(4), 10, 64)
		w, h := uint32(w64), uint32(h64)
		fw, fh := FitPixels(w, h, mx)
		b := "none"
		if n, ok := RGBABytes(w, h); ok {
			b = strconv.FormatUint(n, 10)
		}
		return fmt.Sprintf("%s %v %v %d %d", b, RGBAFits(w, h, budget), DecodeFits(w, h, budget), fw, fh)
	case "region":
		r := RegionFromTag(unhex(g(1)))
		if g(2) != "-" {
			r = r.WithTerritory(unhex(g(2)))
		}
		opt := func(i int) *string {
			if g(i) == "-" {
				return nil
			}
			s := unhex(g(i))
			return &s
		}
		r = r.WithPaperAndMeasurement(opt(3), opt(4))
		p, _, _ := r.Paper()
		return fmt.Sprintf("%s %s %s %s %s %s %c", hexs(r.Language), hexs(r.Territory), optBool(r.Letter), optBool(r.Inches), p, unitDebug[r.Units()], r.Decimal())
	}
	return "?"
}

// diffCorpus builds the deterministic inputs: special and typical values, then random ones.
func diffCorpus() []string {
	rng := rand.New(rand.NewPCG(3, 4))
	special := []float32{0, float32(math.Copysign(0, -1)), 1, -1, 0.5, 0.25, 0.005, 0.015, 0.125, 72, 90, 45, 30, 180, 270, 360, 359.99997, 720, -90, -0.00001, -1e-30,
		1e-30, 1e30, 3.4e38, -3.4e38, math.MaxFloat32, math.SmallestNonzeroFloat32, float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN()),
		612, 792, 595.28, 841.89, 10.5, 12, 1440, 2.54, 28.346457, 1e7, 16777217, 8388607.5}
	randf := func() float32 {
		switch rng.IntN(4) {
		case 0:
			return special[rng.IntN(len(special))]
		case 1:
			return math.Float32frombits(rng.Uint32())
		case 2:
			return float32(rng.NormFloat64() * 500)
		}
		return float32(rng.IntN(20000)-10000) / 4
	}
	f := func() string { return fbits(randf()) }
	flag := func() string { return strconv.Itoa(rng.IntN(2)) }
	var out []string
	for _, v := range special {
		out = append(out, "to_twips "+fbits(v), "to_emu "+fbits(v), "to_half "+fbits(v), "norm "+fbits(v))
		for u := 0; u < 5; u++ {
			out = append(out, fmt.Sprintf("number %d %s %s", u, fbits(v), hexs(".")), fmt.Sprintf("number %d %s %s", u, fbits(v), hexs(",")))
		}
	}
	for _, n := range []int64{0, 1, -1, 1440, 914400, 24, math.MaxInt64, math.MinInt64, 1 << 24, 1<<24 + 1, 1<<53 + 1} {
		out = append(out, fmt.Sprintf("twips %d", n), fmt.Sprintf("emu %d", n), fmt.Sprintf("half %d", n))
	}
	measureTokens := []string{"1", "2", "0", "9", ".", ",", "-", "+", "−", " ", "in", "inch", "cm", "mm", "pt", "pts", "pi", "pc", "li", "lines", "px",
		`"`, "IN", "Cm", "e5", "3.4", "38", "999999999999999999999999999999999999999", "furlong", "x", "\t", " ", "é", "1e39"}
	for range 4000 {
		var b strings.Builder
		for k := 1 + rng.IntN(5); k > 0; k-- {
			b.WriteString(measureTokens[rng.IntN(len(measureTokens))])
		}
		out = append(out, fmt.Sprintf("parse %d %s", rng.IntN(5), hexs(b.String())))
	}
	for range 6000 {
		out = append(out,
			"to_twips "+f(), "to_emu "+f(), "to_half "+f(), "norm "+f(),
			fmt.Sprintf("number %d %s %s", rng.IntN(5), f(), hexs([]string{".", ","}[rng.IntN(2)])),
			fmt.Sprintf("twips %d", rng.Int64()>>rng.IntN(63)), fmt.Sprintf("emu %d", rng.Int64()>>rng.IntN(63)),
			strings.Join([]string{"spin", f(), flag(), flag(), f(), f(), f(), f(), f(), f(), f(), f()}, " "),
			strings.Join([]string{"within", f(), flag(), flag(), f(), flag(), flag()}, " "),
			strings.Join([]string{"rect", f(), f(), f(), f(), f(), f(), f(), f(), f(), f(), f(), f(), f(), f(), f()}, " "),
			"paper_name "+f()+" "+f(), "fit "+f()+" "+f()+" "+f()+" "+f(),
		)
	}
	papers := []string{"Letter", " a4 ", "LEGAL", "Quarto", "", "a3", "B4 (JIS)", "envelope dl", "Ketter", "Letterx"}
	for _, p := range PaperSizes {
		out = append(out, "paper_name "+fbits(p.W)+" "+fbits(p.H), "paper_name "+fbits(p.H+0.49)+" "+fbits(p.W-0.49), "fit "+fbits(612)+" "+fbits(792)+" "+fbits(p.W)+" "+fbits(p.H))
	}
	for _, p := range papers {
		out = append(out, "paper_by "+hexs(p))
	}
	pix := []uint64{0, 1, 2, 16, 612, 792, 16000, 65535, 65536, 1 << 31, math.MaxUint32}
	budgets := []uint64{0, 511, 512, 1023, 1024, MaxDecodedBytes, MaxRasterPixels, math.MaxUint64, 100}
	for range 3000 {
		out = append(out, fmt.Sprintf("pix %d %d %d %d", pix[rng.IntN(len(pix))], pix[rng.IntN(len(pix))], budgets[rng.IntN(len(budgets))], budgets[rng.IntN(len(budgets))]))
	}
	tags := []string{"pl-PL", "de_AT.UTF-8@euro", "zh-Hans-CN", "es-419", "en-u-ms-metric", "fr", "", "C", "POSIX", "posix", "poſix", "1234", "x", "--",
		"ünïcode-ÄÄ", strings.Repeat("a", 100), "en-US", "fr-CA", "en-DE", "en-GB", "de-CH", "es-MX", "ja-JP", "pl_PL.UTF-8", "en_US.UTF-8", "C.UTF-8",
		"de-DE", "it-IT", "en", "EN-us", "sr-Latn-RS", "en--US", "en-_GB", "de-LI", "es-US", "en-ZA", "pt-BR", "  en-GB  ", "en-GB "}
	terr := []string{"-", hexs("de"), hexs("Germany"), hexs(" 419 "), hexs("ch"), hexs("")}
	pick := func() string { return tags[rng.IntN(len(tags))] }
	for _, tag := range tags {
		out = append(out, "region "+hexs(tag)+" - - -")
	}
	for range 3000 {
		opt := func() string {
			if rng.IntN(2) == 0 {
				return "-"
			}
			return hexs(pick())
		}
		out = append(out, "region "+hexs(pick())+" "+terr[rng.IntN(len(terr))]+" "+opt()+" "+opt())
	}
	return out
}
