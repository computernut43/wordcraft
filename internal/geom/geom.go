// Package geom holds WordCraft's geometry and units: the Go port of crates/geom.
//
// Layout works in points (1/72 inch, float32). File formats use other units: OOXML uses
// twentieths of a point (twips) for most lengths, half-points for font sizes, eighths of a point
// for borders and English Metric Units (EMU, 914 400 per inch) for drawings. The conversions live
// here so every package agrees on them.
//
// Arithmetic is float32, as in Rust, with each operation rounded on its own (products are
// converted explicitly so the compiler can't fuse them into FMA instructions, which Rust doesn't
// do). Float-to-integer conversions saturate and turn NaN into 0, like Rust's `as`. Trigonometry
// (Spin) goes through float64 and can differ from Rust's platform sinf/cosf in the last bit.
package geom

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"
)

// Unit conversions.
const (
	PtPerIn    float32 = 72        // points per inch
	TwipsPerPt float32 = 20        // twips (1/20 pt) per point
	EMUPerPt   float32 = 12_700    // English Metric Units per point
	PtPerCm    float32 = 72 / 2.54 // points per centimetre
)

// Twips converts twips to points.
func Twips(v int64) float32 { return float32(v) / TwipsPerPt }

// ToTwips converts points to twips (rounded).
func ToTwips(pt float32) int64 { return roundToInt64(Finite(float32(pt * TwipsPerPt))) }

// EMU converts English Metric Units to points.
func EMU(v int64) float32 { return float32(v) / EMUPerPt }

// ToEMU converts points to EMU (rounded, in float64 as in Rust).
func ToEMU(pt float32) int64 { return satInt64(math.Round(float64(Finite(pt)) * float64(EMUPerPt))) }

// HalfPoints converts half-points (OOXML font sizes) to points.
func HalfPoints(v int64) float32 { return float32(v) / 2 }

// ToHalfPoints converts points to half-points (rounded).
func ToHalfPoints(pt float32) int64 { return roundToInt64(float32(Finite(pt) * 2)) }

// Finite replaces NaN and infinities with 0 so conversions and arithmetic stay defined.
func Finite(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return 0
	}
	return v
}

func roundToInt64(v float32) int64 { return satInt64(math.Round(float64(v))) }

// satInt64 converts like Rust's `as i64`: saturating, NaN to 0 (Go's conversion of an
// out-of-range float is implementation-defined).
func satInt64(v float64) int64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= 9.223372036854775807e18:
		return math.MaxInt64
	case v <= -9.223372036854775808e18:
		return math.MinInt64
	}
	return int64(v)
}

// fmin and fmax are Rust's f32::min and f32::max: a NaN argument yields the other one (Go's
// built-in min and max return NaN), and operands that compare equal (+0 and -0) yield the second,
// as Rust does on x86-64. (On ARM Rust orders -0 below +0; the sign of such a zero is the only
// difference.)
func fmin(a, b float32) float32 {
	if a != a || b <= a {
		return b
	}
	return a
}

func fmax(a, b float32) float32 {
	if a != a || b >= a {
		return b
	}
	return a
}

func abs32(v float32) float32 { return math.Float32frombits(math.Float32bits(v) &^ (1 << 31)) }

// Unit is a measurement unit shown in the UI (Word's Options › Advanced › "Show measurements in
// units of"). It is "inches", "centimeters", … in JSON.
type Unit int

// Units.
const (
	Inches Unit = iota
	Centimeters
	Millimeters
	Points
	Picas
)

var unitNames = [...]string{"inches", "centimeters", "millimeters", "points", "picas"}

func (u Unit) String() string {
	if int(u) >= 0 && int(u) < len(unitNames) {
		return unitNames[u]
	}
	return "inches"
}

// MarshalJSON writes the unit's lower-case name.
func (u Unit) MarshalJSON() ([]byte, error) { return json.Marshal(u.String()) }

// UnmarshalJSON reads a lower-case unit name.
func (u *Unit) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for i, n := range unitNames {
		if n == s {
			*u = Unit(i)
			return nil
		}
	}
	return fmt.Errorf("geom: unknown unit %q", s)
}

// Suffix is how the unit is written after a number (`"`, ` cm`, …).
func (u Unit) Suffix() string {
	switch u {
	case Centimeters:
		return " cm"
	case Millimeters:
		return " mm"
	case Points:
		return " pt"
	case Picas:
		return " pi"
	}
	return `"`
}

// PtPerUnit is the number of points in one of the unit.
func (u Unit) PtPerUnit() float32 {
	switch u {
	case Centimeters:
		return PtPerCm
	case Millimeters:
		return PtPerCm / 10
	case Points:
		return 1
	case Picas:
		return 12
	}
	return PtPerIn
}

// Format writes pt in this unit the way Word's fields show it (`1"`, `2.54 cm`, `12 pt`).
func (u Unit) Format(pt float32) string { return u.FormatWith(pt, '.') }

// FormatWith is Format with the region's decimal separator (`2,54 cm` with ',').
func (u Unit) FormatWith(pt float32, decimal rune) string { return u.Number(pt, decimal) + u.Suffix() }

// Number is pt as a number in this unit, at most two decimals, without the unit (`2,54`).
func (u Unit) Number(pt float32, decimal rune) string {
	v := Finite(pt) / u.PtPerUnit()
	s := strconv.FormatFloat(float64(v), 'f', 2, 32)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" {
		s = "0"
	}
	return strings.ReplaceAll(s, ".", string(decimal))
}

// ParseMeasure reads a measurement typed by a user: `1"`, `1 in`, `2.5cm`, `12pt`, `3 pi`,
// `10 mm`, `1.5 li` (lines are 12 pt), `16 px`. A bare number is in def. It returns points, and
// false for anything unreadable or out of range. Either decimal separator works (`2,5 cm`).
func ParseMeasure(s string, def Unit) (float32, bool) {
	t := strings.ReplaceAll(lowerASCII(strings.TrimSpace(s)), "−", "-")
	if t == "" {
		return 0, false
	}
	split := strings.IndexFunc(t, func(c rune) bool {
		return !(('0' <= c && c <= '9') || c == '.' || c == '-' || c == '+' || c == ',')
	})
	if split < 0 {
		split = len(t)
	}
	num, unit := t[:split], t[split:]
	f, err := strconv.ParseFloat(strings.ReplaceAll(num, ",", "."), 32)
	if err != nil {
		return 0, false
	}
	v := float32(f)
	if Finite(v) != v || v != v {
		return 0, false
	}
	var k float32
	switch strings.TrimSpace(unit) {
	case "":
		k = def.PtPerUnit()
	case `"`, "in", "inch", "inches":
		k = PtPerIn
	case "cm":
		k = PtPerCm
	case "mm":
		k = PtPerCm / 10
	case "pt", "pts", "point", "points":
		k = 1
	case "pi", "pc", "pica", "picas":
		k = 12
	case "li", "line", "lines":
		k = 12
	case "px":
		k = 0.75
	default:
		return 0, false
	}
	res := float32(v * k)
	if Finite(res) != res {
		return 0, false
	}
	return res, true
}

// eqFoldASCII compares ignoring ASCII case only, as Rust's eq_ignore_ascii_case (strings.EqualFold
// also folds non-ASCII letters: the long s "ſ" equals "s" there).
func eqFoldASCII(a, b string) bool { return len(a) == len(b) && lowerASCII(a) == lowerASCII(b) }

// lowerASCII lower-cases ASCII letters only, as Rust's to_ascii_lowercase.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Point is a point in page space (points, y down).
type Point struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// Rect is an axis-aligned rectangle (points, y down). W and H are never negative when built
// with NewRect.
type Rect struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

// NewRect builds a rectangle, replacing non-finite values with 0 and negative sizes with 0.
func NewRect(x, y, w, h float32) Rect {
	return Rect{X: Finite(x), Y: Finite(y), W: fmax(Finite(w), 0), H: fmax(Finite(h), 0)}
}

// Right is x + w.
func (r Rect) Right() float32 { return r.X + r.W }

// Bottom is y + h.
func (r Rect) Bottom() float32 { return r.Y + r.H }

// Contains reports whether p is inside r or on its edge.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.Right() && p.Y >= r.Y && p.Y <= r.Bottom()
}

// Intersects reports whether r and o overlap (touching edges don't).
func (r Rect) Intersects(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// Union is the smallest rectangle holding both.
func (r Rect) Union(o Rect) Rect {
	x, y := fmin(r.X, o.X), fmin(r.Y, o.Y)
	return NewRect(x, y, fmax(r.Right(), o.Right())-x, fmax(r.Bottom(), o.Bottom())-y)
}

// Inset moves each side inwards by its amount.
func (r Rect) Inset(l, t, rt, b float32) Rect {
	return NewRect(r.X+l, r.Y+t, r.W-l-rt, r.H-t-b)
}

// Expand grows r by d on every side (shrinks for negative d, down to zero size).
func (r Rect) Expand(d float32) Rect { return r.Inset(-d, -d, -d, -d) }

// NormalizeDegrees brings an angle in degrees into [0, 360) (NaN and infinities become 0).
func NormalizeDegrees(deg float32) float32 {
	if Finite(deg) != deg {
		return 0
	}
	d := float32(math.Mod(float64(deg), 360)) // exact, like Rust's %
	if d < 0 {
		d += 360
	}
	// Adding 360 can round up to exactly 360 for tiny negative angles.
	if d >= 0 && d < 360 {
		return d
	}
	return 0
}

// radPerDeg is Rust's f32 to_radians factor: PI (as f32) / 180, rounded once to f32.
const radPerDeg = float32(math.Pi) / 180

// Spin says how a drawing is turned about its centre: mirrored first (FlipH left to right, FlipV
// top to bottom), then rotated Deg degrees clockwise (page space, y down), as DrawingML's a:xfrm
// flipH, flipV and rot say (ECMA-376 §20.1.7.6).
type Spin struct {
	Deg          float32
	FlipH, FlipV bool
}

// NewSpin builds a spin with its angle normalised to [0, 360).
func NewSpin(deg float32, flipH, flipV bool) Spin {
	return Spin{Deg: NormalizeDegrees(deg), FlipH: flipH, FlipV: flipV}
}

// IsIdentity reports no rotation and no mirroring.
func (s Spin) IsIdentity() bool { return NormalizeDegrees(s.Deg) == 0 && !s.FlipH && !s.FlipV }

// sincos is Rust's f32 sin_cos (the platform's sinf and cosf; rustcompat gives Linux x86-64's on
// every platform).
func sincos(r float32) (float32, float32) {
	return rustcompat.Sin32(r), rustcompat.Cos32(r)
}

// linear is the linear part (a, b, c, d: x' = a·x + c·y, y' = b·x + d·y).
func (s Spin) linear() (a, b, c, d float32) {
	sn, cs := sincos(float32(NormalizeDegrees(s.Deg) * radPerDeg))
	var fx, fy float32 = 1, 1
	if s.FlipH {
		fx = -1
	}
	if s.FlipV {
		fy = -1
	}
	return float32(cs * fx), float32(sn * fx), float32(-sn * fy), float32(cs * fy)
}

// Matrix is the affine map (a, b, c, d, e, f: x' = a·x + c·y + e, y' = b·x + d·y + f) turning the
// page about (cx, cy).
func (s Spin) Matrix(cx, cy float32) [6]float32 {
	a, b, c, d := s.linear()
	return matrixOf(a, b, c, d, cx, cy)
}

// Apply is where point (x, y) goes when turned about (cx, cy).
func (s Spin) Apply(cx, cy, x, y float32) (float32, float32) {
	a, b, c, d := s.linear()
	return applyOf(a, b, c, d, cx, cy, x, y)
}

// Unapply is the point Apply takes to (x, y). The linear part is orthogonal: its inverse is its
// transpose.
func (s Spin) Unapply(cx, cy, x, y float32) (float32, float32) {
	a, b, c, d := s.linear()
	return applyOf(a, c, b, d, cx, cy, x, y)
}

// Bounds is the axis-aligned box r covers once turned about its centre.
func (s Spin) Bounds(r Rect) Rect {
	bw, bh := s.Extent(r.W, r.H)
	return boundsOf(r, bw, bh)
}

// Extent is the width and height of the box a w × h rectangle covers once turned.
func (s Spin) Extent(w, h float32) (float32, float32) {
	sn, cs := sincos(float32(NormalizeDegrees(s.Deg) * radPerDeg))
	return extentOf(abs32(sn), abs32(cs), w, h)
}

// The arithmetic after the trigonometry, kept apart so tests can check it exactly against Rust
// given Rust's own sine and cosine.

func matrixOf(a, b, c, d, cx, cy float32) [6]float32 {
	cx, cy = Finite(cx), Finite(cy)
	return [6]float32{a, b, c, d, cx - (float32(a*cx) + float32(c*cy)), cy - (float32(b*cx) + float32(d*cy))}
}

func applyOf(a, b, c, d, cx, cy, x, y float32) (float32, float32) {
	u, v := x-cx, y-cy
	return cx + float32(a*u) + float32(c*v), cy + float32(b*u) + float32(d*v)
}

func boundsOf(r Rect, bw, bh float32) Rect {
	return NewRect(r.X+(r.W-bw)/2, r.Y+(r.H-bh)/2, bw, bh)
}

// extentOf takes |sin| and |cos| of the angle.
func extentOf(sn, cs, w, h float32) (float32, float32) {
	w, h = abs32(Finite(w)), abs32(Finite(h))
	return float32(w*cs) + float32(h*sn), float32(w*sn) + float32(h*cs)
}

// Within is s applied inside outer (a group member's spin within its group's): the one spin
// doing both. A single mirror in outer reverses the inner turn.
func (s Spin) Within(outer Spin) Spin {
	inner := s.Deg
	if outer.FlipH != outer.FlipV {
		inner = -s.Deg
	}
	return NewSpin(outer.Deg+inner, outer.FlipH != s.FlipH, outer.FlipV != s.FlipV)
}

// Paper is a standard paper size in points (portrait).
type Paper struct {
	Name string
	W, H float32
}

// PaperSizes are the standard paper sizes (Word's Layout › Size gallery).
var PaperSizes = []Paper{
	{"Letter", 612.0, 792.0},
	{"Legal", 612.0, 1008.0},
	{"Executive", 522.0, 756.0},
	{"A3", 841.89, 1190.55},
	{"A4", 595.28, 841.89},
	{"A5", 419.53, 595.28},
	{"B4 (JIS)", 728.5, 1031.8},
	{"B5 (JIS)", 515.9, 728.5},
	{"Tabloid", 792.0, 1224.0},
	{"Statement", 396.0, 612.0},
	{"Envelope #10", 297.0, 684.0},
	{"Envelope DL", 311.8, 623.6},
}

// PaperName names the paper size whose dimensions match (either orientation, within half a point).
func PaperName(w, h float32) (string, bool) {
	for _, p := range PaperSizes {
		if (abs32(p.W-w) < 0.5 && abs32(p.H-h) < 0.5) || (abs32(p.W-h) < 0.5 && abs32(p.H-w) < 0.5) {
			return p.Name, true
		}
	}
	return "", false
}

// PaperByName finds the standard paper size named name, ignoring ASCII case and surrounding space.
func PaperByName(name string) (Paper, bool) {
	name = strings.TrimSpace(name)
	for _, p := range PaperSizes {
		if eqFoldASCII(p.Name, name) {
			return p, true
		}
	}
	return Paper{}, false
}

// PaperFit says how a page is placed on other paper when printing with "Scale to paper size".
type PaperFit struct {
	PaperW, PaperH float32 // the sheet, turned to the page's orientation (points)
	K              float32 // page points to sheet points
	DX, DY         float32 // where the page's top-left corner lands (the page is centred)
}

// FitToPaper fits a pageW × pageH page onto paperW × paperH paper: the paper is turned to the
// page's orientation, the page is scaled by one factor to fill it and centred. Degenerate or
// non-finite sizes give the page as it is (scale 1 on paper of its own size).
func FitToPaper(pageW, pageH, paperW, paperH float32) PaperFit {
	ok := func(v float32) bool { return Finite(v) == v && v >= 1 }
	asIs := PaperFit{PaperW: pageW, PaperH: pageH, K: 1}
	if !(ok(pageW) && ok(pageH) && ok(paperW) && ok(paperH)) {
		return asIs
	}
	short, long := fmin(paperW, paperH), fmax(paperW, paperH)
	pw, ph := short, long
	if pageW > pageH {
		pw, ph = long, short
	}
	k := fmin(pw/pageW, ph/pageH)
	dx, dy := (pw-float32(pageW*k))/2, (ph-float32(pageH*k))/2
	if !(Finite(k) == k && k > 0 && Finite(dx) == dx && Finite(dy) == dy) {
		return asIs
	}
	return PaperFit{PaperW: pw, PaperH: ph, K: k, DX: dx, DY: dy}
}
