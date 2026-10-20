package doc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/computernut43/craft-libs/serdejson"
)

// TestPropsMatchRust reads random property JSON (valid values of every field, the forms serde
// also takes, single and double mistakes, and the same values inside a paragraph block, where
// serde reads them from buffered Content) with this package and with crates/doc, and compares
// what each writes back, byte for byte, or that both refuse it. It also runs CharProps'
// overlay, minus, unlinked, cleared, formatting and without_symbol, ParaProps' and Borders'
// overlay, on both sides. It runs when WORDCRAFT_PROPS_RUST names testdata/rust-oracle's props
// binary; WORDCRAFT_PROPS_CASES sets how many inputs (default 30,000).
func TestPropsMatchRust(t *testing.T) {
	bin := os.Getenv("WORDCRAFT_PROPS_RUST")
	if bin == "" {
		t.Skip("set WORDCRAFT_PROPS_RUST to testdata/rust-oracle's release props binary to compare with Rust")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("WORDCRAFT_PROPS_RUST must be absolute")
	}
	n := 30000
	if v := os.Getenv("WORDCRAFT_PROPS_CASES"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("WORDCRAFT_PROPS_CASES=%q: want a positive count", v)
		}
	}
	ask := func(inputs [][]byte) []string {
		var in bytes.Buffer
		for _, x := range inputs {
			in.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(x))))
			in.Write(x)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin)
		cmd.Stdin, cmd.Env = &in, []string{}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("reference: %v", err)
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		sc.Buffer(make([]byte, 1<<20), 1<<28)
		var lines []string
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if len(lines) != len(inputs) {
			t.Fatalf("reference answered %d of %d", len(lines), len(inputs))
		}
		return lines
	}
	// The parts of a document Go doesn't type yet, as Rust writes their defaults: documents
	// carry them so both sides start from the same (Raw keeps what it reads).
	defaults := ask([][]byte{[]byte("d{}")})[0]
	untyped, err := untypedParts(defaults)
	if err != nil {
		t.Fatalf("default document %s: %v", defaults, err)
	}
	rng := rand.New(rand.NewPCG(4, 11))
	inputs := propEdgeInputs(untyped)
	for range n {
		g := &pgen{rng: rng, hostile: rng.IntN(5) < 2, mistakes: 1 + rng.IntN(2), untyped: untyped}
		inputs = append(inputs, g.input())
	}
	lines := ask(inputs)
	bad, read := 0, map[byte]int{}
	for i, x := range inputs {
		got := propsAnswer(x)
		if got != lines[i] {
			bad++
			if bad <= 5 {
				t.Errorf("input %d %.600q:\n%s", i, x, firstDiff([]byte(got), []byte(lines[i])))
			}
		} else if got != "ERROR" {
			read[x[0]]++
		}
	}
	t.Logf("%d inputs, %d differ; read by both, per operation: %v", len(inputs), bad, readCounts(read))
	if bad > 0 {
		t.Fatalf("%d of %d differ", bad, len(inputs))
	}
}

func readCounts(m map[byte]int) string {
	var b strings.Builder
	for _, op := range "cptrexkbdOMULFWEQB" {
		b.WriteString(string(op) + "=" + strconv.Itoa(m[byte(op)]) + " ")
	}
	return b.String()
}

// untypedParts are the default document's fields that are Raw here, as JSON members.
func untypedParts(pretty string) (string, error) {
	text := unescapeLine(pretty)
	var d Document
	if err := serdejson.Unmarshal([]byte(text), &d); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range []struct {
		name string
		raw  Raw
	}{{"lastSection", d.LastSection}, {"parts", d.Parts}, {"styles", d.Styles}, {"numbering", d.Numbering}, {"comments", d.Comments},
		{"revisions", d.Revisions}, {"settings", d.Settings}, {"sources", d.Sources}, {"customProps", d.CustomProps}} {
		b.WriteString(`,"` + f.name + `":` + string(f.raw))
	}
	return b.String(), nil
}

// unescapeLine undoes the oracle's one-line escaping (\\ for \, \n for a newline).
func unescapeLine(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			if s[i] == 'n' {
				b.WriteByte('\n')
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// propsAnswer is what the reference program answers for one input.
func propsAnswer(x []byte) string {
	if len(x) == 0 {
		return "BAD OP"
	}
	op, js := x[0], x[1:]
	var v any
	switch op {
	case 'c':
		v = new(CharProps)
	case 'p':
		v = new(ParaProps)
	case 't':
		v = new(TableProps)
	case 'r':
		v = new(RowProps)
	case 'e':
		v = new(CellProps)
	case 'x':
		v = new(TextEffects)
	case 'k':
		v = new(CoreProps)
	case 'b':
		v = new(Block)
	case 'd':
		v = new(Document)
	case 'O', 'M':
		var pair [2]CharProps
		if serdejson.Unmarshal(js, &pair) != nil {
			return "ERROR"
		}
		if op == 'O' {
			return show(pair[0].Overlaid(&pair[1]))
		}
		return show(pair[0].Minus(&pair[1]))
	case 'U', 'L', 'F', 'W', 'E':
		var c CharProps
		if serdejson.Unmarshal(js, &c) != nil {
			return "ERROR"
		}
		switch op {
		case 'U':
			return show(c.Unlinked())
		case 'L':
			return show(c.Cleared())
		case 'F':
			return show(c.Formatting())
		case 'W':
			return show(c.WithoutSymbol())
		}
		var font *string
		if f, ok := c.ShownFont(); ok {
			font = &f
		}
		return show([]any{c.IsEmpty(), font})
	case 'Q':
		var pair [2]ParaProps
		if serdejson.Unmarshal(js, &pair) != nil {
			return "ERROR"
		}
		return show(pair[0].Overlaid(&pair[1]))
	case 'B':
		var pair [2]Borders
		if serdejson.Unmarshal(js, &pair) != nil {
			return "ERROR"
		}
		pair[0].Overlay(&pair[1])
		return show(pair[0])
	default:
		return "BAD OP"
	}
	if serdejson.Unmarshal(js, v) != nil {
		return "ERROR"
	}
	return show(v)
}

func show(v any) string {
	b, err := serdejson.Marshal(v)
	if err != nil {
		return "SERIALIZE " + err.Error()
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(b), `\`, `\\`), "\n", `\n`)
}

// pgen writes random property JSON.
type pgen struct {
	rng      *rand.Rand
	b        strings.Builder
	hostile  bool
	mistakes int
	untyped  string
	depth    int
	// The untyped parts' place in the input, which damage leaves alone (Raw keeps them as read,
	// where Rust would drop a mangled key and fill in its default).
	untypedAt, untypedEnd int
}

func (g *pgen) chance(n int) bool { return g.rng.IntN(n) == 0 }

// oops is a mistake, made only in hostile inputs and only while their budget lasts.
func (g *pgen) oops(n int) bool {
	if !g.hostile || g.mistakes == 0 || !g.chance(n) {
		return false
	}
	g.mistakes--
	return true
}

func (g *pgen) pick(xs ...string) string { return xs[g.rng.IntN(len(xs))] }
func (g *pgen) w(s string)               { g.b.WriteString(s) }

var f32Edge = []float32{0, 1, -1, 12, 12.5, 0.1, 1e-7, 1.5e-6, 123456789, 1e13, 3.4028235e38, 1.1754944e-38, 1e-45,
	float32(math.Copysign(0, -1)), 72, 0.33333334, 16777216, 0.75, 1.15, 100, 50}

func (g *pgen) f32() {
	switch {
	case g.oops(15):
		g.w(g.pick("1e39", "-1e39", "1e400", `"1"`, "true", "[]", "1e-46", "3.4028235677973366e38", "-0", "16777217",
			"1.0000000596046447753906250000001"))
	case g.chance(4):
		f := math.Float64frombits(g.rng.Uint64()>>2 | 0x3800000000000000)
		g.w(strconv.FormatFloat(f*float64(1-2*g.rng.IntN(2)), 'g', 17+g.rng.IntN(4), 64))
	case g.chance(3):
		g.w(strconv.Itoa(g.rng.IntN(2000) - 500))
	default:
		g.w(serdejson.FormatF32(f32Edge[g.rng.IntN(len(f32Edge))]))
	}
}

func (g *pgen) uint(bits int) {
	if g.oops(15) {
		g.w(g.pick("-1", "1.0", "1e2", `"1"`, "256", "65536", "4294967296", "18446744073709551616", "-0"))
		return
	}
	switch g.rng.IntN(4) {
	case 0:
		g.w(strconv.Itoa(g.rng.IntN(10)))
	case 1:
		g.w(strconv.FormatUint(uint64(1)<<bits-1, 10))
	default:
		g.w(strconv.FormatUint(g.rng.Uint64()>>(64-bits), 10))
	}
}

func (g *pgen) i32() {
	if g.oops(15) {
		g.w(g.pick("2147483648", "-2147483649", "1.5", `"1"`))
		return
	}
	g.w(strconv.Itoa(int(int32(g.rng.Uint32()))))
}

func (g *pgen) boolean() {
	if g.oops(15) {
		g.w(g.pick("1", `"true"`, "0"))
		return
	}
	g.w(g.pick("true", "false"))
}

func (g *pgen) str() {
	parts := []string{"a", "Arial", "Times New Roman", "Hyperlink", "Heading1", " ", `\"`, `\\`, `\n`, `\u0001`, "é", "😀", `\ud83d\ude00`,
		"\u2028", "<&>", "https://example.org/a?b=c", "#bm", "pl-PL", "Wingdings"}
	if g.oops(10) {
		parts = append(parts, `\ud800`, `\udc00`, `\x`)
	}
	var s strings.Builder
	s.WriteByte('"')
	for range g.rng.IntN(3) {
		s.WriteString(parts[g.rng.IntN(len(parts))])
	}
	if g.oops(40) {
		s.WriteString(g.pick("\x01", "\xff"))
	}
	s.WriteByte('"')
	g.w(s.String())
}

func (g *pgen) rgb() {
	if g.oops(6) {
		g.w(g.pick("[1,2]", "[1,2,3,4]", "[256,0,0]", "[-1,0,0]", "[1.0,2,3]", `"12345"`, `"zzzzzz"`, `"é12345"`, `""`, "7", `{"r":1}`,
			`"1234567"`, `"-1-1-1"`, `"++1234"`, `["1",2,3]`))
		return
	}
	switch g.rng.IntN(5) {
	case 0, 1:
		g.w("[" + strconv.Itoa(g.rng.IntN(256)) + "," + strconv.Itoa(g.rng.IntN(256)) + " , " + strconv.Itoa(g.rng.IntN(256)) + "]")
	case 2:
		g.w(`"` + RGB{uint8(g.rng.Uint32()), uint8(g.rng.Uint32()), uint8(g.rng.Uint32())}.Hex() + `"`)
	default:
		g.w(g.pick(`"#1b1611"`, `" #FF00ff "`, `"+F+F+F"`, `"##123456"`, `"\t000000\n"`, `"ffFFff"`, `"\u00a0#0a0B0c"`))
	}
}

func (g *pgen) textColor() {
	if g.oops(5) {
		g.w(g.pick(`""`, `"red"`, `{"Rgb":[1,2]}`, `{"Hex":[1,2,3]}`, "7", "[27,22,17]", `{"Auto":{}}`, `{"Auto":null,"Rgb":[1,2,3]}`,
			`{"rgb":[1,2,3]}`, `"Rgb"`, `{"Rgb":"1B16"}`, `{}`, `"autö"`, `{"Rgb":[1,2,3],"Rgb":[1e400]}`))
		return
	}
	switch g.rng.IntN(4) {
	case 0:
		g.w(g.pick(`"auto"`, `"AUTO"`, `"Automatic"`, `"Auto"`, `"\u0061uto"`, `{"Auto":null}`))
	case 1:
		g.w(g.pick(`"1B1611"`, `"#1b1611"`, `{"Rgb":"1B1611"}`, `{"Rgb":[1,2,3],"Rgb":[4,5,6]}`))
	default:
		g.w(`{"Rgb":`)
		g.rgb()
		g.w("}")
	}
}

// enum writes a fieldless enum's value: its name, or {"name": null}; in Content also {"name": {}}.
func (g *pgen) enum(names []string) {
	if g.oops(8) {
		n := names[g.rng.IntN(len(names))]
		g.w(g.pick(`"`+strings.ToUpper(n[:1])+n[1:]+`"`, `"bogus"`, "0", `{}`, `{"`+n+`":{}}`, `{"`+n+`":1}`, `["`+n+`"]`,
			`{"`+n+`":null,"`+n+`":null}`))
		return
	}
	n := names[g.rng.IntN(len(names))]
	if g.chance(10) {
		g.w(`{"` + n + `":null}`)
		return
	}
	g.w(`"` + n + `"`)
}

type pfield struct {
	name     string
	gen      func(*pgen)
	optional bool // may be missing (an Option or a default)
	nullable bool // null is None
}

// object writes a struct: its fields as a map in any order (some missing, some repeated, some
// unknown), or as a sequence.
func (g *pgen) object(fields []pfield) {
	if g.depth > 4 {
		g.w("{}")
		return
	}
	g.depth++
	defer func() { g.depth-- }()
	if g.oops(30) {
		g.w(g.pick("null", "1", `"s"`, "true"))
		return
	}
	if g.chance(40) {
		// A struct as the sequence of its fields; trailing defaults may be left off.
		g.w("[")
		n := g.rng.IntN(min(len(fields), 4) + 1)
		required := 0
		for i, f := range fields {
			if !f.optional {
				required = i + 1
			}
		}
		n = max(n, required)
		if g.oops(5) {
			n = g.rng.IntN(len(fields) + 2)
		}
		for i := range n {
			if i > 0 {
				g.w(",")
			}
			if i < len(fields) {
				fields[i].gen(g)
			} else {
				g.w("0")
			}
		}
		g.w("]")
		return
	}
	order := g.rng.Perm(len(fields))
	if g.chance(2) {
		for i := range order {
			order[i] = i
		}
	}
	g.w("{")
	first := true
	sep := func() {
		if !first {
			g.w(",")
		}
		first = false
	}
	for _, i := range order {
		f := fields[i]
		if f.optional && !g.chance(3) || !f.optional && g.oops(20) {
			continue
		}
		reps := 1
		if g.oops(30) {
			reps = 2
		}
		for range reps {
			sep()
			g.w(`"` + f.name + `":`)
			switch {
			case f.nullable && g.chance(8), g.oops(40):
				g.w("null")
			default:
				f.gen(g)
			}
		}
	}
	if g.chance(8) {
		sep()
		g.w(g.pick(`"unknown":[1,{"x":"\ud800"}]`, `"Bold":true`, `"bold_cs":true`, `"insideV":null`, `"x":{"deep":[[[]]]}`))
	}
	g.w("}")
}

func opt(name string, gen func(*pgen)) pfield  { return pfield{name, gen, true, true} }
func dflt(name string, gen func(*pgen)) pfield { return pfield{name, gen, true, false} }
func req(name string, gen func(*pgen)) pfield  { return pfield{name, gen, false, false} }

func (g *pgen) enumOf(names []string) func(*pgen) { return func(g *pgen) { g.enum(names) } }

func (g *pgen) border() {
	g.object([]pfield{dflt("style", g.enumOf(borderStyleNames)), dflt("width", (*pgen).f32), opt("color", (*pgen).rgb), dflt("space", (*pgen).f32)})
}

func (g *pgen) borders() {
	b := (*pgen).border
	g.object([]pfield{opt("top", b), opt("left", b), opt("bottom", b), opt("right", b), opt("between", b), opt("inside_v", b)})
}

func (g *pgen) lineSpacing() {
	if g.oops(5) {
		g.w(g.pick(`{"rule":"multiple"}`, `{"value":1}`, `{"rule":"auto","value":1}`, `{"rule":"multiple","value":1,"rule":"exactly"}`,
			`{"value":1,"rule":"multiple","value":2}`, `["exactly"]`, `[0,1.5]`, `{"rule":{"multiple":{}},"value":1}`, `{"rule":0,"value":1}`,
			`{"rule":"multiple","value":"1"}`, `{"rule":"multiple","value":1e39}`, `{"value":1e39,"rule":"multiple"}`))
		return
	}
	rule := g.pick(lineRuleNames...)
	var v strings.Builder
	sub := &pgen{rng: g.rng}
	sub.f32()
	v.WriteString(sub.b.String())
	g.w(g.pick(
		`{"rule":"`+rule+`","value":`+v.String()+`}`,
		`{"value":`+v.String()+`,"rule":"`+rule+`"}`,
		`["`+rule+`",`+v.String()+`]`,
		`{"x":1,"rule":{"`+rule+`":null},"y":[],"value":`+v.String()+`}`,
	))
}

func (g *pgen) tabs() {
	if g.oops(20) {
		g.w(g.pick("{}", `"x"`, "[null]"))
		return
	}
	g.w("[")
	for i := range g.rng.IntN(4) {
		if i > 0 {
			g.w(",")
		}
		g.object([]pfield{req("pos", (*pgen).f32), dflt("align", g.enumOf(tabAlignNames)), dflt("leader", g.enumOf(tabLeaderNames))})
	}
	g.w("]")
}

func (g *pgen) list(elem func(*pgen)) {
	if g.oops(20) {
		g.w(g.pick("{}", `"x"`, "null"))
		return
	}
	g.w("[")
	for i := range g.rng.IntN(4) {
		if i > 0 {
			g.w(",")
		}
		elem(g)
	}
	g.w("]")
}

func (g *pgen) four(elem func(*pgen)) {
	n := 4
	if g.oops(10) {
		n = g.rng.IntN(6)
	}
	g.w("[")
	for i := range n {
		if i > 0 {
			g.w(",")
		}
		elem(g)
	}
	g.w("]")
}

func (g *pgen) textEffects() {
	fill := func(g *pgen) {
		if g.oops(6) {
			g.w(g.pick(`{"type":"solid"}`, `{"type":"Solid","color":[1,2,3]}`, `{"color":[1,2,3]}`, `{"type":"gradient"}`, `{"type":0}`,
				`{"type":"none","type":"none"}`, `["solid"]`))
			return
		}
		switch g.rng.IntN(3) {
		case 0:
			g.w(g.pick(`{"type":"none"}`, `{"x":1,"type":"none"}`, `["none",1,2]`))
		case 1:
			g.object([]pfield{req("type", func(g *pgen) { g.w(`"solid"`) }), req("color", (*pgen).rgb), dflt("transparency", (*pgen).f32)})
		default:
			g.object([]pfield{req("type", func(g *pgen) { g.w(`"gradient"`) }), req("stops", func(g *pgen) {
				g.list(func(g *pgen) {
					g.object([]pfield{req("pos", (*pgen).f32), req("color", (*pgen).rgb), dflt("transparency", (*pgen).f32)})
				})
			}), dflt("angle", (*pgen).f32)})
		}
	}
	g.object([]pfield{
		opt("fill", fill),
		opt("outline", func(g *pgen) {
			g.object([]pfield{opt("color", (*pgen).rgb), dflt("width", (*pgen).f32), dflt("transparency", (*pgen).f32)})
		}),
		opt("shadow", func(g *pgen) {
			g.object([]pfield{dflt("color", (*pgen).rgb), dflt("transparency", (*pgen).f32), dflt("blur", (*pgen).f32),
				dflt("distance", (*pgen).f32), dflt("angle", (*pgen).f32), dflt("rotWithShape", (*pgen).boolean)})
		}),
		opt("glow", func(g *pgen) {
			g.object([]pfield{dflt("color", (*pgen).rgb), dflt("size", (*pgen).f32), dflt("transparency", (*pgen).f32)})
		}),
		opt("reflection", func(g *pgen) {
			g.object([]pfield{dflt("transparency", (*pgen).f32), dflt("size", (*pgen).f32), dflt("distance", (*pgen).f32),
				dflt("blur", (*pgen).f32)})
		}),
	})
}

func (g *pgen) charProps() {
	s, b, f := (*pgen).str, (*pgen).boolean, (*pgen).f32
	g.object([]pfield{
		opt("style", s), opt("font", s), opt("symFont", s), opt("size", f), opt("bold", b), opt("italic", b),
		opt("underline", g.enumOf(underlineNames)), opt("underlineColor", (*pgen).rgb), opt("strike", b), opt("doubleStrike", b),
		opt("color", (*pgen).textColor), opt("highlight", g.enumOf(highlightNames)), opt("shading", (*pgen).rgb), opt("border", (*pgen).border),
		opt("vertAlign", g.enumOf(vertAlignNames)), opt("caps", b), opt("smallCaps", b), opt("hidden", b), opt("spacing", f), opt("scale", f),
		opt("position", f), opt("kern", f), opt("ligatures", func(g *pgen) {
			g.object([]pfield{dflt("standard", b), dflt("contextual", b), dflt("historical", b), dflt("discretional", b)})
		}),
		opt("outline", b), opt("shadow", b), opt("emboss", b), opt("engrave", b), opt("textEffects", (*pgen).textEffects), opt("lang", s),
		opt("noProof", b), opt("rtl", b), opt("cs", b), opt("fontCs", s), opt("fontEa", s), opt("sizeCs", f), opt("boldCs", b), opt("italicCs", b),
		opt("langBidi", s), opt("eastAsianLayout", func(g *pgen) {
			g.object([]pfield{opt("id", (*pgen).i32), opt("combine", b), opt("combineBrackets", g.enumOf(combineBracketsNames)), opt("vert", b),
				opt("vertCompress", b)})
		}),
		opt("link", s), opt("ins", func(g *pgen) { g.uint(32) }), opt("del", func(g *pgen) { g.uint(32) }),
		opt("fmtChange", func(g *pgen) { g.propChange((*pgen).charProps) }),
	})
}

func (g *pgen) propChange(old func(*pgen)) {
	if g.depth > 2 {
		g.w("null")
		return
	}
	g.object([]pfield{req("rev", func(g *pgen) { g.uint(32) }), req("old", old)})
}

func (g *pgen) paraProps() {
	s, b, f := (*pgen).str, (*pgen).boolean, (*pgen).f32
	g.object([]pfield{
		opt("style", s), opt("align", g.enumOf(alignNames)), opt("indentLeft", f), opt("indentRight", f), opt("indentFirst", f),
		opt("indentLeftChars", f), opt("indentRightChars", f), opt("indentFirstChars", f), opt("spaceBefore", f), opt("spaceAfter", f),
		opt("spaceBeforeAuto", b), opt("spaceAfterAuto", b), opt("lineSpacing", (*pgen).lineSpacing), opt("contextualSpacing", b),
		opt("mirrorIndents", b), opt("keepNext", b), opt("keepLines", b), opt("pageBreakBefore", b), opt("widowControl", b),
		opt("outlineLevel", func(g *pgen) { g.uint(8) }), opt("numbering", func(g *pgen) {
			g.object([]pfield{req("num", func(g *pgen) { g.uint(32) }), req("level", func(g *pgen) { g.uint(8) })})
		}),
		opt("tabs", (*pgen).tabs), opt("shading", (*pgen).rgb), opt("borders", (*pgen).borders), opt("suppressHyphens", b),
		opt("suppressLineNumbers", b), opt("bidi", b), opt("dropCap", func(g *pgen) { g.uint(8) }), opt("dropCapMargin", b),
		opt("dropCapDistance", f), opt("dropCapFont", s), opt("frame", func(g *pgen) {
			g.object([]pfield{opt("w", f), opt("h", f), dflt("hExact", b), dflt("x", f), dflt("y", f), opt("xAlign", s), opt("yAlign", s),
				dflt("hAnchor", s), dflt("vAnchor", s), dflt("wrap", s), dflt("hSpace", f), dflt("vSpace", f)})
		}),
		opt("kinsoku", b), opt("wordWrap", b), opt("overflowPunct", b), opt("topLinePunct", b), opt("autoSpaceDE", b), opt("autoSpaceDN", b),
		opt("numChange", func(g *pgen) { g.object([]pfield{dflt("rev", func(g *pgen) { g.uint(32) }), dflt("original", s)}) }),
		opt("fmtChange", func(g *pgen) { g.propChange((*pgen).paraProps) }), opt("snapToGrid", b),
	})
}

func (g *pgen) tableProps() {
	s, b, f := (*pgen).str, (*pgen).boolean, (*pgen).f32
	anchor := g.enumOf(anchorNames)
	g.object([]pfield{
		opt("style", s), opt("width", f), opt("widthPct", f), opt("align", g.enumOf(alignNames)), opt("indent", f),
		opt("borders", (*pgen).borders), opt("cellMargins", func(g *pgen) { g.four(f) }), dflt("fixed", b), dflt("look", func(g *pgen) {
			g.object([]pfield{dflt("headerRow", b), dflt("totalRow", b), dflt("bandedRows", b), dflt("firstColumn", b), dflt("lastColumn", b),
				dflt("bandedColumns", b)})
		}),
		opt("shading", (*pgen).rgb), opt("caption", s), opt("description", s), opt("float", func(g *pgen) {
			g.object([]pfield{dflt("hRel", anchor), dflt("vRel", anchor), dflt("x", f), dflt("y", f), opt("hAlign", g.enumOf(floatAlignNames)),
				opt("vAlign", g.enumOf(floatAlignNames)), dflt("dist", func(g *pgen) { g.four(f) }), dflt("overlap", b)})
		}),
		opt("fmtChange", func(g *pgen) { g.propChange((*pgen).tableProps) }), dflt("bidiVisual", b),
	})
}

func (g *pgen) rowProps() {
	b, f := (*pgen).boolean, (*pgen).f32
	g.object([]pfield{
		opt("height", f), dflt("heightRule", g.enumOf(heightRuleNames)), dflt("header", b), dflt("cantSplit", b),
		opt("exceptions", (*pgen).tableProps), dflt("gridBefore", func(g *pgen) { g.uint(32) }), opt("wBefore", f),
		dflt("gridAfter", func(g *pgen) { g.uint(32) }), opt("wAfter", f), opt("fmtChange", func(g *pgen) { g.propChange((*pgen).rowProps) }),
	})
}

func (g *pgen) cellProps() {
	b, f := (*pgen).boolean, (*pgen).f32
	g.object([]pfield{
		opt("width", f), opt("widthPct", f), dflt("span", func(g *pgen) { g.uint(32) }), dflt("vmerge", g.enumOf(vMergeNames)),
		opt("shading", (*pgen).rgb), opt("borders", (*pgen).borders), opt("valign", g.enumOf(vAlignNames)), opt("margins", func(g *pgen) { g.four(f) }),
		opt("partialMargins", func(g *pgen) {
			g.four(func(g *pgen) {
				if g.chance(3) {
					g.w("null")
				} else {
					g.f32()
				}
			})
		}),
		dflt("textDirection", g.enumOf(textDirectionNames)), dflt("textDirectionSet", b), dflt("noWrap", b), dflt("hideMark", b),
		opt("fmtChange", func(g *pgen) { g.propChange((*pgen).cellProps) }),
	})
}

// block writes a paragraph block: serde reads it all as Content, then the paragraph from that.
func (g *pgen) block() {
	text := g.pick("", "abc", "héllo wörld", "😀x", "a\tb")
	runs := func(g *pgen) {
		g.list(func(g *pgen) {
			g.object([]pfield{req("len", func(g *pgen) {
				if g.oops(10) {
					g.w(g.pick("-1", "1.5", "18446744073709551615", `"3"`))
					return
				}
				g.w(strconv.Itoa(g.rng.IntN(len(text) + 2)))
			}), dflt("props", (*pgen).charProps)})
		})
	}
	fields := []pfield{
		req("text", func(g *pgen) { g.w(strconv.Quote(text)) }), req("runs", runs), dflt("props", (*pgen).paraProps),
		dflt("mark", (*pgen).charProps),
	}
	kind := `"para"`
	if g.oops(10) {
		kind = g.pick(`"Para"`, "0", `{"para":null}`, "null")
	}
	at := g.rng.IntN(len(fields) + 1)
	all := append(append(append([]pfield(nil), fields[:at]...), req("kind", func(g *pgen) { g.w(kind) })), fields[at:]...)
	if g.chance(10) {
		all = append([]pfield{req("kind", func(g *pgen) { g.w(kind) })}, fields...)
	}
	g.object(all)
}

func (g *pgen) document() {
	g.w(`{"body":`)
	g.list((*pgen).block)
	g.untypedAt = g.b.Len()
	g.w(g.untyped)
	g.untypedEnd = g.b.Len()
	if g.chance(2) {
		g.w(`,"core":`)
		s := (*pgen).str
		u := func(g *pgen) { g.uint(32) }
		g.object([]pfield{dflt("title", s), dflt("subject", s), dflt("creator", s), dflt("keywords", s), dflt("description", s),
			dflt("category", s), dflt("lastModifiedBy", s), dflt("created", s), dflt("modified", s), dflt("revision", u)})
	}
	if g.chance(3) {
		g.w(`,"footnoteSeparators":`)
		blocks := func(g *pgen) { g.list((*pgen).block) }
		g.object([]pfield{opt("separator", blocks), opt("continuationSeparator", blocks), opt("continuationNotice", blocks)})
	}
	if g.chance(3) {
		g.w(`,"media":{"a.png":[1,2,255],"b":[]}`)
	}
	g.w("}")
}

func (g *pgen) input() []byte {
	ops := "cccppptreexkbbbbddOMULFWEQB"
	op := ops[g.rng.IntN(len(ops))]
	g.b.WriteByte(op)
	pair := func(f func(*pgen)) {
		g.w("[")
		f(g)
		g.w(",")
		f(g)
		g.w("]")
	}
	switch op {
	case 'c', 'U', 'L', 'F', 'W', 'E':
		g.charProps()
	case 'p':
		g.paraProps()
	case 't':
		g.tableProps()
	case 'r':
		g.rowProps()
	case 'e':
		g.cellProps()
	case 'x':
		g.textEffects()
	case 'k':
		g.w(`{"title":"T","revision":`)
		g.uint(32)
		g.w(`,"created":"2026-01-01T00:00:00Z","unknown":1}`)
	case 'b':
		g.block()
	case 'd':
		g.document()
	case 'O', 'M':
		pair((*pgen).charProps)
	case 'Q':
		pair((*pgen).paraProps)
	case 'B':
		pair((*pgen).borders)
	}
	out := []byte(g.b.String())
	if g.oops(15) && len(out) > 2 {
		i := 1 + g.rng.IntN(len(out)-1)
		if i >= g.untypedAt && i < g.untypedEnd {
			i = g.untypedAt
		}
		switch g.rng.IntN(2) {
		case 0:
			out = out[:i]
		default:
			const damage = "{}[],:\"\\ 0-.e\xff"
			out[i] = damage[g.rng.IntN(len(damage))]
		}
	}
	return out
}

// propEdgeInputs are fixed inputs whose outcome depends on details: where a value is read from
// (JSON text or Content), number edges, and the string forms of colours.
func propEdgeInputs(untyped string) [][]byte {
	var out [][]byte
	add := func(op byte, js string) { out = append(out, append([]byte{op}, js...)) }
	para := func(field, v string) string {
		return `{"kind":"para","text":"","runs":[],"props":{"` + field + `":` + v + `}}`
	}
	mark := func(field, v string) string {
		return `{"kind":"para","text":"","runs":[],"mark":{"` + field + `":` + v + `}}`
	}
	for _, c := range []struct{ field, v string }{
		{"lineSpacing", `{"value":1.0000000596046447753906250000001,"rule":"multiple"}`},
		{"lineSpacing", `{"rule":"multiple","value":1.0000000596046447753906250000001}`},
		{"lineSpacing", `{"value":1e39,"rule":"multiple"}`}, {"lineSpacing", `[0,1.5]`}, {"lineSpacing", `{"rule":{"exactly":{}},"value":3}`},
		{"align", `{"center":{}}`}, {"align", `"Center"`}, {"outlineLevel", `256`}, {"outlineLevel", `-0`}, {"shading", `"+F+F+F"`},
		{"shading", `" #ABCDEF\u2028"`}, {"shading", `"#abcdeg"`}, {"shading", `"ab cdef"`}, {"shading", `"é1234"`}, {"tabs", `[{"pos":1e39}]`},
		{"numbering", `{"num":1}`}, {"numbering", `[1,2]`}, {"numbering", `[1,2,3]`}, {"frame", `{"w":null,"h":1}`},
	} {
		add('p', `{"`+c.field+`":`+c.v+`}`)
		add('b', para(c.field, c.v))
	}
	for _, c := range []struct{ field, v string }{
		{"size", `1e39`}, {"size", `16777217`}, {"size", `-0`}, {"color", `"automatic"`}, {"color", `"AUTOMATIC"`}, {"color", `" 1b1611 "`},
		{"color", `{"Rgb":" 1b1611"}`}, {"color", `{"Rgb":[1,2,3],"x":1}`}, {"color", `{"Auto":{}}`}, {"underline", `{"single":{}}`},
		{"underline", `"wavyDouble"`}, {"highlight", `"gray50"`}, {"ins", `4294967296`}, {"border", `{"style":"nil"}`}, {"border", `[]`},
		{"textEffects", `{"fill":{"type":"none","x":1}}`}, {"textEffects", `{"fill":["solid",[1,2,3]]}`}, {"textEffects", `{"fill":[1,[1,2,3]]}`},
		{"textEffects", `{"fill":{"type":"gradient","stops":[]}}`}, {"ligatures", `[true]`}, {"fmtChange", `{"rev":1,"old":{"bold":true}}`},
		{"fmtChange", `{"old":{}}`}, {"eastAsianLayout", `{"id":-2147483648}`},
	} {
		add('c', `{"`+c.field+`":`+c.v+`}`)
		add('b', mark(c.field, c.v))
	}
	for _, js := range []string{`{}`, `[]`, `{"look":{}}`, `{"look":{"totalRow":true}}`, `{"look":[false]}`, `[null,null,null,null,null,null,null,false]`,
		`{"float":{}}`, `{"float":{"dist":[1,2,3]}}`, `{"bidiVisual":true}`} {
		add('t', js)
	}
	for _, js := range []string{`{}`, `{"exceptions":{}}`, `{"gridBefore":0,"gridAfter":3}`} {
		add('r', js)
	}
	for _, js := range []string{`{}`, `{"partialMargins":[null,1,null,2]}`, `{"textDirectionSet":false,"hideMark":true}`} {
		add('e', js)
	}
	// Documents carry the untyped parts (Raw keeps what it reads, where Rust fills in defaults).
	for _, js := range []string{`"x":1`, `"body":[]`, `"body":[{"kind":"para","text":"","runs":[]}],"media":{}`, `"footnoteSeparators":{}`,
		`"footnoteSeparators":{"separator":[]}`, `"core":{"revision":4294967295}`, `"body":null`, `"core":[]`, `"core":["t"]`} {
		add('d', "{"+js+untyped+"}")
	}
	add('O', `[{"textEffects":{"glow":{}}},{"textEffects":{"shadow":{}}}]`)
	add('O', `[{},{"textEffects":{}}]`)
	add('M', `[{"size":1,"fontEa":"x","textEffects":{}},{"size":1,"fontEa":"x","textEffects":{}}]`)
	add('M', `[{"size":1e39},{"size":1e39}]`)
	add('U', `{"link":"x","style":"Hyperlink"}`)
	add('U', `{"style":"Hyperlink"}`)
	add('E', `{"symFont":" ","font":"Arial"}`)
	add('E', `{"symFont":"\u3000"}`)
	add('E', `{"size":null}`)
	return out
}
