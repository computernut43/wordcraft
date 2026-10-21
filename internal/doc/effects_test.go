package doc

import (
	"math"
	"testing"
)

// Ported from crates/doc/src/wordart.rs hostile_values_are_reduced (the text fill part), with
// the negative-angle and solid-fill cases of TextFill::sanitized.
func TestTextFillSanitized(t *testing.T) {
	stops := Vec[GradStop]{}
	for i := range 50 {
		pos := F32(1e9)
		if i == 0 {
			pos = F32(math.NaN())
		}
		stops = append(stops, GradStop{Pos: pos, Color: Black, Transparency: -5})
	}
	f, ok := TextFill{Kind: FillGradient, Stops: stops, Angle: F32(math.Inf(1))}.Sanitized()
	if !ok || f.Kind != FillGradient {
		t.Fatalf("not a gradient: %+v", f)
	}
	if len(f.Stops) != MaxStops {
		t.Fatalf("%d stops", len(f.Stops))
	}
	for _, s := range f.Stops {
		if s.Pos < 0 || s.Pos > 100 || s.Transparency != 0 {
			t.Fatalf("stop %+v", s)
		}
	}
	if f.Angle != 0 {
		t.Fatalf("angle %v", f.Angle)
	}
	if _, ok := (TextFill{Kind: FillGradient, Stops: Vec[GradStop]{}}).Sanitized(); ok {
		t.Fatal("a gradient without stops")
	}
	// f32::rem_euclid(360): -90° is 270°.
	g, _ := TextFill{Kind: FillGradient, Stops: Vec[GradStop]{{Pos: 5}}, Angle: -90}.Sanitized()
	if g.Angle != 270 {
		t.Fatalf("angle %v", g.Angle)
	}
	for _, c := range []struct{ in, want F32 }{{-5, 0}, {200, 100}, {F32(math.NaN()), 0}, {40, 40}} {
		s, _ := TextFill{Kind: FillSolid, Transparency: c.in}.Sanitized()
		if s.Transparency != c.want {
			t.Errorf("solid transparency %v → %v, want %v", c.in, s.Transparency, c.want)
		}
	}
}
