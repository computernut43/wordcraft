package docx

// Legacy VML drawings (w:pict, ECMA-376 Part 4): pictures, text boxes, lines, rectangles, ovals,
// preset shapes, freeform paths, WordArt, groups and the header text watermark (Rust
// crates/docx/src/read/story.rs read_vml_all, read_vml_group, vml_group_members, read_vml_shape,
// vml_spt, vml_coordpair, vml_textbox, vml_style, vml_float, vml_watermark, vml_fraction,
// vml_spin), and OLE objects (w:object, read_object) shown as their picture.

import (
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
	"github.com/computernut43/wordcraft/internal/geom"
)

// maxVMLGroupDepth is the deepest VML group-in-group nesting read.
const maxVMLGroupDepth = 8

// vmlTextWarps are the DrawingML text warps of VML's WordArt shapes, o:spt 136 (plain text) to
// 175 (can down), in order: the same shapes under their DrawingML names (ST_TextShapeType).
var vmlTextWarps = [40]string{
	"textPlain", "textStop", "textTriangle", "textTriangleInverted", "textChevron", "textChevronInverted",
	"textRingInside", "textRingOutside", "textArchUp", "textArchDown", "textCircle", "textButton",
	"textArchUpPour", "textArchDownPour", "textCirclePour", "textButtonPour", "textCurveUp", "textCurveDown",
	"textCascadeUp", "textCascadeDown", "textWave1", "textWave2", "textDoubleWave1", "textWave4",
	"textInflate", "textDeflate", "textInflateBottom", "textDeflateBottom", "textInflateTop", "textDeflateTop",
	"textDeflateInflate", "textDeflateInflateDeflate", "textFadeRight", "textFadeLeft", "textFadeUp",
	"textFadeDown", "textSlantUp", "textSlantDown", "textCanUp", "textCanDown",
}

// readVMLAll reads the VML drawings in pict (a w:pict), in order. In a header or footer, the
// document's watermark is taken out instead (the first one found): it is drawn on every page, not
// as a shape.
func (r *reader) readVMLAll(sc *storyCtx, pict *El, rels Rels) []doc.Object {
	var out []doc.Object
	for _, e := range pict.Els() {
		if !strings.HasPrefix(e.Name, "v:") || e.Local() == "shapetype" {
			continue
		}
		if len(out) >= doc.MaxGroupChildren {
			break
		}
		if sc.inHF {
			if wm, ok := vmlWatermark(e); ok {
				if r.doc.Settings.Watermark == nil {
					r.doc.Settings.Watermark = &wm
				}
				continue
			}
		}
		var o doc.Object
		var ok bool
		if e.Local() == "group" {
			o, ok = r.readVMLGroup(sc, pict, e, rels)
		} else {
			o, ok = r.readVMLShape(sc, pict, e, rels, nil)
		}
		if ok {
			out = append(out, o)
		}
	}
	return out
}

// vmlLen is a length in a VML style, points (0 when missing or junk), clamped to 0..MaxLenPt.
func vmlLen(style, key string) float32 {
	v := float32(0)
	if s, ok := vmlStyle(style, key); ok {
		if m, ok := measure(s, 1); ok {
			v = m
		}
	}
	return fclamp(v, 0, MaxLenPt)
}

// readVMLGroup reads a v:group: its own box from its style, its members in the space coordsize
// and coordorigin give it, nested groups flattened into it as groupMembers does for DrawingML.
// False when it has no member to show. pict is the picture the group sits in, where a member's
// v:shapetype is written.
func (r *reader) readVMLGroup(sc *storyCtx, pict, grp *El, rels Rels) (doc.Object, bool) {
	style := attr(grp, "style")
	w, h := vmlLen(style, "width"), vmlLen(style, "height")
	float := vmlFloat(grp, style, sc.inHF)
	// The members' space; without a usable coordsize it is the group's own box.
	chW, chH := rustcompat.Max(w, 1), rustcompat.Max(h, 1)
	if cw, ch, ok := vmlCoordpair(grp, "coordsize"); ok && cw > 0 && ch > 0 {
		chW, chH = cw, ch
	}
	cx, cy, _ := vmlCoordpair(grp, "coordorigin")
	var children doc.Vec[doc.GroupChild]
	r.vmlGroupMembers(sc, pict, grp, rels, [4]float32{-cx, -cy, 1, 1}, 0, &children)
	if len(children) == 0 {
		return doc.Object{}, false
	}
	return doc.Object{InlineObject: doc.Group{W: doc.F32(w), H: doc.F32(h), Float: float, ChW: doc.F32(chW), ChH: doc.F32(chH), Children: children}}, true
}

// vmlGroupMembers reads the members of VML group g into out, mapped into the outermost group's
// space by t (x0, y0, sx, sy: outer = x0 + inner × sx). A member is placed by left/top in that
// space, except a v:line, which carries its own offset in from.
func (r *reader) vmlGroupMembers(sc *storyCtx, pict, g *El, rels Rels, t [4]float32, depth int, out *doc.Vec[doc.GroupChild]) {
	x0, y0, sx, sy := t[0], t[1], t[2], t[3]
	fit := func(v, lo float32) float32 { return fclamp(geom.Finite(v), lo, MaxLenPt) }
	for _, e := range g.Els() {
		if len(*out) >= doc.MaxGroupChildren {
			return
		}
		if !strings.HasPrefix(e.Name, "v:") || e.Local() == "shapetype" {
			continue
		}
		style := attr(e, "style")
		num := func(k string) float32 {
			if s, ok := vmlStyle(style, k); ok {
				if m, ok := measure(s, 1); ok {
					return geom.Finite(m)
				}
			}
			return 0
		}
		ox, oy, ew, eh := num("left"), num("top"), num("width"), num("height")
		if e.Local() == "group" {
			if depth >= maxVMLGroupDepth {
				continue
			}
			// Its members' space maps onto its own box. Each product is rounded on its own
			// (float32(…)): Go may otherwise fuse a multiply and an add, which Rust never does.
			cw, ch := rustcompat.Max(ew, 1), rustcompat.Max(eh, 1)
			if w, h, ok := vmlCoordpair(e, "coordsize"); ok && w > 0 && h > 0 {
				cw, ch = w, h
			}
			cox, coy, _ := vmlCoordpair(e, "coordorigin")
			kx, ky := ew/cw, eh/ch
			nt := [4]float32{
				x0 + float32((ox-float32(cox*kx))*sx),
				y0 + float32((oy-float32(coy*ky))*sy),
				sx * kx, sy * ky,
			}
			r.vmlGroupMembers(sc, pict, e, rels, nt, depth+1, out)
			continue
		}
		obj, ok := r.readVMLShape(sc, pict, e, rels, &[2]float32{sx, sy})
		if !ok {
			continue
		}
		// A member is placed by x/y, so a line's own offset comes off the member itself.
		var dx, dy float32
		switch v := obj.InlineObject.(type) {
		case doc.Image:
			dx, dy = float32(v.Float.X), float32(v.Float.Y)
			v.Float.X, v.Float.Y = 0, 0
			obj = doc.Object{InlineObject: v}
		case doc.Shape:
			dx, dy = float32(v.Float.X), float32(v.Float.Y)
			v.Float.X, v.Float.Y = 0, 0
			obj = doc.Object{InlineObject: v}
		}
		*out = append(*out, doc.GroupChild{X: doc.F32(fit(x0+float32((ox+dx)*sx), -MaxLenPt)), Y: doc.F32(fit(y0+float32((oy+dy)*sy), -MaxLenPt)), Obj: obj})
	}
}

// offVal reports a VML boolean attribute value that is false (f, false or 0).
func offVal(v string) bool { return v == "f" || v == "false" || v == "0" }

// readVMLShape reads one legacy VML picture, text box, line, rectangle, oval or preset shape (best
// effort). pict is the picture it sits in, where its v:shapetype is written: for a group's member
// too, since Word writes each shapetype once, beside the group rather than inside it. grp scales
// a member from its group's space, and leaves placing it to the caller.
func (r *reader) readVMLShape(sc *storyCtx, pict, shape *El, rels Rels, grp *[2]float32) (doc.Object, bool) {
	style := attr(shape, "style")
	sx, sy := float32(1), float32(1)
	if grp != nil {
		sx, sy = grp[0], grp[1]
	}
	w := rustcompat.Min(float32(vmlLen(style, "width")*sx), MaxLenPt)
	h := rustcompat.Min(float32(vmlLen(style, "height")*sy), MaxLenPt)
	// A member of a group never floats: the group does, and places it.
	var float doc.Float
	if grp != nil {
		v, ok := vmlStyle(style, "visibility")
		float.Hidden = ok && asciiEqualFold(v, "hidden")
		float.SetSpin(vmlSpin(style))
	} else {
		float = vmlFloat(shape, style, sc.inHF)
	}
	if img := shape.Find("v:imagedata"); img != nil {
		id, ok := img.Attr("r:id")
		if !ok {
			if id, ok = img.Attr("r:pict"); !ok {
				return doc.Object{}, false
			}
		}
		media, ok := r.mediaFor(rels, id)
		if !ok {
			return doc.Object{}, false
		}
		alt, ok := shape.Attr("alt")
		if !ok {
			alt = attr(img, "o:title")
		}
		return doc.Object{InlineObject: doc.Image{Media: media, W: doc.F32(w), H: doc.F32(h), Alt: alt, Float: float}}, true
	}
	// The shapetype the shape is drawn from (its type, ECMA-376 Part 4 VML), when written in the
	// same picture: what the shape leaves out, it takes from there.
	var typeID *string
	if t, ok := shape.Attr("type"); ok {
		typeID = ptr(strings.TrimLeft(rustcompat.Trim(t), "#"))
	}
	var shapetype *El
	if typeID != nil {
		for _, e := range pict.Els() {
			if id, ok := e.Attr("id"); ok && e.Name == "v:shapetype" && id == *typeID {
				shapetype = e
				break
			}
		}
	}
	vattr := func(name string) (string, bool) {
		if v, ok := shape.Attr(name); ok {
			return v, true
		}
		if shapetype != nil {
			return shapetype.Attr(name)
		}
		return "", false
	}
	on := func(name string) bool {
		v, ok := vattr(name)
		return !ok || !offVal(v)
	}
	paint := func(onName, colorName string, def doc.RGB) *doc.RGB {
		if !on(onName) {
			return nil
		}
		v, ok := vattr(colorName)
		if !ok {
			return &def
		}
		if c, ok := vmlColor(v); ok {
			return &c
		}
		return nil
	}
	stroke := paint("stroked", "strokecolor", doc.Black)
	strokeWidth := float32(0.75)
	if v, ok := vattr("strokeweight"); ok {
		if m, ok := measure(v, 1); ok {
			strokeWidth = m
		}
	}
	strokeWidth = fclamp(strokeWidth, 0, 100)
	fill := paint("filled", "fillcolor", doc.White)
	var kind *doc.ShapeKind
	switch shape.Local() {
	case "line":
		kind = ptr(doc.ShapeLine)
	case "rect":
		kind = ptr(doc.ShapeRectangle)
	case "roundrect":
		kind = ptr(doc.ShapeRoundedRectangle)
	case "oval":
		kind = ptr(doc.ShapeEllipse)
	case "shape":
		// A preset, unless the shape's own path replaces its shapetype's geometry.
		_, hasPath := shape.Attr("path")
		ownPath := false
		if p := shape.Child("v:path"); p != nil {
			_, ownPath = p.Attr("v")
		}
		if !hasPath && !ownPath {
			if spt, ok := vmlSpt(shape, shapetype, typeID); ok {
				if k, ok := vmlPresetKind(spt); ok {
					kind = &k
				}
			}
		}
	}
	// Word 2007 WordArt: a v:shape whose v:textpath draws its string (the header watermark is
	// taken before this). Shown as a text box holding that text, in the textpath's font and the
	// shape's fillcolor; the box itself has neither fill nor outline, as the letters are the art.
	if tp := shape.Child("v:textpath"); shape.Local() == "shape" && tp != nil && sc.storyDepth < maxStoryDepth {
		if v, ok := tp.Attr("on"); !ok || !offVal(v) {
			if text, ok := tp.Attr("string"); ok {
				text = takeChars(text, 200)
				if rustcompat.Trim(text) != "" {
					return r.vmlWordArt(shape, shapetype, typeID, tp, text, vattr, w, h, strokeWidth, float), true
				}
			}
		}
	}
	if tb, t, ok := vmlTextbox(shape, 0); ok && sc.storyDepth < maxStoryDepth {
		id := r.readTextbox(sc, t, rels)
		if part, ok := r.doc.Parts[id]; ok {
			// Internal margins inset="l,t,r,b" (ECMA-376 Part 4 §19.1.2.22), each one left out
			// keeping Word's default; a bare number is in EMUs, VML's base unit.
			// o:insetmode="auto" leaves them to the application.
			inset := doc.TextBoxInset
			mode, hasMode := tb.Attr("o:insetmode")
			custom := !hasMode || !asciiEqualFold(mode, "auto")
			spec := ""
			if custom {
				spec = attr(tb, "inset")
			}
			for i, v := range strings.Split(spec, ",") {
				if i >= len(inset) {
					break
				}
				if pt, ok := measure(v, 12_700); ok {
					inset[i] = rustcompat.U32(rustcompat.Min(roundF32(float32(rustcompat.Max(pt, 0)*12_700)), float32(doc.MaxTextBoxInset)))
				}
			}
			part.Body.Inset = nil
			if inset != doc.TextBoxInset {
				part.Body.Inset = &inset
			}
			// Resize shape to fit text (DrawingML's a:spAutoFit).
			fitv, ok := vmlStyle(attr(tb, "style"), "mso-fit-shape-to-text")
			part.Body.AutoFit = ok && (asciiEqualFold(fitv, "t") || asciiEqualFold(fitv, "true"))
			// Where the text sits between the box's top and bottom (DrawingML's anchor).
			if a, ok := vmlStyle(style, "v-text-anchor"); ok {
				switch {
				case strings.HasPrefix(a, "middle"):
					part.Body.Anchor = doc.VAlignCenter
				case strings.HasPrefix(a, "bottom"):
					part.Body.Anchor = doc.VAlignBottom
				default:
					part.Body.Anchor = doc.VAlignTop
				}
			}
			r.doc.Parts[id] = part
		}
		// Text in a preset shape keeps the shape around it; otherwise it is a text box.
		k := doc.ShapeTextBox
		if kind != nil && *kind != doc.ShapeRectangle && !kind.IsOpen() {
			k = *kind
		}
		return doc.Object{InlineObject: doc.Shape{Kind: k, W: doc.F32(w), H: doc.F32(h), Fill: fill, Stroke: stroke,
			StrokeWidth: doc.F32(strokeWidth), Float: float, Story: ptr(id)}}, true
	}
	// A freeform: its path, in its coordsize space; a v:fill with no opacity or nf in the path
	// leaves it unfilled. Not WordArt (v:textpath).
	if path, ok := shape.Attr("path"); ok && shape.Local() == "shape" && shape.Child("v:textpath") == nil {
		var cs, co *string
		if v, ok := shape.Attr("coordsize"); ok {
			cs = &v
		}
		if v, ok := shape.Attr("coordorigin"); ok {
			co = &v
		}
		if vp, ok := vmlPath(path, cs, co, w, h); ok {
			opacity := func(el string) float32 {
				if e := shape.Child(el); e != nil {
					if v, ok := e.Attr("opacity"); ok {
						if f, ok := vmlFraction(v); ok {
							return f
						}
					}
				}
				return 1
			}
			if vp.noFill || opacity("v:fill") <= 0 {
				fill = nil
			}
			if vp.noStroke {
				stroke = nil
			}
			if stroke == nil && fill == nil {
				return doc.Object{}, false
			}
			f := vp.freeform
			f.Alpha = doc.F32(opacity("v:stroke"))
			return doc.Object{InlineObject: doc.Shape{Kind: doc.ShapeFreeform, W: doc.F32(w), H: doc.F32(h), Fill: fill, Stroke: stroke,
				StrokeWidth: doc.F32(strokeWidth), Float: float, Freeform: &f}}, true
		}
	}
	if kind == nil {
		return doc.Object{}, false
	}
	var extra doc.ShapeExtra
	if *kind == doc.ShapeLine {
		// Arrowheads (v:stroke startarrow / endarrow): any but none is drawn.
		arrow := func(name string) bool {
			if s := shape.Child("v:stroke"); s != nil {
				v, ok := s.Attr(name)
				return ok && v != "none"
			}
			return false
		}
		extra.ArrowStart = arrow("startarrow")
		extra.ArrowEnd = arrow("endarrow")
	}
	if shape.Local() == "line" {
		// A line runs from → to (points, offsets from where its style places it). A preset line
		// (v:shape) runs corner to corner of the box its style gives it.
		pt := func(name string) (float32, float32) {
			v, ok := shape.Attr(name)
			if !ok {
				v = "0,0"
			}
			x, y, ok := strings.Cut(v, ",")
			if !ok {
				x, y = v, "0"
			}
			m := func(s string) float32 {
				f := float32(0)
				if v, ok := measure(s, 1); ok {
					f = geom.Finite(v)
				}
				return fclamp(f, -MaxLenPt, MaxLenPt)
			}
			return m(x), m(y)
		}
		lx0, ly0 := pt("from")
		lx1, ly1 := pt("to")
		abs := func(v float32) float32 {
			if v < 0 {
				return -v
			}
			return v
		}
		w = rustcompat.Min(float32(abs(lx1-lx0)*sx), MaxLenPt)
		h = rustcompat.Min(float32(abs(ly1-ly0)*sy), MaxLenPt)
		float.X = doc.F32(fclamp(float32(float.X)+rustcompat.Min(lx0, lx1), -MaxLenPt, MaxLenPt))
		float.Y = doc.F32(fclamp(float32(float.Y)+rustcompat.Min(ly0, ly1), -MaxLenPt, MaxLenPt))
		// Drawn top-left to bottom-right; flipped for the other directions.
		float.FlipH = float.FlipH != (lx1 < lx0)
		float.FlipV = float.FlipV != (ly1 < ly0)
	}
	if stroke == nil && (fill == nil || *kind == doc.ShapeLine) {
		return doc.Object{}, false
	}
	if *kind == doc.ShapeLine {
		fill = nil
	}
	return doc.Object{InlineObject: doc.Shape{Kind: *kind, W: doc.F32(w), H: doc.F32(h), Fill: fill, Stroke: stroke,
		StrokeWidth: doc.F32(strokeWidth), Float: float, Extra: extra}}, true
}

// vmlWordArt is Word 2007 WordArt (a v:shape whose v:textpath tp draws text) as a text box
// holding the text.
func (r *reader) vmlWordArt(shape, shapetype *El, typeID *string, tp *El, text string, vattr func(string) (string, bool),
	w, h, strokeWidth float32, float doc.Float) doc.Object {
	var font *string
	if f, ok := vmlStyle(attr(tp, "style"), "font-family"); ok {
		if f = takeChars(strings.TrimFunc(f, isFontQuote), 100); f != "" {
			font = &f
		}
	}
	// The shapetype's WordArt shape (o:spt 136-175) bends the text like DrawingML's warp.
	var warp *doc.TextWarp
	if spt, ok := vmlSpt(shape, shapetype, typeID); ok && spt >= 136 && int(spt-136) < len(vmlTextWarps) {
		if tw, ok := doc.NewTextWarp(vmlTextWarps[spt-136]); ok {
			warp = &tw
		}
	}
	// The letters fill the box (fitshape): sized to its height.
	props := doc.CharProps{Font: font, Size: ptr(doc.F32(fclamp(float32(h*0.75), 8, 200)))}
	if v, ok := vattr("fillcolor"); ok {
		if c, ok := vmlColor(v); ok {
			props.Color = ptr(doc.FixedColor(c))
		}
	}
	id := r.doc.AddPart(doc.PartTextBox, doc.Vec[doc.Block]{paraBlock(doc.ParagraphWithText(text, props))})
	if part, ok := r.doc.Parts[id]; ok {
		part.Body.Anchor = doc.VAlignCenter
		part.Body.Inset = &[4]uint32{}
		r.doc.Parts[id] = part
	}
	return doc.Object{InlineObject: doc.Shape{Kind: doc.ShapeTextBox, W: doc.F32(w), H: doc.F32(h), StrokeWidth: doc.F32(strokeWidth),
		Float: float, Story: ptr(id), Extra: doc.ShapeExtra{Warp: warp}}}
}

// isFontQuote is what's trimmed off a VML font-family: quotes and whitespace.
func isFontQuote(c rune) bool { return c == '"' || c == '\'' || rustcompat.IsWhitespace(c) }

// vmlSpt is a VML shape's preset type (o:spt): its own, its shapetype's, or the number in Word's
// shapetype id _x0000_tNN (typeID), Word writing each shapetype only at its first use.
func vmlSpt(shape, shapetype *El, typeID *string) (uint32, bool) {
	num := func(v string) (uint32, bool) {
		n, ok := rustcompat.ParseUint(rustcompat.Trim(v), 32)
		return uint32(n), ok
	}
	if v, ok := shape.Attr("o:spt"); ok {
		if n, ok := num(v); ok {
			return n, true
		}
	}
	if shapetype != nil {
		if v, ok := shapetype.Attr("o:spt"); ok {
			if n, ok := num(v); ok {
				return n, true
			}
		}
	}
	if typeID != nil {
		if n, ok := strings.CutPrefix(*typeID, "_x0000_t"); ok {
			return num(n)
		}
	}
	return 0, false
}

// vmlCoordpair reads a VML coordsize="w,h" or coordorigin="x,y" pair (ECMA-376 Part 4
// §14.1.2): the member space of a group, in its own unitless coordinates.
func vmlCoordpair(e *El, name string) (float32, float32, bool) {
	v, ok := e.Attr(name)
	if !ok {
		return 0, 0, false
	}
	xs, ys, ok := strings.Cut(v, ",")
	if !ok {
		return 0, 0, false
	}
	x, okx := measure(rustcompat.Trim(xs), 1)
	y, oky := measure(rustcompat.Trim(ys), 1)
	if !okx || !oky {
		return 0, 0, false
	}
	return geom.Finite(x), geom.Finite(y), true
}

// vmlTextbox is the first v:textbox under e holding text, with its w:txbxContent (depth-first,
// bounded): in a group, a member's margins go with that member's text.
func vmlTextbox(e *El, depth int) (*El, *El, bool) {
	if depth > MaxDepth {
		return nil, nil, false
	}
	for _, c := range e.Els() {
		if t := c.Child("w:txbxContent"); t != nil && c.Name == "v:textbox" {
			return c, t, true
		}
		if tb, t, ok := vmlTextbox(c, depth+1); ok {
			return tb, t, true
		}
	}
	return nil, nil, false
}

// vmlStyle is the value of property key in a VML style attribute (CSS declarations; the last one
// wins).
func vmlStyle(style, key string) (string, bool) {
	decls := strings.Split(style, ";")
	for i := len(decls) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(decls[i], ":")
		if ok && asciiEqualFold(rustcompat.Trim(k), key) {
			return rustcompat.Trim(v), true
		}
	}
	return "", false
}

// vmlFloat is the placement of a VML shape (ECMA-376 Part 4, VML): position:absolute in its style
// makes it float, offset by left/margin-left and top/margin-top from the area named by
// mso-position-horizontal-relative / mso-position-vertical-relative (unless aligned with
// mso-position-horizontal / mso-position-vertical), wrapped as its w10:wrap says. Without wrapping
// (none, or no w10:wrap) it's in front of the text, or behind it at a negative z-index. Anything
// else stays inline.
func vmlFloat(shape *El, style string, inHF bool) doc.Float {
	get := func(k string) (string, bool) { return vmlStyle(style, k) }
	vis, ok := get("visibility")
	hidden := ok && asciiEqualFold(vis, "hidden")
	if p, ok := get("position"); !ok || !asciiEqualFold(p, "absolute") {
		// Inline: only the turn (an OLE object turned in WordCraft is saved so).
		f := doc.Float{Hidden: hidden}
		f.SetSpin(vmlSpin(style))
		return f
	}
	off := func(k string) float32 {
		if v, ok := get(k); ok {
			if m, ok := measure(v, 1); ok {
				return m
			}
		}
		return 0
	}
	behind := false
	if z, ok := get("z-index"); ok {
		if n, ok := intOf(z); ok {
			behind = n < 0
		}
	}
	wrapType := "none"
	if wr := shape.Child("w10:wrap"); wr != nil {
		if t, ok := wr.Attr("type"); ok {
			wrapType = t
		}
	}
	var wrap doc.Wrap
	switch {
	case wrapType == "square":
		wrap = doc.WrapSquare
	case wrapType == "tight":
		wrap = doc.WrapTight
	case wrapType == "through":
		wrap = doc.WrapThrough
	case wrapType == "topAndBottom":
		wrap = doc.WrapTopAndBottom
	case behind:
		wrap = doc.WrapBehindText
	default:
		wrap = doc.WrapInFrontOfText
	}
	hr, _ := get("mso-position-horizontal-relative")
	hRel := doc.AnchorColumn
	switch hr {
	case "page":
		hRel = doc.AnchorPage
	case "margin":
		hRel = doc.AnchorMargin
	case "char":
		hRel = doc.AnchorCharacter
	case "left-margin-area":
		hRel = doc.AnchorLeftMargin
	case "right-margin-area":
		hRel = doc.AnchorRightMargin
	case "inner-margin-area":
		hRel = doc.AnchorInsideMargin
	case "outer-margin-area":
		hRel = doc.AnchorOutsideMargin
	}
	vr, _ := get("mso-position-vertical-relative")
	vRel := doc.AnchorParagraph
	switch vr {
	case "page":
		vRel = doc.AnchorPage
	case "margin":
		vRel = doc.AnchorMargin
	case "line":
		vRel = doc.AnchorLine
	case "top-margin-area":
		vRel = doc.AnchorTopMargin
	case "bottom-margin-area":
		vRel = doc.AnchorBottomMargin
	case "inner-margin-area":
		vRel = doc.AnchorInsideMargin
	case "outer-margin-area":
		vRel = doc.AnchorOutsideMargin
	}
	dist := func(k string) float32 { return fclamp(off(k), 0, 1584) }
	align := func(k string) *doc.FloatAlign {
		if v, ok := get(k); ok {
			if a, ok := doc.FloatAlignFromOOXML(v); ok {
				return &a
			}
		}
		return nil
	}
	f := doc.Float{
		Wrap:       wrap,
		HRel:       hRel,
		VRel:       vRel,
		X:          doc.F32(fclamp(off("left")+off("margin-left"), -MaxLenPt, MaxLenPt)),
		Y:          doc.F32(fclamp(off("top")+off("margin-top"), -MaxLenPt, MaxLenPt)),
		HAlign:     align("mso-position-horizontal"),
		VAlign:     align("mso-position-vertical"),
		Dist:       doc.F32(rustcompat.Max(dist("mso-wrap-distance-left"), dist("mso-wrap-distance-right"))),
		DistTop:    doc.F32(dist("mso-wrap-distance-top")),
		DistBottom: doc.F32(dist("mso-wrap-distance-bottom")),
		Hidden:     hidden,
		// Word for Mac draws a VML shape anchored in a header table cell on the page whatever
		// o:allowincell says, as for Word's picture and text watermarks.
		OutsideCell: inHF,
	}
	f.SetSpin(vmlSpin(style))
	return f
}

// vmlWatermark is Word's text watermark (Design › Watermark): a VML shape in a header whose
// v:textpath draws its string as text (ECMA-376 Part 4, VML textpath), in the textpath's
// font-family, filled with the shape's fillcolor, semitransparent when its v:fill has an opacity
// below 1, and diagonal when the shape is turned. Only Word's watermark shape counts (its id), or
// text placed absolutely and centred both ways; other header WordArt (a logo or a title) is
// false.
func vmlWatermark(shape *El) (doc.Watermark, bool) {
	tp := shape.Child("v:textpath")
	if tp == nil {
		return doc.Watermark{}, false
	}
	if v, ok := tp.Attr("on"); ok && offVal(v) {
		return doc.Watermark{}, false
	}
	style := attr(shape, "style")
	centred := func(k string) bool {
		v, ok := vmlStyle(style, k)
		return ok && asciiEqualFold(v, "center")
	}
	id, ok := shape.Attr("id")
	word := ok && strings.HasPrefix(id, "PowerPlusWaterMarkObject")
	p, ok := vmlStyle(style, "position")
	placed := ok && asciiEqualFold(p, "absolute") && centred("mso-position-horizontal") && centred("mso-position-vertical")
	if !word && !placed {
		return doc.Watermark{}, false
	}
	s, ok := tp.Attr("string")
	if !ok {
		return doc.Watermark{}, false
	}
	text := takeChars(s, 200)
	if rustcompat.Trim(text) == "" {
		return doc.Watermark{}, false
	}
	var wm doc.Watermark
	wm.SetDefaults()
	wm.Text = text
	if f, ok := vmlStyle(attr(tp, "style"), "font-family"); ok {
		if f = takeChars(strings.TrimFunc(f, isFontQuote), 100); f != "" {
			wm.Font = f
		}
	}
	if v, ok := shape.Attr("fillcolor"); ok {
		if c, ok := vmlColor(v); ok {
			wm.Color = c
		}
	}
	opacity := float32(1)
	if fe := shape.Child("v:fill"); fe != nil {
		if v, ok := fe.Attr("opacity"); ok {
			if f, ok := vmlFraction(v); ok {
				opacity = f
			}
		}
	}
	wm.Diagonal = vmlSpin(style).Deg != 0
	wm.Semitransparent = opacity < 1
	return wm, true
}

// vmlFraction reads a VML fraction (opacity): a number, a percentage, or 65536ths with an f
// suffix; 0..=1.
func vmlFraction(v string) (float32, bool) {
	v = rustcompat.Trim(v)
	var f float32
	var ok bool
	if n, cut := strings.CutSuffix(v, "f"); cut {
		f, ok = rustcompat.ParseF32(rustcompat.Trim(n))
		f /= 65_536
	} else if n, cut := strings.CutSuffix(v, "%"); cut {
		f, ok = rustcompat.ParseF32(rustcompat.Trim(n))
		f /= 100
	} else {
		f, ok = rustcompat.ParseF32(v)
	}
	if !ok || f != f || f > maxF32 || f < -maxF32 {
		return 0, false
	}
	return fclamp(f, 0, 1), true
}

// maxF32 is the largest finite float32.
const maxF32 = 3.40282346638528859811704183484516925440e+38

// vmlSpin is a VML shape's turn: rotation in degrees, or 65536ths of one with an fd suffix; flip:
// x and/or y.
func vmlSpin(style string) geom.Spin {
	rot := float32(0)
	if v, ok := vmlStyle(style, "rotation"); ok {
		if fd, cut := strings.CutSuffix(v, "fd"); cut {
			if f, ok := rustcompat.ParseF32(rustcompat.Trim(fd)); ok {
				rot = f / 65_536
			}
		} else if f, ok := rustcompat.ParseF32(rustcompat.Trim(v)); ok {
			rot = f
		}
	}
	flip, _ := vmlStyle(style, "flip")
	return geom.NewSpin(rot, strings.Contains(flip, "x"), strings.Contains(flip, "y"))
}

// readObject reads an OLE object (w:object, ECMA-376 §17.3.3.19): shown as its picture (VML, or
// a DrawingML picture in w:drawing), and kept whole so saving writes the object back.
func (r *reader) readObject(sc *storyCtx, obj *El, rels Rels) (doc.Object, bool) {
	var o doc.Object
	if all := r.readVMLAll(sc, obj, rels); len(all) > 0 {
		o = all[0]
	} else {
		d := obj.Child("w:drawing")
		if d == nil {
			return doc.Object{}, false
		}
		var ok bool
		if o, ok = r.readDrawing(sc, d, rels); !ok {
			return doc.Object{}, false
		}
	}
	if img, ok := o.InlineObject.(doc.Image); ok {
		img.OLE = nil
		if e, ok := r.keepEmbedded(obj, rels, nil); ok {
			img.OLE = e
		}
		o = doc.Object{InlineObject: img}
	}
	return o, true
}
