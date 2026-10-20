package docx

// The .docx reader: package parts into a document (Rust crates/docx/src/read/mod.rs).
//
// Ported so far: the main document's body (paragraphs, runs, hyperlinks, tracked changes and
// moves, fields, bookmarks, comment and permission ranges, breaks, symbols, ruby, positional
// tabs, note references, tables), sections with their headers and footers, styles, numbering,
// settings, the theme's colours and fonts, footnotes and endnotes with their separators,
// comments, core and extended properties, and the parts kept verbatim (the font table and
// whatever the model doesn't hold), content controls, and drawings (pictures, shapes, text boxes,
// groups, canvases, freeforms, legacy VML). What isn't ported yet (charts, diagrams, ink, OLE
// objects, equations, run text effects, the math settings, custom properties, bibliography
// sources, custom XML, glossaries, macro projects, encrypted packages) makes Read refuse the file
// with an ErrNotPorted error rather than drop it.

import (
	"bytes"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

// Reader limits (Rust MAX_* in read/mod.rs).
const (
	maxTableFonts = 2000
	maxFontName   = 256
	// maxParts is the most parts (headers, notes, comments, text boxes) created from one file.
	maxParts = 50_000
	// maxHFDepth is the deepest chain of headers/footers whose own sections reference further
	// headers/footers.
	maxHFDepth = 4
	// maxBibliographyParts is how many custom XML parts are looked at for bibliography sources.
	maxBibliographyParts = 64
	// maxTemplatePath is the longest attached-template path kept (bytes).
	maxTemplatePath = 32 * 1024
	maxOpenMoves    = 64
	maxMoves        = 100_000
)

// Relationship types the reader needs beyond package.go's.
const (
	relCustomXMLRT = RelCustomXML
	appKept        = "wordcraft:app.kept"
	settingsOrig   = "wordcraft:settings.orig"
	embeddedParts  = "wordcraft:embedded.parts"
	unmodelledRoot = "wordcraft:unmodelled.roots"
)

// appKeptProps are the docProps/app.xml properties carried over from the file.
var appKeptProps = []string{"Template", "Manager", "Company", "HyperlinkBase", "ScaleCrop", "LinksUpToDate", "SharedDoc", "HyperlinksChanged"}

type revKey struct {
	kind         uint8
	author, date string
	moved        bool
	move         string
}

type noteID struct {
	kind doc.NoteKind
	id   uint32
}

// moveCtx are the move ranges of a file (ECMA-376 §17.13.5.22–27).
type moveCtx struct {
	// open source and destination ranges: (w:id, w:name).
	open [2][][2]string
	// seen per name: a source range started / ended, a destination range started / ended (bits
	// 0–3).
	seen map[string]uint8
	// closedHere is the range of each kind that ended last in the paragraph being read.
	closedHere [2]*string
}

// reader holds the state of one read (Rust Reader).
type reader struct {
	pkg           *Package
	doc           *doc.Document
	pc            PropCtx
	revs          map[revKey]uint32
	moves         moveCtx
	mediaByPath   map[string]string
	hfByPath      map[string]uint32
	hfDepth       int
	footnotes     map[int64]uint32
	endnotes      map[int64]uint32
	currentNote   *noteID
	inNoteSep     bool
	commentMap    map[string]uint32
	commentsEnded map[uint32]bool
	bookmarks     map[string]string
	perms         map[string]uint32
	main          string
	contentTypes  *ContentTypes
	embedded      map[string]*embeddedPart
	// txbxLinks are the linked text boxes read: (wps:txbx/@id or wps:linkedTxbx/@id, seq (0 for
	// the box holding the text), text box part).
	txbxLinks [][3]uint32
	// glued are the drawing ids connectors are glued to; shapeIDs whether any shape's id was kept
	// while reading (those not glued to are dropped at the end).
	glued    map[uint32]bool
	shapeIDs bool
	err      error
}

// embeddedPart is one part kept verbatim: its content type and its own relationships.
type embeddedPart struct {
	contentType string
	rels        Rels
}

func (r *reader) unported(format string, args ...any) {
	if r.err == nil {
		r.err = notPorted(format, args...)
	}
}

func (r *reader) types() *ContentTypes {
	if r.contentTypes == nil {
		ct := ReadContentTypes(r.pkg)
		r.contentTypes = &ct
	}
	return r.contentTypes
}

var cfbMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// Read reads a .docx package (Rust read::read). Encrypted packages aren't ported yet.
func Read(b []byte) (d *doc.Document, err error) {
	defer func() {
		if p := recover(); p != nil {
			d, err = nil, &Error{ErrLimit, "reader failed"}
		}
	}()
	if bytes.HasPrefix(b, cfbMagic) {
		return nil, notPorted("compound files (encrypted packages)")
	}
	pkg, err := OpenPackage(b)
	if err != nil {
		return nil, err
	}
	return readOpened(pkg)
}

func internalByType(rels Rels, t string) (string, bool) {
	rel, ok := rels.ByType(t)
	if !ok || rel.External {
		return "", false
	}
	return rel.Target, true
}

// readOpened reads a package whose parts are already unpacked.
func readOpened(pkg *Package) (*doc.Document, error) {
	rootRels := pkg.Rels("")
	main, ok := internalByType(rootRels, RelOfficeDoc)
	if !ok {
		main = "word/document.xml"
	}
	root, err := pkg.XML(main)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, &Error{ErrMissingPart, main}
	}
	if root.Name != "w:document" {
		return nil, &Error{ErrNotWord, "root element is " + root.Name}
	}
	rels := pkg.Rels(main)
	d := doc.New()
	d.Body = doc.Vec[doc.Block]{}
	r := &reader{
		pkg:           pkg,
		doc:           d,
		revs:          map[revKey]uint32{},
		moves:         moveCtx{seen: map[string]uint8{}},
		mediaByPath:   map[string]string{},
		hfByPath:      map[string]uint32{},
		footnotes:     map[int64]uint32{},
		endnotes:      map[int64]uint32{},
		commentMap:    map[string]uint32{},
		commentsEnded: map[uint32]bool{},
		bookmarks:     map[string]string{},
		perms:         map[string]uint32{},
		main:          main,
		embedded:      map[string]*embeddedPart{},
		glued:         map[uint32]bool{},
	}
	r.pc.Alias = map[string]string{}
	r.pc.MajorFont = d.Settings.MajorFont
	r.pc.MinorFont = d.Settings.MinorFont
	part := func(t string) (string, bool) { return internalByType(rels, t) }

	// The font table and the fonts embedded through it ride along as opaque parts.
	if p, ok := part(RelFontTable); ok {
		r.keepPartTree(p)
		r.readFontFamilies(p)
	}
	if p, ok := part(RelTheme); ok {
		r.readTheme(p)
	}
	// Secondary parts are optional: a broken one is skipped, not fatal. Styles come before
	// settings, but their theme colours already follow its colour mapping.
	if p, ok := part(RelSettings); ok {
		r.readColorMapping(p)
	}
	if p, ok := part(RelStyles); ok {
		_ = r.readStyles(p)
	}
	if p, ok := part(RelNumbering); ok {
		_ = r.readNumbering(p)
	}
	// Without a stated mode (or settings at all) Word lays a document out as Word 2007 did.
	d.Settings.CompatMode = doc.LegacyCompatMode
	if p, ok := part(RelSettings); ok {
		_ = r.readSettings(p)
	}
	if p, ok := part(RelFootnotes); ok {
		_ = r.readNotes(p, true)
	}
	if p, ok := part(RelEndnotes); ok {
		_ = r.readNotes(p, false)
	}
	if p, ok := part(RelComments); ok {
		ex, _ := part(RelCommentsEx)
		ids, _ := part(RelCommentsIDs)
		cex, _ := part(RelCommentsExtensible)
		_ = r.readComments(p, ex, ids, cex)
	}
	// Bibliography sources live in a custom XML data store (b:Sources), related from the package
	// root or the main part, or under the conventional customXml/itemN.xml names. Not ported.
	var bib []string
	for _, rel := range append(slices.Clone(rootRels), rels...) {
		if RelIs(rel.Kind, relCustomXMLRT) && !rel.External && !slices.Contains(bib, rel.Target) {
			bib = append(bib, rel.Target)
			if len(bib) >= maxBibliographyParts {
				break
			}
		}
	}
	for i := 1; i <= 16; i++ {
		path := "customXml/item" + itoa(i) + ".xml"
		if _, ok := pkg.Get(path); ok && len(bib) < maxBibliographyParts && !slices.Contains(bib, path) {
			bib = append(bib, path)
		}
	}
	if len(bib) > 0 {
		r.unported("custom XML parts (bibliography sources, data stores)")
	}

	if bg := root.Child("w:background"); bg != nil {
		if c, ok := parseRGBAttr(bg, "w:color"); ok {
			d.Settings.PageColor = &c
		}
		// A picture or texture fill: w:background/v:background/v:fill with a relationship id.
		if vb := bg.Child("v:background"); vb != nil {
			if fill := vb.Child("v:fill"); fill != nil {
				rid, ok := fill.Attr("r:id")
				if !ok {
					rid, ok = fill.Attr("r:embed")
				}
				if !ok {
					rid, ok = fill.Attr("o:relid")
				}
				if ok {
					if media, ok := r.mediaFor(rels, rid); ok {
						d.Settings.PagePicture = &doc.PagePicture{
							Media:    media,
							FillType: takeChars(attr(fill, "type"), 32),
							Title:    takeChars(attr(fill, "o:title"), 255),
						}
					}
				}
			}
		}
	}
	if body := root.Child("w:body"); body != nil {
		sc := &storyCtx{}
		blocks := doc.Vec[doc.Block]{}
		r.readBlocks(sc, body, rels, &blocks, 0)
		r.flushPending(sc, &blocks)
		d.Body = blocks
		unstyleTOCLinks(d)
		tocFieldToHeading(d.Body)
		if s := body.Child("w:sectPr"); s != nil {
			d.LastSection = r.readSection(s, rels)
		}
	}

	core := "docProps/core.xml"
	if rel, ok := rootRels.ByType(RelCore); ok {
		core = rel.Target
	}
	r.readCore(core)
	if rel, ok := rootRels.ByType(RelApp); ok {
		r.readApp(rel.Target)
	}
	if rel, ok := rootRels.ByType(RelCustom); ok {
		if x, err := r.pkg.XML(rel.Target); err == nil && x != nil {
			r.unported("custom document properties")
		}
	}
	// A macro project rides along as opaque bytes. Not ported.
	if v, ok := part(RelVBAProject); ok {
		if b, ok := pkg.Get(v); ok && len(b) > 0 {
			r.unported("macro projects")
		}
	}
	for _, rel := range rels {
		if !rel.External && RelIs(rel.Kind, RelGlossary) {
			r.unported("glossary documents (building blocks)")
			break
		}
	}
	r.keepUnmodelled(rootRels, rels)
	if len(r.embedded) > 0 {
		d.Passthrough = ensureMap(d.Passthrough)
		d.Passthrough[embeddedParts] = r.manifestBytes()
	}
	r.finishMoves()
	linkTextBoxes(d, r.txbxLinks)
	if r.shapeIDs {
		keepGluedIDs(d, r.glued)
	}
	if r.err == nil {
		r.err = r.pc.err
	}
	if r.err != nil {
		return nil, r.err
	}
	d.EnsureNonempty()
	return d, nil
}

func ensureMap(m doc.StrMap[doc.Vec[uint8]]) doc.StrMap[doc.Vec[uint8]] {
	if m == nil {
		return doc.StrMap[doc.Vec[uint8]]{}
	}
	return m
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// moveRange reads a w:moveFromRangeStart/End or w:moveToRangeStart/End.
func (r *reader) moveRange(e *El) {
	var side int
	var start bool
	switch e.Name {
	case "w:moveFromRangeStart":
		side, start = 0, true
	case "w:moveFromRangeEnd":
		side, start = 0, false
	case "w:moveToRangeStart":
		side, start = 1, true
	case "w:moveToRangeEnd":
		side, start = 1, false
	default:
		return
	}
	id := takeChars(attr(e, "w:id"), 64)
	m := &r.moves
	if start {
		name := takeChars(attr(e, "w:name"), 256)
		_, seen := m.seen[name]
		if name == "" || len(m.open[side]) >= maxOpenMoves || (len(m.seen) >= maxMoves && !seen) {
			return
		}
		m.seen[name] |= 1 << (side * 2)
		m.open[side] = append(m.open[side], [2]string{id, name})
		return
	}
	for i := len(m.open[side]) - 1; i >= 0; i-- {
		if m.open[side][i][0] == id {
			name := m.open[side][i][1]
			m.open[side] = slices.Delete(m.open[side], i, i+1)
			m.seen[name] |= 2 << (side * 2)
			m.closedHere[side] = ptr(name)
			return
		}
	}
}

// moveRevision is the revision of a w:moveFrom (to false) or w:moveTo: one end of the innermost
// open range of its kind, else (no range) a plain deletion or insertion.
func (r *reader) moveRevision(to bool, e *El, mark bool) uint32 {
	side := 0
	if to {
		side = 1
	}
	var name *string
	if o := r.moves.open[side]; len(o) > 0 {
		name = ptr(o[len(o)-1][1])
	}
	// A paragraph mark comes after its paragraph's text: a range that ended in it counts.
	if mark && name == nil && r.moves.closedHere[side] != nil {
		name = ptr(*r.moves.closedHere[side])
	}
	kind := doc.RevDelete
	if to {
		kind = doc.RevInsert
	}
	return r.revisionNamed(kind, e, name)
}

// finishMoves makes moves missing an end (a source or destination range, or its text) plain
// deletions and insertions.
func (r *reader) finishMoves() {
	ends := map[string]uint8{}
	for _, rev := range r.doc.Revisions {
		if rev.MoveName != nil {
			if rev.Kind == doc.RevDelete {
				ends[*rev.MoveName] |= 1
			} else {
				ends[*rev.MoveName] |= 2
			}
		}
	}
	for i := range r.doc.Revisions {
		rev := &r.doc.Revisions[i]
		if rev.MoveName == nil {
			continue
		}
		if !(ends[*rev.MoveName] == 3 && r.moves.seen[*rev.MoveName] == 0b1111) {
			rev.MoveName = nil
		}
	}
}

// isTOC reports whether a field code is a table of contents.
func isTOC(instr string) bool {
	f := strings.FieldsFunc(instr, rustcompat.IsWhitespace)
	return len(f) > 0 && asciiEqualFold(f[0], "TOC")
}

// unstyleTOCLinks drops the Hyperlink character style from a table of contents' entry links:
// Word draws them without it (no blue, no underline) though the entries carry it. The links stay.
func unstyleTOCLinks(d *doc.Document) {
	for _, f := range d.FieldRanges(doc.StoryRef{}) {
		if isTOC(f.Instr) {
			_ = d.FormatRange(f.Start, f.End, func(c *doc.CharProps) {
				if c.Link != nil && c.Style != nil && *c.Style == "Hyperlink" {
					c.Style = nil
				}
			})
		}
	}
}

func asPara(body doc.Vec[doc.Block], i int) *doc.Paragraph {
	if i < 0 || i >= len(body) {
		return nil
	}
	return body[i].Para
}

// tocFieldToHeading normalizes only a complete TOC range that the engine's heading/entry
// representation can reproduce exactly. Other imported TOCs keep their explicit boundaries.
func tocFieldToHeading(body doc.Vec[doc.Block]) {
	hasTOC := func(o doc.Object) bool {
		f, ok := o.InlineObject.(doc.Field)
		return ok && isTOC(f.Instr)
	}
	for i := 1; i < len(body); i++ {
		h := asPara(body, i-1)
		if h == nil || h.Props.Style == nil || *h.Props.Style != "TOCHeading" || slices.ContainsFunc(h.Objects, hasTOC) {
			continue
		}
		p := asPara(body, i)
		if p == nil {
			continue
		}
		o, ok := p.ObjectAt(0)
		if !ok {
			continue
		}
		var field doc.Object
		end := -1
		switch v := o.InlineObject.(type) {
		case doc.FieldStart:
			if !isTOC(v.Instr) {
				continue
			}
			e, ok := tocRangeEnd(body, i)
			if !ok {
				continue
			}
			field = doc.Object{InlineObject: doc.Field{Instr: v.Instr, Locked: v.Locked, Dirty: v.Dirty}}
			end = e
		case doc.Field:
			if v.Result != "" || !hasTOC(o) {
				continue
			}
			field = o
		default:
			continue
		}
		props := p.PropsOfChar(0)
		// Delete the end first: the two markers may occupy the same paragraph.
		if ep := asPara(body, end); ep != nil {
			off := max(ep.Len()-len(string(doc.Obj)), 0)
			_ = ep.Delete(off, ep.Len())
		}
		_ = p.Delete(0, len(string(doc.Obj)))
		_ = h.InsertObject(h.Len(), field, props)
	}
}

// tocRangeEnd is the matching end of the TOC range starting in body[start], which must also be
// the end the styled writer would choose: at the end of the last contiguous TOC entry.
func tocRangeEnd(body doc.Vec[doc.Block], start int) (int, bool) {
	entry := func(p *doc.Paragraph) bool {
		return p != nil && p.Section == nil && p.Props.Style != nil && strings.HasPrefix(*p.Props.Style, "TOC") && *p.Props.Style != "TOCHeading"
	}
	depth := 0
	for i := start; i < len(body); i++ {
		p := asPara(body, i)
		if !entry(p) {
			return 0, false
		}
		for j, off := range p.ObjectOffsets() {
			if j >= len(p.Objects) {
				break
			}
			switch p.Objects[j].InlineObject.(type) {
			case doc.FieldStart:
				depth++
			case doc.FieldEnd:
				if depth == 0 {
					return 0, false
				}
				depth--
				if depth == 0 {
					following := entry(asPara(body, i+1))
					return i, off+len(string(doc.Obj)) == p.Len() && !following
				}
			}
		}
	}
	return 0, false
}

// lowerNames are built-in style names Word stores in lower case, and how the UI shows them.
var lowerNames = [][2]string{
	{"caption", "Caption"},
	{"header", "Header"},
	{"footer", "Footer"},
	{"footnote text", "Footnote Text"},
	{"footnote reference", "Footnote Reference"},
	{"endnote text", "Endnote Text"},
	{"endnote reference", "Endnote Reference"},
	{"annotation text", "Annotation Text"},
	{"annotation reference", "Annotation Reference"},
	{"annotation subject", "Annotation Subject"},
	{"table of figures", "Table of Figures"},
	{"normal indent", "Normal Indent"},
	{"line number", "Line Number"},
	{"page number", "Page Number"},
	{"index heading", "Index Heading"},
	{"envelope address", "Envelope Address"},
	{"envelope return", "Envelope Return"},
	{"toa heading", "TOA Heading"},
	{"table of authorities", "Table of Authorities"},
}

// uiStyleName is a file style name's display name.
func uiStyleName(n string) string {
	for _, l := range lowerNames {
		if l[0] == n {
			return l[1]
		}
	}
	for _, p := range [][2]string{{"heading ", "Heading "}, {"toc ", "TOC "}, {"index ", "Index "}} {
		if d, ok := strings.CutPrefix(n, p[0]); ok && len(d) == 1 && d[0] >= '0' && d[0] <= '9' {
			return p[1] + d
		}
	}
	return n
}

// readFontFamilies reads the fonts the font table classes as roman.
func (r *reader) readFontFamilies(path string) {
	table, err := r.pkg.XML(path)
	if err != nil || table == nil {
		return
	}
	fonts := doc.Vec[string]{}
	for _, f := range table.Children("w:font") {
		fam := f.Child("w:family")
		if fam == nil || !attrIs(fam, "w:val", "roman") {
			continue
		}
		n, ok := f.Attr("w:name")
		if !ok || n == "" || len(n) > maxFontName {
			continue
		}
		if len(fonts) >= maxTableFonts {
			break
		}
		fonts = append(fonts, n)
	}
	r.doc.Settings.RomanFonts = fonts
}

func (r *reader) revision(kind doc.RevisionKind, e *El) uint32 { return r.revisionNamed(kind, e, nil) }

func (r *reader) revisionNamed(kind doc.RevisionKind, e *El, moveName *string) uint32 {
	author := takeChars(attr(e, "w:author"), 256)
	date := takeChars(attr(e, "w:date"), 64)
	key := revKey{kind: uint8(kind), author: author, date: date}
	if moveName != nil {
		key.moved, key.move = true, *moveName
	}
	if i, ok := r.revs[key]; ok {
		return i
	}
	i := uint32(len(r.doc.Revisions))
	r.doc.Revisions = append(r.doc.Revisions, doc.Revision{Kind: kind, Author: author, Date: date, MoveName: moveName})
	r.revs[key] = i
	return i
}

// mediaFor is the media key for an image relationship (the bytes are loaded once).
func (r *reader) mediaFor(rels Rels, id string) (string, bool) {
	rel, ok := rels.ByID(id)
	if !ok || rel.External {
		return "", false
	}
	if k, ok := r.mediaByPath[rel.Target]; ok {
		return k, true
	}
	b, ok := r.pkg.Get(rel.Target)
	if !ok {
		return "", false
	}
	last := rel.Target
	if i := strings.LastIndexByte(last, '/'); i >= 0 {
		last = last[i+1:]
	}
	var base strings.Builder
	for _, c := range last {
		if c < 0x80 && (isASCIIAlpha(byte(c)) || '0' <= c && c <= '9' || c == '.' || c == '_' || c == '-') {
			base.WriteRune(c)
		}
	}
	bs := base.String()
	if bs == "" || bs[0] == '.' {
		bs = "image" + bs
	}
	key := bs
	for n := 1; ; {
		if _, taken := r.doc.Media[key]; !taken {
			break
		}
		n++
		if i := strings.LastIndexByte(bs, '.'); i >= 0 {
			key = bs[:i] + "_" + itoa(n) + "." + bs[i+1:]
		} else {
			key = bs + "_" + itoa(n)
		}
	}
	if r.doc.Media == nil {
		r.doc.Media = doc.StrMap[doc.Vec[uint8]]{}
	}
	r.doc.Media[key] = doc.Vec[uint8](b)
	r.mediaByPath[rel.Target] = key
	return key, true
}

func (r *reader) loadHeaderFooter(path string, footer bool) (uint32, bool) {
	if id, ok := r.hfByPath[path]; ok {
		return id, true
	}
	if len(r.doc.Parts) >= maxParts || r.hfDepth >= maxHFDepth {
		return 0, false
	}
	// Reserve the id first so a (malformed) self-reference can't recurse.
	kind := doc.PartHeader
	if footer {
		kind = doc.PartFooter
	}
	id := r.doc.AddPart(kind, doc.Vec[doc.Block]{})
	r.hfByPath[path] = id
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return id, true
	}
	rels := r.pkg.Rels(path)
	sc := &storyCtx{inHF: true}
	blocks := doc.Vec[doc.Block]{}
	r.hfDepth++
	r.readBlocks(sc, root, rels, &blocks, 0)
	r.hfDepth = max(r.hfDepth-1, 0)
	r.flushPending(sc, &blocks)
	r.setPartBlocks(id, blocks)
	return id, true
}

func (r *reader) setPartBlocks(id uint32, blocks doc.Vec[doc.Block]) {
	if p, ok := r.doc.Parts[id]; ok && len(blocks) > 0 {
		p.Blocks = blocks
		r.doc.Parts[id] = p
	}
}

var themeSlots = []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"}

func (r *reader) readTheme(path string) {
	t, err := r.pkg.XML(path)
	if err != nil || t == nil {
		return
	}
	s := &r.doc.Settings
	if n, ok := t.Attr("name"); ok && n != "" {
		s.ThemeName = n
	}
	el := t.Child("a:themeElements")
	if el == nil {
		return
	}
	if cs := el.Child("a:clrScheme"); cs != nil {
		colors := make(doc.Vec[doc.RGB], 12)
		copy(colors, s.ThemeColors)
		for i := len(s.ThemeColors); i < 12; i++ {
			colors[i] = doc.Black
		}
		for i, n := range themeSlots {
			c := cs.Child("a:" + n)
			if c == nil {
				continue
			}
			var rgb doc.RGB
			ok := false
			if x := c.Child("a:srgbClr"); x != nil {
				rgb, ok = parseRGBAttr(x, "val")
			}
			if !ok {
				if x := c.Child("a:sysClr"); x != nil {
					rgb, ok = parseRGBAttr(x, "lastClr")
				}
			}
			if ok {
				colors[i] = rgb
			}
		}
		r.pc.ThemeColors = slices.Clone(colors)
		s.ThemeColors = colors
	}
	if fs := el.Child("a:fontScheme"); fs != nil {
		face := func(n string) (string, bool) {
			f := fs.Child(n)
			if f == nil {
				return "", false
			}
			l := f.Child("a:latin")
			if l == nil {
				return "", false
			}
			v, ok := l.Attr("typeface")
			return v, ok && v != ""
		}
		if f, ok := face("a:majorFont"); ok {
			s.MajorFont = f
		}
		if f, ok := face("a:minorFont"); ok {
			s.MinorFont = f
		}
		cs := func(n string) string {
			if f := fs.Child(n); f != nil {
				if c := f.Child("a:cs"); c != nil {
					return attr(c, "typeface")
				}
			}
			return ""
		}
		r.pc.MajorCS = cs("a:majorFont")
		r.pc.MinorCS = cs("a:minorFont")
	}
	r.pc.MajorFont = s.MajorFont
	r.pc.MinorFont = s.MinorFont
	r.pc.Theme = slices.Clone(s.ThemeColors)
}

// readColorMapping reads w:settings/w:clrSchemeMapping: which scheme colour text1, background1…
// stand for.
func (r *reader) readColorMapping(path string) {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return
	}
	m := root.Child("w:clrSchemeMapping")
	if m == nil {
		return
	}
	names := []string{"dark1", "light1", "dark2", "light2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hyperlink", "followedHyperlink"}
	attrs := []string{"w:t1", "w:bg1", "w:t2", "w:bg2", "w:accent1", "w:accent2", "w:accent3", "w:accent4", "w:accent5", "w:accent6", "w:hyperlink", "w:followedHyperlink"}
	var cm [12]int
	for i := range cm {
		cm[i] = i
		if v, ok := m.Attr(attrs[i]); ok {
			if j := slices.Index(names, v); j >= 0 {
				cm[i] = j
			}
		}
	}
	r.pc.ColorMap = &cm
}

func isTrue1(e *El, name string) bool {
	v, ok := e.Attr(name)
	return ok && (v == "1" || v == "true")
}

// maxLatentStylesXML is the largest w:latentStyles kept.
const maxLatentStylesXML = 1 << 20

func (r *reader) readStyles(path string) error {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return err
	}
	// The default paragraph style must be called Normal in the model.
	styles := root.Children("w:style")
	hasNormal := slices.ContainsFunc(styles, func(s *El) bool { return attrIs(s, "w:styleId", "Normal") })
	if !hasNormal {
		for _, s := range styles {
			if attrIs(s, "w:type", "paragraph") && isTrue1(s, "w:default") {
				if id, ok := s.Attr("w:styleId"); ok {
					r.pc.Alias[id] = "Normal"
				}
				break
			}
		}
	}
	sheet := doc.EmptyStyleSheet()
	dd := root.Child("w:docDefaults")
	sheet.DefaultParaPresent = dd != nil && dd.Child("w:pPrDefault") != nil
	if dd != nil {
		if d := dd.Child("w:rPrDefault"); d != nil {
			if rp := d.Child("w:rPr"); rp != nil {
				sheet.DefaultChr = r.pc.RPr(rp)
			}
		}
		if d := dd.Child("w:pPrDefault"); d != nil {
			if pp := d.Child("w:pPr"); pp != nil {
				sheet.DefaultPara, _ = r.pc.PPr(pp)
			}
		}
	}
	if sheet.DefaultChr.Size == nil {
		sheet.DefaultChr.Size = f32p(10)
	}
	// Built-in styles' behaviour (gallery, priority, visibility), written back as is.
	if l := root.Child("w:latentStyles"); l != nil {
		if x := l.ToXML(); len(x) <= maxLatentStylesXML {
			sheet.LatentStyles = &x
		}
	}
	for i, s := range styles {
		if i >= 10_000 {
			break
		}
		rawID, ok := s.Attr("w:styleId")
		if !ok || rawID == "" {
			continue
		}
		id := r.pc.StyleID(rawID)
		kind := doc.StyleParagraph
		switch t, _ := s.Attr("w:type"); t {
		case "character":
			kind = doc.StyleCharacter
		case "table":
			kind = doc.StyleTable
		case "numbering":
			kind = doc.StyleNumbering
		}
		name := id
		if v, ok := s.ChildVal("w:name"); ok {
			name = uiStyleName(v)
		}
		ref := func(n string) *string {
			if v, ok := s.ChildVal(n); ok && v != "" {
				return ptr(r.pc.StyleID(v))
			}
			return nil
		}
		fl := func(n string) bool {
			v, _ := flag(s, n)
			return v
		}
		st := doc.Style{
			ID:      id,
			Name:    name,
			Kind:    kind,
			BasedOn: ref("w:basedOn"),
			Next:    ref("w:next"),
			Linked:  ref("w:link"),
			Quick:   fl("w:qFormat"),
			Hidden:  fl("w:semiHidden") || fl("w:hidden"),
			Builtin: !isTrue1(s, "w:customStyle"),
			Locked:  fl("w:locked"),
		}
		if st.BasedOn != nil && *st.BasedOn == id {
			st.BasedOn = nil
		}
		if v, ok := childU32(s, "w:uiPriority"); ok {
			st.Priority = &v
		}
		// Only where the writer couldn't tell from the id (it flags the English ids).
		st.Default = isTrue1(s, "w:default") && !(kind == doc.StyleParagraph && id == "Normal" ||
			kind == doc.StyleCharacter && id == "DefaultParagraphFont" ||
			kind == doc.StyleTable && id == "TableNormal" ||
			kind == doc.StyleNumbering && id == "NoList")
		if p := s.Child("w:pPr"); p != nil {
			st.Para, _ = r.pc.PPr(p)
		}
		if rp := s.Child("w:rPr"); rp != nil {
			st.Chr = r.pc.RPr(rp)
		}
		if kind == doc.StyleTable {
			parts := r.tableStyleParts(s, &st)
			if len(parts.Regions) == 0 {
				parts.Regions = nil
			}
			if !isZeroParts(parts) {
				st.Table = &parts
			}
		}
		// A style defined twice is the last definition, whole: Word drops the earlier one
		// rather than merging the two.
		if j := slices.IndexFunc(sheet.Styles, func(x doc.Style) bool { return x.ID == id }); j >= 0 {
			sheet.Styles[j] = st
		} else {
			sheet.Styles = append(sheet.Styles, st)
		}
	}
	// Styles that give only a list level take the list from the style they are based on.
	fixed := make([]*doc.NumRef, len(sheet.Styles))
	for i, s := range sheet.Styles {
		n := s.Para.Numbering
		if n != nil && n.Num == levelOnly {
			fixed[i] = nil
			if s.BasedOn != nil {
				if num, ok := chainList(&sheet, *s.BasedOn); ok {
					fixed[i] = &doc.NumRef{Num: num, Level: n.Level}
				}
			}
		} else {
			fixed[i] = n
		}
	}
	for i := range sheet.Styles {
		sheet.Styles[i].Para.Numbering = fixed[i]
	}
	if n := sheet.DefaultPara.Numbering; n != nil && n.Num == levelOnly {
		sheet.DefaultPara.Numbering = nil
	}
	// A producer that never writes w:qFormat (LibreOffice) still gets Word's usual gallery.
	if !slices.ContainsFunc(sheet.Styles, func(s doc.Style) bool { return s.Quick }) {
		sheet.AddDefaultGallery()
	}
	r.doc.Styles = sheet
	return nil
}

func isZeroParts(t doc.TableStyleParts) bool {
	a, err1 := jsonOf(t)
	b, err2 := jsonOf(doc.TableStyleParts{})
	return err1 == nil && err2 == nil && a == b
}

// tableStyleParts reads a table style's table-level formatting and conditional formats. The
// wholeTable region applies to every cell, so its paragraph and run properties go to the style's
// own (st).
func (r *reader) tableStyleParts(s *El, st *doc.Style) doc.TableStyleParts {
	var t doc.TableStyleParts
	if tp := s.Child("w:tblPr"); tp != nil {
		if b := tp.Child("w:tblBorders"); b != nil {
			t.Borders = ptr(borders(b))
		}
		if e := tp.Child("w:shd"); e != nil {
			t.Fill = r.pc.shdFillPtr(e)
		}
		if e := tp.Child("w:tblCellMar"); e != nil {
			t.CellMargins = ptr(margins(e))
		}
		if n, ok := childU32(tp, "w:tblStyleRowBandSize"); ok && n > 0 {
			t.BandSize = ptr(min(n, 1000))
		}
	}
	for i, c := range s.Children("w:tblStylePr") {
		if i >= 64 {
			break
		}
		v, ok := c.Attr("w:type")
		if !ok {
			continue
		}
		part, ok := doc.TablePartFromOOXML(v)
		if !ok {
			continue
		}
		tc := c.Child("w:tcPr")
		var ppr *doc.ParaProps
		if p := c.Child("w:pPr"); p != nil {
			pp, _ := r.pc.PPr(p)
			ppr = &pp
		}
		var f doc.PartFormat
		if rp := c.Child("w:rPr"); rp != nil {
			f.Chr = r.pc.RPr(rp)
		}
		if tc != nil {
			if e := tc.Child("w:shd"); e != nil {
				f.Fill = r.pc.shdFillPtr(e)
			}
			if e := tc.Child("w:tcBorders"); e != nil {
				f.Borders = ptr(borders(e))
			}
			if v, ok := tc.ChildVal("w:vAlign"); ok {
				va := doc.VAlignTop
				switch v {
				case "center":
					va = doc.VAlignCenter
				case "bottom":
					va = doc.VAlignBottom
				}
				f.VAlign = &va
			}
		}
		if ppr != nil {
			f.Align = ppr.Align
		}
		if part == doc.WholeTable {
			// The whole table's paragraph and run properties are the style's own; its borders
			// the table borders.
			if ppr != nil {
				st.Para.Overlay(ppr)
			}
			st.Chr.Overlay(&f.Chr)
			if f.Fill != nil {
				t.Fill = f.Fill
			}
			if tp := c.Child("w:tblPr"); tp != nil {
				if b := tp.Child("w:tblBorders"); b != nil {
					t.Borders = ptr(borders(b))
				}
			}
			if f.VAlign != nil {
				if t.Regions == nil {
					t.Regions = doc.TableRegions{}
				}
				t.Regions[part] = doc.PartFormat{VAlign: f.VAlign}
			}
		} else {
			t.SetPart(part, f)
		}
	}
	return t
}

func isHex8(v string) bool {
	if len(v) != 8 {
		return false
	}
	for i := range 8 {
		c := v[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

func toASCIIUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'a' <= c && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}

func (r *reader) readNumbering(path string) error {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return err
	}
	var abstracts doc.Vec[doc.AbstractNum]
	type link struct {
		id    uint32
		style string
	}
	var links []link
	for i, a := range root.Children("w:abstractNum") {
		if i >= 10_000 {
			break
		}
		v, ok := a.Attr("w:abstractNumId")
		if !ok {
			continue
		}
		id, ok := u32Of(v)
		if !ok {
			continue
		}
		levels := make(doc.Vec[doc.Level], 9)
		for j := range levels {
			levels[j] = defaultLevel()
		}
		for _, l := range a.Children("w:lvl") {
			i := 0
			if v, ok := l.Attr("w:ilvl"); ok {
				if n, ok := u32Of(v); ok {
					i = int(n)
				}
			}
			if i >= 0 && i < len(levels) {
				levels[i] = r.level(l, i)
			}
		}
		if v, ok := a.ChildVal("w:numStyleLink"); ok {
			links = append(links, link{id, v})
		}
		an := doc.AbstractNum{ID: id, Levels: levels}
		if v, ok := a.ChildVal("w:name"); ok {
			an.Name = ptr(v)
		}
		if v, ok := a.ChildVal("w:nsid"); ok && isHex8(v) {
			an.NSID = ptr(toASCIIUpper(v))
		}
		if v, ok := a.ChildVal("w:tmpl"); ok && isHex8(v) {
			an.Tmpl = ptr(toASCIIUpper(v))
		}
		if v, ok := a.ChildVal("w:multiLevelType"); ok && (v == "singleLevel" || v == "hybridMultilevel") {
			an.MultiLevelType = ptr(v)
		}
		abstracts = append(abstracts, an)
	}
	var nums doc.Vec[doc.Num]
	for i, n := range root.Children("w:num") {
		if i >= 10_000 {
			break
		}
		v, ok := n.Attr("w:numId")
		if !ok {
			continue
		}
		id, ok := u32Of(v)
		if !ok {
			continue
		}
		abs, ok := childU32(n, "w:abstractNumId")
		if !ok {
			continue
		}
		num := doc.Num{ID: id, AbstractID: abs, StartOverrides: doc.Vec[doc.Pair[uint8, uint32]]{}}
		for j, o := range n.Children("w:lvlOverride") {
			if j >= 9 {
				break
			}
			lvl := uint32(0)
			if v, ok := o.Attr("w:ilvl"); ok {
				if m, ok := u32Of(v); ok {
					lvl = m
				}
			}
			l8 := uint8(min(lvl, 8))
			if s, ok := childU32(o, "w:startOverride"); ok {
				num.StartOverrides = append(num.StartOverrides, doc.Pair[uint8, uint32]{A: l8, B: s})
			} else if l := o.Child("w:lvl"); l != nil {
				if s, ok := childU32(l, "w:start"); ok {
					num.StartOverrides = append(num.StartOverrides, doc.Pair[uint8, uint32]{A: l8, B: s})
				}
			}
			// A whole level definition replaces the abstract list's for this list.
			if l := o.Child("w:lvl"); l != nil {
				num.LevelOverrides = append(num.LevelOverrides, doc.Pair[uint8, doc.Level]{A: l8, B: r.level(l, int(l8))})
			}
		}
		nums = append(nums, num)
	}
	// Abstracts that only link to a numbering style take that style's list levels.
	for _, l := range links {
		st, ok := r.doc.Styles.Get(r.pc.StyleID(l.style))
		if !ok || st.Para.Numbering == nil {
			continue
		}
		n := st.Para.Numbering.Num
		k := slices.IndexFunc(nums, func(x doc.Num) bool { return x.ID == n })
		if k < 0 {
			continue
		}
		src := nums[k].AbstractID
		if src == l.id {
			continue
		}
		s := slices.IndexFunc(abstracts, func(a doc.AbstractNum) bool { return a.ID == src })
		if s < 0 {
			continue
		}
		levels := slices.Clone(abstracts[s].Levels)
		if a := slices.IndexFunc(abstracts, func(a doc.AbstractNum) bool { return a.ID == l.id }); a >= 0 {
			abstracts[a].Levels = levels
		}
	}
	if abstracts == nil {
		abstracts = doc.Vec[doc.AbstractNum]{}
	}
	if nums == nil {
		nums = doc.Vec[doc.Num]{}
	}
	r.doc.Numbering.Abstracts = abstracts
	r.doc.Numbering.Nums = nums
	return nil
}

// defaultLevel is a level as the file defines it before its own elements: no indents, no text.
func defaultLevel() doc.Level {
	l := doc.NewLevel()
	l.Indent, l.Hanging, l.Text = 0, 0, ""
	return l
}

func (r *reader) level(l *El, i int) doc.Level {
	_ = i
	lv := defaultLevel()
	if s, ok := childU32(l, "w:start"); ok {
		lv.Start = s
	}
	if f, ok := l.ChildVal("w:numFmt"); ok {
		lv.Format = doc.NumFormatFromOOXML(f)
	}
	lv.Text = ""
	if t, ok := l.ChildVal("w:lvlText"); ok {
		lv.Text = takeChars(t, 64)
	}
	if j, ok := l.ChildVal("w:lvlJc"); ok {
		lv.Align = align(j)
	}
	if p := l.Child("w:pPr"); p != nil {
		if ind := p.Child("w:ind"); ind != nil {
			if v, ok := tw(ind, "w:left"); ok {
				lv.Indent = doc.F32(v)
			} else if v, ok := tw(ind, "w:start"); ok {
				lv.Indent = doc.F32(v)
			}
			if h, ok := tw(ind, "w:hanging"); ok {
				lv.Hanging = doc.F32(h)
			} else if f, ok := tw(ind, "w:firstLine"); ok {
				lv.Hanging = doc.F32(-f)
			}
		}
	}
	if rp := l.Child("w:rPr"); rp != nil {
		lv.Chr = r.pc.RPr(rp)
	}
	switch v, _ := l.ChildVal("w:suff"); v {
	case "space":
		lv.Suffix = doc.SuffixSpace
	case "nothing":
		lv.Suffix = doc.SuffixNothing
	default:
		lv.Suffix = doc.SuffixTab
	}
	lv.Legal, _ = flag(l, "w:isLgl")
	restart, hasRestart := childInt(l, "w:lvlRestart")
	lv.Restart = !hasRestart || restart != 0
	lv.RestartAfter = nil
	if hasRestart && restart >= 1 && restart <= 9 {
		lv.RestartAfter = ptr(uint8(restart))
	}
	// The tab stop after the number ("num" tab in the level's paragraph properties).
	lv.Tab = nil
	if p := l.Child("w:pPr"); p != nil {
		if tabs := p.Child("w:tabs"); tabs != nil {
			for _, t := range tabs.Children("w:tab") {
				if attrIs(t, "w:val", "num") {
					if v, ok := tw(t, "w:pos"); ok {
						lv.Tab = f32p(fclamp(v, -1584, 1584))
					}
					break
				}
			}
		}
	}
	lv.Style = nil
	if s, ok := l.ChildVal("w:pStyle"); ok && s != "" {
		lv.Style = ptr(r.pc.StyleID(s))
	}
	return lv
}

func (r *reader) readSettings(path string) error {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return err
	}
	if b, ok := r.pkg.Get(path); ok && settingsHasUnowned(b) {
		r.doc.Passthrough = ensureMap(r.doc.Passthrough)
		r.doc.Passthrough[settingsOrig] = slices.Clone(b)
	}
	rels := r.pkg.Rels(path)
	s := &r.doc.Settings
	for _, k := range root.Els() {
		switch k.Name {
		case "w:attachedTemplate":
			// §17.15.1.6: the template, by a relationship (usually external) of the settings part.
			s.AttachedTemplate = nil
			if id, ok := k.Attr("r:id"); ok {
				if rel, ok := rels.ByID(id); ok {
					if t := rustcompat.Trim(rel.Target); t != "" && len(t) <= maxTemplatePath {
						s.AttachedTemplate = ptr(t)
					}
				}
			}
		case "w:linkStyles":
			s.LinkStyles = onOff(k)
		case "w:removePersonalInformation":
			s.RemovePersonalInfo = onOff(k)
		case "w:removeDateAndTime":
			s.RemoveDateTime = onOff(k)
		case "w:compat":
			c := k.Child("w:allowSpaceOfSameStyleInTable")
			s.AllowSpaceOfSameStyleInTable = c != nil && onOff(c)
			// Only Word's own setting: another w:uri names a different application's.
			for _, c := range k.Children("w:compatSetting") {
				uri, hasURI := c.Attr("w:uri")
				if attrIs(c, "w:name", "compatibilityMode") && (!hasURI || uri == "http://schemas.microsoft.com/office/word") {
					if v, ok := c.Attr("w:val"); ok {
						if m, ok := u32Of(v); ok {
							s.CompatMode = min(max(m, 11), 99)
						}
					}
					break
				}
			}
			s.SumParagraphSpacing = slices.ContainsFunc(k.Children("w:doNotUseHTMLParagraphAutoSpacing"), onOff)
		case "w:trackRevisions":
			s.TrackChanges = onOff(k)
		case "w:doNotTrackMoves":
			s.TrackMoves = !onOff(k)
		case "w:defaultTabStop":
			if v, ok := tw(k, "w:val"); ok && v > 0 {
				s.DefaultTab = doc.F32(fclamp(v, 1, 1584))
			}
		case "w:evenAndOddHeaders":
			s.EvenOddHeaders = onOff(k)
		case "w:drawingGridHorizontalSpacing", "w:drawingGridVerticalSpacing":
			if v, ok := tw(k, "w:val"); ok && v > 0 {
				v = fclamp(v, 0.5, 1584)
				if k.Name == "w:drawingGridHorizontalSpacing" {
					s.GridH = doc.F32(v)
				} else {
					s.GridV = doc.F32(v)
				}
			}
		case "m:mathPr":
			r.unported("math settings (m:mathPr)")
		case "w:mirrorMargins":
			s.MirrorMargins = onOff(k)
		case "w:autoHyphenation":
			s.AutoHyphenation = onOff(k)
		case "w:doNotHyphenateCaps":
			s.HyphenateCaps = !onOff(k)
		case "w:hyphenationZone":
			if v, ok := tw(k, "w:val"); ok && v >= 0 {
				s.HyphenationZone = doc.F32(rustcompat.Min(v, 1584))
			}
		case "w:consecutiveHyphenLimit":
			if v, ok := k.Attr("w:val"); ok {
				if n, ok := u32Of(v); ok {
					s.ConsecutiveHyphenLimit = min(n, 32_767)
				}
			}
		case "w:footnotePr":
			if f, ok := k.ChildVal("w:numFmt"); ok {
				s.FootnoteFormat = doc.NumFormatFromOOXML(f)
			}
			s.FootnotePr = notePr(k, false)
		case "w:endnotePr":
			if f, ok := k.ChildVal("w:numFmt"); ok {
				s.EndnoteFormat = doc.NumFormatFromOOXML(f)
			}
			s.EndnotePr = notePr(k, false)
		case "w:documentProtection":
			readProtection(k, s)
		case "w:zoom":
			percent := uint32(100)
			if v, ok := k.Attr("w:percent"); ok {
				if n, ok := u32Of(strings.TrimRight(v, "%")); ok {
					percent = n
				}
			}
			percent = min(max(percent, 10), 500)
			var fit *doc.ZoomFit
			switch attr(k, "w:val") {
			case "fullPage":
				fit = ptr(doc.FitOnePage)
			case "bestFit":
				fit = ptr(doc.FitPageWidth)
			case "textFit":
				fit = ptr(doc.FitTextWidth)
			}
			s.Zoom = nil
			if percent != 100 || fit != nil {
				s.Zoom = &doc.Zoom{Percent: percent, Fit: fit}
			}
		}
	}
	return nil
}

// protectionHashAttrs are the password hash attributes of w:documentProtection that are kept.
var protectionHashAttrs = []string{
	"w:algorithmName", "w:hashValue", "w:saltValue", "w:spinCount", "w:cryptProviderType", "w:cryptAlgorithmClass",
	"w:cryptAlgorithmType", "w:cryptAlgorithmSid", "w:cryptSpinCount", "w:cryptProvider", "w:algIdExt", "w:algIdExtSource",
	"w:cryptProviderTypeExt", "w:cryptProviderTypeExtSource", "w:hash", "w:salt",
}

// readProtection reads Restrict Editing (w:documentProtection, §17.15.1.29). Only enforced
// protection is kept.
func readProtection(k *El, s *doc.Settings) {
	on := func(v string) bool { return !(v == "0" || v == "false" || v == "off") }
	if v, ok := k.Attr("w:enforcement"); !ok || !on(v) {
		return
	}
	edit, hasEdit := k.Attr("w:edit")
	hasEdit = hasEdit && (edit == "readOnly" || edit == "comments" || edit == "trackedChanges" || edit == "forms")
	fv, ok := k.Attr("w:formatting")
	formatting := ok && on(fv)
	if !hasEdit && !formatting {
		return
	}
	if !hasEdit {
		edit = "none"
	}
	s.Protection = &edit
	s.ProtectFormatting = formatting
	s.ProtectHash = doc.Vec[[2]string]{}
	for _, n := range protectionHashAttrs {
		if v, ok := k.Attr(n); ok && v != "" && len(v) <= 1024 {
			s.ProtectHash = append(s.ProtectHash, [2]string{n, v})
		}
	}
}

func (r *reader) readNotes(path string, foot bool) error {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return err
	}
	rels := r.pkg.Rels(path)
	tag, pk, kind := "w:endnote", doc.PartEndnote, doc.Endnote
	if foot {
		tag, pk, kind = "w:footnote", doc.PartFootnote, doc.Footnote
	}
	for _, n := range root.Children(tag) {
		if t, ok := n.Attr("w:type"); ok && t != "normal" {
			if t == "separator" || t == "continuationSeparator" || t == "continuationNotice" {
				r.readNoteSep(n, rels, foot, t)
			}
			continue
		}
		v, ok := n.Attr("w:id")
		if !ok {
			continue
		}
		fid, ok := intOf(v)
		if !ok {
			continue
		}
		if len(r.doc.Parts) >= maxParts {
			break
		}
		// Reserve the id first: the note's own reference mark points at it.
		id := r.doc.AddPart(pk, doc.Vec[doc.Block]{})
		sc := &storyCtx{}
		blocks := doc.Vec[doc.Block]{}
		r.currentNote = &noteID{kind, id}
		r.readBlocks(sc, n, rels, &blocks, 0)
		r.flushPending(sc, &blocks)
		r.currentNote = nil
		r.setPartBlocks(id, blocks)
		if foot {
			r.footnotes[fid] = id
		} else {
			r.endnotes[fid] = id
		}
	}
	return nil
}

// readNoteSep reads a document's own separator, continuation separator or continuation notice
// for its footnotes or endnotes. The first of each kind counts.
func (r *reader) readNoteSep(n *El, rels Rels, foot bool, kind string) {
	seps := &r.doc.EndnoteSeparators
	if foot {
		seps = &r.doc.FootnoteSeparators
	}
	slot := &seps.ContinuationNotice
	switch kind {
	case "separator":
		slot = &seps.Separator
	case "continuationSeparator":
		slot = &seps.ContinuationSeparator
	}
	if *slot != nil {
		return
	}
	sc := &storyCtx{}
	blocks := doc.Vec[doc.Block]{}
	r.inNoteSep = true
	r.readBlocks(sc, n, rels, &blocks, 0)
	r.flushPending(sc, &blocks)
	r.inNoteSep = false
	if len(blocks) == 0 {
		blocks = append(blocks, doc.Block{Para: doc.NewParagraph()})
	}
	*slot = &blocks
}

// readApp keeps the extended properties a save carries over.
func (r *reader) readApp(path string) {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return
	}
	var kept strings.Builder
	for _, e := range root.Els() {
		name, value := e.Local(), e.Text()
		if slices.Contains(appKeptProps, name) && len(value) <= 2048 && !strings.ContainsAny(value, "\t\n") {
			kept.WriteString(name + "\t" + value + "\n")
		}
	}
	if kept.Len() > 0 {
		r.doc.Passthrough = ensureMap(r.doc.Passthrough)
		r.doc.Passthrough[appKept] = doc.Vec[uint8](kept.String())
	}
}

// localAttr is the first attribute whose local name is name.
func localAttr(e *El, name string) (string, bool) {
	for _, a := range e.Attrs {
		k := a.Key
		if i := strings.LastIndexByte(k, ':'); i >= 0 {
			k = k[i+1:]
		}
		if k == name {
			return a.Val, true
		}
	}
	return "", false
}

// durableIDOK reports whether d is a usable w16cid:durableId: 1–8 hex digits, below 0x7FFFFFFF,
// not 0.
func durableIDOK(d string) bool {
	v, ok := fromStrRadix16(d, 32)
	return len(d) <= 8 && ok && v != 0 && v < 0x7FFF_FFFF
}

func (r *reader) readComments(path, ext, ids, cex string) error {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return err
	}
	rels := r.pkg.Rels(path)
	byPara := map[string]uint32{}
	nextID := uint32(0)
	entries := doc.U32Map[doc.Comment]{}
	used := func(id uint32) bool {
		for _, v := range r.commentMap {
			if v == id {
				return true
			}
		}
		return false
	}
	for _, c := range root.Children("w:comment") {
		if len(r.doc.Parts) >= maxParts {
			break
		}
		fileID := attr(c, "w:id")
		if _, ok := r.commentMap[fileID]; ok {
			continue
		}
		var id uint32
		if v, ok := rustcompat.ParseUint(fileID, 32); ok && !hasKey(entries, uint32(v)) {
			id = uint32(v)
		} else {
			for hasKey(entries, nextID) || used(nextID) {
				nextID++
			}
			id = nextID
		}
		sc := &storyCtx{}
		blocks := doc.Vec[doc.Block]{}
		r.readBlocks(sc, c, rels, &blocks, 0)
		r.flushPending(sc, &blocks)
		part := r.doc.AddPart(doc.PartComment, blocks)
		if ps := c.Children("w:p"); len(ps) > 0 {
			if pid, ok := ps[len(ps)-1].Attr("w14:paraId"); ok {
				byPara[pid] = id
			}
		}
		entries[id] = doc.Comment{Author: attr(c, "w:author"), Initials: attr(c, "w:initials"), Date: attr(c, "w:date"), Part: part}
		r.commentMap[fileID] = id
	}
	if ext != "" {
		if x, err := r.pkg.XML(ext); err == nil && x != nil {
			for _, e := range x.Els() {
				if e.Local() != "commentEx" {
					continue
				}
				pid, ok := e.Attr("w15:paraId")
				if !ok {
					continue
				}
				cid, ok := byPara[pid]
				if !ok {
					continue
				}
				var parent *uint32
				if pp, ok := e.Attr("w15:paraIdParent"); ok {
					if p, ok := byPara[pp]; ok && p != cid {
						parent = &p
					}
				}
				done := isTrue1(e, "w15:done")
				if c, ok := entries[cid]; ok {
					c.Parent, c.Resolved = parent, done
					entries[cid] = c
				}
			}
		}
	}
	// Durable ids (by paragraph id) and UTC dates (by durable id), which the writer carries
	// over. Their namespaces (w16cid, w16cex) have no canonical prefix here, so attributes are
	// found by local name.
	byDurable := map[string]uint32{}
	if ids != "" {
		if x, err := r.pkg.XML(ids); err == nil && x != nil {
			for _, e := range x.Els() {
				if e.Local() != "commentId" {
					continue
				}
				pid, ok := localAttr(e, "paraId")
				if !ok {
					continue
				}
				cid, ok := byPara[pid]
				if !ok {
					continue
				}
				durable, ok := localAttr(e, "durableId")
				if !ok || !durableIDOK(durable) {
					continue
				}
				if c, ok := entries[cid]; ok {
					up := toASCIIUpper(durable)
					c.DurableID = &up
					entries[cid] = c
					byDurable[up] = cid
				}
			}
		}
	}
	if cex != "" {
		if x, err := r.pkg.XML(cex); err == nil && x != nil {
			for _, e := range x.Els() {
				if e.Local() != "commentExtensible" {
					continue
				}
				d, ok := localAttr(e, "durableId")
				if !ok {
					continue
				}
				cid, ok := byDurable[toASCIIUpper(d)]
				if !ok {
					continue
				}
				date, ok := localAttr(e, "dateUtc")
				if !ok || len(date) > 40 || strings.IndexFunc(date, func(c rune) bool {
					return !(c < 0x80 && (isASCIIAlpha(byte(c)) || '0' <= c && c <= '9') || strings.ContainsRune(":-.+", c))
				}) >= 0 {
					continue
				}
				if c, ok := entries[cid]; ok {
					c.DateUTC = &date
					entries[cid] = c
				}
			}
		}
	}
	r.doc.Comments = entries
	return nil
}

func hasKey[V any](m doc.U32Map[V], k uint32) bool {
	_, ok := m[k]
	return ok
}

func (r *reader) readCore(path string) {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil {
		return
	}
	c := &r.doc.Core
	for _, k := range root.Els() {
		t := rustcompat.Trim(k.Text())
		switch k.Name {
		case "dc:title":
			c.Title = t
		case "dc:subject":
			c.Subject = t
		case "dc:creator":
			c.Creator = t
		case "cp:keywords":
			c.Keywords = t
		case "dc:description":
			c.Description = t
		case "cp:lastModifiedBy":
			c.LastModifiedBy = t
		case "cp:revision":
			c.Revision, _ = u32Of(t)
		case "dcterms:created":
			c.Created = t
		case "dcterms:modified":
			c.Modified = t
		case "cp:category":
			c.Category = t
		}
	}
}

// handledRels are relationship types (last path segment) the reader models or the writer
// produces itself, so they are never carried verbatim.
var handledRels = []string{
	"officeDocument", "core-properties", "extended-properties", "custom-properties", "styles", "numbering", "settings", "theme",
	"fontTable", "footnotes", "endnotes", "comments", "commentsExtended", "commentsIds", "commentsExtensible", "customXml", "vbaProject",
	"header", "footer", "image", "hyperlink", "oleObject", "package", "chart", "chartEx", "diagramData", "diagramDrawing", "diagramLayout",
	"diagramQuickStyle", "diagramColors", "video", "audio", "media", "control", "subDocument", "ink",
}

// Limits of the parts kept verbatim (Rust MAX_EMBEDDED_PARTS, MAX_EMBEDDED_RELS).
const (
	maxEmbeddedParts = 4096
	maxEmbeddedRels  = 1024
)

// manifestFieldOK reports whether s can be a field of a manifest line.
func manifestFieldOK(s string) bool { return s != "" && !strings.ContainsAny(s, "\t\r\n") }

// keepUnmodelled keeps the parts the package and the main document relate to that WordCraft
// neither models nor writes itself, with every part they reach, so saving carries them
// unchanged. Skipped: the relationship types the writer produces or the reader models and any
// relationship the main document's markup refers to by id.
func (r *reader) keepUnmodelled(pkgRels, docRels Rels) {
	main, _ := r.pkg.Get(r.main)
	referenced := func(id string) bool { return bytes.Contains(main, []byte(`"`+id+`"`)) }
	var roots strings.Builder
	glossary := false
	for _, owner := range []struct {
		name string
		rels Rels
	}{{"P", pkgRels}, {"D", docRels}} {
		n := 0
		for _, rel := range owner.rels {
			if rel.External {
				continue
			}
			if n >= maxEmbeddedRels {
				break
			}
			n++
			tail := rel.Kind
			if i := strings.LastIndexByte(tail, '/'); i >= 0 {
				tail = tail[i+1:]
			}
			skip := slices.Contains(handledRels, tail) || owner.name == "D" && referenced(rel.ID)
			if !skip && owner.name == "D" && tail == "glossaryDocument" {
				skip = glossary
				glossary = true
			}
			if skip || !manifestFieldOK(rel.Kind) || !manifestFieldOK(rel.Target) {
				continue
			}
			if r.keepPartTree(rel.Target) {
				roots.WriteString(owner.name + "\t" + rel.Kind + "\t" + rel.Target + "\n")
			}
		}
	}
	if roots.Len() > 0 {
		r.doc.Passthrough = ensureMap(r.doc.Passthrough)
		r.doc.Passthrough[unmodelledRoot] = doc.Vec[uint8](roots.String())
	}
}

// keepPartTree keeps part root and every part it reaches through its relationships. False when
// root itself can't be kept.
func (r *reader) keepPartTree(root string) bool {
	if _, ok := r.embedded[root]; ok {
		return true
	}
	if !r.keepPart(root) {
		return false
	}
	todo := []string{root}
	for len(todo) > 0 {
		part := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		var kept Rels
		for _, rel := range r.pkg.Rels(part) {
			if len(kept) >= maxEmbeddedRels {
				break
			}
			if !manifestFieldOK(rel.ID) || !manifestFieldOK(rel.Kind) || !manifestFieldOK(rel.Target) ||
				slices.ContainsFunc(kept, func(k Rel) bool { return k.ID == rel.ID }) {
				continue
			}
			if _, ok := r.embedded[rel.Target]; !rel.External && !ok {
				if !r.keepPart(rel.Target) {
					continue
				}
				todo = append(todo, rel.Target)
			}
			kept = append(kept, rel)
		}
		if p, ok := r.embedded[part]; ok {
			p.rels = kept
		}
	}
	return true
}

// keepPart keeps one part's bytes and content type (not its relationships).
func (r *reader) keepPart(part string) bool {
	lower := toASCIILower(part)
	if !manifestFieldOK(part) || strings.HasSuffix(lower, ".rels") || lower == "[content_types].xml" || asciiEqualFold(part, r.main) {
		return false
	}
	if len(r.embedded) >= maxEmbeddedParts {
		return false
	}
	b, ok := r.pkg.Get(part)
	if !ok {
		return false
	}
	ct, ok := r.types().Of(part)
	if !ok {
		ct = "application/octet-stream"
	}
	if !manifestFieldOK(ct) {
		ct = "application/octet-stream"
	}
	r.doc.Passthrough = ensureMap(r.doc.Passthrough)
	if _, ok := r.doc.Passthrough[part]; !ok {
		r.doc.Passthrough[part] = doc.Vec[uint8](b)
	}
	r.embedded[part] = &embeddedPart{contentType: ct}
	return true
}

// manifestBytes writes the kept parts as an EmbeddedManifest: P\tpart\tcontent type per part,
// then R\tpart\tid\ttype\ttarget\tI|E per relationship, parts in name order.
func (r *reader) manifestBytes() doc.Vec[uint8] {
	names := slices.Collect(maps.Keys(r.embedded))
	sort.Strings(names)
	var s strings.Builder
	for _, n := range names {
		p := r.embedded[n]
		if manifestFieldOK(n) && manifestFieldOK(p.contentType) {
			s.WriteString("P\t" + n + "\t" + p.contentType + "\n")
		}
	}
	for _, n := range names {
		for _, rel := range r.embedded[n].rels {
			if !manifestFieldOK(rel.ID) || !manifestFieldOK(rel.Kind) || !manifestFieldOK(rel.Target) {
				continue
			}
			mode := "I"
			if rel.External {
				mode = "E"
			}
			s.WriteString("R\t" + n + "\t" + rel.ID + "\t" + rel.Kind + "\t" + rel.Target + "\t" + mode + "\n")
		}
	}
	return doc.Vec[uint8](s.String())
}
