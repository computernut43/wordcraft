package docx

// Stories: blocks, paragraphs, runs, fields, tables (Rust crates/docx/src/read/story.rs).
// Ink and equations aren't ported yet: meeting one makes the read
// refuse the file.

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/computernut43/craft-libs/rustcompat"
	"github.com/computernut43/craft-libs/serdejson"

	"github.com/computernut43/wordcraft/internal/doc"
)

// Story limits (Rust MAX_* in read/story.rs).
const (
	// maxTableDepth is the deepest table nesting built (deeper tables become plain paragraphs).
	maxTableDepth = 24
	// maxFieldDepth is the deepest field nesting tracked.
	maxFieldDepth = 32
	// maxInstr is the longest field code kept.
	maxInstr = 2 * 1024 * 1024
	// maxForm is the longest legacy form field definition (w:ffData) kept.
	maxForm = 64 * 1024
)

var objLen = utf8.RuneLen(doc.Obj)

func jsonOf(v any) (string, error) {
	b, err := serdejson.MarshalCompact(v)
	return string(b), err
}

// runCtx is the inherited run context (from w:hyperlink, w:ins, w:del wrappers).
type runCtx struct {
	link     *string
	ins, del *uint32
}

type phase uint8

const (
	phaseInstr phase = iota
	phaseResult
)

// fieldItem is a buffered piece of a field's result: text, or an object when obj is set.
type fieldItem struct {
	text  string
	obj   *doc.Object
	props doc.CharProps
}

type sizeAt struct {
	start int
	size  *doc.F32
}

type fieldState struct {
	instr   string
	phase   phase
	spilled bool
	buf     []fieldItem
	props   doc.CharProps
	locked  bool
	// dirty is w:dirty from the begin marker: the result is stale, Word refreshes it on open.
	dirty bool
	// rangeField is kept as a range: its result flows into the document as ordinary content
	// between FieldStart and FieldEnd markers instead of being buffered.
	rangeField bool
	// sizes is the font size of each piece of the code (where it starts in instr).
	sizes []sizeAt
	// form is a legacy form field's w:ffData, as raw XML.
	form string
}

// enclosure is enclosed characters (eq \o\ac(○,字)), with their style from the code's font sizes.
func enclosure(f *fieldState) (doc.Enclosure, bool) {
	e, ok := doc.ParseEnclosure(f.instr)
	if !ok {
		return e, false
	}
	sizeOf := func(at int) *doc.F32 {
		for i := len(f.sizes) - 1; i >= 0; i-- {
			if f.sizes[i].start <= at {
				return f.sizes[i].size
			}
		}
		return nil
	}
	sym := -1
	for i, c := range f.instr {
		if s, ok := doc.EncloseShapeFromSymbol(string(c)); ok && s == e.Shape {
			sym = i
			break
		}
	}
	if sym < 0 {
		return e, false
	}
	txt := sym
	if c, n := utf8.DecodeRuneInString(e.Text); n > 0 {
		if j := strings.IndexRune(f.instr[sym:], c); j >= 0 {
			txt = sym + j
		}
	}
	base := f.props.Size
	s, t := sizeOf(sym), sizeOf(txt)
	switch {
	case s != nil && t != nil && float32(*s) > float32(*t)+0.01 && (base == nil || float32(*t) >= float32(*base)-0.01):
		e.Style = doc.EncloseEnlarge
	case s != nil && t == nil && (base == nil || float32(*s) > float32(*base)+0.01):
		e.Style = doc.EncloseEnlarge
	default:
		e.Style = doc.EncloseShrink
	}
	return e, true
}

// isRangeInstr reports fields kept as ranges: those whose result another program writes (TOCs
// and citation managers' ADDIN fields).
func isRangeInstr(instr string) bool {
	if isTOC(instr) {
		return true
	}
	t := rustcompat.TrimStart(instr)
	return len(t) >= 5 && isCharBoundary(t, 5) && asciiEqualFold(t[:5], "ADDIN")
}

// lastPara is the last paragraph of a block list: its last block, or inside a final table the
// last paragraph of its last cell.
func lastPara(out doc.Vec[doc.Block], depth int) *doc.Paragraph {
	if len(out) == 0 {
		return nil
	}
	b := out[len(out)-1]
	switch {
	case b.Para != nil:
		return b.Para
	case b.Table != nil && depth < maxTableDepth:
		rows := b.Table.Rows
		if len(rows) == 0 {
			return nil
		}
		cells := rows[len(rows)-1].Cells
		if len(cells) == 0 {
			return nil
		}
		return lastPara(cells[len(cells)-1].Blocks, depth+1)
	}
	return nil
}

// storyCtx is per-story parsing state (fields may span paragraphs; block-level markers wait for
// the next paragraph).
type storyCtx struct {
	fields        []*fieldState
	ignoredBegins int
	pending       []doc.Object
	inHF          bool
	// storyDepth is the text-box nesting depth.
	storyDepth int
}

// sink is the field that buffers content now: the innermost one that isn't a range field,
// unless it has spilled (then content goes straight into the paragraph).
func (sc *storyCtx) sink() *fieldState {
	for i := len(sc.fields) - 1; i >= 0; i-- {
		f := sc.fields[i]
		if !f.rangeField {
			if f.spilled {
				return nil
			}
			return f
		}
	}
	return nil
}

func (sc *storyCtx) anyInstr() bool {
	return slices.ContainsFunc(sc.fields, func(f *fieldState) bool { return f.phase == phaseInstr })
}

// paraBuilder is paragraph content under construction.
type paraBuilder struct {
	text    strings.Builder
	runs    doc.Vec[doc.Run]
	objects doc.Vec[doc.Object]
}

func (pb *paraBuilder) pushText(s string, props doc.CharProps) {
	if s == "" {
		return
	}
	pb.text.WriteString(s)
	pb.pushRun(len(s), props)
}

func (pb *paraBuilder) pushObj(o doc.Object, props doc.CharProps) {
	pb.text.WriteRune(doc.Obj)
	pb.objects = append(pb.objects, o)
	pb.pushRun(objLen, props)
}

func (pb *paraBuilder) pushRun(n int, props doc.CharProps) {
	if k := len(pb.runs); k > 0 && pb.runs[k-1].Props.Equal(props) {
		pb.runs[k-1].Len += n
		return
	}
	pb.runs = append(pb.runs, doc.Run{Len: n, Props: props})
}

// cleanText cleans text from w:t: drops characters with special meaning in the model and control
// characters XML producers shouldn't have put there.
func cleanText(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c == '\n' || c == '\r':
			b.WriteByte(' ')
		case c == '\t':
			b.WriteByte('\t')
		case c == doc.Obj || c < 0x20:
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

var knownRequires = []string{"wps", "wpg", "wpc", "wpi", "w14", "w15", "wp14", "a14", "w16se", "w16cid", "w16", "w16cex", "w16sdtdh", "v", "m"}

// childrenOfChoice prefers a Choice whose requirements are understood; else the Fallback; else any
// Choice.
func childrenOfChoice(ac *El) *El {
	for _, c := range ac.Children("mc:Choice") {
		req := strings.FieldsFunc(attr(c, "Requires"), rustcompat.IsWhitespace)
		if !slices.ContainsFunc(req, func(r string) bool { return !slices.Contains(knownRequires, r) }) {
			return c
		}
	}
	if f := ac.Child("mc:Fallback"); f != nil {
		return f
	}
	return ac.Child("mc:Choice")
}

func paraBlock(p *doc.Paragraph) doc.Block { return doc.Block{Para: p} }

// readBlocks reads a story's blocks from a container element (w:body, w:hdr, w:tc…).
func (r *reader) readBlocks(sc *storyCtx, parent *El, rels Rels, out *doc.Vec[doc.Block], depth int) {
	if depth > MaxDepth {
		return
	}
	for _, e := range parent.Els() {
		switch e.Name {
		case "w:p":
			p := r.readPara(sc, e, rels, depth)
			// A framed drop-cap paragraph merges into the paragraph it starts.
			if n := len(*out); n > 0 {
				if prev := (*out)[n-1].Para; prev != nil && prev.Props.DropCap != nil && prev.Section == nil && len(prev.Text) <= 16 && p.Props.DropCap == nil {
					first := prev.Clone()
					*out = (*out)[:n-1]
					props := p.Props
					props.DropCap = first.Props.DropCap
					props.DropCapMargin = first.Props.DropCapMargin
					props.DropCapDistance = first.Props.DropCapDistance
					// A letter in a font of its own: the drop cap's font, the letter itself in the
					// text's.
					var capFont, textFont *string
					if len(first.Runs) > 0 {
						capFont = first.Runs[0].Props.Font
					}
					if len(p.Runs) > 0 {
						textFont = p.Runs[0].Props.Font
					}
					if capFont != nil && (textFont == nil || *capFont != *textFont) {
						for i := range first.Runs {
							first.Runs[i].Props.Font = textFont
						}
						props.DropCapFont = capFont
					}
					first.Append(p)
					first.Props = props
					*out = append(*out, paraBlock(first))
					continue
				}
			}
			*out = append(*out, paraBlock(p))
		case "w:tbl":
			if depth/4 >= maxTableDepth {
				*out = append(*out, paraBlock(doc.ParagraphWithText(cleanText(e.DeepText()), doc.CharProps{})))
			} else if t := r.readTable(sc, e, rels, depth); t != nil {
				*out = append(*out, doc.Block{Table: t})
			}
		case "w:sdt":
			r.readBlockSdt(sc, e, rels, out, depth)
		case "w:customXml", "w:smartTag", "w:ins", "w:moveTo":
			r.readBlocks(sc, e, rels, out, depth+1)
		case "m:oMathPara", "m:oMath":
			r.unported("equations")
		case "mc:AlternateContent":
			if c := childrenOfChoice(e); c != nil {
				r.readBlocks(sc, c, rels, out, depth+1)
			}
		case "w:bookmarkStart", "w:bookmarkEnd", "w:commentRangeStart", "w:commentRangeEnd", "w:permStart", "w:permEnd":
			if o, ok := r.marker(e); ok {
				sc.pending = append(sc.pending, o)
			}
		case "w:moveFromRangeStart", "w:moveFromRangeEnd", "w:moveToRangeStart", "w:moveToRangeEnd":
			r.moveRange(e)
		}
	}
}

// flushPending gives markers left over at the end of a story to its last paragraph (after a final
// table: the last paragraph of its last cell).
func (r *reader) flushPending(sc *storyCtx, out *doc.Vec[doc.Block]) {
	if len(sc.pending) == 0 {
		return
	}
	if p := lastPara(*out, 0); p != nil {
		for _, o := range sc.pending {
			var props doc.CharProps
			if n := len(p.Runs); n > 0 {
				props = p.Runs[n-1].Props
			}
			p.Text += string(doc.Obj)
			p.Objects = append(p.Objects, o)
			p.Runs = append(p.Runs, doc.Run{Len: objLen, Props: props})
		}
		p.Normalize()
	}
	sc.pending = nil
}

func (r *reader) marker(e *El) (doc.Object, bool) {
	id := rustcompat.Trim(attr(e, "w:id"))
	obj := func(o doc.InlineObject) (doc.Object, bool) { return doc.Object{InlineObject: o}, true }
	switch e.Name {
	case "w:bookmarkStart":
		if len(r.bookmarks) >= 100_000 {
			return doc.Object{}, false
		}
		name := rustcompat.Trim(attr(e, "w:name"))
		if name == "" || len(name) > 256 {
			return doc.Object{}, false
		}
		r.bookmarks[id] = name
		return obj(doc.BookmarkStart{Name: name})
	case "w:bookmarkEnd":
		if name, ok := r.bookmarks[id]; ok {
			return obj(doc.BookmarkEnd{Name: name})
		}
		if v, ok := e.Attr("w:name"); ok {
			if n := rustcompat.Trim(v); n != "" && len(n) <= 256 {
				return obj(doc.BookmarkEnd{Name: n})
			}
		}
	case "w:commentRangeStart":
		if cid, ok := r.commentMap[id]; ok {
			return obj(doc.CommentStart{ID: cid})
		}
	case "w:permStart":
		// Bounded: a hostile file can't grow the map without end.
		if len(r.perms) >= 100_000 {
			return doc.Object{}, false
		}
		pid, ok := r.perms[id]
		if !ok {
			pid = uint32(len(r.perms))
			r.perms[id] = pid
		}
		text := func(a string) string {
			if v, ok := e.Attr(a); ok && len(v) <= 256 {
				return v
			}
			return ""
		}
		group, editor := text("w:edGrp"), text("w:ed")
		if group == "" && editor == "" {
			return doc.Object{}, false
		}
		return obj(doc.PermStart{ID: pid, Group: group, Editor: editor})
	case "w:permEnd":
		if pid, ok := r.perms[id]; ok {
			return obj(doc.PermEnd{ID: pid})
		}
	case "w:commentRangeEnd":
		if cid, ok := r.commentMap[id]; ok {
			r.commentsEnded[cid] = true
			return obj(doc.CommentEnd{ID: cid})
		}
	}
	return doc.Object{}, false
}

func (r *reader) readPara(sc *storyCtx, e *El, rels Rels, depth int) *doc.Paragraph {
	var pb paraBuilder
	para := doc.NewParagraph()
	var markMoves [2]*El
	r.moves.closedHere = [2]*string{}
	pending := sc.pending
	sc.pending = nil
	for _, o := range pending {
		pb.pushObj(o, doc.CharProps{})
	}
	if ppr := e.Child("w:pPr"); ppr != nil {
		props, mark := r.pc.PPr(ppr)
		resolveLevelOnly(&r.doc.Styles, &props, "Normal")
		para.Props = props
		para.Mark = mark
		// A tracked paragraph mark (ECMA-376 §17.13.5.16 / §17.13.5.15): w:ins / w:del in the
		// mark's w:rPr.
		if rpr := ppr.Child("w:rPr"); rpr != nil {
			for _, k := range rpr.Els() {
				switch k.Name {
				case "w:ins":
					para.Mark.Ins = ptr(r.revision(doc.RevInsert, k))
				case "w:del":
					para.Mark.Del = ptr(r.revision(doc.RevDelete, k))
				// A moved paragraph mark: its move range usually starts in the paragraph's
				// content, so it is read after that.
				case "w:moveTo":
					markMoves[1] = k
				case "w:moveFrom":
					markMoves[0] = k
				}
			}
			para.Mark.FmtChange = r.rprChange(rpr)
		}
		// Tracked formatting changes (§17.13.5.29, §17.13.5.19): the paragraph properties before
		// the change, and a change of the list numbering.
		if ch := ppr.Child("w:pPrChange"); ch != nil {
			var old doc.ParaProps
			if o := ch.Child("w:pPr"); o != nil {
				old, _ = r.pc.PPr(o)
				resolveLevelOnly(&r.doc.Styles, &old, "Normal")
				old = old.Formatting()
			}
			para.Props.FmtChange = &doc.PropChange[doc.ParaProps]{Rev: r.revision(doc.RevFormat, ch), Old: old}
		}
		if para.Props.Numbering != nil {
			if np := ppr.Child("w:numPr"); np != nil {
				if ch := np.Child("w:numberingChange"); ch != nil {
					original := takeChars(attr(ch, "w:original"), 256)
					para.Props.NumChange = &doc.NumChange{Rev: r.revision(doc.RevFormat, ch), Original: original}
				}
			}
		}
		if s := ppr.Child("w:sectPr"); s != nil {
			para.Section = ptr(r.readSection(s, rels))
		}
	}
	ctx := runCtx{}
	r.readInlineChildren(sc, &pb, e, rels, ctx, depth+1)
	r.spillFields(sc, &pb)
	if k := markMoves[0]; k != nil {
		para.Mark.Del = ptr(r.moveRevision(false, k, true))
	}
	if k := markMoves[1]; k != nil {
		para.Mark.Ins = ptr(r.moveRevision(true, k, true))
	}
	para.Text = pb.text.String()
	para.Runs = pb.runs
	if para.Runs == nil {
		para.Runs = doc.Vec[doc.Run]{}
	}
	para.Objects = pb.objects
	// Paragraph boundaries inside an instruction are hidden with the field code.
	if sc.anyInstr() && para.PlainText() == "" {
		para.Mark.Hidden = ptr(true)
	}
	para.Normalize()
	return para
}

// rprChange reads a tracked run formatting change in rpr (w:rPrChange, §17.13.5.31): the
// formatting before it.
func (r *reader) rprChange(rpr *El) *doc.PropChange[doc.CharProps] {
	ch := rpr.Child("w:rPrChange")
	if ch == nil {
		return nil
	}
	var old doc.CharProps
	if o := ch.Child("w:rPr"); o != nil {
		old = r.pc.RPr(o).Formatting()
	}
	return &doc.PropChange[doc.CharProps]{Rev: r.revision(doc.RevFormat, ch), Old: old}
}

func (r *reader) readSection(s *El, rels Rels) doc.SectionProps {
	sp, refs := sectPr(s)
	// §17.13.5.32: the section's properties before a tracked change (no header references).
	if ch := s.Child("w:sectPrChange"); ch != nil {
		old := doc.NewSectionProps()
		if o := ch.Child("w:sectPr"); o != nil {
			old, _ = sectPr(o)
		}
		sp.FmtChange = &doc.PropChange[doc.SectionProps]{Rev: r.revision(doc.RevFormat, ch), Old: old}
	}
	for _, ref := range refs {
		rel, ok := rels.ByID(ref.rid)
		if !ok || rel.External {
			continue
		}
		id, ok := r.loadHeaderFooter(rel.Target, ref.footer)
		if !ok {
			continue
		}
		set := &sp.Headers
		if ref.footer {
			set = &sp.Footers
		}
		switch ref.kind {
		case "first":
			set.First = ptr(id)
		case "even":
			set.Even = ptr(id)
		default:
			set.Default = ptr(id)
		}
	}
	return sp
}

func (r *reader) readInlineChildren(sc *storyCtx, pb *paraBuilder, e *El, rels Rels, ctx runCtx, depth int) {
	if depth > MaxDepth {
		return
	}
	for _, k := range e.Els() {
		r.readInline(sc, pb, k, rels, ctx, depth)
	}
}

// chartexURI is the a:graphicData URI of an Office 2016 chart (cx:chart).
const chartexURI = "http://schemas.microsoft.com/office/drawing/2014/chartex"

// hasDrawing reports alternate content that Rust reads as native ink (the selected choice's
// drawing) or as an Office 2016 chart (any choice's drawing), which aren't ported yet.
func hasDrawing(ac *El) bool {
	uriOf := func(c *El) string {
		if d := c.Child("w:drawing"); d != nil {
			if g := d.Find("a:graphicData"); g != nil {
				return attr(g, "uri")
			}
		}
		return ""
	}
	if c := childrenOfChoice(ac); c != nil && c.Name == "mc:Choice" && uriOf(c) == inkURI {
		return true
	}
	return slices.ContainsFunc(ac.Children("mc:Choice"), func(c *El) bool { return uriOf(c) == chartexURI })
}

func (r *reader) readInline(sc *storyCtx, pb *paraBuilder, k *El, rels Rels, ctx runCtx, depth int) {
	switch k.Name {
	case "w:r":
		r.readRun(sc, pb, k, rels, ctx, depth)
	case "w:hyperlink":
		c := ctx
		var url *string
		if id, ok := k.Attr("r:id"); ok {
			if rel, ok := rels.ByID(id); ok {
				if u := rustcompat.Trim(rel.Target); u != "" {
					url = &u
				}
			}
		}
		anchor, hasAnchor := k.Attr("w:anchor")
		hasAnchor = hasAnchor && anchor != ""
		switch {
		case url != nil && hasAnchor:
			c.link = ptr(*url + "#" + anchor)
		case url != nil:
			c.link = url
		case hasAnchor:
			c.link = ptr("#" + anchor)
		}
		r.readInlineChildren(sc, pb, k, rels, c, depth+1)
	case "w:ins", "w:moveTo":
		c := ctx
		if k.Name == "w:moveTo" {
			c.ins = ptr(r.moveRevision(true, k, false))
		} else {
			c.ins = ptr(r.revision(doc.RevInsert, k))
		}
		r.readInlineChildren(sc, pb, k, rels, c, depth+1)
	case "w:del", "w:moveFrom":
		c := ctx
		if k.Name == "w:moveFrom" {
			c.del = ptr(r.moveRevision(false, k, false))
		} else {
			c.del = ptr(r.revision(doc.RevDelete, k))
		}
		r.readInlineChildren(sc, pb, k, rels, c, depth+1)
	case "w:moveFromRangeStart", "w:moveFromRangeEnd", "w:moveToRangeStart", "w:moveToRangeEnd":
		r.moveRange(k)
	case "w:fldSimple":
		instr := attr(k, "w:instr")
		props := r.runPropsNone(ctx)
		if run := k.Child("w:r"); run != nil {
			if p := run.Child("w:rPr"); p != nil {
				props = r.runProps(p, ctx)
			}
		}
		r.beginField(sc, props, instr, onOffAttr(k, "w:fldLock"), onOffAttr(k, "w:dirty"), "")
		r.separateField(sc, pb)
		r.readInlineChildren(sc, pb, k, rels, ctx, depth+1)
		r.endField(sc, pb)
	case "w:bookmarkStart", "w:bookmarkEnd", "w:commentRangeStart", "w:commentRangeEnd", "w:permStart", "w:permEnd":
		if o, ok := r.marker(k); ok {
			// Markers go straight into the paragraph (never into a field's cached result).
			pb.pushObj(o, r.runPropsNone(ctx))
		}
	case "w:sdt":
		c := k.Child("w:sdtContent")
		if c == nil {
			return
		}
		ctl := r.controlOf(k, false)
		// The markers take the formatting of the content's first run (typing at the start of
		// the content continues it).
		props := r.runPropsNone(ctx)
		if p := c.Find("w:rPr"); p != nil {
			props = r.runProps(p, ctx)
		}
		at, k0 := pb.text.Len(), len(pb.objects)
		pb.pushObj(doc.Object{InlineObject: doc.ControlStart{Control: ctl}}, props)
		r.readInlineChildren(sc, pb, c, rels, ctx, depth+1)
		if cs, ok := pb.objects[k0].InlineObject.(doc.ControlStart); ok && cs.Control.ShowingPlaceholder {
			shown := strings.ReplaceAll(pb.text.String()[at:], string(doc.Obj), "")
			cs.Control.PlaceholderText = placeholderOf(shown)
			pb.objects[k0] = doc.Object{InlineObject: cs}
		}
		endProps := props
		if n := len(pb.runs); n > 0 {
			endProps = pb.runs[n-1].Props
		}
		pb.pushObj(doc.Object{InlineObject: doc.ControlEnd{}}, endProps)
	case "w:smartTag", "w:customXml", "w:dir", "w:bdo":
		r.readInlineChildren(sc, pb, k, rels, ctx, depth+1)
	case "m:oMath", "m:oMathPara":
		r.unported("equations")
	case "mc:AlternateContent":
		if hasDrawing(k) {
			r.unported("ink and Office 2016 charts")
		} else if c := childrenOfChoice(k); c != nil {
			r.readInlineChildren(sc, pb, c, rels, ctx, depth+1)
		}
	// Some generators write a break directly under the paragraph instead of inside a run.
	case "w:br", "w:cr":
		var note *noteMark
		r.readRunChild(sc, pb, k, r.runPropsNone(ctx), rels, &note, depth+1)
	}
}

func (r *reader) runProps(rpr *El, ctx runCtx) doc.CharProps {
	p := r.pc.RPr(rpr)
	p.Link, p.Ins, p.Del = ctx.link, ctx.ins, ctx.del
	p.FmtChange = r.rprChange(rpr)
	return p
}

func (r *reader) runPropsNone(ctx runCtx) doc.CharProps {
	return doc.CharProps{Link: ctx.link, Ins: ctx.ins, Del: ctx.del}
}

// noteMark is a note reference waiting for the end of its run: custom is the custom mark when the
// reference says one follows (nil: none).
type noteMark struct {
	kind   doc.NoteKind
	id     uint32
	custom *string
}

func (r *reader) readRun(sc *storyCtx, pb *paraBuilder, run *El, rels Rels, ctx runCtx, depth int) {
	props := r.runPropsNone(ctx)
	if p := run.Child("w:rPr"); p != nil {
		props = r.runProps(p, ctx)
	}
	var note *noteMark
	for _, k := range run.Els() {
		r.readRunChild(sc, pb, k, props, rels, &note, depth+1)
	}
	if note != nil {
		r.emitObj(sc, pb, noteRef(note), props)
	}
}

func noteRef(n *noteMark) doc.Object {
	custom := ""
	if n.custom != nil {
		custom = *n.custom
	}
	return doc.Object{InlineObject: doc.NoteRef{Kind: n.kind, ID: n.id, Custom: custom}}
}

func (r *reader) readRunChild(sc *storyCtx, pb *paraBuilder, k *El, props doc.CharProps, rels Rels, note **noteMark, depth int) {
	if depth > MaxDepth {
		return
	}
	// A custom footnote mark: the text right after the reference is the mark.
	if n := *note; n != nil && n.custom != nil && k.Name == "w:t" {
		*n.custom += cleanText(k.Text())
		return
	}
	if k.Name != "w:rPr" && *note != nil {
		n := *note
		*note = nil
		r.emitObj(sc, pb, noteRef(n), props)
	}
	switch k.Name {
	case "w:t", "w:delText":
		r.emitText(sc, pb, cleanText(k.Text()), props)
	case "w:instrText", "w:delInstrText":
		if n := len(sc.fields); n > 0 {
			f := sc.fields[n-1]
			if f.phase == phaseInstr && !f.spilled && len(f.instr) < maxInstr {
				if len(f.sizes) < 64 {
					f.sizes = append(f.sizes, sizeAt{len(f.instr), props.Size})
				}
				f.instr += k.Text()
			}
		}
	case "w:tab":
		r.emitText(sc, pb, "\t", props)
	case "w:ptab":
		// Alignment and base are required (ECMA-376 §17.3.3.23); without them it stays a plain
		// tab.
		a, okA := k.Attr("w:alignment")
		b, okB := k.Attr("w:relativeTo")
		var al doc.PTabAlign
		var base doc.PTabBase
		if okA {
			al, okA = doc.PTabAlignFromOOXML(a)
		}
		if okB {
			base, okB = doc.PTabBaseFromOOXML(b)
		}
		if okA && okB {
			leader, ok := k.Attr("w:leader")
			if !ok {
				leader = "none"
			}
			r.emitObj(sc, pb, doc.Object{InlineObject: doc.PositionalTab{Align: al, RelativeTo: base, Leader: tabLeader(leader)}}, props)
		} else {
			r.emitText(sc, pb, "\t", props)
		}
	case "w:br":
		if sc.anyInstr() {
			return
		}
		c := "\n"
		switch t, _ := k.Attr("w:type"); t {
		case "page":
			c = "\u000C"
		case "column":
			c = "\u000E"
		}
		r.emitText(sc, pb, c, props)
	case "w:cr":
		if !sc.anyInstr() {
			r.emitText(sc, pb, "\n", props)
		}
	case "w:noBreakHyphen":
		r.emitText(sc, pb, "\u2011", props)
	case "w:softHyphen":
		r.emitText(sc, pb, "\u00AD", props)
	case "w:sym":
		h, ok := k.Attr("w:char")
		if !ok {
			return
		}
		v, ok := fromStrRadix16(rustcompat.Trim(h), 32)
		if !ok || v < 0x20 || v > utf8.MaxRune || v >= 0xD800 && v <= 0xDFFF {
			return
		}
		p := props
		// The symbol's own font, kept apart from the run's (ECMA-376 §17.3.3.30).
		if f, ok := k.Attr("w:font"); ok && rustcompat.Trim(f) != "" {
			p.SymFont = ptr(f)
		}
		r.emitText(sc, pb, string(rune(v)), p)
	case "w:fldChar":
		switch t, _ := k.Attr("w:fldCharType"); t {
		case "begin":
			form := ""
			if f := k.Child("w:ffData"); f != nil {
				if x := f.ToXML(); len(x) <= maxForm {
					form = x
				}
			}
			r.beginField(sc, props, "", onOffAttr(k, "w:fldLock"), onOffAttr(k, "w:dirty"), form)
		case "separate":
			r.separateField(sc, pb)
		case "end":
			r.endField(sc, pb)
		}
	case "w:drawing":
		if o, ok := r.readDrawing(sc, k, rels); ok {
			r.emitObj(sc, pb, o, props)
		}
	case "w:pict":
		for _, o := range r.readVMLAll(sc, k, rels) {
			r.emitObj(sc, pb, o, props)
		}
	case "w:object":
		if o, ok := r.readObject(sc, k, rels); ok {
			r.emitObj(sc, pb, o, props)
		}
	case "w:contentPart":
		r.unported("ink (w:contentPart)")
	case "w:footnoteReference", "w:endnoteReference":
		foot := k.Name == "w:footnoteReference"
		fid := int64(math.MinInt64)
		if v, ok := k.Attr("w:id"); ok {
			if n, ok := intOf(v); ok {
				fid = n
			}
		}
		m := r.endnotes
		kind := doc.Endnote
		if foot {
			m, kind = r.footnotes, doc.Footnote
		}
		if id, ok := m[fid]; ok {
			n := &noteMark{kind: kind, id: id}
			if onOffAttr(k, "w:customMarkFollows") {
				n.custom = ptr("")
			}
			*note = n
		}
	// The note's own number at the start of its text: a reference to the note being read.
	case "w:footnoteRef", "w:endnoteRef":
		kind := doc.Endnote
		if k.Name == "w:footnoteRef" {
			kind = doc.Footnote
		}
		if c := r.currentNote; c != nil && c.kind == kind {
			*note = &noteMark{kind: kind, id: c.id}
		}
	// The lines of a note separator (only there).
	case "w:separator", "w:continuationSeparator":
		if r.inNoteSep {
			r.emitObj(sc, pb, doc.NoteSeparator(k.Name == "w:continuationSeparator"), props)
		}
	case "w:commentReference":
		if id, ok := k.Attr("w:id"); ok {
			if cid, ok := r.commentMap[id]; ok && !r.commentsEnded[cid] {
				r.commentsEnded[cid] = true
				pb.pushObj(doc.Object{InlineObject: doc.CommentEnd{ID: cid}}, props)
			}
		}
	case "w:pgNum":
		r.emitObj(sc, pb, doc.Object{InlineObject: doc.Field{Instr: "PAGE"}}, props)
	case "w:ruby":
		r.readRuby(sc, pb, k, props)
	case "mc:AlternateContent":
		if hasDrawing(k) {
			r.unported("ink and Office 2016 charts")
		} else if c := childrenOfChoice(k); c != nil {
			for _, x := range c.Els() {
				r.readRunChild(sc, pb, x, props, rels, note, depth+1)
			}
		}
	}
}

// readRuby reads w:ruby (ECMA-376 §17.3.3.25): ruby text (w:rt) over base text (w:rubyBase), kept
// as one Ruby object in the base text's formatting (the first base run's).
func (r *reader) readRuby(sc *storyCtx, pb *paraBuilder, k *El, props doc.CharProps) {
	const maxRuby = 4096
	// The text of a part's runs and the formatting of its first run.
	part := func(name string) (string, *doc.CharProps) {
		var text strings.Builder
		var first *doc.CharProps
		if e := k.Child(name); e != nil {
			for _, run := range e.Children("w:r") {
				if first == nil {
					var p doc.CharProps
					if rp := run.Child("w:rPr"); rp != nil {
						p = r.pc.RPr(rp)
					}
					first = &p
				}
				for _, t := range run.Els() {
					if (t.Name == "w:t" || t.Name == "w:delText") && text.Len() < maxRuby {
						text.WriteString(strings.ReplaceAll(cleanText(t.Text()), "\t", " "))
					}
				}
			}
		}
		return takeChars(text.String(), maxRuby), first
	}
	base, baseProps := part("w:rubyBase")
	ruby, rubyProps := part("w:rt")
	var bp doc.CharProps
	if baseProps != nil {
		bp = *baseProps
	}
	bp.Link, bp.Ins, bp.Del = props.Link, props.Ins, props.Del
	if base == "" {
		return
	}
	if ruby == "" {
		r.emitText(sc, pb, base, bp)
		return
	}
	pr := k.Child("w:rubyPr")
	val := func(n string) (string, bool) {
		if pr == nil {
			return "", false
		}
		return pr.ChildVal(n)
	}
	pt := func(n string) (float32, bool) {
		v, ok := val(n)
		if !ok {
			return 0, false
		}
		m, ok := measure(v, 2)
		if !ok || m < 0 {
			return 0, false
		}
		return m, true
	}
	var rp doc.CharProps
	if rubyProps != nil {
		rp = *rubyProps
	}
	var baseSize float32
	if v, ok := pt("w:hpsBaseText"); ok && v > 0 {
		baseSize = v
	} else if bp.Size != nil {
		baseSize = float32(*bp.Size)
	}
	baseSize = rustcompat.Min(baseSize, 1584)
	var size float32
	if v, ok := pt("w:hps"); ok && v > 0 {
		size = v
	} else if rp.Size != nil {
		size = float32(*rp.Size)
	} else if baseSize > 0 {
		size = baseSize / 2
	} else {
		size = 5
	}
	size = fclamp(size, 1, 1584)
	raise, ok := pt("w:hpsRaise")
	if !ok {
		raise = float32(doc.RubyRaise(0, doc.F32(rustcompat.Max(baseSize, size)), doc.F32(size)))
	}
	raise = rustcompat.Min(raise, 1584)
	// The ruby's size is size; its run keeps what it sets beyond the base text's formatting
	// (writers repeat the base's).
	rp.Size, rp.SizeCS = nil, nil
	rp = rp.Minus(&bp)
	al := doc.RubyCenter
	if v, ok := val("w:rubyAlign"); ok {
		if a, ok := doc.RubyAlignFromOOXML(v); ok {
			al = a
		}
	}
	lang := ""
	if v, ok := val("w:lid"); ok {
		lang = takeChars(v, 32)
	}
	obj := doc.Ruby{Base: base, Ruby: ruby, Align: al, Size: doc.F32(size), Raise: doc.F32(raise), BaseSize: doc.F32(baseSize), Lang: lang, Props: rp}
	r.emitObj(sc, pb, doc.Object{InlineObject: obj}, bp)
}

// ---- fields ----

func (r *reader) beginField(sc *storyCtx, props doc.CharProps, instr string, locked, dirty bool, form string) {
	// Fields nested deeper than the cap are ignored (their content flows to the enclosing field).
	if len(sc.fields) >= maxFieldDepth || sc.ignoredBegins > 0 {
		sc.ignoredBegins++
		return
	}
	sc.fields = append(sc.fields, &fieldState{instr: instr, phase: phaseInstr, props: props, locked: locked, dirty: dirty, form: form})
}

// parentInInstr reports whether the field below the innermost one is still collecting its code (a
// field nested in another's code contributes text to that code, never content).
func parentInInstr(sc *storyCtx) bool {
	n := len(sc.fields)
	if n < 2 {
		return false
	}
	p := sc.fields[n-2]
	return !p.spilled && !p.rangeField && p.phase == phaseInstr
}

// separateField handles separate: the code is complete; a range field starts its result here.
func (r *reader) separateField(sc *storyCtx, pb *paraBuilder) {
	if sc.ignoredBegins > 0 {
		return
	}
	nested := parentInInstr(sc)
	if len(sc.fields) == 0 {
		return
	}
	f := sc.fields[len(sc.fields)-1]
	if f.rangeField {
		return
	}
	f.phase = phaseResult
	if !f.spilled && !nested && isRangeInstr(f.instr) {
		f.rangeField = true
		o := doc.Object{InlineObject: doc.FieldStart{Instr: rustcompat.Trim(f.instr), Locked: f.locked, Dirty: f.dirty}}
		r.emitObj(sc, pb, o, f.props)
	}
}

func (r *reader) endField(sc *storyCtx, pb *paraBuilder) {
	if sc.ignoredBegins > 0 {
		sc.ignoredBegins--
		return
	}
	nested := parentInInstr(sc)
	if len(sc.fields) == 0 {
		return
	}
	f := sc.fields[len(sc.fields)-1]
	sc.fields = sc.fields[:len(sc.fields)-1]
	start := func() doc.Object {
		return doc.Object{InlineObject: doc.FieldStart{Instr: rustcompat.Trim(f.instr), Locked: f.locked, Dirty: f.dirty}}
	}
	end := doc.Object{InlineObject: doc.FieldEnd{}}
	if f.rangeField {
		r.emitObj(sc, pb, end, f.props)
		return
	}
	if f.spilled {
		return
	}
	if f.phase == phaseInstr && !nested && isRangeInstr(f.instr) {
		// A range field with no result yet.
		r.emitObj(sc, pb, start(), f.props)
		r.emitObj(sc, pb, end, f.props)
		return
	}
	// A cached result holding a picture, chart or shape can't be a text result: keep the field
	// as a range around it. A tracked insertion or deletion inside the result keeps the field a
	// range too.
	tracked := slices.ContainsFunc(f.buf, func(it fieldItem) bool {
		return !eqU32(it.props.Ins, f.props.Ins) || !eqU32(it.props.Del, f.props.Del)
	})
	framed := slices.ContainsFunc(f.buf, func(it fieldItem) bool {
		if it.obj == nil {
			return false
		}
		_, _, _, ok := it.obj.Frame()
		return ok
	})
	if !nested && (tracked || framed) {
		r.emitObj(sc, pb, start(), f.props)
		for _, it := range f.buf {
			if it.obj != nil {
				r.emitObj(sc, pb, *it.obj, it.props)
			} else {
				r.emitText(sc, pb, it.text, it.props)
			}
		}
		r.emitObj(sc, pb, end, f.props)
		return
	}
	var result strings.Builder
	for _, it := range f.buf {
		if it.obj != nil {
			result.WriteString(it.obj.PlainText())
		} else {
			result.WriteString(it.text)
		}
	}
	instr, res := rustcompat.Trim(f.instr), result.String()
	if e, ok := enclosure(f); ok {
		// Word writes no result for enclosed characters: the text is their plain text.
		instr = e.Instr()
		if res == "" {
			res = e.Text
		}
	}
	// The visible result owns the field's formatting; empty results keep the begin's.
	props := f.props
	for _, it := range f.buf {
		if it.obj == nil && it.text != "" || it.obj != nil && it.obj.PlainText() != "" {
			props = it.props
			break
		}
	}
	obj := doc.Object{InlineObject: doc.Field{Instr: instr, Result: res, Locked: f.locked, Dirty: f.dirty, Form: f.form}}
	if n := len(sc.fields); n > 0 {
		if p := sc.fields[n-1]; !p.spilled && p.phase == phaseInstr {
			p.instr += obj.PlainText()
			return
		}
	}
	r.emitObj(sc, pb, obj, props)
}

func eqU32(a, b *uint32) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// spillFields: at a paragraph end, open fields can't keep buffering: emit them (empty result) and
// let the rest of their result flow as ordinary text.
func (r *reader) spillFields(sc *storyCtx, pb *paraBuilder) {
	for _, f := range sc.fields {
		if f.spilled || f.rangeField {
			continue
		}
		f.spilled = true
		form := f.form
		f.form = ""
		pb.pushObj(doc.Object{InlineObject: doc.Field{Instr: rustcompat.Trim(f.instr), Locked: f.locked, Dirty: f.dirty, Form: form}}, f.props)
		for _, it := range f.buf {
			if it.obj != nil {
				pb.pushObj(*it.obj, it.props)
			} else {
				pb.pushText(it.text, it.props)
			}
		}
		f.buf = nil
	}
}

func (r *reader) emitText(sc *storyCtx, pb *paraBuilder, s string, props doc.CharProps) {
	if f := sc.sink(); f != nil {
		if f.phase == phaseResult && len(f.buf) < 100_000 {
			f.buf = append(f.buf, fieldItem{text: s, props: props})
		}
		return
	}
	pb.pushText(s, props)
}

func (r *reader) emitObj(sc *storyCtx, pb *paraBuilder, o doc.Object, props doc.CharProps) {
	if f := sc.sink(); f != nil {
		if f.phase == phaseResult && len(f.buf) < 100_000 {
			f.buf = append(f.buf, fieldItem{obj: &o, props: props})
		}
		return
	}
	pb.pushObj(o, props)
}

func onOffAttr(e *El, name string) bool {
	v, ok := e.Attr(name)
	return ok && !(v == "0" || v == "false" || v == "off")
}

// ---- tables ----

type wrapped struct {
	el   *El
	wrap doc.ControlWrap
}

func (r *reader) readTable(sc *storyCtx, t *El, rels Rels, depth int) *doc.Table {
	table := &doc.Table{}
	table.Props.SetDefaults()
	if p := t.Child("w:tblPr"); p != nil {
		table.Props = r.pc.TblPr(p)
		if ch := p.Child("w:tblPrChange"); ch != nil {
			var old doc.TableProps
			old.SetDefaults()
			if o := ch.Child("w:tblPr"); o != nil {
				old = r.pc.TblPr(o)
			}
			table.Props.FmtChange = &doc.PropChange[doc.TableProps]{Rev: r.revision(doc.RevFormat, ch), Old: old}
		}
	}
	table.Grid = doc.Vec[doc.F32]{}
	if g := t.Child("w:tblGrid"); g != nil {
		for i, c := range g.Children("w:gridCol") {
			if i >= doc.MaxCols {
				break
			}
			w, _ := tw(c, "w:w")
			table.Grid = append(table.Grid, doc.F32(fclamp(w, 0, MaxLenPt)))
		}
	}
	var rows []wrapped
	r.collect(t, "w:tr", &rows, &doc.Vec[doc.ContentControl]{}, 0)
	table.Rows = doc.Vec[doc.Row]{}
	for i, tr := range rows {
		if i >= doc.MaxRows {
			break
		}
		row := doc.Row{Cells: doc.Vec[doc.Cell]{}, Controls: tr.wrap}
		trp := tr.el.Child("w:trPr")
		if trp != nil {
			row.Props = trPr(trp)
		}
		if e := tr.el.Child("w:tblPrEx"); e != nil {
			row.Props.Exceptions = ptr(r.pc.TblPr(e))
		}
		if trp != nil {
			if ch := trp.Child("w:trPrChange"); ch != nil {
				var old doc.RowProps
				if o := ch.Child("w:trPr"); o != nil {
					old = trPr(o)
				}
				row.Props.FmtChange = &doc.PropChange[doc.RowProps]{Rev: r.revision(doc.RevFormat, ch), Old: old}
			}
		}
		var tcs []wrapped
		r.collect(tr.el, "w:tc", &tcs, &doc.Vec[doc.ContentControl]{}, 0)
		for j, tc := range tcs {
			if j >= doc.MaxCols {
				break
			}
			props, hcont := doc.CellProps{Span: 1}, false
			tcp := tc.el.Child("w:tcPr")
			if tcp != nil {
				props, hcont = tcPr(&r.pc, tcp)
				if ch := tcp.Child("w:tcPrChange"); ch != nil {
					var old doc.CellProps
					if o := ch.Child("w:tcPr"); o != nil {
						old, _ = tcPr(&r.pc, o)
					}
					props.FmtChange = &doc.PropChange[doc.CellProps]{Rev: r.revision(doc.RevFormat, ch), Old: old}
				}
			}
			if n := len(row.Cells); hcont && n > 0 {
				prev := &row.Cells[n-1]
				prev.Props.Span = min(prev.Props.Span+max(props.Span, 1), 63)
				continue
			}
			blocks := doc.Vec[doc.Block]{}
			r.readBlocks(sc, tc.el, rels, &blocks, depth+4)
			r.flushPending(sc, &blocks)
			row.Cells = append(row.Cells, doc.Cell{Props: props, Blocks: blocks, Controls: tc.wrap})
		}
		if len(row.Cells) > 0 {
			table.Rows = append(table.Rows, row)
		}
	}
	if len(table.Rows) == 0 {
		return nil
	}
	if len(table.Grid) == 0 {
		cols := min(max(table.Cols(), 1), doc.MaxCols)
		for range cols {
			table.Grid = append(table.Grid, 72)
		}
	}
	return table
}

// collect gathers name children (rows, cells), looking through w:sdt/w:customXml wrappers; each
// with the content controls that open before it and how many close after it. pending holds
// controls opened but not yet given to an element.
func (r *reader) collect(e *El, name string, out *[]wrapped, pending *doc.Vec[doc.ContentControl], depth int) {
	if depth > 8 {
		return
	}
	for _, k := range e.Els() {
		switch k.Name {
		case name:
			open := *pending
			if open == nil {
				open = doc.Vec[doc.ContentControl]{}
			}
			*pending = nil
			*out = append(*out, wrapped{el: k, wrap: doc.ControlWrap{Open: open}})
		case "w:sdt":
			c := k.Child("w:sdtContent")
			if c == nil {
				continue
			}
			ctl := sdtPr(k.Child("w:sdtPr"), true)
			ctl.EndPrXML = sdtEndPr(k.Child("w:sdtEndPr"))
			before := len(*out)
			*pending = append(*pending, ctl)
			r.collect(c, name, out, pending, depth+1)
			if n := len(*out); n > before {
				if w := &(*out)[n-1].wrap; w.Close < math.MaxUint32 {
					w.Close++
				}
			} else if n := len(*pending); n > 0 {
				// Nothing inside: the control goes.
				*pending = (*pending)[:n-1]
			}
		case "w:customXml":
			r.collect(k, name, out, pending, depth+1)
		}
	}
}

// controlOf is the content control a w:sdt describes, with its end properties and its run
// properties.
func (r *reader) controlOf(e *El, block bool) doc.ContentControl {
	pr := e.Child("w:sdtPr")
	ctl := sdtPr(pr, block)
	ctl.EndPrXML = sdtEndPr(e.Child("w:sdtEndPr"))
	if pr != nil {
		if rp := pr.Child("w:rPr"); rp != nil {
			ctl.RunProps = ptr(r.pc.RPr(rp))
		}
	}
	return ctl
}

// placeholderOf is placeholder text as read from a control showing it: one line, bounded.
func placeholderOf(s string) string {
	var b strings.Builder
	n := 0
	for _, c := range s {
		if n == doc.MaxControlText {
			break
		}
		if c < 0x20 || 0x7F <= c && c <= 0x9F {
			c = ' '
		}
		b.WriteRune(c)
		n++
	}
	return rustcompat.Trim(b.String())
}

// readBlockSdt reads a block-level content control: its content's blocks, between a start marker
// at the start of the first paragraph and an end marker at the end of the last (a table at either
// end carries the control instead).
func (r *reader) readBlockSdt(sc *storyCtx, e *El, rels Rels, out *doc.Vec[doc.Block], depth int) {
	content := e.Child("w:sdtContent")
	if content == nil {
		return
	}
	ctl := r.controlOf(e, true)
	inner := doc.Vec[doc.Block]{}
	r.readBlocks(sc, content, rels, &inner, depth+1)
	// A table of contents' control belongs to the TOC: the writer puts it back around the TOC
	// field and its entries.
	toc := ctl.Kind.Type == doc.GalleryControl && !ctl.Kind.List && ctl.Kind.Gallery == "Table of Contents"
	if len(inner) == 0 || toc {
		*out = append(*out, inner...)
		return
	}
	if ctl.ShowingPlaceholder && inner[0].Para != nil {
		ctl.PlaceholderText = placeholderOf(inner[0].Para.PlainText())
	}
	if p := inner[0].Para; p != nil {
		_ = p.InsertObject(0, doc.Object{InlineObject: doc.ControlStart{Control: ctl}}, p.PropsOfChar(0))
	} else if t := inner[0].Table; t != nil {
		t.Controls.Open = append(doc.Vec[doc.ContentControl]{ctl}, t.Controls.Open...)
	}
	last := inner[len(inner)-1]
	if p := last.Para; p != nil {
		props := p.Mark
		if n := len(p.Runs); n > 0 {
			props = p.Runs[n-1].Props
		}
		_ = p.InsertObject(p.Len(), doc.Object{InlineObject: doc.ControlEnd{}}, props)
	} else if t := last.Table; t != nil && t.Controls.Close < math.MaxUint32 {
		t.Controls.Close++
	}
	*out = append(*out, inner...)
}
