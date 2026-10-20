package doc

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/computernut43/craft-libs/serdejson"
)

// The styles, lists, tables and content controls part of TestPropsMatchRust: operations y, n, a,
// h (read and write back), Y, H, z, Z, l, j and w (testdata/rust-oracle/src/bin/props.rs says
// what each runs).

// seq reads a JSON sequence of exactly len(elems) values into elems, as serde reads a tuple.
type seq []any

func (s seq) DecodeSerde(d serdejson.Decoder) error {
	if d.Peek() != serdejson.KindArray {
		return d.Fail("invalid type, expected a tuple")
	}
	n := 0
	err := d.Array(func(i int) error {
		if i >= len(s) {
			return d.Fail("trailing characters")
		}
		n = i + 1
		return d.Decode(s[i])
	})
	if err != nil {
		return err
	}
	if n != len(s) {
		return d.Fail("invalid length")
	}
	return nil
}

func readSeq(js []byte, elems ...any) bool {
	s := seq(elems)
	return serdejson.Unmarshal(js, &s) == nil
}

func compact(v any) string {
	b, err := serdejson.MarshalCompact(v)
	if err != nil {
		return "SERIALIZE " + err.Error()
	}
	return string(b)
}

// opt is an Option: v when ok, else null.
func optional[T any](v T, ok bool) any {
	if !ok {
		return nil
	}
	return v
}

func styleIDs(v []Style) []string {
	out := []string{}
	for _, s := range v {
		out = append(out, s.ID)
	}
	return out
}

type numberingOp struct {
	Op    uint8
	Num   uint32
	Level uint8
	Kind  *ListKind
}

type tableOp struct {
	Op         uint8
	A, B, C, D int
}

func modelAnswer(op byte, js []byte) string {
	switch op {
	case 'Y':
		var sheet StyleSheet
		var id, name string
		if !readSeq(js, &sheet, &id, &name) {
			return "ERROR"
		}
		var ts any
		if t, ok := sheet.TableStyle(id); ok {
			ts = []any{t.Para, t.Chr, t.Parts}
		}
		get, okGet := sheet.Get(id)
		find, okFind := sheet.Find(name)
		left, first := sheet.IndentsOverList(id)
		looked := []any{optional(get.ID, okGet), optional(find.ID, okFind), styleIDs(sheet.Gallery()), styleIDs(sheet.Chain(id)),
			[]bool{left, first}, ts, sheet.NewID(name)}
		removed := sheet
		gone, okGone := removed.Remove(id)
		sheet.AddDefaultGallery()
		return show([]any{looked, optional(gone.ID, okGone), removed, sheet})
	case 'H':
		var st Style
		var p TablePart
		var f PartFormat
		if !readSeq(js, &st, &p, &f) {
			return "ERROR"
		}
		var parts []PartFormat
		for _, q := range TableParts {
			parts = append(parts, st.TablePart(q))
		}
		st.SetTablePart(p, f)
		return show([]any{parts, st})
	case 'z':
		var look TableLook
		var band uint32
		var ri, nrows, g, span, ncols uint64
		if !readSeq(js, &look, &band, &ri, &nrows, &g, &span, &ncols) {
			return "ERROR"
		}
		// Rust's usize is wider than int only past what a table can hold; inputs stay within int.
		c := func(v uint64) int { return int(min(v, uint64(maxInt))) }
		return show(PartSetOfCell(look, band, c(ri), c(nrows), c(g), c(span), c(ncols)).Parts())
	case 'Z':
		var n Numbering
		var ops []numberingOp
		if !readSeq(js, &n, &ops) {
			return "ERROR"
		}
		var c Counters
		out := []string{}
		for _, o := range ops {
			kind := ListKind{Kind: ListBullet}
			if o.Kind != nil {
				kind = *o.Kind
			}
			var r any
			switch o.Op {
			case 0:
				if label, def, ok := c.NextLabel(&n, o.Num, o.Level); ok {
					r = []any{label, def}
				}
			case 1:
				r = optional(c.Values(&n, o.Num))
			case 2:
				r = n.AddList(kind)
			case 3:
				r = optional(n.Restart(o.Num))
			case 4:
				r = optional(n.FindKind(kind))
			case 5:
				r = n.ShowsLabel(o.Num, o.Level)
			default:
				r = optional(n.Level(o.Num, o.Level))
			}
			out = append(out, compact(r))
		}
		return show([]any{out, n})
	case 'l':
		var k ListKind
		var s string
		if !readSeq(js, &k, &s) {
			return "ERROR"
		}
		return show([]any{LevelsFor(k), optional(ParseListKind(s)), k.IsBullet()})
	case 'j':
		var t Table
		var ops []tableOp
		if !readSeq(js, &t, &ops) {
			return "ERROR"
		}
		out := []string{}
		for _, o := range ops {
			var r any
			switch o.Op {
			case 0:
				t.InsertRow(o.A, o.B)
			case 1:
				t.DeleteRow(o.A)
			case 2:
				t.InsertCol(o.A)
			case 3:
				t.DeleteCol(o.A)
			case 4:
				t.MergeRight(o.A, o.B, o.C)
			case 5:
				t.Merge(o.A, o.B, o.C, o.D)
			case 6:
				r = append([]int{}, t.SplitCell(o.A, o.B, o.C)...)
			case 7:
				r = t.SplitCellGrid(o.A, o.B, o.C, o.D)
			case 8:
				if top, k, ok := t.VerticalExtent(o.A, o.B); ok {
					r = []int{top, k}
				}
			case 9:
				r = optional(t.CellAtGrid(o.A, o.B))
			case 10:
				r = t.GridCol(o.A, o.B)
			case 11:
				r = t.Cols()
			case 12:
				r = t.TotalWidth()
			default:
				t = *NewTable(o.A, o.B, F32(float32(o.C)))
			}
			out = append(out, compact(r))
		}
		return show([]any{out, t})
	case 'w':
		var s string
		if serdejson.Unmarshal(js, &s) != nil {
			return "ERROR"
		}
		sheet := EmptyStyleSheet()
		return show(sheet.NewID(s))
	}
	return "BAD OP"
}

// DecodeSerde reads a numbering op [op, num, level, kind] (all present).
func (o *numberingOp) DecodeSerde(d serdejson.Decoder) error {
	s := seq{&o.Op, &o.Num, &o.Level, &o.Kind}
	return s.DecodeSerde(d)
}

// DecodeSerde reads a table op [op, a, b, c, d] (usize arguments).
func (o *tableOp) DecodeSerde(d serdejson.Decoder) error {
	var a, b, c, e uint64
	s := seq{&o.Op, &a, &b, &c, &e}
	if err := s.DecodeSerde(d); err != nil {
		return err
	}
	cl := func(v uint64) int { return int(min(v, uint64(maxInt))) }
	o.A, o.B, o.C, o.D = cl(a), cl(b), cl(c), cl(e)
	return nil
}

// Generators.

func (g *pgen) styleID() {
	if g.chance(6) {
		g.str()
		return
	}
	g.w(`"` + g.pick("Normal", "Heading1", "Heading2", "TableGrid", "TableNormal", "PlainTable1", "Title", "Strong", "A", "B", "C",
		"ListParagraph", "Hyperlink", "a", "") + `"`)
}

func (g *pgen) styleName() {
	if g.chance(4) {
		g.str()
		return
	}
	g.w(`"` + g.pick("Normal", "heading 1", "Heading 2", "HEADING 3", "Title", "Subtitle", "Table Grid", "No Spacing", "My Style", "!!!",
		"Ünïcödé 1", "ⅷ x", "x́y", "") + `"`)
}

func (g *pgen) partFormat() {
	g.object([]pfield{dflt("chr", (*pgen).charProps), opt("fill", (*pgen).rgb), opt("borders", (*pgen).borders),
		opt("align", g.enumOf(alignNames)), opt("vAlign", g.enumOf(vAlignNames))})
}

func (g *pgen) tableStyleParts() {
	r, bs, c, f := (*pgen).rgb, (*pgen).borders, (*pgen).charProps, (*pgen).f32
	g.object([]pfield{
		opt("borders", bs), opt("fill", r), opt("cellMargins", func(g *pgen) { g.four(f) }), opt("headerFill", r), dflt("headerChr", c),
		opt("headerBorders", bs), opt("bandFill", r), dflt("bandChr", c), opt("bandBorders", bs), opt("bandSize", func(g *pgen) { g.uint(32) }),
		dflt("firstColChr", c), dflt("totalChr", c), opt("totalBorderTop", (*pgen).border), opt("firstColFill", r), opt("firstColBorders", bs),
		opt("lastColFill", r), dflt("lastColChr", c), opt("lastColBorders", bs), opt("totalFill", r), opt("totalBorders", bs),
		opt("colBandFill", r), dflt("colBandChr", c), opt("colBandBorders", bs), dflt("regions", func(g *pgen) {
			if g.oops(10) {
				g.w(g.pick(`{"bogus":{}}`, `[]`, `{"FirstRow":{}}`, `{"wholeTable":1}`))
				return
			}
			g.w("{")
			for i := range g.rng.IntN(4) {
				if i > 0 {
					g.w(",")
				}
				g.w(`"` + g.pick(tablePartNames...) + `":`)
				g.partFormat()
			}
			g.w("}")
		}),
	})
}

func (g *pgen) style() {
	s, b := (*pgen).str, (*pgen).boolean
	g.object([]pfield{
		dflt("id", (*pgen).styleID), dflt("name", (*pgen).styleName), dflt("kind", g.enumOf(styleKindNames)), opt("basedOn", (*pgen).styleID),
		opt("next", (*pgen).styleID), opt("linked", (*pgen).styleID), dflt("para", (*pgen).paraProps), dflt("chr", (*pgen).charProps),
		opt("priority", func(g *pgen) { g.w(g.pick("0", "1", "10", "99", "100", "4294967295")) }), dflt("quick", b), dflt("hidden", b),
		dflt("builtin", b), dflt("locked", b), dflt("default", b), opt("table", (*pgen).tableStyleParts), dflt("x", s),
	})
}

func (g *pgen) styleSheet() {
	g.object([]pfield{dflt("defaultChr", (*pgen).charProps), dflt("defaultPara", (*pgen).paraProps), dflt("defaultParaPresent", (*pgen).boolean),
		dflt("styles", func(g *pgen) { g.list((*pgen).style) }), opt("latentStyles", (*pgen).str)})
}

func (g *pgen) smallU32() {
	if g.oops(10) {
		g.w(g.pick("-1", "4294967296", "1.0", `"1"`))
		return
	}
	g.w(g.pick("0", "1", "2", "3", "4", "4294967295", "4294967294"))
}

func (g *pgen) level() {
	b, f := (*pgen).boolean, (*pgen).f32
	g.object([]pfield{
		dflt("start", func(g *pgen) { g.w(g.pick("0", "1", "2", "5", "10", "4294967295")) }), dflt("format", g.enumOf(numFormatNames)),
		dflt("text", func(g *pgen) {
			g.w(strconv.Quote(g.pick("%1.", "%1.%2.", "•", "%3)", "(%9)", "%0", "%%1", "第%1章", "", "%1.%2.%3.%4.%5.%6.%7.%8.%9.", "%", "x%2%")))
		}),
		dflt("align", g.enumOf(alignNames)), dflt("indent", f), dflt("hanging", f), dflt("suffix", g.enumOf(levelSuffixNames)),
		dflt("chr", (*pgen).charProps), dflt("restart", b), opt("restartAfter", func(g *pgen) { g.uint(8) }), opt("tab", f), dflt("legal", b),
		opt("style", (*pgen).styleID),
	})
}

func (g *pgen) levels() {
	if g.oops(20) {
		g.w(g.pick("{}", "null", "[1]"))
		return
	}
	g.w("[")
	for i := range g.rng.IntN(10) {
		if i > 0 {
			g.w(",")
		}
		g.level()
	}
	g.w("]")
}

func (g *pgen) pairList(a, b func(*pgen)) {
	g.list(func(g *pgen) {
		if g.oops(10) {
			g.w(g.pick("[1]", "[1,2,3]", `{"0":1,"1":2}`, "1"))
			return
		}
		g.w("[")
		a(g)
		g.w(",")
		b(g)
		g.w("]")
	})
}

func (g *pgen) numbering() {
	s := (*pgen).str
	abstract := func(g *pgen) {
		g.object([]pfield{dflt("id", (*pgen).smallU32), opt("name", s), dflt("levels", (*pgen).levels), opt("nsid", s), opt("tmpl", s),
			opt("multi_level_type", s)})
	}
	num := func(g *pgen) {
		g.object([]pfield{dflt("id", (*pgen).smallU32), dflt("abstractId", (*pgen).smallU32),
			dflt("startOverrides", func(g *pgen) { g.pairList(func(g *pgen) { g.uint(8) }, (*pgen).smallU32) }),
			dflt("levelOverrides", func(g *pgen) { g.pairList(func(g *pgen) { g.w(g.pick("0", "1", "2", "8", "9")) }, (*pgen).level) })})
	}
	g.object([]pfield{dflt("abstracts", func(g *pgen) { g.list(abstract) }), dflt("nums", func(g *pgen) { g.list(num) })})
}

func (g *pgen) listKind() {
	if g.oops(8) {
		g.w(g.pick(`"bulletChar"`, `{"bulletChar":"ab"}`, `{"bulletChar":""}`, `{"bullet":null,"legal":null}`, `{}`, `"Bullet"`))
		return
	}
	if g.chance(4) {
		g.w(`{"bulletChar":` + strconv.Quote(g.pick("★", "-", "–", "•", "o", "😀", "▪")) + `}`)
		return
	}
	g.enum([]string{"bullet", "numbered", "numberedParen", "outline", "upperLetter", "lowerLetter", "lowerLetterDot", "lowerRoman", "legal"})
}

func (g *pgen) controlKind() {
	s, b := (*pgen).str, (*pgen).boolean
	char := func(g *pgen) {
		if g.oops(10) {
			g.w(g.pick(`""`, `"ab"`, "9746"))
			return
		}
		g.w(strconv.Quote(g.pick("☒", "☐", "x", "😀")))
	}
	items := func(g *pgen) {
		g.list(func(g *pgen) { g.object([]pfield{dflt("display", s), dflt("value", s)}) })
	}
	typ := g.pick(controlTypeNames...)
	if g.oops(10) {
		typ = g.pick("RichText", "plainText", "bogus")
	}
	var fields []pfield
	switch typ {
	case "text":
		fields = []pfield{dflt("multiLine", b)}
	case "checkBox":
		fields = []pfield{req("checked", b), req("checkedChar", char), req("checkedFont", s), req("uncheckedChar", char), req("uncheckedFont", s)}
	case "comboBox", "dropDown":
		fields = []pfield{req("items", items), dflt("lastValue", s)}
	case "date":
		fields = []pfield{dflt("fullDate", s), dflt("format", s), dflt("lid", s), dflt("calendar", s), dflt("storeAs", s)}
	case "gallery":
		fields = []pfield{dflt("list", b), dflt("gallery", s), dflt("category", s), dflt("unique", b)}
	case "repeatingSection":
		fields = []pfield{dflt("title", s), dflt("noInsertDelete", b)}
	}
	at := g.rng.IntN(len(fields) + 1)
	tag := req("type", func(g *pgen) { g.w(`"` + typ + `"`) })
	all := append(append(append([]pfield(nil), fields[:at]...), tag), fields[at:]...)
	g.object(all)
}

func (g *pgen) contentControl() {
	s, b := (*pgen).str, (*pgen).boolean
	g.object([]pfield{
		opt("id", func(g *pgen) { g.w(g.pick("0", "-1", "123456789", "9223372036854775807", "-9223372036854775808")) }), dflt("tag", s),
		dflt("title", s), dflt("lock", g.enumOf(controlLockNames)), dflt("placeholder", s), dflt("showingPlaceholder", b), dflt("temporary", b),
		dflt("kind", (*pgen).controlKind), dflt("placeholderText", s), dflt("block", b), dflt("rprXml", s), opt("runProps", (*pgen).charProps),
		dflt("extra", func(g *pgen) { g.list(s) }), dflt("endPrXml", s),
	})
}

func (g *pgen) controlWrap() {
	g.object([]pfield{dflt("open", func(g *pgen) { g.list((*pgen).contentControl) }), dflt("close", func(g *pgen) { g.uint(32) })})
}

func (g *pgen) blocks() { g.list((*pgen).block) }

func (g *pgen) cell() {
	g.object([]pfield{dflt("props", (*pgen).cellProps), req("blocks", (*pgen).blocks), dflt("controls", (*pgen).controlWrap)})
}

func (g *pgen) row() {
	g.object([]pfield{dflt("props", (*pgen).rowProps), req("cells", func(g *pgen) { g.list((*pgen).cell) }),
		dflt("controls", (*pgen).controlWrap)})
}

func (g *pgen) tableFields() []pfield {
	return []pfield{dflt("props", (*pgen).tableProps), req("grid", func(g *pgen) { g.list((*pgen).f32) }),
		req("rows", func(g *pgen) { g.list((*pgen).row) }), dflt("controls", (*pgen).controlWrap)}
}

func (g *pgen) table() { g.object(g.tableFields()) }

// smallTable is a table with simple cells, for the table edits.
func (g *pgen) smallTable() {
	rows, cols := 1+g.rng.IntN(4), 1+g.rng.IntN(4)
	g.w(`{"props":{},"grid":[`)
	for c := range cols + g.rng.IntN(2) - g.rng.IntN(2) {
		if c > 0 {
			g.w(",")
		}
		g.w(g.pick("72.0", "100.0", "36.5", "0.0", "-5.0", "1e30", "0.001", "0.004"))
	}
	g.w(`],"rows":[`)
	for r := range rows {
		if r > 0 {
			g.w(",")
		}
		g.w(`{"props":{"header":` + g.pick("false", "true") + `},"cells":[`)
		for c := range cols - g.rng.IntN(2) {
			if c > 0 {
				g.w(",")
			}
			g.w(`{"props":{"span":` + g.pick("1", "1", "1", "2", "0", "3") + `,"vmerge":"` + g.pick("none", "none", "restart", "continue") +
				`"},"blocks":[{"kind":"para","text":` + strconv.Quote(g.pick("", "", "a", "bc")) + `,"runs":[]}]}`)
		}
		g.w("]}")
	}
	g.w("]}")
}

func (g *pgen) tableOps() {
	g.w("[")
	for i := range 1 + g.rng.IntN(6) {
		if i > 0 {
			g.w(",")
		}
		op := g.rng.IntN(14)
		arg := func() string {
			if g.chance(15) {
				return g.pick("63", "64", "100", "1099511627776")
			}
			return strconv.Itoa(g.rng.IntN(6))
		}
		a, b, c, d := arg(), arg(), arg(), arg()
		if op == 7 || op == 13 {
			c, d = strconv.Itoa(g.rng.IntN(5)), strconv.Itoa(g.rng.IntN(5))
		}
		if op == 13 {
			a, b, c = strconv.Itoa(g.rng.IntN(5)), strconv.Itoa(g.rng.IntN(5)), g.pick("0", "100", "468", "5")
		}
		g.w("[" + strconv.Itoa(op) + "," + a + "," + b + "," + c + "," + d + "]")
	}
	g.w("]")
}

func (g *pgen) numberingOps() {
	g.w("[")
	for i := range 1 + g.rng.IntN(10) {
		if i > 0 {
			g.w(",")
		}
		g.w("[" + strconv.Itoa(g.rng.IntN(7)) + "," + g.pick("0", "1", "2", "3", "4", "4294967295") + "," + g.pick("0", "0", "1", "2", "3", "8", "9", "255") + ",")
		if g.chance(2) {
			g.w("null")
		} else {
			g.listKind()
		}
		g.w("]")
	}
	g.w("]")
}

// modelInput writes the input of one of the operations y n a h Y H z Z l j w.
func (g *pgen) modelInput(op byte) {
	switch op {
	case 'y':
		g.styleSheet()
	case 'n':
		g.numbering()
	case 'a':
		g.table()
	case 'h':
		g.contentControl()
	case 'Y':
		g.w("[")
		if g.chance(3) {
			g.w("{}")
		} else {
			g.styleSheet()
		}
		g.w(",")
		g.styleID()
		g.w(",")
		g.styleName()
		g.w("]")
	case 'H':
		g.w("[")
		g.style()
		g.w(",")
		g.enum(tablePartNames)
		g.w(",")
		g.partFormat()
		g.w("]")
	case 'z':
		g.w("[")
		g.object([]pfield{dflt("headerRow", (*pgen).boolean), dflt("totalRow", (*pgen).boolean), dflt("bandedRows", (*pgen).boolean),
			dflt("firstColumn", (*pgen).boolean), dflt("lastColumn", (*pgen).boolean), dflt("bandedColumns", (*pgen).boolean)})
		g.w("," + g.pick("0", "1", "2", "3", "1000", "1001", "4294967295"))
		for range 5 {
			g.w("," + g.pick("0", "1", "2", "3", "4", "5", "7", "9223372036854775806", "9223372036854775807"))
		}
		g.w("]")
	case 'Z':
		g.w("[")
		if g.chance(3) {
			g.w(`{}`)
		} else {
			g.numbering()
		}
		g.w(",")
		g.numberingOps()
		g.w("]")
	case 'l':
		g.w("[")
		g.listKind()
		g.w(",")
		g.w(strconv.Quote(g.pick("bullet", "number", "numbered", "multilevel", "legal", "x", "", "ab", "★", "😀", "outline", "Bullet")))
		g.w("]")
	case 'j':
		g.w("[")
		if g.chance(4) {
			g.table()
		} else {
			g.smallTable()
		}
		g.w(",")
		g.tableOps()
		g.w("]")
	case 'w':
		g.styleName()
	}
}

// modelEdgeInputs are fixed inputs for the model operations: every code point through new_id
// (char::is_alphanumeric), the built-in sheet's lookups, every list kind, the Rust tests' list
// and table scenarios.
func modelEdgeInputs() [][]byte {
	var out [][]byte
	add := func(op byte, js string) { out = append(out, append([]byte{op}, js...)) }
	var b strings.Builder
	for c := rune(0); c <= utf8.MaxRune; c++ {
		if c >= 0xD800 && c <= 0xDFFF {
			continue
		}
		b.WriteRune(c)
		if c%2048 == 2047 || c == utf8.MaxRune {
			add('w', strconv.QuoteToASCII(b.String()))
			b.Reset()
		}
	}
	for _, c := range [][2]string{{"Heading1", "heading 2"}, {"Normal", "My Style"}, {"TableGrid", "Normal"}, {"GridTable4AccentBlue", "!!!"},
		{"ListTable3AccentOrange", "List Table 3 – Orange"}, {"Heading9", "TOC 9"}, {"TOCHeading", "toc heading"}, {"Nope", ""}} {
		add('Y', `[{},"`+c[0]+`","`+c[1]+`"]`)
	}
	add('Y', `[{"styles":[{"id":"B","name":"b","quick":true,"priority":5},{"id":"A","name":"a","quick":true,"priority":5},`+
		`{"id":"C","name":"C","quick":true},{"id":"D","name":"B","quick":true,"priority":99}]},"A","b"]`)
	add('Y', `[{"styles":[{"id":"A","basedOn":"B","kind":"table"},{"id":"B","basedOn":"A","kind":"table"}]},"A","a"]`)
	add('Y', `[{"styles":[{"id":"N","builtin":true,"name":"normal"},{"id":"Heading1","name":"x","builtin":true}]},"N","heading 1"]`)
	add('Y', `[{"styles":[{"id":"L","para":{"numbering":{"num":1,"level":0},"indentLeft":3}},{"id":"M","basedOn":"L","para":{"indentFirstChars":0}},`+
		`{"id":"K","basedOn":"M","para":{"indentFirstChars":2}}]},"K","k"]`)
	for _, k := range []string{`"bullet"`, `"numbered"`, `"numberedParen"`, `"outline"`, `"upperLetter"`, `"lowerLetter"`, `"lowerLetterDot"`,
		`"lowerRoman"`, `"legal"`, `{"bulletChar":"★"}`, `{"bulletChar":"-"}`, `{"bulletChar":"–"}`, `{"bulletChar":"o"}`} {
		add('l', `[`+k+`,"multilevel"]`)
	}
	add('Z', `[{},[[2,0,0,"numbered"],[0,1,0,null],[0,1,0,null],[0,1,1,null],[0,1,1,null],[0,1,0,null],[0,1,1,null],[0,999,0,null],`+
		`[3,1,0,null],[0,2,0,null],[2,0,0,"bullet"],[0,3,0,null],[4,0,0,"bullet"],[1,1,0,null]]]`)
	add('Z', `[{},[[2,0,0,"legal"],[0,1,0,null],[0,1,1,null],[0,1,2,null],[0,1,1,null],[6,1,4,null],[5,1,8,null],[5,1,9,null]]]`)
	add('Z', `[{"abstracts":[{"id":0,"levels":[{"text":"%1."},{"text":"%1.%2.","restart":false},{"text":"(%3)","restartAfter":1},`+
		`{"text":"%4","restartAfter":2}]}],"nums":[{"id":1,"abstractId":0,"startOverrides":[]}]},`+
		`[[0,1,0,null],[0,1,1,null],[0,1,2,null],[0,1,3,null],[0,1,1,null],[0,1,2,null],[0,1,3,null],[0,1,0,null],[0,1,1,null],[0,1,2,null],[0,1,3,null]]]`)
	add('Z', `[{"abstracts":[{"id":4294967295,"levels":[{}]}],"nums":[{"id":4294967295,"abstractId":4294967295,"startOverrides":[[0,4294967295]]}]},`+
		`[[0,4294967295,0,null],[0,4294967295,0,null],[2,0,0,"numbered"],[3,4294967295,0,null]]]`)
	add('Z', `[{"abstracts":[{"id":0,"levels":[{"format":"chineseCounting","text":"第%1章"},{"format":"chineseCounting","text":"%2、"},`+
		`{"format":"chineseCounting","text":"%1.%2.%3","legal":true},{"format":"decimalZero","text":"%1.%4","legal":true}]}],`+
		`"nums":[{"id":1,"abstractId":0,"startOverrides":[],"levelOverrides":[[1,{"format":"upperRoman","text":"Part %2","start":5}]]}]},`+
		`[[0,1,0,null],[0,1,1,null],[0,1,1,null],[0,1,2,null],[0,1,3,null],[3,1,0,null],[0,2,1,null]]]`)
	add('j', `[{"grid":[],"rows":[]},[[13,3,4,468,0],[11,0,0,0,0],[12,0,0,0,0],[0,1,0,0,0],[2,1,0,0,0],[3,0,0,0,0],[1,0,0,0,0],[1,99,0,0,0],[3,99,0,0,0]]]`)
	add('j', `[{"grid":[],"rows":[]},[[13,2,3,300,0],[4,0,0,1,0],[9,0,1,0,0],[10,0,1,0,0],[2,1,0,0,0],[6,0,0,3,0]]]`)
	add('j', `[{"grid":[],"rows":[]},[[13,3,2,100,0],[5,0,2,0,0],[8,1,0,0,0],[8,2,0,0,0],[7,1,0,2,3],[1,0,0,0,0]]]`)
	add('j', `[{"grid":[],"rows":[]},[[13,2,2,100,0],[7,0,0,3,2],[7,0,1,1,3],[8,1,1,0,0]]]`)
	add('j', `[{"grid":[],"rows":[]},[[13,0,0,0,0],[13,2,1000,100,0],[11,0,0,0,0]]]`)
	add('j', `[{"grid":[0.004,0.004],"rows":[{"cells":[{"props":{"span":2},"blocks":[]}]}]},[[6,0,0,3,0],[6,0,0,2,0]]]`)
	add('j', `[{"grid":[1,2],"rows":[{"props":{"header":true},"cells":[{"props":{"span":1,"vmerge":"restart"},"blocks":[]}]}]},[[0,1,0,0,0],[0,0,0,0,0]]]`)
	add('j', `[{"grid":[72.0],"rows":[{"cells":[{"props":{"span":5},"blocks":[]}]}]},[[6,0,0,3,0],[6,0,0,63,0],[11,0,0,0,0]]]`)
	for _, js := range []string{`{}`, `{"styles":[]}`, `{"defaultParaPresent":false}`, `{"latentStyles":null}`, `[]`, `{"styles":[{}]}`} {
		add('y', js)
	}
	for _, js := range []string{`{}`, `{"abstracts":[{"levels":[{}]}],"nums":[{"startOverrides":[[1,2]],"levelOverrides":[]}]}`,
		`{"nums":[{"startOverrides":[[1]]}]}`, `{"nums":[{"startOverrides":[[256,1]]}]}`, `{"nums":[{"startOverrides":[{"0":1,"1":2}]}]}`} {
		add('n', js)
	}
	for _, js := range []string{`{}`, `{"kind":{"type":"richText","x":1}}`, `{"kind":{"type":"text"}}`, `{"kind":["text",true]}`,
		`{"kind":{"type":"checkBox","checked":true,"checkedChar":"x","checkedFont":"","uncheckedChar":"y","uncheckedFont":""}}`,
		`{"kind":{"type":"picture","items":[]}}`, `{"kind":["picture"]}`, `{"kind":["picture",1]}`, `{"kind":{"type":"comboBox"}}`} {
		add('h', js)
	}
	for _, js := range []string{`{"grid":[],"rows":[]}`, `{"rows":[]}`, `[{},[],[]]`, `[{},[1],[{"cells":[]}],{"close":2}]`,
		`{"grid":[1],"rows":[{"cells":[{"blocks":[{"kind":"table","grid":[],"rows":[]}]}]}],"controls":{"open":[{"kind":{"type":"date"}}]}}`} {
		add('a', js)
		add('b', `{"kind":"table",`+js[1:])
	}
	add('b', `["table",{},[],[]]`)
	add('d', `{"body":[{"kind":"table","grid":[],"rows":[{"cells":[{"blocks":[]}]}]}],"styles":{"styles":[]},"numbering":{"nums":[{}]}}`)
	return out
}
