package doc

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/computernut43/craft-libs/serdejson"
)

// Styles: paragraph, character, table and linked styles with "based on" chains, plus the built-in
// style set (crates/doc/src/styles.rs; our own values in the spirit of a modern word processor's
// defaults).

// StyleKind is what a style formats.
type StyleKind uint8

// Style kinds.
const (
	StyleParagraph StyleKind = iota
	StyleCharacter
	StyleTable
	StyleNumbering
)

var styleKindNames = []string{"paragraph", "character", "table", "numbering"}

func (k *StyleKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, styleKindNames, k) }
func (k StyleKind) MarshalJSON() ([]byte, error)           { return writeEnum(styleKindNames, k) }

// Style is a style.
type Style struct {
	// ID is the stable id (Heading1), referenced by ParaProps.Style / CharProps.Style.
	ID string `json:"id"`
	// Name is the display name (Heading 1).
	Name    string    `json:"name"`
	Kind    StyleKind `json:"kind"`
	BasedOn *string   `json:"basedOn"`
	// Next is the style for the paragraph after this one (Enter at the end).
	Next *string `json:"next"`
	// Linked is the linked character/paragraph style id.
	Linked *string   `json:"linked"`
	Para   ParaProps `json:"para"`
	Chr    CharProps `json:"chr"`
	// Priority is the gallery order (lower first).
	Priority *uint32 `json:"priority"`
	// Quick: shown in the Styles gallery on the Home tab.
	Quick  bool `json:"quick"`
	Hidden bool `json:"hidden"`
	// Builtin: built in (cannot be deleted).
	Builtin bool `json:"builtin"`
	// Locked: can't be applied while Restrict Editing limits formatting to a selection of styles
	// (w:locked).
	Locked bool `json:"locked,omitempty"`
	// Default: the default style of its kind (w:default), whatever its id: localized documents
	// call the default character, table and list styles something other than the English ids.
	Default bool `json:"default,omitempty"`
	// Table is a table style's conditional formatting for header row, banding etc.
	Table *TableStyleParts `json:"table"`
}

func (Style) SerdeDefault() {}

// TableStyleParts are a table style's conditional formats (shading and character formatting per
// region).
type TableStyleParts struct {
	Borders *Borders `json:"borders"`
	// Fill is the shading of every cell (w:tblPr/w:shd, or the wholeTable region's cell shading).
	Fill *RGB `json:"fill"`
	// CellMargins are the cell margins, points: [top, left, bottom, right] (w:tblCellMar).
	CellMargins *[4]F32   `json:"cellMargins"`
	HeaderFill  *RGB      `json:"headerFill"`
	HeaderChr   CharProps `json:"headerChr"`
	// HeaderBorders are the cell borders of the header row (firstRow region's w:tcBorders):
	// Between is the border between header rows, InsideV between its cells.
	HeaderBorders *Borders `json:"headerBorders"`
	BandFill      *RGB     `json:"bandFill"`
	// BandChr is the character formatting of the odd row bands (band1Horz region).
	BandChr CharProps `json:"bandChr"`
	// BandBorders are the cell borders of the odd row bands (band1Horz region's w:tcBorders).
	BandBorders *Borders `json:"bandBorders"`
	// BandSize is the rows per band (w:tblStyleRowBandSize); one when unset.
	BandSize       *uint32   `json:"bandSize"`
	FirstColChr    CharProps `json:"firstColChr"`
	TotalChr       CharProps `json:"totalChr"`
	TotalBorderTop *Border   `json:"totalBorderTop"`
	// FirstColFill and FirstColBorders are the shading and cell borders of the first column
	// (firstCol region).
	FirstColFill    *RGB     `json:"firstColFill"`
	FirstColBorders *Borders `json:"firstColBorders"`
	// LastCol… are the last column (lastCol region).
	LastColFill    *RGB      `json:"lastColFill"`
	LastColChr     CharProps `json:"lastColChr"`
	LastColBorders *Borders  `json:"lastColBorders"`
	// TotalFill and TotalBorders are the shading and cell borders of the total (last) row
	// (lastRow region); a top edge left unset falls back to TotalBorderTop.
	TotalFill    *RGB     `json:"totalFill"`
	TotalBorders *Borders `json:"totalBorders"`
	// ColBand… are the odd column bands (band1Vert region).
	ColBandFill    *RGB      `json:"colBandFill"`
	ColBandChr     CharProps `json:"colBandChr"`
	ColBandBorders *Borders  `json:"colBandBorders"`
	// Regions is the per-part formatting the named fields above don't carry: every field of the
	// parts without one (even bands, corner cells), and the cell text alignment of every part.
	// Read and write parts through Part / SetPart.
	Regions TableRegions `json:"regions,omitempty"`
}

func (TableStyleParts) SerdeDefault() {}

// IsZero reports the default (no formatting).
func (t TableStyleParts) IsZero() bool { return equal(t, TableStyleParts{}) }

// TablePart is a conditional formatting part of a table style (w:tblStylePr/@w:type, ECMA-376
// §17.7.6). Its order is the declaration order of Rust's enum, which orders Regions.
type TablePart uint8

// Table style parts.
const (
	WholeTable TablePart = iota
	FirstRow
	LastRow
	FirstCol
	LastCol
	Band1Vert
	Band2Vert
	Band1Horz
	Band2Horz
	NeCell
	NwCell
	SeCell
	SwCell
)

var tablePartNames = []string{"wholeTable", "firstRow", "lastRow", "firstCol", "lastCol", "band1Vert", "band2Vert", "band1Horz", "band2Horz",
	"neCell", "nwCell", "seCell", "swCell"}

func (p *TablePart) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, tablePartNames, p) }
func (p TablePart) MarshalJSON() ([]byte, error)           { return writeEnum(tablePartNames, p) }

// TableParts is every part, lowest precedence first (ECMA-376 §17.7.6: whole table, bands,
// first/last column, first/last row, corner cells).
var TableParts = [13]TablePart{WholeTable, Band1Vert, Band2Vert, Band1Horz, Band2Horz, FirstCol, LastCol, FirstRow, LastRow, NeCell, NwCell,
	SeCell, SwCell}

// OOXML is the w:type value (the same as the JSON name).
func (p TablePart) OOXML() string { return enumName(tablePartNames, p) }

// TablePartFromOOXML reads a w:type value.
func TablePartFromOOXML(s string) (TablePart, bool) {
	for _, p := range TableParts {
		if p.OOXML() == s {
			return p, true
		}
	}
	return 0, false
}

// IsRow reports a part spanning whole rows (its left/right edges are the table's sides, InsideV
// the edges between its cells).
func (p TablePart) IsRow() bool {
	return p == FirstRow || p == LastRow || p == Band1Horz || p == Band2Horz
}

// IsColumn reports a part spanning whole columns (its top/bottom edges are the table's, Between
// the edges between its cells).
func (p TablePart) IsColumn() bool {
	return p == FirstCol || p == LastCol || p == Band1Vert || p == Band2Vert
}

func (p TablePart) bit() uint16 {
	return 1 << max(slices.Index(TableParts[:], p), 0)
}

// TableRegions is a Rust BTreeMap<TablePart, PartFormat>: written in part order.
type TableRegions map[TablePart]PartFormat

// DecodeSerde reads a map keyed by part names (a repeated key: the last wins, as in Rust).
func (r *TableRegions) DecodeSerde(d serdejson.Decoder) error {
	m := TableRegions{}
	err := d.Object(func(k serdejson.Key) error {
		i := slices.Index(tablePartNames, k.Name)
		if i < 0 {
			return d.Fail("unknown variant `" + k.Name + "`")
		}
		var f PartFormat
		if err := d.Decode(&f); err != nil {
			return err
		}
		m[TablePart(i)] = f
		return nil
	})
	if err != nil {
		return err
	}
	*r = m
	return nil
}

// MarshalJSON writes the parts in order.
func (r TableRegions) MarshalJSON() ([]byte, error) {
	out := []byte{'{'}
	for p := WholeTable; p <= SwCell; p++ {
		f, ok := r[p]
		if !ok {
			continue
		}
		b, err := serdejson.MarshalCompact(f)
		if err != nil {
			return nil, err
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = append(out, `"`+p.OOXML()+`":`...)
		out = append(out, b...)
	}
	return append(out, '}'), nil
}

// PartSet is the table style parts one cell is in (always the whole table).
type PartSet uint16

// PartSetOfCell is the parts of the cell at row ri of nrows, grid column g spanning span of
// ncols, in a table with options look and row bands of bandSize rows.
func PartSetOfCell(look TableLook, bandSize uint32, ri, nrows, g, span, ncols int) PartSet {
	header := look.HeaderRow && ri == 0
	total := look.TotalRow && ri+1 == nrows && nrows > 1
	firstCol := look.FirstColumn && g == 0
	lastCol := look.LastColumn && satAdd(g, span) >= ncols && ncols > 1
	size := int(min(max(bandSize, 1), 1000))
	rowBand := look.BandedRows && !header
	oddRow := satSub(ri, b2i(look.HeaderRow))/size%2 == 0
	colBand := look.BandedColumns && !firstCol
	oddCol := satSub(g, b2i(look.FirstColumn))%2 == 0
	bits := WholeTable.bit()
	for _, x := range []struct {
		on bool
		p  TablePart
	}{
		{colBand && oddCol, Band1Vert},
		{colBand && !oddCol, Band2Vert},
		{rowBand && oddRow, Band1Horz},
		{rowBand && !oddRow, Band2Horz},
		{firstCol, FirstCol},
		{lastCol, LastCol},
		{header, FirstRow},
		{total, LastRow},
		{header && lastCol, NeCell},
		{header && firstCol, NwCell},
		{total && lastCol, SeCell},
		{total && firstCol, SwCell},
	} {
		if x.on {
			bits |= x.p.bit()
		}
	}
	return PartSet(bits)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// satSub is usize saturating subtraction on non-negative ints.
func satSub(a, b int) int { return max(a-b, 0) }

// Contains reports whether the set holds p.
func (s PartSet) Contains(p TablePart) bool { return uint16(s)&p.bit() != 0 }

// Parts are the parts, lowest precedence first.
func (s PartSet) Parts() []TablePart {
	var out []TablePart
	for _, p := range TableParts {
		if s.Contains(p) {
			out = append(out, p)
		}
	}
	return out
}

// PartFormat is what one table style part gives its cells.
type PartFormat struct {
	Chr CharProps `json:"chr"`
	// Fill is the cell shading.
	Fill *RGB `json:"fill"`
	// Borders are the cell borders (the whole table's are the table borders).
	Borders *Borders `json:"borders"`
	// Align is the horizontal alignment of the cells' paragraphs (w:pPr/w:jc).
	Align *Align `json:"align"`
	// VAlign is the vertical alignment in the cells (w:tcPr/w:vAlign).
	VAlign *VAlign `json:"vAlign"`
}

func (PartFormat) SerdeDefault() {}

// IsEmpty reports the default (nothing set).
func (f PartFormat) IsEmpty() bool { return equal(f, PartFormat{}) }

// mergeBorders is patch over base edge by edge, either alone when the other is unset.
func mergeBorders(base, patch *Borders) *Borders {
	if base != nil && patch != nil {
		b := *base
		b.Overlay(patch)
		return &b
	}
	if patch != nil {
		return patch
	}
	return base
}

// or is Rust's `a.or(b)` on Options.
func or[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

// Overlay applies patch: what it sets wins; borders merge edge by edge.
func (f *PartFormat) Overlay(patch *PartFormat) {
	f.Chr.Overlay(&patch.Chr)
	f.Fill = or(patch.Fill, f.Fill)
	f.Borders = mergeBorders(f.Borders, patch.Borders)
	f.Align = or(patch.Align, f.Align)
	f.VAlign = or(patch.VAlign, f.VAlign)
}

// Overlay applies patch (a style further down a based-on chain) over these parts: what it sets
// wins, the rest is inherited; borders merge edge by edge.
func (t *TableStyleParts) Overlay(patch *TableStyleParts) {
	t.Borders = mergeBorders(t.Borders, patch.Borders)
	t.HeaderBorders = mergeBorders(t.HeaderBorders, patch.HeaderBorders)
	t.BandBorders = mergeBorders(t.BandBorders, patch.BandBorders)
	t.BandSize = or(patch.BandSize, t.BandSize)
	t.BandChr.Overlay(&patch.BandChr)
	t.Fill = or(patch.Fill, t.Fill)
	t.CellMargins = or(patch.CellMargins, t.CellMargins)
	t.HeaderFill = or(patch.HeaderFill, t.HeaderFill)
	t.HeaderChr.Overlay(&patch.HeaderChr)
	t.BandFill = or(patch.BandFill, t.BandFill)
	t.FirstColChr.Overlay(&patch.FirstColChr)
	t.TotalChr.Overlay(&patch.TotalChr)
	t.TotalBorderTop = or(patch.TotalBorderTop, t.TotalBorderTop)
	t.FirstColFill = or(patch.FirstColFill, t.FirstColFill)
	t.FirstColBorders = mergeBorders(t.FirstColBorders, patch.FirstColBorders)
	t.LastColFill = or(patch.LastColFill, t.LastColFill)
	t.LastColChr.Overlay(&patch.LastColChr)
	t.LastColBorders = mergeBorders(t.LastColBorders, patch.LastColBorders)
	t.TotalFill = or(patch.TotalFill, t.TotalFill)
	t.TotalBorders = mergeBorders(t.TotalBorders, patch.TotalBorders)
	t.ColBandFill = or(patch.ColBandFill, t.ColBandFill)
	t.ColBandChr.Overlay(&patch.ColBandChr)
	t.ColBandBorders = mergeBorders(t.ColBandBorders, patch.ColBandBorders)
	if len(patch.Regions) > 0 {
		regions := make(TableRegions, len(t.Regions)+len(patch.Regions))
		for k, f := range t.Regions {
			regions[k] = f
		}
		for k, f := range patch.Regions {
			r := regions[k]
			r.Overlay(&f)
			regions[k] = r
		}
		t.Regions = regions
	}
}

// Part is part p's formatting. The whole table's text formatting is the style's own
// (Style.TablePart adds it); its borders are the table borders.
func (t *TableStyleParts) Part(p TablePart) PartFormat {
	extra := t.Regions[p]
	named := func(chr CharProps, fill *RGB, borders *Borders) PartFormat {
		return PartFormat{Chr: chr, Fill: fill, Borders: borders, Align: extra.Align, VAlign: extra.VAlign}
	}
	switch p {
	case WholeTable:
		return PartFormat{Fill: t.Fill, Borders: t.Borders, VAlign: extra.VAlign}
	case FirstRow:
		return named(t.HeaderChr, t.HeaderFill, t.HeaderBorders)
	case LastRow:
		// The built-in top rule is the region's top edge unless its borders set one.
		var borders *Borders
		switch {
		case t.TotalBorders != nil:
			b := *t.TotalBorders
			b.Top = or(b.Top, t.TotalBorderTop)
			borders = &b
		case t.TotalBorderTop != nil:
			borders = &Borders{Top: t.TotalBorderTop}
		}
		return named(t.TotalChr, t.TotalFill, borders)
	case FirstCol:
		return named(t.FirstColChr, t.FirstColFill, t.FirstColBorders)
	case LastCol:
		return named(t.LastColChr, t.LastColFill, t.LastColBorders)
	case Band1Vert:
		return named(t.ColBandChr, t.ColBandFill, t.ColBandBorders)
	case Band1Horz:
		return named(t.BandChr, t.BandFill, t.BandBorders)
	}
	return extra
}

// SetPart replaces part p's formatting (for the whole table only its fill, borders and vertical
// alignment: Style.SetTablePart stores the rest).
func (t *TableStyleParts) SetPart(p TablePart, f PartFormat) {
	alignOnly := PartFormat{Align: f.Align, VAlign: f.VAlign}
	rest := alignOnly
	switch p {
	case WholeTable:
		t.Fill, t.Borders = f.Fill, f.Borders
		rest = PartFormat{VAlign: f.VAlign}
	case FirstRow:
		t.HeaderChr, t.HeaderFill, t.HeaderBorders = f.Chr, f.Fill, f.Borders
	case LastRow:
		t.TotalChr, t.TotalFill = f.Chr, f.Fill
		t.TotalBorderTop, t.TotalBorders = nil, nil
		if f.Borders != nil {
			t.TotalBorderTop = f.Borders.Top
			b := *f.Borders
			b.Top = nil
			if !equal(b, Borders{}) {
				t.TotalBorders = &b
			}
		}
	case FirstCol:
		t.FirstColChr, t.FirstColFill, t.FirstColBorders = f.Chr, f.Fill, f.Borders
	case LastCol:
		t.LastColChr, t.LastColFill, t.LastColBorders = f.Chr, f.Fill, f.Borders
	case Band1Vert:
		t.ColBandChr, t.ColBandFill, t.ColBandBorders = f.Chr, f.Fill, f.Borders
	case Band1Horz:
		t.BandChr, t.BandFill, t.BandBorders = f.Chr, f.Fill, f.Borders
	default:
		rest = f
	}
	regions := make(TableRegions, len(t.Regions)+1)
	for k, v := range t.Regions {
		regions[k] = v
	}
	if rest.IsEmpty() {
		delete(regions, p)
	} else {
		regions[p] = rest
	}
	t.Regions = regions
	if len(regions) == 0 {
		t.Regions = nil
	}
}

// TablePart is table style part p as this style alone sets it (the whole table's text formatting
// and paragraph alignment are the style's own).
func (s *Style) TablePart(p TablePart) PartFormat {
	var f PartFormat
	if s.Table != nil {
		f = s.Table.Part(p)
	}
	if p == WholeTable {
		f.Chr, f.Align = s.Chr, s.Para.Align
	}
	return f
}

// SetTablePart replaces part p of this table style.
func (s *Style) SetTablePart(p TablePart, f PartFormat) {
	if p == WholeTable {
		s.Chr, s.Para.Align = f.Chr, f.Align
	}
	var t TableStyleParts
	if s.Table != nil {
		t = *s.Table
	}
	t.SetPart(p, f)
	s.Table = nil
	if !t.IsZero() {
		s.Table = &t
	}
}

// TableStyleProps is a table style with its based-on chain merged: what it gives the text in the
// table's cells (Para, Chr) and the table itself (Parts).
type TableStyleProps struct {
	Para  ParaProps
	Chr   CharProps
	Parts TableStyleParts
}

// Part is part p with the based-on chain merged (see Style.TablePart).
func (t *TableStyleProps) Part(p TablePart) PartFormat {
	f := t.Parts.Part(p)
	if p == WholeTable {
		f.Chr, f.Align = t.Chr, t.Para.Align
	}
	return f
}

// StyleSheet is a document's styles.
type StyleSheet struct {
	// DefaultChr and DefaultPara are the document defaults (w:docDefaults).
	DefaultChr  CharProps `json:"defaultChr"`
	DefaultPara ParaProps `json:"defaultPara"`
	// DefaultParaPresent preserves an explicit w:pPrDefault, even when empty. Omission leaves
	// defaults to the application; a nonempty DefaultPara is always written after spacing edits.
	DefaultParaPresent bool       `json:"defaultParaPresent"`
	Styles             Vec[Style] `json:"styles"`
	// LatentStyles is the file's w:latentStyles (behaviour of built-in styles not defined in the
	// document: gallery, priority, visibility), as XML, written back as is.
	LatentStyles *string `json:"latentStyles,omitempty"`
}

func (StyleSheet) SerdeDefault() {}

// SetDefaults is Rust's StyleSheet::default(): the built-in styles.
func (s *StyleSheet) SetDefaults() { *s = BuiltinStyles() }

// HeadingBlue is the default heading colour.
var HeadingBlue = RGB{0x0F, 0x47, 0x61}

func color(c RGB) *TextColor { return ptr(FixedColor(c)) }

// EmptyStyleSheet has no styles and no defaults.
func EmptyStyleSheet() StyleSheet { return StyleSheet{DefaultParaPresent: true, Styles: Vec[Style]{}} }

func pStyle() Style { return Style{Kind: StyleParagraph, Builtin: true} }

func multiple(v F32) *LineSpacing { return &LineSpacing{LineMultiple, v} }

// BuiltinStyles is the built-in style set used for new documents.
func BuiltinStyles() StyleSheet {
	s := StyleSheet{
		DefaultChr: CharProps{Font: ptr(BodyFont), Size: ptr[F32](12), Kern: ptr[F32](1), Lang: ptr("en-US")},
		DefaultPara: ParaProps{SpaceAfter: ptr[F32](8), SpaceBefore: ptr[F32](0), LineSpacing: multiple(1.15),
			WidowControl: ptr(true)},
		DefaultParaPresent: true,
		Styles:             Vec[Style]{},
	}
	add := func(st Style) { s.Styles = append(s.Styles, st) }
	st := pStyle()
	st.ID, st.Name, st.Priority, st.Quick = "Normal", "Normal", ptr[uint32](0), true
	add(st)
	add(Style{ID: "DefaultParagraphFont", Name: "Default Paragraph Font", Kind: StyleCharacter, Priority: ptr[uint32](1), Hidden: true,
		Builtin: true})
	st = pStyle()
	st.ID, st.Name, st.BasedOn = "NoSpacing", "No Spacing", ptr("Normal")
	st.Para = ParaProps{SpaceAfter: ptr[F32](0), LineSpacing: multiple(1)}
	st.Priority, st.Quick = ptr[uint32](1), true
	add(st)
	heads := []struct {
		size, before, after F32
		italic              *bool
		c                   RGB
		font                string
	}{
		{20, 18, 4, nil, HeadingBlue, HeadingFont},
		{16, 8, 4, nil, HeadingBlue, HeadingFont},
		{14, 8, 4, nil, HeadingBlue, BodyFont},
		{12, 4, 2, ptr(true), HeadingBlue, BodyFont},
		{12, 4, 2, nil, HeadingBlue, BodyFont},
		{12, 2, 0, ptr(true), RGB{0x59, 0x59, 0x59}, BodyFont},
		{12, 2, 0, nil, RGB{0x59, 0x59, 0x59}, BodyFont},
		{12, 0, 0, ptr(true), RGB{0x27, 0x27, 0x27}, BodyFont},
		{12, 0, 0, nil, RGB{0x27, 0x27, 0x27}, BodyFont},
	}
	for i, h := range heads {
		n := strconv.Itoa(i + 1)
		st := pStyle()
		st.ID, st.Name = "Heading"+n, "Heading "+n
		st.BasedOn, st.Next, st.Linked = ptr("Normal"), ptr("Normal"), ptr("Heading"+n+"Char")
		st.Para = ParaProps{SpaceBefore: ptr(h.before), SpaceAfter: ptr(h.after), KeepNext: ptr(true), KeepLines: ptr(true),
			OutlineLevel: ptr(uint8(i))}
		st.Chr = CharProps{Font: ptr(h.font), Size: ptr(h.size), Italic: h.italic, Color: color(h.c)}
		st.Priority, st.Quick = ptr(uint32(9+i+1)), i+1 <= 3
		add(st)
	}
	st = pStyle()
	st.ID, st.Name, st.BasedOn, st.Next = "Title", "Title", ptr("Normal"), ptr("Normal")
	st.Para = ParaProps{SpaceAfter: ptr[F32](4), ContextualSpacing: ptr(true), LineSpacing: multiple(1)}
	st.Chr = CharProps{Font: ptr(HeadingFont), Size: ptr[F32](28), Spacing: ptr[F32](-0.5), Kern: ptr[F32](14)}
	st.Priority, st.Quick = ptr[uint32](10), true
	add(st)
	st = pStyle()
	st.ID, st.Name, st.BasedOn, st.Next = "Subtitle", "Subtitle", ptr("Normal"), ptr("Normal")
	st.Chr = CharProps{Size: ptr[F32](14), Spacing: ptr[F32](0.75), Color: color(RGB{0x59, 0x59, 0x59})}
	st.Priority, st.Quick = ptr[uint32](11), true
	add(st)
	cStyle := func(id, name string, prio uint32, c CharProps) Style {
		return Style{ID: id, Name: name, Kind: StyleCharacter, BasedOn: ptr("DefaultParagraphFont"), Chr: c, Priority: ptr(prio), Quick: true,
			Builtin: true}
	}
	add(cStyle("SubtleEmphasis", "Subtle Emphasis", 19, CharProps{Italic: ptr(true), Color: color(RGB{0x40, 0x40, 0x40})}))
	add(cStyle("Emphasis", "Emphasis", 20, CharProps{Italic: ptr(true)}))
	add(cStyle("IntenseEmphasis", "Intense Emphasis", 21, CharProps{Italic: ptr(true), Color: color(HeadingBlue)}))
	add(cStyle("Strong", "Strong", 22, CharProps{Bold: ptr(true)}))
	st = pStyle()
	st.ID, st.Name, st.BasedOn, st.Next = "Quote", "Quote", ptr("Normal"), ptr("Normal")
	st.Para = ParaProps{Align: ptr(AlignCenter), SpaceBefore: ptr[F32](8)}
	st.Chr = CharProps{Italic: ptr(true), Color: color(RGB{0x40, 0x40, 0x40})}
	st.Priority, st.Quick = ptr[uint32](29), true
	add(st)
	blueLine := Border{Style: BorderSingle, Width: 0.5, Color: ptr(HeadingBlue), Space: 10}
	st = pStyle()
	st.ID, st.Name, st.BasedOn, st.Next = "IntenseQuote", "Intense Quote", ptr("Normal"), ptr("Normal")
	st.Para = ParaProps{Align: ptr(AlignCenter), SpaceBefore: ptr[F32](18), SpaceAfter: ptr[F32](18), IndentLeft: ptr[F32](43.2),
		IndentRight: ptr[F32](43.2), Borders: &Borders{Top: ptr(blueLine), Bottom: ptr(blueLine)}}
	st.Chr = CharProps{Italic: ptr(true), Color: color(HeadingBlue)}
	st.Priority, st.Quick = ptr[uint32](30), true
	add(st)
	add(cStyle("SubtleReference", "Subtle Reference", 31, CharProps{SmallCaps: ptr(true), Color: color(RGB{0x5A, 0x5A, 0x5A})}))
	add(cStyle("IntenseReference", "Intense Reference", 32, CharProps{Bold: ptr(true), SmallCaps: ptr(true), Spacing: ptr[F32](0.25),
		Color: color(HeadingBlue)}))
	add(cStyle("BookTitle", "Book Title", 33, CharProps{Bold: ptr(true), Italic: ptr(true), Spacing: ptr[F32](0.25)}))
	st = pStyle()
	st.ID, st.Name, st.BasedOn = "ListParagraph", "List Paragraph", ptr("Normal")
	st.Para = ParaProps{IndentLeft: ptr[F32](36), ContextualSpacing: ptr(true)}
	st.Priority, st.Quick = ptr[uint32](34), true
	add(st)
	hl := cStyle("Hyperlink", "Hyperlink", 99, CharProps{Color: color(RGB{0x46, 0x78, 0x86}), Underline: ptr(UnderlineSingle)})
	hl.Quick = false
	add(hl)
	st = pStyle()
	st.ID, st.Name, st.BasedOn, st.Next = "Caption", "Caption", ptr("Normal"), ptr("Normal")
	st.Para = ParaProps{SpaceAfter: ptr[F32](10), LineSpacing: multiple(1)}
	st.Chr = CharProps{Italic: ptr(true), Size: ptr[F32](9), Color: color(RGB{0x0E, 0x28, 0x41})}
	st.Priority = ptr[uint32](35)
	add(st)
	for _, id := range []string{"Header", "Footer"} {
		st := pStyle()
		st.ID, st.Name, st.BasedOn = id, id, ptr("Normal")
		st.Para = ParaProps{SpaceAfter: ptr[F32](0), LineSpacing: multiple(1),
			Tabs: &Vec[TabStop]{{Pos: 234, Align: TabCenter, Leader: LeaderNone}, {Pos: 468, Align: TabRight, Leader: LeaderNone}}}
		st.Priority = ptr[uint32](99)
		add(st)
	}
	noteText := func(id, name string) Style {
		st := pStyle()
		st.ID, st.Name, st.BasedOn = id, name, ptr("Normal")
		st.Para = ParaProps{SpaceAfter: ptr[F32](0), LineSpacing: multiple(1)}
		st.Chr = CharProps{Size: ptr[F32](10)}
		st.Priority = ptr[uint32](99)
		return st
	}
	noteRef := func(id, name string) Style {
		st := cStyle(id, name, 99, CharProps{VertAlign: ptr(Superscript)})
		st.Quick = false
		return st
	}
	add(noteText("FootnoteText", "Footnote Text"))
	add(noteRef("FootnoteReference", "Footnote Reference"))
	add(noteText("EndnoteText", "Endnote Text"))
	add(noteRef("EndnoteReference", "Endnote Reference"))
	st = pStyle()
	st.ID, st.Name, st.BasedOn, st.Next = "TOCHeading", "TOC Heading", ptr("Heading1"), ptr("Normal")
	st.Para = ParaProps{OutlineLevel: ptr[uint8](9), SpaceBefore: ptr[F32](12)}
	st.Priority = ptr[uint32](39)
	add(st)
	for n := 1; n <= 9; n++ {
		st := pStyle()
		st.ID, st.Name, st.BasedOn, st.Next = "TOC"+strconv.Itoa(n), "TOC "+strconv.Itoa(n), ptr("Normal"), ptr("Normal")
		st.Para = ParaProps{SpaceAfter: ptr[F32](5), IndentLeft: ptr(F32(12 * float32(n-1)))}
		st.Priority = ptr[uint32](39)
		add(st)
	}
	add(Style{ID: "TableGrid", Name: "Table Grid", Kind: StyleTable, Para: ParaProps{SpaceAfter: ptr[F32](0), LineSpacing: multiple(1)},
		Priority: ptr[uint32](59), Builtin: true, Table: &TableStyleParts{Borders: ptr(AllBorders(SingleBorder(0.5)))}})
	s.Styles = append(s.Styles, tableStyles()...)
	return s
}

// tableStyles are a few table styles of our own (grid, plain, light/medium accents).
func tableStyles() []Style {
	thin := func(c RGB) Border { return Border{Style: BorderSingle, Width: 0.5, Color: ptr(c)} }
	mk := func(id, name string, parts TableStyleParts) Style {
		return Style{ID: id, Name: name, Kind: StyleTable, BasedOn: ptr("TableNormal"),
			Para: ParaProps{SpaceAfter: ptr[F32](0), LineSpacing: multiple(1)}, Priority: ptr[uint32](60), Builtin: true, Table: &parts}
	}
	bold := CharProps{Bold: ptr(true)}
	boldWhite := CharProps{Bold: ptr(true), Color: color(White)}
	v := []Style{
		{ID: "TableNormal", Name: "Normal Table", Kind: StyleTable, Hidden: true, Builtin: true},
		mk("PlainTable1", "Plain Table 1", TableStyleParts{Borders: ptr(AllBorders(thin(RGB{0xBF, 0xBF, 0xBF}))), HeaderChr: bold,
			BandFill: ptr(RGB{0xF2, 0xF2, 0xF2}), FirstColChr: bold}),
		mk("GridTable1Light", "Grid Table 1 Light", TableStyleParts{Borders: ptr(AllBorders(thin(RGB{0x99, 0x99, 0x99}))), HeaderChr: bold,
			FirstColChr: bold}),
	}
	for _, a := range []struct {
		name        string
		dark, light RGB
	}{
		{"Blue", RGB{0x15, 0x60, 0x82}, RGB{0xC1, 0xE4, 0xF5}},
		{"Orange", RGB{0xE9, 0x71, 0x32}, RGB{0xFB, 0xE2, 0xD5}},
		{"Green", RGB{0x19, 0x6B, 0x24}, RGB{0xC1, 0xF0, 0xC8}},
		{"Purple", RGB{0x71, 0x2E, 0x8D}, RGB{0xE5, 0xD2, 0xEE}},
	} {
		v = append(v,
			mk("GridTable4Accent"+a.name, "Grid Table 4 – "+a.name, TableStyleParts{Borders: ptr(AllBorders(thin(a.light))),
				HeaderFill: ptr(a.dark), HeaderChr: boldWhite, BandFill: ptr(a.light), FirstColChr: bold}),
			mk("ListTable3Accent"+a.name, "List Table 3 – "+a.name, TableStyleParts{Borders: ptr(BoxBorders(thin(a.dark))),
				HeaderFill: ptr(a.dark), HeaderChr: boldWhite, TotalBorderTop: ptr(thin(a.dark))}))
	}
	return v
}

// Get is style id.
func (s *StyleSheet) Get(id string) (Style, bool) {
	if i := s.index(id); i >= 0 {
		return s.Styles[i], true
	}
	return Style{}, false
}

func (s *StyleSheet) index(id string) int {
	return slices.IndexFunc(s.Styles, func(st Style) bool { return st.ID == id })
}

// Find finds a style by id, or by display name (ASCII case-insensitive).
func (s *StyleSheet) Find(nameOrID string) (Style, bool) {
	if st, ok := s.Get(nameOrID); ok {
		return st, true
	}
	if i := slices.IndexFunc(s.Styles, func(st Style) bool { return asciiEqualFold(st.Name, nameOrID) }); i >= 0 {
		return s.Styles[i], true
	}
	return Style{}, false
}

// asciiEqualFold is Rust's eq_ignore_ascii_case.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// Gallery is the Quick Styles gallery, in priority order.
func (s *StyleSheet) Gallery() []Style {
	var v []Style
	for _, st := range s.Styles {
		if st.Quick && !st.Hidden && st.Kind != StyleTable {
			v = append(v, st)
		}
	}
	prio := func(st Style) uint32 {
		if st.Priority != nil {
			return *st.Priority
		}
		return 99
	}
	slices.SortStableFunc(v, func(a, b Style) int {
		if pa, pb := prio(a), prio(b); pa != pb {
			if pa < pb {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return v
}

// Chain is the BasedOn chain from the root down to id (bounded, cycle-safe).
func (s *StyleSheet) Chain(id string) []Style {
	var out []Style
	cur, ok := s.Get(id)
	for ok {
		if len(out) >= 32 || slices.ContainsFunc(out, func(o Style) bool { return o.ID == cur.ID }) {
			break
		}
		out = append(out, cur)
		if cur.BasedOn == nil {
			break
		}
		cur, ok = s.Get(*cur.BasedOn)
	}
	slices.Reverse(out)
	return out
}

// IndentsOverList reports which indents (left, first line) of a paragraph that gets its list
// from style id the style chain sets over the list level's own. Word applies the level's indents
// on top of the most derived style that gives a list, so that style and the ones based on it win
// over the level, and the styles it is based on don't (Heading 3 with its own left indent on
// Heading 2's list). (false, false) when no style in the chain gives a list.
func (s *StyleSheet) IndentsOverList(id string) (left, first bool) {
	chain := s.Chain(id)
	k := -1
	for i, st := range chain {
		if st.Para.Numbering != nil {
			k = i
		}
	}
	if k < 0 {
		return false, false
	}
	set := func(v, chars *F32) bool { return v != nil || chars != nil && *chars != 0 }
	for _, st := range chain[k:] {
		left = left || set(st.Para.IndentLeft, st.Para.IndentLeftChars)
		first = first || set(st.Para.IndentFirst, st.Para.IndentFirstChars)
	}
	return left, first
}

// TableStyle is table style id with its based-on chain merged, base styles first (false when
// there is no such table style). Styles of other kinds in the chain are skipped.
func (s *StyleSheet) TableStyle(id string) (TableStyleProps, bool) {
	var chain []Style
	for _, st := range s.Chain(id) {
		if st.Kind == StyleTable {
			chain = append(chain, st)
		}
	}
	if len(chain) == 0 || chain[len(chain)-1].ID != id {
		return TableStyleProps{}, false
	}
	var out TableStyleProps
	for _, st := range chain {
		out.Para.Overlay(&st.Para)
		out.Chr.Overlay(&st.Chr)
		if st.Table != nil {
			out.Parts.Overlay(st.Table)
		}
	}
	out.Para.Style, out.Chr.Style = nil, nil
	return out, true
}

// Upsert adds or replaces a style.
func (s *StyleSheet) Upsert(st Style) {
	styles := slices.Clone(s.Styles)
	if i := s.index(st.ID); i >= 0 {
		styles[i] = st
	} else {
		styles = append(styles, st)
	}
	s.Styles = styles
}

// AddDefaultGallery is for a file from a producer that never writes w:qFormat (LibreOffice): it
// gives it Word's usual Quick Styles gallery. Each built-in style whose id or name matches a
// quick style of BuiltinStyles becomes quick (taking the built-in's priority when it has none),
// and the quick built-ins the file lacks are added with their linked styles. Existing ids and
// names are never overwritten. Callers use it only when no style in the file is quick.
func (s *StyleSheet) AddDefaultGallery() {
	builtin := BuiltinStyles()
	s.Styles = slices.Clone(s.Styles)
	for _, q := range builtin.Styles {
		if !q.Quick || q.Kind == StyleTable {
			continue
		}
		found := s.index(q.ID)
		if found < 0 {
			found = slices.IndexFunc(s.Styles, func(st Style) bool { return asciiEqualFold(st.Name, q.Name) })
		}
		if found >= 0 {
			st := &s.Styles[found]
			if st.Builtin && st.Kind == q.Kind {
				st.Quick = true
				st.Priority = or(st.Priority, q.Priority)
			}
			continue
		}
		ids := []string{q.ID}
		if q.Linked != nil {
			ids = append(ids, *q.Linked)
		}
		for _, sid := range ids {
			b, ok := builtin.Get(sid)
			if !ok {
				continue
			}
			if _, has := s.Get(sid); has {
				continue
			}
			if _, has := s.Find(b.Name); has {
				continue
			}
			s.Styles = append(s.Styles, b)
		}
	}
}

// Remove removes style id and returns it. Styles based on it are rebased onto its own base, and
// Next / Linked references to it are dropped. Text that uses it is the document's to retarget.
func (s *StyleSheet) Remove(id string) (Style, bool) {
	i := s.index(id)
	if i < 0 {
		return Style{}, false
	}
	gone := s.Styles[i]
	styles := slices.Delete(slices.Clone(s.Styles), i, i+1)
	for j := range styles {
		st := &styles[j]
		if st.BasedOn != nil && *st.BasedOn == id {
			st.BasedOn = nil
			if gone.BasedOn != nil && *gone.BasedOn != st.ID {
				st.BasedOn = gone.BasedOn
			}
		}
		if st.Next != nil && *st.Next == id {
			st.Next = nil
		}
		if st.Linked != nil && *st.Linked == id {
			st.Linked = nil
		}
	}
	s.Styles = styles
	return gone, true
}

// NewID is an id not used yet, derived from a display name.
func (s *StyleSheet) NewID(name string) string {
	var b strings.Builder
	for _, c := range name {
		if isAlphanumeric(c) {
			b.WriteRune(c)
		}
	}
	base := b.String()
	if base == "" {
		base = "Style"
	}
	if s.index(base) < 0 {
		return base
	}
	for i := 1; i < 10000; i++ {
		if id := base + strconv.Itoa(i); s.index(id) < 0 {
			return id
		}
	}
	return base
}

// isAlphanumeric is Rust's char::is_alphanumeric: the Alphabetic property (letters, letter
// numbers and Other_Alphabetic) or a number (Nd, Nl, No).
func isAlphanumeric(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsNumber(c) || unicode.Is(unicode.Other_Alphabetic, c)
}
