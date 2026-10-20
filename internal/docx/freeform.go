package docx

// Freeform shapes (DrawingML custom geometry, a:custGeom; legacy VML paths) and the ink strokes
// WordCraft draws with them (Rust crates/docx/src/read/freeform.rs).

import (
	"slices"
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

const (
	// curveSteps is the straight pieces a Bézier curve is flattened into.
	curveSteps = 8
	// maxSegPoints is the points read from one path segment. A conformant a:lnTo/a:moveTo has
	// one, a:quadBezTo two and a:cubicBezTo three; the cap bounds the de Casteljau control
	// polygon so a segment stuffed with points can't make flattening quadratic in their count.
	maxSegPoints = 8
	// maxEMU is the largest path coordinate kept, EMUs.
	maxEMU = 1.0e12
)

// custGeom is the paths of the custom geometry in sppr (wps:spPr), mapped onto a w × h shape
// (points): each a:path in its own w × h space (EMUs of the shape's size when it gives none).
// Lines and Bézier curves (flattened) are kept; arcs are drawn straight to their end. False when
// there is no a:custGeom or it has no points.
func custGeom(sppr *El, w, h float32) (doc.Freeform, bool) {
	cg := sppr.Child("a:custGeom")
	if cg == nil {
		return doc.Freeform{}, false
	}
	list := cg.Child("a:pathLst")
	if list == nil {
		return doc.Freeform{}, false
	}
	paths := doc.Vec[doc.FreePath]{}
	all := list.Children("a:path")
	for _, p := range all[:min(len(all), doc.MaxFreeformPaths)] {
		// Subpaths count too: stop once the shape has as many paths as it may keep.
		if len(paths) >= doc.MaxFreeformPaths {
			break
		}
		dim := func(n string, shape float32) float64 {
			if v, ok := p.Attr(n); ok {
				if k, ok := intOf(v); ok && k > 0 {
					return float64(k)
				}
			}
			return float64(shape) * 12_700
		}
		pw, ph := dim("w", w), dim("h", h)
		scale := func(pt [2]float64) [2]doc.F32 {
			sx, sy := 1.0/12_700, 1.0/12_700
			if pw > 0 {
				sx = float64(w) / pw
			}
			if ph > 0 {
				sy = float64(h) / ph
			}
			return [2]doc.F32{doc.F32(float32(pt[0] * sx)), doc.F32(float32(pt[1] * sy))}
		}
		point := func(e *El) ([2]float64, bool) {
			c := func(n string) (float64, bool) {
				v, ok := e.Attr(n)
				if !ok {
					return 0, false
				}
				k, ok := intOf(v)
				if !ok {
					return 0, false
				}
				return rustcompat.Clamp(float64(k), -maxEMU, maxEMU), true
			}
			x, okx := c("x")
			if !okx {
				return [2]float64{}, false
			}
			y, oky := c("y")
			return [2]float64{x, y}, oky
		}
		type sub struct {
			pts    [][2]float64
			closed bool
		}
		// Each subpath (from an a:moveTo) becomes a path of its own.
		var cur [][2]float64
		var done []sub
		fillNone := attrIs(p, "fill", "none")
		for _, seg := range p.Els() {
			if len(cur) >= doc.MaxFreeformPoints || len(done) >= doc.MaxFreeformPaths {
				break
			}
			var pts [][2]float64
			for _, e := range seg.Children("a:pt") {
				if len(pts) == maxSegPoints {
					break
				}
				if v, ok := point(e); ok {
					pts = append(pts, v)
				}
			}
			switch seg.Local() {
			case "moveTo":
				if len(cur) > 0 {
					done = append(done, sub{cur, false})
					cur = nil
				}
				if len(pts) > 0 {
					cur = append(cur, pts[0])
				}
			case "lnTo":
				if len(pts) > 0 {
					cur = append(cur, pts[0])
				}
			case "cubicBezTo", "quadBezTo":
				if len(cur) == 0 {
					if len(pts) > 0 {
						cur = append(cur, pts[len(pts)-1])
					}
					continue
				}
				ctrl := append([][2]float64{cur[len(cur)-1]}, pts...)
				for i := 1; i <= curveSteps; i++ {
					cur = append(cur, bezier(ctrl, float64(i)/curveSteps))
				}
			case "close":
				if len(cur) > 0 {
					done = append(done, sub{cur, true})
					cur = nil
				}
			}
		}
		if len(cur) > 0 {
			// A path drawn back to where it started is closed, though it has no a:close (Word's
			// cover pages), so it's filled.
			back := len(cur) > 2 && cur[0] == cur[len(cur)-1]
			done = append(done, sub{cur, back})
		}
		for _, d := range done[:min(len(done), max(doc.MaxFreeformPaths-len(paths), 0))] {
			pts := doc.Vec[[2]doc.F32]{}
			for _, pt := range d.pts[:min(len(d.pts), doc.MaxFreeformPoints)] {
				pts = append(pts, scale(pt))
			}
			paths = append(paths, doc.FreePath{Pts: pts, Closed: d.closed && !fillNone})
		}
	}
	f := doc.Freeform{W: doc.F32(w), H: doc.F32(h), Paths: paths, Alpha: 1}.Sanitized()
	return f, len(f.Paths) > 0
}

// inkTool is the pen of an ink stroke written by WordCraft, from its drawing's name ("Ink Pen 3",
// "Ink Highlighter 4").
func inkTool(name string) (doc.InkTool, bool) {
	words := strings.FieldsFunc(name, rustcompat.IsWhitespace)
	if len(words) < 2 || words[0] != "Ink" {
		return 0, false
	}
	return doc.ParseInkTool(words[1])
}

// inkOrder is the drawing order of an ink stroke written by WordCraft, from its drawing's name
// ("Ink Pen 3 #12": the twelfth stroke drawn), for Ink Replay.
func inkOrder(name string) (uint32, bool) {
	words := strings.FieldsFunc(name, rustcompat.IsWhitespace)
	for _, w := range words[min(len(words), 3):] {
		if n, ok := strings.CutPrefix(w, "#"); ok {
			if v, ok := rustcompat.ParseUint(n, 32); ok {
				return uint32(v), true
			}
		}
	}
	return 0, false
}

// lineAlpha is the outline opacity of ln (a:ln): its colour's a:alpha, 0–1.
func lineAlpha(ln *El) float32 {
	if ln == nil {
		return 1
	}
	f := ln.Child("a:solidFill")
	if f == nil {
		return 1
	}
	els := f.Els()
	if len(els) == 0 {
		return 1
	}
	a := els[0].Child("a:alpha")
	if a == nil {
		return 1
	}
	v, ok := a.Attr("val")
	if !ok {
		return 1
	}
	k, ok := intOf(v)
	if !ok {
		return 1
	}
	return float32(min(max(k, 0), 100_000)) / 100_000
}

// bezier is the point at t on the Bézier curve with control points c (de Casteljau).
func bezier(c [][2]float64, t float64) [2]float64 {
	p := append([][2]float64(nil), c...)
	for len(p) > 1 {
		next := make([][2]float64, 0, len(p)-1)
		for i := 0; i+1 < len(p); i++ {
			a, b := p[i], p[i+1]
			// Each product is rounded on its own (float64(…)): Go may otherwise fuse a multiply
			// and an add, which Rust never does.
			next = append(next, [2]float64{a[0] + float64((b[0]-a[0])*t), a[1] + float64((b[1]-a[1])*t)})
		}
		p = next
	}
	if len(p) == 0 {
		return [2]float64{}
	}
	return p[0]
}

const (
	// maxVML is the largest VML path coordinate kept (coordinate-space units).
	maxVML = 1.0e12
	// maxVMLPath is the characters of a VML path attribute read; the rest is ignored.
	maxVMLPath = 1 << 20
	// maxVMLParams is the parameters of a VML path kept, all commands together.
	maxVMLParams = doc.MaxFreeformPoints * 8
)

// vmlPathShape is a legacy VML freeform (v:shape with a path, ECMA-376 Part 4 §14.1.2.19).
type vmlPathShape struct {
	freeform doc.Freeform
	// noFill and noStroke are nf and ns in the path.
	noFill, noStroke bool
}

// vmlTwo are the VML path commands that are two letters long.
var vmlTwo = []string{"nf", "ns", "ae", "al", "at", "ar", "wa", "wr", "qx", "qy", "qb"}

// vmlPath is the VML path of a shape, mapped onto its w × h frame (points) through its coordsize
// (default 1000,1000) and coordorigin (default 0,0). Commands: m/t move (absolute / relative),
// l/r line, c/v cubic Bézier (flattened), x close, e end, nf/ns no fill / no outline; arcs (at,
// ar, wa, wr) and quadrants (qx, qy) are drawn straight to their end, qb straight to its last
// point, and ellipses (ae, al) are skipped. An empty parameter is 0. False when the path has no
// points, or uses formulas (@n, from v:formulas, which aren't evaluated): such a shape isn't drawn
// rather than drawn wrong.
func vmlPath(path string, coordsize, coordorigin *string, w, h float32) (vmlPathShape, bool) {
	if strings.Contains(path, "@") {
		return vmlPathShape{}, false
	}
	pair := func(v *string) (float64, float64, bool) {
		if v == nil {
			return 0, 0, false
		}
		a, b, _ := strings.Cut(*v, ",")
		n := func(s string) (float64, bool) {
			i, ok := intOf(s)
			return rustcompat.Clamp(float64(i), -maxVML, maxVML), ok
		}
		x, ok := n(a)
		if !ok {
			return 0, 0, false
		}
		y, ok := n(b)
		if !ok {
			y = 0
		}
		return x, y, true
	}
	size := func(v float64) float64 {
		if v > 0 {
			return v
		}
		return 1000
	}
	cw, ch := 1000.0, 1000.0
	if a, b, ok := pair(coordsize); ok {
		cw, ch = size(a), size(b)
	}
	ox, oy, _ := pair(coordorigin)
	sx, sy := float64(w)/cw, float64(h)/ch
	scale := func(p [2]float64) [2]doc.F32 {
		return [2]doc.F32{doc.F32(float32((p[0] - ox) * sx)), doc.F32(float32((p[1] - oy) * sy))}
	}
	var d vmlDraw
	noFill, noStroke := false, false
	for _, c := range vmlCommands(path) {
		if d.full() {
			break
		}
		cmd, params := c.cmd, c.params
		pts := make([][2]float64, 0, len(params)/2)
		for i := 0; i+1 < len(params); i += 2 {
			pts = append(pts, [2]float64{params[i], params[i+1]})
		}
		switch cmd {
		case "m", "t":
			for i, p := range pts {
				if cmd == "t" {
					p = d.rel(p)
				}
				if i == 0 {
					d.moveTo(p)
				} else {
					d.lineTo(p)
				}
			}
		case "l", "r", "qx", "qy":
			for _, p := range pts {
				if cmd == "r" {
					p = d.rel(p)
				}
				d.lineTo(p)
			}
		case "c", "v":
			for i := 0; i+2 < len(pts); i += 3 {
				ctrl := [][2]float64{d.at}
				for _, p := range pts[i : i+3] {
					if cmd == "v" {
						p = d.rel(p)
					}
					ctrl = append(ctrl, p)
				}
				for k := 1; k <= curveSteps; k++ {
					d.lineTo(bezier(ctrl, float64(k)/curveSteps))
				}
			}
		case "qb":
			if len(pts) > 0 {
				d.lineTo(pts[len(pts)-1])
			}
		// Bounding box, then start and end: at/wa draw a line to the start, ar/wr move there.
		case "at", "ar", "wa", "wr":
			for i := 0; i+3 < len(pts); i += 4 {
				st, en := pts[i+2], pts[i+3]
				if cmd == "ar" || cmd == "wr" {
					d.moveTo(st)
				} else {
					d.lineTo(st)
				}
				d.lineTo(en)
			}
		case "x":
			d.end(true)
		case "e":
			d.end(false)
		case "nf":
			noFill = true
		case "ns":
			noStroke = true
		}
	}
	d.end(false)
	paths := doc.Vec[doc.FreePath]{}
	for _, sub := range d.done {
		pts := doc.Vec[[2]doc.F32]{}
		for _, p := range sub.pts {
			pts = append(pts, scale(p))
		}
		paths = append(paths, doc.FreePath{Pts: pts, Closed: sub.closed})
	}
	f := doc.Freeform{W: doc.F32(w), H: doc.F32(h), Paths: paths, Alpha: 1}.Sanitized()
	return vmlPathShape{freeform: f, noFill: noFill, noStroke: noStroke}, len(f.Paths) > 0
}

// vmlDraw is the pen while a VML path is drawn: where it is, where the subpath began, the subpath
// being drawn and those done (closed or not).
type vmlDraw struct {
	at, start [2]float64
	cur       [][2]float64
	done      []vmlSub
}

type vmlSub struct {
	pts    [][2]float64
	closed bool
}

func (d *vmlDraw) full() bool { return len(d.done) >= doc.MaxFreeformPaths }

func (d *vmlDraw) rel(p [2]float64) [2]float64 {
	return [2]float64{rustcompat.Clamp(d.at[0]+p[0], -maxVML, maxVML), rustcompat.Clamp(d.at[1]+p[1], -maxVML, maxVML)}
}

func (d *vmlDraw) moveTo(p [2]float64) {
	d.end(false)
	d.at, d.start = p, p
	d.cur = append(d.cur, p)
}

func (d *vmlDraw) lineTo(p [2]float64) {
	if len(d.cur) == 0 {
		d.cur = append(d.cur, d.at)
		d.start = d.at
	}
	if len(d.cur) < doc.MaxFreeformPoints {
		d.cur = append(d.cur, p)
	}
	d.at = p
}

// end finishes the subpath being drawn, closed back to its start or not.
func (d *vmlDraw) end(closed bool) {
	if len(d.cur) > 0 && !d.full() {
		d.done = append(d.done, vmlSub{d.cur, closed})
	}
	d.cur = nil
	if closed {
		d.at = d.start
	}
}

type vmlCommand struct {
	cmd    string
	params []float64
}

// vmlCommands is the commands of a VML path with their parameters: a command is a letter (or one
// of vmlTwo) followed by numbers separated by commas or spaces. An empty parameter (nothing before
// a comma) is 0, and so is a formula reference (@n, not expected here: see vmlPath); fractions
// are dropped. Anything else is skipped.
func vmlCommands(path string) []vmlCommand {
	var out []vmlCommand
	chars := []rune(takeChars(path, maxVMLPath))
	// Whether a number was read since the command or the last comma.
	hasNum := true
	kept := 0
	push := func(v float64) {
		if kept < maxVMLParams && len(out) > 0 {
			last := &out[len(out)-1]
			last.params = append(last.params, v)
			kept++
		}
	}
	isDigit := func(c rune) bool { return '0' <= c && c <= '9' }
	isAlpha := func(c rune) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
	lower := func(c rune) rune {
		if 'A' <= c && c <= 'Z' {
			return c + ('a' - 'A')
		}
		return c
	}
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		switch {
		case isAlpha(c):
			if !hasNum {
				push(0)
			}
			hasNum = false
			cmd := string(lower(c))
			if i+1 < len(chars) {
				two := cmd + string(lower(chars[i+1]))
				if slices.Contains(vmlTwo, two) {
					i++
					cmd = two
				}
			}
			out = append(out, vmlCommand{cmd: cmd})
		case c == ',':
			if !hasNum {
				push(0)
			}
			hasNum = false
		case c == '@' || c == '-' || c == '+' || c == '.' || isDigit(c):
			v := 0.0
			if isDigit(c) {
				v = float64(c - '0')
			}
			frac := c == '.'
			for i+1 < len(chars) {
				n := chars[i+1]
				if n == '.' {
					frac = true
				} else if isDigit(n) {
					if !frac {
						// Rounded on its own (float64(…)): Go may otherwise fuse the multiply
						// and add, which Rust never does.
						v = min(float64(v*10)+float64(n-'0'), maxVML)
					}
				} else {
					break
				}
				i++
			}
			switch c {
			case '@':
				v = 0
			case '-':
				v = -v
			}
			hasNum = true
			push(v)
		}
		// Whitespace and anything else only separate numbers.
	}
	if !hasNum {
		push(0)
	}
	return out
}
