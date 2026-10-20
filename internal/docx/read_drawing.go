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
func (r *reader) readDrawing(d *El, rels Rels) (doc.Object, bool) {
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
	case gd.Find("a:blip") == nil:
		if gd.Find("wps:wsp") != nil {
			r.unported("shapes and text boxes")
		}
		return doc.Object{}, false
	}
	obj, ok := r.readPic(gd, rels, w, h, alt, float)
	if !ok {
		return doc.Object{}, false
	}
	if alt != "" {
		if o, ok := obj.WithAltText(alt); ok {
			obj = o
		}
	}
	// Word's effect extent also covers a rotated object's overhang; the model keeps that apart
	// (it follows from the angle), so take it back out.
	if img, ok := obj.InlineObject.(doc.Image); ok {
		px, py := img.Float.SpinPad(w, h)
		for i, p := range []float32{px, py, px, py} {
			// Zero adds no effects room when rotation contracts this axis.
			if e := float32(img.Float.Effect[i]); e != 0 {
				img.Float.Effect[i] = doc.F32(fclamp(e-p, 0, 1584))
			}
		}
		obj = doc.Object{InlineObject: img}
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
