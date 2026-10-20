package doc

import (
	"math"
	"testing"
)

// Ported from crates/doc/src/freeform.rs hostile_geometry_is_sanitized.
func TestHostileGeometryIsSanitized(t *testing.T) {
	nan, inf := F32(math.NaN()), F32(math.Inf(1))
	pts := Vec[[2]F32]{{nan, 1}, {2, inf}, {1, 2}, {1e30, -1e30}}
	for range MaxFreeformPoints * 2 {
		pts = append(pts, [2]F32{3, 3})
	}
	f := Freeform{W: nan, H: -4, Paths: Vec[FreePath]{{Pts: pts}}, Alpha: 7}.Sanitized()
	if f.W != 0 || f.H != 0 || f.Alpha != 1 {
		t.Fatalf("w, h, alpha = %v, %v, %v", f.W, f.H, f.Alpha)
	}
	p := f.Paths[0].Pts
	if len(p) != MaxFreeformPoints {
		t.Fatalf("%d points", len(p))
	}
	if p[0] != [2]F32{1, 2} || p[1] != [2]F32{F32(maxFreeformCoord), F32(-maxFreeformCoord)} {
		t.Fatalf("points %v %v", p[0], p[1])
	}
}

// A path left with no finite point is dropped, and the paths are capped.
func TestSanitizedDropsEmptyPaths(t *testing.T) {
	var paths Vec[FreePath]
	paths = append(paths, FreePath{Pts: Vec[[2]F32]{{F32(math.NaN()), 0}}})
	for range MaxFreeformPaths + 5 {
		paths = append(paths, FreePath{Pts: Vec[[2]F32]{{1, 1}}, Closed: true})
	}
	f := Freeform{Paths: paths, Alpha: 1}.Sanitized()
	if len(f.Paths) != MaxFreeformPaths-1 {
		t.Fatalf("%d paths", len(f.Paths))
	}
}
