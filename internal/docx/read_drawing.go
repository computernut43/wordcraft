package docx

// Drawings (w:drawing): their frame, anchoring and wrapping, and pictures (Rust
// crates/docx/src/read/story.rs read_drawing, read_pic, nv_alt, anchor_float, wrap_polygon,
// xfrm_spin). Charts, diagrams, ink, canvases, groups and shapes aren't ported yet: meeting one
// makes the read refuse the file.

import (
	"slices"
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
	"github.com/computernut43/wordcraft/internal/geom"
)

// inkURI is the a:graphicData URI of native ink.
const inkURI = "http://schemas.microsoft.com/office/word/2010/wordprocessingInk"

// readDrawing reads a w:drawing: false when it holds nothing to show.
func (r *reader) readDrawing(sc *storyCtx, d *El, rels Rels) (doc.Object, bool) {
	c, anchored := d.Child("wp:inline"), false
	if c == nil {
		if c = d.Child("wp:anchor"); c == nil {
			return doc.Object{}, false
		}
		anchored = true
	}
	ext := c.Child("wp:extent")
	dim := func(n string) float32 {
		v := float32(0)
		if ext != nil {
			if m, ok := attrMeasure(ext, n, 12_700); ok {
				v = m
			}
		}
		return fclamp(v, 0, MaxLenPt)
	}
	w, h := dim("cx"), dim("cy")
	docPr := c.Child("wp:docPr")
	alt, meta := nvAlt(docPr)
	var float doc.Float
	if anchored {
		float = anchorFloat(c)
	}
	float.Alt = meta
	float.Hidden = docPr != nil && onOffAttr(docPr, "hidden")
	if e := c.Child("wp:effectExtent"); e != nil {
		for i, n := range []string{"l", "t", "r", "b"} {
			// Negative for a rotated object narrower than its frame; settled below.
			v, _ := attrMeasure(e, n, 12_700)
			float.Effect[i] = doc.F32(fclamp(v, -1584, 1584))
		}
	}
	g := c.Child("a:graphic")
	if g == nil {
		return doc.Object{}, false
	}
	gd := g.Child("a:graphicData")
	if gd == nil {
		return doc.Object{}, false
	}
	// Checked by URI before the picture search: a diagram's data can hold a picture further down.
	uri := attr(gd, "uri")
	switch {
	case uri == inkURI:
		r.unported("ink")
		return doc.Object{}, false
	case strings.HasSuffix(uri, "/chart"), strings.HasSuffix(uri, "/diagram"):
		r.unported("charts and diagrams")
		return doc.Object{}, false
	case gd.Child("wpc:wpc") != nil:
		r.unported("drawing canvases")
		return doc.Object{}, false
	case gd.Child("wpg:wgp") != nil:
		r.unported("drawing groups")
		return doc.Object{}, false
	}
	var obj doc.Object
	if gd.Find("a:blip") != nil {
		o, ok := r.readPic(gd, rels, w, h, alt, float)
		if !ok {
			return doc.Object{}, false
		}
		obj = o
	} else {
		wsp := gd.Find("wps:wsp")
		if wsp == nil {
			return doc.Object{}, false
		}
		// Not ported yet: freeforms (custom geometry, ink drawn as freeforms) and colours in
		// linear RGB (a:scrgbClr needs glibc's pow).
		if sp := wsp.Child("wps:spPr"); sp != nil && sp.Child("a:custGeom") != nil {
			r.unported("freeform shapes")
			return doc.Object{}, false
		}
		if wsp.Find("a:scrgbClr") != nil {
			r.unported("linear RGB colours (a:scrgbClr)")
			return doc.Object{}, false
		}
		obj = r.readWsp(sc, wsp, rels, w, h, float)
	}
	if alt != "" {
		if o, ok := obj.WithAltText(alt); ok {
			obj = o
		}
	}
	if docPr != nil {
		id, has := docPr.Attr("id")
		r.setShapeID(&obj, id, has)
	}
	// Word's effect extent also covers a rotated object's overhang; the model keeps that apart
	// (it follows from the angle), so take it back out.
	settle := func(f *doc.Float) {
		px, py := f.SpinPad(w, h)
		for i, p := range []float32{px, py, px, py} {
			// Zero adds no effects room when rotation contracts this axis.
			if e := float32(f.Effect[i]); e != 0 {
				f.Effect[i] = doc.F32(fclamp(e-p, 0, 1584))
			}
		}
	}
	switch v := obj.InlineObject.(type) {
	case doc.Image:
		settle(&v.Float)
		obj = doc.Object{InlineObject: v}
	case doc.Shape:
		settle(&v.Float)
		obj = doc.Object{InlineObject: v}
	}
	return obj, true
}

// readPic reads a picture (an a:blip under pic).
func (r *reader) readPic(pic *El, rels Rels, w, h float32, alt string, float doc.Float) (doc.Object, bool) {
	blip := pic.Find("a:blip")
	if blip == nil {
		return doc.Object{}, false
	}
	float.SetSpin(xfrmSpin(pic.Find("pic:spPr")))
	id, ok := blip.Attr("r:embed")
	if !ok {
		return doc.Object{}, false
	}
	media, ok := r.mediaFor(rels, id)
	if !ok {
		return doc.Object{}, false
	}
	var crop [4]doc.F32
	if sr := pic.Find("a:srcRect"); sr != nil {
		for i, n := range []string{"l", "t", "r", "b"} {
			if v, ok := sr.Attr(n); ok {
				if k, ok := intOf(v); ok {
					crop[i] = doc.F32(fclamp(float32(min(max(k, 0), 100_000))/100_000, 0, 1))
				}
			}
		}
	}
	return doc.Object{InlineObject: doc.Image{Media: media, W: doc.F32(w), H: doc.F32(h), Alt: alt, Float: float, Crop: crop}}, true
}

// nvAlt is the alternative text of a wp:docPr, pic:cNvPr or wps:cNvPr (ECMA-376
// CT_NonVisualDrawingProps): the description (the title when there is none), and the title and
// decorative mark (the adec:decorative extension of [MS-ODRAWXML]).
func nvAlt(p *El) (string, doc.AltText) {
	if p == nil {
		return "", doc.AltText{}
	}
	title := doc.CapAlt(attr(p, "title"))
	descr := title
	if d, ok := p.Attr("descr"); ok && d != "" {
		descr = doc.CapAlt(d)
	}
	decorative := false
	if l := p.Child("a:extLst"); l != nil {
		for _, x := range l.Children("a:ext") {
			if u, ok := x.Attr("uri"); !ok || !asciiEqualFold(u, DecorativeExt) {
				continue
			}
			if slices.ContainsFunc(x.Els(), func(d *El) bool { return d.Local() == "decorative" && onOffAttr(d, "val") }) {
				decorative = true
			}
		}
	}
	return descr, doc.AltText{Title: title, Decorative: decorative}
}

func anchorFloat(c *El) doc.Float {
	f := doc.Float{Wrap: doc.WrapSquare}
	behind := onOffAttr(c, "behindDoc")
	// Word wraps a position relative to the page's size (wp14:pctPosVOffset) in
	// mc:AlternateContent; its Fallback gives the same position as an offset.
	var kids []*El
	for _, k := range c.Els() {
		if k.Name == "mc:AlternateContent" {
			if fb := k.Child("mc:Fallback"); fb != nil {
				kids = append(kids, fb.Els()...)
			}
			continue
		}
		kids = append(kids, k)
	}
	wrapText := func(k *El) doc.WrapText {
		if v, ok := k.Attr("wrapText"); ok {
			if t, ok := doc.WrapTextFromOOXML(v); ok {
				return t
			}
		}
		return doc.WrapBothSides
	}
	for _, k := range kids {
		switch k.Name {
		case "wp:wrapSquare":
			f.Wrap = doc.WrapSquare
			f.WrapText = wrapText(k)
		case "wp:wrapTight", "wp:wrapThrough":
			f.Wrap = doc.WrapThrough
			if k.Name == "wp:wrapTight" {
				f.Wrap = doc.WrapTight
			}
			f.WrapText = wrapText(k)
			f.WrapPolygon = nil
			if p := k.Child("wp:wrapPolygon"); p != nil {
				if poly, ok := wrapPolygon(p); ok {
					f.WrapPolygon = &poly
				}
			}
		case "wp:wrapTopAndBottom":
			f.Wrap = doc.WrapTopAndBottom
		case "wp:wrapNone":
			f.Wrap = doc.WrapInFrontOfText
			if behind {
				f.Wrap = doc.WrapBehindText
			}
		case "wp:positionH", "wp:positionV":
			horiz := k.Name == "wp:positionH"
			rel := doc.AnchorParagraph
			if horiz {
				rel = doc.AnchorColumn
			}
			switch from := attr(k, "relativeFrom"); {
			case from == "page":
				rel = doc.AnchorPage
			case from == "margin":
				rel = doc.AnchorMargin
			case from == "leftMargin" && horiz:
				rel = doc.AnchorLeftMargin
			case from == "rightMargin" && horiz:
				rel = doc.AnchorRightMargin
			case from == "insideMargin":
				rel = doc.AnchorInsideMargin
			case from == "outsideMargin":
				rel = doc.AnchorOutsideMargin
			case from == "character" && horiz:
				rel = doc.AnchorCharacter
			case from == "topMargin" && !horiz:
				rel = doc.AnchorTopMargin
			case from == "bottomMargin" && !horiz:
				rel = doc.AnchorBottomMargin
			case from == "line" && !horiz:
				rel = doc.AnchorLine
			}
			var al *doc.FloatAlign
			if a := k.Child("wp:align"); a != nil {
				if v, ok := doc.FloatAlignFromOOXML(rustcompat.Trim(a.Text())); ok {
					al = &v
				}
			}
			off := float32(0)
			if o := k.Child("wp:posOffset"); o != nil {
				if v, ok := measure(o.Text(), 12_700); ok {
					off = v
				}
			}
			if horiz {
				f.HRel, f.HAlign, f.X = rel, al, doc.F32(off)
			} else {
				f.VRel, f.VAlign, f.Y = rel, al, doc.F32(off)
			}
		}
	}
	dist := func(n string) float32 {
		v, _ := attrMeasure(c, n, 12_700)
		return fclamp(v, 0, 1584)
	}
	f.Dist = doc.F32(rustcompat.Max(dist("distL"), dist("distR")))
	f.DistTop = doc.F32(dist("distT"))
	f.DistBottom = doc.F32(dist("distB"))
	// Required by the schema; a file leaving it out lays out in the cell.
	_, has := c.Attr("layoutInCell")
	f.OutsideCell = !onOffAttr(c, "layoutInCell") && has
	return f
}

// wrapPolygon reads a wp:wrapPolygon: wp:start, then the wp:lineTo points (ECMA-376 Part 1,
// §20.4.2.16).
func wrapPolygon(e *El) (doc.WrapPolygon, bool) {
	var pts [][2]int64
	taken := 0
	for _, k := range e.Els() {
		if k.Name != "wp:start" && k.Name != "wp:lineTo" {
			continue
		}
		if taken == doc.MaxWrapPoints+1 {
			break
		}
		taken++
		xs, okx := k.Attr("x")
		ys, oky := k.Attr("y")
		if !okx || !oky {
			continue
		}
		x, okx := intOf(xs)
		y, oky := intOf(ys)
		if okx && oky {
			pts = append(pts, [2]int64{x, y})
		}
	}
	// The last point usually repeats the start to close the polygon.
	if len(pts) > 3 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	edited := onOffAttr(e, "edited")
	// An unedited polygon round the whole frame says nothing the object doesn't: keep deriving
	// the outline.
	const s = int64(doc.WrapSpace)
	corners := slices.Clone(pts)
	slices.SortFunc(corners, func(a, b [2]int64) int {
		if a[0] != b[0] {
			return cmpI64(a[0], b[0])
		}
		return cmpI64(a[1], b[1])
	})
	if !edited && slices.Equal(corners, [][2]int64{{0, 0}, {0, s}, {s, 0}, {s, s}}) {
		return doc.WrapPolygon{}, false
	}
	return doc.NewWrapPolygon(edited, pts)
}

func cmpI64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// xfrmSpin is the rotation and flips of the a:xfrm in shape properties sppr (ECMA-376 §20.1.7.6:
// rot in 60000ths of a degree, clockwise; flipH, flipV). None when missing or junk.
func xfrmSpin(sppr *El) geom.Spin {
	var x *El
	if sppr != nil {
		if sppr.Name == "a:xfrm" || sppr.Name == "w14:xfrm" {
			x = sppr
		} else {
			x = sppr.Child("a:xfrm")
		}
	}
	if x == nil {
		return geom.Spin{}
	}
	rot := float32(0)
	if v, ok := x.Attr("rot"); ok {
		if n, ok := intOf(v); ok {
			rot = float32(rustcompat.RemEuclid(n, 21_600_000)) / 60_000
		}
	}
	return geom.NewSpin(rot, onOffAttr(x, "flipH"), onOffAttr(x, "flipV"))
}

// maxStoryDepth is the deepest text-box-in-text-box nesting read.
const maxStoryDepth = 4

// readWsp reads a Word shape (wps:wsp) of w × h.
func (r *reader) readWsp(sc *storyCtx, wsp *El, rels Rels, w, h float32, float doc.Float) doc.Object {
	sppr := wsp.Child("wps:spPr")
	float.SetSpin(xfrmSpin(sppr))
	prst := "rect"
	var geom *El
	if sppr != nil {
		if geom = sppr.Child("a:prstGeom"); geom != nil {
			if v, ok := geom.Attr("prst"); ok {
				prst = v
			}
		}
	}
	var txbx *El
	if t := wsp.Child("wps:txbx"); t != nil {
		txbx = t.Child("w:txbxContent")
	}
	kind := presetKind(prst)
	isTB := false
	if c := wsp.Child("wps:cNvSpPr"); c != nil {
		v, _ := c.Attr("txBox")
		isTB = v == "1" || v == "true"
	}
	if isTB || (txbx != nil && kind == doc.ShapeRectangle) {
		kind = doc.ShapeTextBox
	}
	theme := r.doc.Settings.ThemeColors
	// Explicit paint, else the theme's through the style references.
	paint := shapePaint(sppr, wsp.Child("wps:style"), theme)
	fill := paint.fill
	if kind.IsOpen() {
		fill = nil
	}
	var ln *El
	var effects doc.ShapeEffects
	if sppr != nil {
		ln = sppr.Child("a:ln")
		if l := sppr.Child("a:effectLst"); l != nil {
			effects = effectList(l)
		}
	}
	var story *uint32
	if txbx != nil && sc.storyDepth < maxStoryDepth {
		story = ptr(r.readTextbox(sc, txbx, rels))
	}
	// A box continuing another's text (Create Link): its own, empty story, linked up once the
	// whole document is read.
	parseU32 := func(e *El, n string) (uint32, bool) {
		v, ok := e.Attr(n)
		if !ok {
			return 0, false
		}
		k, ok := rustcompat.ParseUint(rustcompat.Trim(v), 32)
		return uint32(k), ok
	}
	if l := wsp.Child("wps:linkedTxbx"); l != nil && txbx == nil {
		kind = doc.ShapeTextBox
		id, okID := parseU32(l, "id")
		seq, okSeq := parseU32(l, "seq")
		if okID && okSeq && seq > 0 {
			part := r.doc.AddPart(doc.PartTextBox, doc.Vec[doc.Block]{})
			r.txbxLinks = append(r.txbxLinks, [3]uint32{id, seq, part})
			story = &part
		}
	} else if story != nil {
		if t := wsp.Child("wps:txbx"); t != nil {
			if id, ok := parseU32(t, "id"); ok {
				r.txbxLinks = append(r.txbxLinks, [3]uint32{id, 0, *story})
			}
		}
	}
	// Text direction and alignment (wps:bodyPr).
	bp := wsp.Child("wps:bodyPr")
	if story != nil && bp != nil {
		if part, ok := r.doc.Parts[*story]; ok {
			if v, ok := bp.Attr("vert"); ok {
				part.Body.Vert = doc.TextVertFromOOXML(v)
			}
			if a, ok := bp.Attr("anchor"); ok {
				part.Body.Anchor = doc.ValignFromAnchor(a)
			}
			// Internal margins, each defaulting to Word's.
			inset := doc.TextBoxInset
			for i, n := range []string{"lIns", "tIns", "rIns", "bIns"} {
				if v, ok := bp.Attr(n); ok {
					if k, ok := intOf(v); ok {
						inset[i] = uint32(min(max(k, 0), doc.MaxTextBoxInset))
					}
				}
			}
			part.Body.Inset = nil
			if inset != doc.TextBoxInset {
				part.Body.Inset = &inset
			}
			part.Body.AutoFit = bp.Child("a:spAutoFit") != nil
			r.doc.Parts[*story] = part
		}
	}
	// The shape style's text colour (a:fontRef, often lt1: white on the accent fill).
	if story != nil {
		if st := wsp.Child("wps:style"); st != nil {
			if c := st.Child("a:fontRef"); c != nil {
				if rgb, ok := drawingColor(c, theme); ok {
					if part, ok := r.doc.Parts[*story]; ok {
						part.Body.AutoColor = &rgb
						r.doc.Parts[*story] = part
					}
				}
			}
		}
	}
	extra := doc.ShapeExtra{Dash: lineDash(ln)}
	// A rounded rectangle's own corner (adj); other presets drawn as rounded rectangles keep the
	// default.
	if prst == "roundRect" && geom != nil {
		if v, ok := presetAdj(geom, "adj"); ok {
			extra.Corner = ptr(uint32(min(max(v, 0), doc.MaxCorner)))
		}
	}
	// Arrowheads: any end type but none is drawn as a triangle.
	arrow := func(tag string) bool {
		if ln == nil {
			return false
		}
		e := ln.Child(tag)
		if e == nil {
			return false
		}
		t, ok := e.Attr("type")
		return ok && t != "none"
	}
	extra.ArrowStart = arrow("a:headEnd")
	extra.ArrowEnd = arrow("a:tailEnd")
	// A connector's glued ends.
	if cn := wsp.Child("wps:cNvCnPr"); cn != nil {
		end := func(tag string) *doc.ConnEnd {
			e := cn.Child(tag)
			if e == nil {
				return nil
			}
			v, ok := e.Attr("id")
			if !ok {
				return nil
			}
			id, ok := u32Of(v)
			if !ok || id == 0 {
				return nil
			}
			site := uint32(0)
			if s, ok := e.Attr("idx"); ok {
				if n, ok := u32Of(s); ok {
					site = n
				}
			}
			return &doc.ConnEnd{ID: id, Site: min(site, doc.MaxSite)}
		}
		extra.Start = end("a:stCxn")
		extra.End = end("a:endCxn")
		for _, e := range []*doc.ConnEnd{extra.Start, extra.End} {
			if e != nil {
				r.glued[e.ID] = true
			}
		}
	}
	if bp != nil {
		if e := bp.Child("a:prstTxWarp"); e != nil {
			if tw, ok := textWarp(e); ok {
				extra.Warp = &tw
			}
		}
	}
	return doc.Object{InlineObject: doc.Shape{Kind: kind, W: doc.F32(w), H: doc.F32(h), Fill: fill, Stroke: paint.stroke,
		StrokeWidth: doc.F32(paint.strokeWidth), Float: float, Story: story, Effects: effects, Extra: extra}}
}

// setShapeID keeps drawing id id on a shape read from a file.
func (r *reader) setShapeID(obj *doc.Object, id string, has bool) {
	s, ok := obj.InlineObject.(doc.Shape)
	if !ok || !has {
		return
	}
	if v, ok := u32Of(id); ok && v != 0 {
		s.Extra.ID = v
		obj.InlineObject = s
		r.shapeIDs = true
	}
}

// readTextbox reads a text box's story into a new part.
func (r *reader) readTextbox(sc *storyCtx, content *El, rels Rels) uint32 {
	inner := &storyCtx{storyDepth: sc.storyDepth + 1}
	blocks := doc.Vec[doc.Block]{}
	r.readBlocks(inner, content, rels, &blocks, 0)
	r.flushPending(inner, &blocks)
	return r.doc.AddPart(doc.PartTextBox, blocks)
}

// textWarp reads a:prstTxWarp.
func textWarp(e *El) (doc.TextWarp, bool) {
	p, ok := e.Attr("prst")
	if !ok {
		return doc.TextWarp{}, false
	}
	w, ok := doc.NewTextWarp(p)
	if !ok {
		return doc.TextWarp{}, false
	}
	if l := e.Child("a:avLst"); l != nil {
		for _, gd := range l.Children("a:gd") {
			name, ok1 := gd.Attr("name")
			f, ok2 := gd.Attr("fmla")
			if !ok1 || !ok2 {
				continue
			}
			if v, ok := fmlaVal(f); ok {
				w.SetAdj(name, v)
			}
		}
	}
	return w, true
}

// fmlaVal reads a guide formula "val N".
func fmlaVal(f string) (int64, bool) {
	rest, ok := strings.CutPrefix(rustcompat.Trim(f), "val")
	if !ok {
		return 0, false
	}
	return rustcompat.ParseInt(rustcompat.Trim(rest), 64)
}

// presetAdj is a preset geometry's adjust value name (a:avLst/a:gd with fmla="val N").
func presetAdj(geom *El, name string) (int64, bool) {
	l := geom.Child("a:avLst")
	if l == nil {
		return 0, false
	}
	for _, gd := range l.Children("a:gd") {
		if n, ok := gd.Attr("name"); ok && n == name {
			f, ok := gd.Attr("fmla")
			if !ok {
				return 0, false
			}
			return fmlaVal(f)
		}
	}
	return 0, false
}

func effectList(l *El) doc.ShapeEffects {
	pt := func(e *El, n string) doc.F32 {
		v, _ := attrMeasure(e, n, 12_700)
		return doc.F32(v)
	}
	var fx doc.ShapeEffects
	if e := l.Child("a:outerShdw"); e != nil {
		c, ok, transparency := effectColor(e)
		if !ok {
			c = doc.Black
		}
		// dir: 60000ths of a degree, clockwise.
		angle := float32(0)
		if v, ok := e.Attr("dir"); ok {
			if n, ok := intOf(v); ok {
				angle = float32(rustcompat.RemEuclid(n, 21_600_000)) / 60_000
			}
		}
		// rotWithShape is true unless it says otherwise.
		rot := true
		if v, ok := e.Attr("rotWithShape"); ok {
			rot = !(v == "0" || v == "false" || v == "off")
		}
		fx.Shadow = &doc.Shadow{Color: c, Transparency: doc.F32(transparency), Blur: pt(e, "blurRad"), Distance: pt(e, "dist"), Angle: doc.F32(angle), RotWithShape: rot}
	}
	if e := l.Child("a:glow"); e != nil {
		c, ok, transparency := effectColor(e)
		if !ok {
			c = doc.DefaultGlow.Color
		}
		fx.Glow = &doc.Glow{Color: c, Size: pt(e, "rad"), Transparency: doc.F32(transparency)}
	}
	if e := l.Child("a:softEdge"); e != nil {
		fx.SoftEdge = ptr(pt(e, "rad"))
	}
	return fx.Sanitized()
}

// effectColor is the colour inside an effect (a:srgbClr, a:prstClr, a:sysClr; theme colours
// aren't resolved here) and its transparency, percent (from a:alpha, opaque when absent).
func effectColor(e *El) (doc.RGB, bool, float32) {
	var c *El
	for _, k := range e.Els() {
		if isColorEl(k) {
			c = k
			break
		}
	}
	if c == nil {
		return doc.RGB{}, false, 0
	}
	var rgb doc.RGB
	ok := false
	switch c.Name {
	case "a:srgbClr":
		rgb, ok = parseRGBAttr(c, "val")
	case "a:sysClr":
		rgb, ok = parseRGBAttr(c, "lastClr")
	case "a:prstClr":
		switch attr(c, "val") {
		case "black":
			rgb, ok = doc.Black, true
		case "white":
			rgb, ok = doc.White, true
		case "gray", "grey":
			rgb, ok = doc.RGB{R: 0x80, G: 0x80, B: 0x80}, true
		}
	}
	// a:alpha: opacity in 1000ths of a percent.
	opacity := float32(100)
	if a := c.Child("a:alpha"); a != nil {
		if v, ok := a.Attr("val"); ok {
			if n, ok := intOf(v); ok {
				opacity = float32(min(max(n, 0), 100_000)) / 1000
			}
		}
	}
	return rgb, ok, 100 - opacity
}

// linkTextBoxes links the text boxes read as (id, seq, part) into chains: each id's boxes in seq
// order after the box holding the text. A repeated seq, a chain without its first box or more
// than doc.MaxLinkedBoxes boxes: the extra boxes stay apart.
func linkTextBoxes(d *doc.Document, links [][3]uint32) {
	slices.SortStableFunc(links, func(a, b [3]uint32) int {
		if a[0] != b[0] {
			return cmpU32(a[0], b[0])
		}
		return cmpU32(a[1], b[1])
	})
	links = slices.CompactFunc(links, func(a, b [3]uint32) bool { return a[0] == b[0] && a[1] == b[1] })
	for i := 0; i < len(links); {
		j := i
		for j < len(links) && links[j][0] == links[i][0] {
			j++
		}
		group := links[i:j]
		i = j
		if group[0][1] != 0 {
			continue
		}
		chain := group[:min(len(group), doc.MaxLinkedBoxes)]
		for k := 0; k+1 < len(chain); k++ {
			a, b := chain[k][2], chain[k+1][2]
			if p, ok := d.Parts[a]; ok {
				p.Body.Next = ptr(b)
				d.Parts[a] = p
			}
		}
	}
}

func cmpU32(a, b uint32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// keepGluedIDs drops the drawing ids read with shapes, except those connectors are glued to (so
// a shape without connectors reads back as it was written).
func keepGluedIDs(d *doc.Document, glued map[uint32]bool) {
	var walk func(bl doc.Vec[doc.Block], depth int)
	walk = func(bl doc.Vec[doc.Block], depth int) {
		if depth > MaxDepth {
			return
		}
		for _, b := range bl {
			switch {
			case b.Para != nil:
				for i, o := range b.Para.Objects {
					if s, ok := o.InlineObject.(doc.Shape); ok && s.Extra.ID != 0 && !glued[s.Extra.ID] {
						s.Extra.ID = 0
						b.Para.Objects[i] = doc.Object{InlineObject: s}
					}
				}
			case b.Table != nil:
				for _, row := range b.Table.Rows {
					for _, c := range row.Cells {
						walk(c.Blocks, depth+1)
					}
				}
			}
		}
	}
	walk(d.Body, 0)
	for _, p := range d.Parts {
		walk(p.Blocks, 0)
	}
}
