package docx

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// EnvReadRust names the reference program built from testdata/read-oracle (its read-oracle
// binary; write-docx is found beside it), and EnvReadCases how many packages TestReadMatchesRust
// compares (default 3,000).
const (
	EnvReadRust  = "WORDCRAFT_READ_RUST"
	EnvReadCases = "WORDCRAFT_READ_CASES"
)

// TestReadMatchesRust reads random .docx packages with Read and with crates/docx's read, and
// compares the documents byte for byte (as JSON) or the error kinds. A package using something
// Read doesn't port yet is refused (ErrNotPorted) and not compared; the test fails when too many
// are, so it can't pass by refusing.
func TestReadMatchesRust(t *testing.T) {
	bin := os.Getenv(EnvReadRust)
	if bin == "" {
		t.Skipf("set %s to testdata/read-oracle's release binary to compare with Rust", EnvReadRust)
	}
	if !filepath.IsAbs(bin) {
		t.Fatalf("%s must be absolute: %q", EnvReadRust, bin)
	}
	n := 3000
	if v := os.Getenv(EnvReadCases); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("%s=%q: want a positive count", EnvReadCases, v)
		}
	}
	rng := rand.New(rand.NewPCG(11, 29))
	cases := make([][]byte, n)
	for i := range cases {
		cases[i] = randomDocx(rng)
	}
	answers := askRead(t, bin, cases)
	failures, compared, refused, errs := 0, 0, 0, 0
	reasons := map[string]int{}
	for i, c := range cases {
		got := goRead(c)
		if strings.HasPrefix(got, "REFUSED ") {
			refused++
			reasons[got]++
			continue
		}
		compared++
		if strings.HasPrefix(got, "ERR ") {
			errs++
		}
		if got != answers[i] {
			failures++
			if failures <= 3 {
				a, b := answers[i], got
				at := 0
				for at < len(a) && at < len(b) && a[at] == b[at] {
					at++
				}
				from := max(at-400, 0)
				t.Errorf("case %d (%d bytes) differs at byte %d:\nrust: …%.1200s\ngo:   …%.1200s", i, len(c), at, a[from:], b[from:])
				if dir := os.Getenv("WORDCRAFT_READ_DUMP"); dir != "" {
					_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("case%d.docx", i)), c, 0o644)
				}
			}
		}
	}
	t.Logf("%d packages: %d compared (%d errors), %d refused as not ported", n, compared, errs, refused)
	for r, k := range reasons {
		t.Logf("  %5d %s", k, r)
	}
	if failures > 0 {
		t.Fatalf("%d of %d compared packages differ", failures, compared)
	}
	if compared < n*7/10 {
		t.Fatalf("only %d of %d packages compared", compared, n)
	}
}

// TestReadRustWrittenMatchesRust reads what Rust's writer makes of documents: the engine's sample
// and the documents Rust reads from random packages, each written with crates/docx's write, are
// read with Read and with crates/docx's read and compared. These files have the shape Word-like
// writers give them (every part, the built-in styles, settings, the theme).
func TestReadRustWrittenMatchesRust(t *testing.T) {
	bin := os.Getenv(EnvReadRust)
	if bin == "" {
		t.Skipf("set %s to testdata/read-oracle's release binary to compare with Rust", EnvReadRust)
	}
	writer := filepath.Join(filepath.Dir(bin), "write-docx")
	n := 1000
	if v := os.Getenv(EnvReadCases); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			n = max(k/10, 1)
		}
	}
	rng := rand.New(rand.NewPCG(3, 17))
	var docs [][]byte
	if b, err := os.ReadFile("../engine/testdata/sample.json"); err == nil {
		docs = append(docs, b)
	}
	cases := make([][]byte, n)
	for i := range cases {
		cases[i] = randomDocx(rng)
	}
	for _, a := range askRead(t, bin, cases) {
		if !strings.HasPrefix(a, "ERR") && a != "PANIC" {
			docs = append(docs, []byte(unescapeLine(a)))
		}
	}
	written := askRead(t, writer, docs)
	var packages [][]byte
	for i, w := range written {
		if strings.HasPrefix(w, "ERR") || w == "PANIC" {
			t.Logf("document %d: Rust doesn't write it: %.200s", i, w)
			continue
		}
		b := make([]byte, len(w)/2)
		for j := range b {
			v, err := strconv.ParseUint(w[2*j:2*j+2], 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			b[j] = byte(v)
		}
		packages = append(packages, b)
	}
	answers := askRead(t, bin, packages)
	failures, compared := 0, 0
	reasons := map[string]int{}
	for i, c := range packages {
		got := goRead(c)
		if strings.HasPrefix(got, "REFUSED ") {
			reasons[got]++
			continue
		}
		compared++
		if got != answers[i] {
			failures++
			if failures <= 3 {
				a, b := answers[i], got
				at := 0
				for at < len(a) && at < len(b) && a[at] == b[at] {
					at++
				}
				from := max(at-400, 0)
				t.Errorf("written package %d differs at byte %d:\nrust: …%.1200s\ngo:   …%.1200s", i, at, a[from:], b[from:])
			}
		}
	}
	t.Logf("%d documents written by Rust: %d compared", len(packages), compared)
	for r, k := range reasons {
		t.Logf("  %5d %s", k, r)
	}
	if failures > 0 {
		t.Fatalf("%d of %d compared packages differ", failures, compared)
	}
	if compared < len(packages)*7/10 {
		t.Fatalf("only %d of %d packages compared", compared, len(packages))
	}
}

// unescapeLine undoes the reference programs' escaping of a pretty JSON answer.
func unescapeLine(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func askRead(t *testing.T, bin string, cases [][]byte) []string {
	t.Helper()
	var in bytes.Buffer
	for _, c := range cases {
		in.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(c))))
		in.Write(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin, cmd.Env = &in, []string{}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != len(cases) {
		t.Fatalf("reference answered %d of %d packages", len(lines), len(cases))
	}
	return lines
}

var errKindNames = map[ErrKind]string{ErrZip: "Zip", ErrLimit: "Limit", ErrXML: "Xml", ErrMissingPart: "MissingPart", ErrNotWord: "NotWord"}

// goRead is the reference program's answer computed with Read.
func goRead(b []byte) string {
	d, err := Read(b)
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			if e.Kind == ErrNotPorted {
				return "REFUSED " + e.Msg
			}
			return "ERR " + errKindNames[e.Kind]
		}
		return "ERR ? " + err.Error()
	}
	j, err := d.Marshal()
	if err != nil {
		return "SERIALIZE " + err.Error()
	}
	s := strings.ReplaceAll(string(j), `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// ---- the generator ----

const (
	nsW     = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	nsDocs  = nsW + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" xmlns:wpg="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup" xmlns:wpc="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas" xmlns:w10="urn:schemas-microsoft-com:office:word"`
	relBase = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

type docxGen struct {
	rng   *rand.Rand
	b     strings.Builder
	n     int // elements written in the current part
	limit int
	// what the parts define, for references to hit (and sometimes miss)
	styles, nums, rels, hfRels, comments, notes, bookmarks []string
	exotic                                                 int // 1 in exotic: a feature the Go reader refuses
	chain                                                  bool
	sdtContent                                             bool
	images                                                 []string
	oles                                                   []string // relationship ids of OLE embeddings and links
	links                                                  []string // 65 external relationship ids, for an object at the id limit
	txbxDepth                                              int
	sdtStack                                               []bool
}

func (g *docxGen) chance(n int) bool { return g.rng.IntN(n) == 0 }
func (g *docxGen) pick(xs ...string) string {
	return xs[g.rng.IntN(len(xs))]
}

func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\t", "&#9;", "\n", "&#10;", "\r", "&#13;")
	return r.Replace(s)
}

func textEsc(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c == '&':
			b.WriteString("&amp;")
		case c == '<':
			b.WriteString("&lt;")
		case c == '>':
			b.WriteString("&gt;")
		case c < 0x20 && c != '\t' && c != '\n':
			b.WriteString("&#" + strconv.Itoa(int(c)) + ";")
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// attr pairs a name with a value maker; an empty name ends nothing.
type ga struct {
	name string
	val  func() string
}

func (g *docxGen) open(name string, attrs ...ga) {
	g.n++
	g.b.WriteString("<" + name)
	for _, a := range attrs {
		if g.chance(3) {
			continue
		}
		g.b.WriteString(" " + a.name + `="` + xmlEsc(a.val()) + `"`)
	}
	g.b.WriteString(">")
}

func (g *docxGen) close(name string) { g.b.WriteString("</" + name + ">") }

func (g *docxGen) leaf(name string, attrs ...ga) {
	g.open(name, attrs...)
	g.close(name)
}

// always is an attribute written every time.
func (g *docxGen) attrAlways(name, val string) {
	g.b.WriteString(" " + name + `="` + xmlEsc(val) + `"`)
}

func (g *docxGen) room() bool { return g.n < g.limit }

// value pools

var (
	numPool = []string{"0", "1", "2", "3", "8", "9", "10", "12", "20", "24", "36", "63", "64", "100", "240", "276", "360", "480", "720", "1440", "2880", "11906", "16838",
		"-1", "-240", "-720", "1.5", "12.5", "2.5pt", "1in", "2cm", "3mm", "1pc", "1pi", "4px", "12700emu", "13.90pt", " 12 ", "+5", "1e3", "1e400", "-1e400",
		"nan", "NaN", "inf", "99999999999999", "4294967296", "-0", "0x10", "abc", "", "%", "50%", "100%", "5000", "1E2", "12 pt", "12pt ", "٣", "1.2.3",
		"--1", ".5", "5.", "e5", "+", "-", "32767", "32768", "158400", "200000", "2147483648", "-2147483649", "1584", "1585", "0.06", "0.001"}
	hexPool   = []string{"FF", "ff", "A0", "+F", "04A0", "0020", "0060", "05E0", "FFFF", "1FFFF", "-1", "zz", "00", "80", "40", "C0", "BF", "00000000", "7FFFFFFE", "7FFFFFFF", "1A2B3C4D", "123456789", "a1b2c3d4", " 3F ", "263A", "F0B7", "1F600", "D800", "110000", "1F", "20"}
	colorPool = []string{"FF0000", "00ff00", "auto", "Auto", "AUTO", "#123456", "##123456", " 0000FF ", "12345", "1234567", "GGGGGG", "+F+F+F", "000000", "FFFFFF", "1F3864", "automatic"}
	onOffPool = []string{"1", "0", "true", "false", "on", "off", "False", "FALSE", " 0 ", "yes", "x", "TRUE"}
	themePool = []string{"dark1", "light1", "dark2", "light2", "text1", "background1", "text2", "background2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hyperlink", "followedHyperlink", "bogus", "none"}
	fontPool  = []string{"Arial", "Times New Roman", "Calibri", "MS Mincho", "SimSun", "Arial Unicode MS", "", " ", "Symbol", "Wingdings", strings.Repeat("F", 300)}
	textPool  = []string{"Hello", "world", " spaced ", "tab\there", "line\nbreak", "ünïcødé", "中文字", "עברית", "🎉", "\uFFFC", "a&b<c>", "", "   ", "x", "The quick brown fox", "○", "字", "12", "\u00a0nbsp", "ctrl\x01\x1fchars", "\x0b"}
	instrPool = []string{"PAGE", " PAGE ", "NUMPAGES", " TOC \\o \"1-3\" \\h \\z \\u ", "TOC", "toc \\f", "ADDIN ZOTERO_ITEM CSL_CITATION {}", "addin x", "ADDI",
		" eq \\o\\ac(○,字)", "EQ \\o\\ac(□;\\s\\up 1(12))", "eq \\o(a,b)", "eq \\o\\ac(◯,1)", "eq \\o\\ac(字,△)", "HYPERLINK \"http://x\"", "", "  ", "MERGEFIELD Name",
		"REF _Ref1 \\h", "SEQ Figure", "FORMTEXT", " eq \\o\\ac(○,字) \\* enlarge", "eq \\o\\al(=,/)"}
)

func (g *docxGen) from(pool []string) func() string { return func() string { return g.pick(pool...) } }

// num is a measure: usually from the pool, sometimes random.
func (g *docxGen) num() func() string {
	return func() string {
		if g.chance(3) {
			return strconv.Itoa(g.rng.IntN(4000) - 500)
		}
		return g.pick(numPool...)
	}
}

func (g *docxGen) enum(vals ...string) func() string {
	return func() string {
		if g.chance(8) {
			return g.pick(numPool...)
		}
		return g.pick(vals...)
	}
}

func (g *docxGen) styleRef() func() string {
	return func() string {
		if len(g.styles) == 0 || g.chance(6) {
			return g.pick("Normal", "Heading1", "missing", "", "TOCHeading", "TOC1", "Hyperlink")
		}
		return g.pick(g.styles...)
	}
}

func (g *docxGen) idRef(pool *[]string, extra ...string) func() string {
	return func() string {
		if len(*pool) == 0 || g.chance(6) {
			return g.pick(append(extra, "999", "", "x")...)
		}
		return g.pick(*pool...)
	}
}

func (g *docxGen) onOff() []ga { return []ga{{"w:val", g.from(onOffPool)}} }

func (g *docxGen) author() []ga {
	return []ga{{"w:id", g.num()}, {"w:author", g.enum("Ann", "Bob", "", strings.Repeat("A", 300))}, {"w:date", g.enum("2024-01-01T00:00:00Z", "2025-05-05T10:00:00Z", "")}}
}

var onOffNames = []string{"w:b", "w:bCs", "w:i", "w:iCs", "w:cs", "w:caps", "w:smallCaps", "w:strike", "w:dstrike", "w:outline", "w:shadow", "w:emboss", "w:imprint", "w:noProof", "w:vanish", "w:rtl", "w:webHidden", "w:specVanish"}

func (g *docxGen) border(name string) {
	g.leaf(name, ga{"w:val", g.enum("single", "double", "dotted", "dashed", "thick", "nil", "none", "wave", "triple", "thinThickSmallGap", "bogus")},
		ga{"w:sz", g.num()}, ga{"w:space", g.num()}, ga{"w:color", g.from(colorPool)}, ga{"w:themeColor", g.from(themePool)})
}

func (g *docxGen) borders(name string, sides ...string) {
	g.open(name)
	for range g.rng.IntN(4) {
		g.border(g.pick(sides...))
	}
	g.close(name)
}

func (g *docxGen) shd() {
	g.leaf("w:shd", ga{"w:val", g.enum("clear", "solid", "pct10", "nil")}, ga{"w:fill", g.from(colorPool)}, ga{"w:color", g.from(colorPool)},
		ga{"w:themeFill", g.from(themePool)}, ga{"w:themeFillTint", g.from(hexPool)}, ga{"w:themeFillShade", g.from(hexPool)},
		ga{"w:themeColor", g.from(themePool)}, ga{"w:themeTint", g.from(hexPool)}, ga{"w:themeShade", g.from(hexPool)})
}

// rPr writes a w:rPr; mark adds what a paragraph mark's may hold.
func (g *docxGen) rPr(mark, change bool) {
	g.open("w:rPr")
	for range g.rng.IntN(7) {
		switch g.rng.IntN(24) {
		case 0:
			g.leaf("w:rStyle", ga{"w:val", g.styleRef()})
		case 1:
			g.leaf("w:rFonts", ga{"w:ascii", g.from(fontPool)}, ga{"w:hAnsi", g.from(fontPool)}, ga{"w:eastAsia", g.from(fontPool)}, ga{"w:cs", g.from(fontPool)},
				ga{"w:asciiTheme", g.enum("minorHAnsi", "majorHAnsi", "minorBidi", "majorBidi", "minorEastAsia", "bogus")},
				ga{"w:hAnsiTheme", g.enum("minorHAnsi", "majorHAnsi", "minorBidi")}, ga{"w:hint", g.enum("eastAsia", "cs", "default")})
		case 2, 3, 4:
			g.leaf(g.pick(onOffNames...), g.onOff()...)
		case 5:
			g.leaf("w:color", ga{"w:val", g.from(colorPool)}, ga{"w:themeColor", g.from(themePool)}, ga{"w:themeTint", g.from(hexPool)}, ga{"w:themeShade", g.from(hexPool)})
		case 6:
			g.leaf(g.pick("w:spacing", "w:w", "w:kern", "w:position"), ga{"w:val", g.num()})
		case 7, 8:
			g.leaf(g.pick("w:sz", "w:szCs"), ga{"w:val", g.num()})
		case 9:
			g.leaf("w:highlight", ga{"w:val", g.enum("yellow", "green", "cyan", "none", "darkBlue", "Yellow")})
		case 10:
			g.leaf("w:u", ga{"w:val", g.enum("single", "double", "wave", "none", "dotDash", "words", "thick")}, ga{"w:color", g.from(colorPool)},
				ga{"w:themeColor", g.from(themePool)}, ga{"w:themeTint", g.from(hexPool)})
		case 11:
			g.border("w:bdr")
		case 12:
			g.shd()
		case 13:
			g.leaf("w:vertAlign", ga{"w:val", g.enum("superscript", "subscript", "baseline")})
		case 14:
			g.leaf("w:eastAsianLayout", ga{"w:id", g.num()}, ga{"w:combine", g.from(onOffPool)}, ga{"w:combineBrackets", g.enum("round", "square", "angle", "curly", "none")},
				ga{"w:vert", g.from(onOffPool)}, ga{"w:vertCompress", g.from(onOffPool)})
		case 15:
			g.leaf("w:lang", ga{"w:val", g.enum("en-US", "fr-FR", "", strings.Repeat("x", 70))}, ga{"w:bidi", g.enum("ar-SA", "he-IL", "")}, ga{"w:eastAsia", g.enum("ja-JP")})
		case 16:
			g.leaf("w14:ligatures", ga{"w14:val", g.enum("none", "standard", "all", "standardContextual", "historical")})
		case 17:
			if change && g.chance(2) {
				g.open("w:rPrChange", g.author()...)
				g.rPr(false, false)
				g.close("w:rPrChange")
			}
		case 18:
			if mark {
				g.leaf(g.pick("w:ins", "w:del", "w:moveTo", "w:moveFrom"), g.author()...)
			}
		case 19:
			g.textEffect()
		case 20:
			g.leaf("w:effect", ga{"w:val", g.enum("blinkBackground")})
		default:
			g.leaf(g.pick(onOffNames...), g.onOff()...)
		}
	}
	g.close("w:rPr")
}

func (g *docxGen) pPr(inStyle bool) {
	g.open("w:pPr")
	for range g.rng.IntN(7) {
		switch g.rng.IntN(22) {
		case 0:
			if !inStyle {
				g.leaf("w:pStyle", ga{"w:val", g.styleRef()})
			}
		case 1, 2:
			g.leaf(g.pick("w:keepNext", "w:keepLines", "w:pageBreakBefore", "w:widowControl", "w:contextualSpacing", "w:mirrorIndents", "w:suppressAutoHyphens",
				"w:suppressLineNumbers", "w:bidi", "w:kinsoku", "w:wordWrap", "w:overflowPunct", "w:topLinePunct", "w:autoSpaceDE", "w:autoSpaceDN", "w:snapToGrid"), g.onOff()...)
		case 3:
			g.leaf("w:framePr", ga{"w:dropCap", g.enum("drop", "margin", "none")}, ga{"w:lines", g.num()}, ga{"w:hSpace", g.num()}, ga{"w:vSpace", g.num()},
				ga{"w:w", g.num()}, ga{"w:h", g.num()}, ga{"w:hRule", g.enum("exact", "atLeast", "auto")}, ga{"w:x", g.num()}, ga{"w:y", g.num()},
				ga{"w:xAlign", g.enum("left", "center", "right", "inside", "outside", "top")}, ga{"w:yAlign", g.enum("top", "center", "bottom", "inside", "inline")},
				ga{"w:hAnchor", g.enum("text", "margin", "page")}, ga{"w:vAnchor", g.enum("text", "margin", "page")},
				ga{"w:wrap", g.enum("around", "auto", "notBeside", "none", "tight", "through")})
		case 4, 5:
			g.open("w:numPr")
			if g.chance(4) {
				g.leaf("w:ilvl", ga{"w:val", g.num()})
			} else if !g.chance(4) {
				g.leaf("w:ilvl", ga{"w:val", g.enum("0", "1", "2", "8", "9")})
			}
			if !g.chance(3) {
				g.leaf("w:numId", ga{"w:val", g.idRef(&g.nums, "0")})
			}
			if g.chance(4) {
				g.leaf("w:numberingChange", append(g.author(), ga{"w:original", g.enum("%1.", "1.", "")})...)
			}
			g.close("w:numPr")
		case 6:
			g.borders("w:pBdr", "w:top", "w:left", "w:start", "w:bottom", "w:right", "w:end", "w:between", "w:bar", "w:insideH")
		case 7:
			g.shd()
		case 8:
			g.open("w:tabs")
			for range g.rng.IntN(4) {
				g.leaf("w:tab", ga{"w:val", g.enum("left", "center", "right", "end", "decimal", "bar", "clear", "num", "start")}, ga{"w:pos", g.num()},
					ga{"w:leader", g.enum("dot", "hyphen", "underscore", "heavy", "middleDot", "none")})
			}
			g.close("w:tabs")
		case 9, 10:
			g.leaf("w:spacing", ga{"w:before", g.num()}, ga{"w:after", g.num()}, ga{"w:beforeAutospacing", g.from(onOffPool)}, ga{"w:afterAutospacing", g.from(onOffPool)},
				ga{"w:line", g.num()}, ga{"w:lineRule", g.enum("auto", "exact", "atLeast", "atleast", "AUTO", "Exact")})
		case 11, 12:
			g.leaf("w:ind", ga{"w:left", g.num()}, ga{"w:start", g.num()}, ga{"w:right", g.num()}, ga{"w:end", g.num()}, ga{"w:hanging", g.num()}, ga{"w:firstLine", g.num()},
				ga{"w:leftChars", g.num()}, ga{"w:startChars", g.num()}, ga{"w:rightChars", g.num()}, ga{"w:endChars", g.num()}, ga{"w:hangingChars", g.num()},
				ga{"w:firstLineChars", g.num()})
		case 13:
			g.leaf("w:jc", ga{"w:val", g.enum("left", "center", "right", "end", "both", "justify", "distribute", "thaiDistribute", "lowKashida", "start")})
		case 14:
			g.leaf("w:outlineLvl", ga{"w:val", g.num()})
		case 15, 16:
			if !inStyle {
				g.rPr(true, true)
			}
		case 17:
			if !inStyle && g.chance(2) {
				g.open("w:pPrChange", g.author()...)
				g.pPr(true)
				g.close("w:pPrChange")
			}
		case 18:
			if !inStyle && g.chance(3) {
				g.sectPr()
			}
		default:
			g.leaf(g.pick("w:textAlignment", "w:textboxTightWrap", "w:divId"), ga{"w:val", g.num()})
		}
	}
	g.close("w:pPr")
}

func (g *docxGen) sectPr() {
	g.open("w:sectPr")
	for range g.rng.IntN(8) {
		switch g.rng.IntN(18) {
		case 0, 1:
			g.leaf(g.pick("w:headerReference", "w:footerReference"), ga{"w:type", g.enum("default", "first", "even")}, ga{"r:id", g.idRef(&g.hfRels)})
		case 2:
			g.leaf("w:type", ga{"w:val", g.enum("continuous", "evenPage", "oddPage", "nextColumn", "nextPage")})
		case 3:
			if g.chance(3) {
				v := g.num()()
				g.b.WriteString(`<w:pgSz w:w="` + xmlEsc(v) + `" w:h="` + xmlEsc(v) + `"/>`)
			} else {
				g.leaf("w:pgSz", ga{"w:w", g.num()}, ga{"w:h", g.num()}, ga{"w:orient", g.enum("landscape", "portrait")})
			}
		case 4:
			g.leaf("w:pgMar", ga{"w:top", g.num()}, ga{"w:bottom", g.num()}, ga{"w:left", g.num()}, ga{"w:right", g.num()}, ga{"w:header", g.num()},
				ga{"w:footer", g.num()}, ga{"w:gutter", g.num()})
		case 5:
			g.open("w:cols", ga{"w:num", g.num()}, ga{"w:space", g.num()}, ga{"w:sep", g.from(onOffPool)}, ga{"w:equalWidth", g.from(onOffPool)})
			for range g.rng.IntN(4) {
				g.leaf("w:col", ga{"w:w", g.num()}, ga{"w:space", g.num()})
			}
			g.close("w:cols")
		case 6, 7:
			name := g.pick("w:footnotePr", "w:endnotePr")
			g.notePr(name)
		case 8:
			g.leaf(g.pick("w:formProt", "w:titlePg", "w:bidi", "w:rtlGutter"), g.onOff()...)
		case 9:
			g.leaf("w:textDirection", ga{"w:val", g.enum("lrTb", "tbRl", "btLr", "lrTbV", "tbRlV", "tbLrV", "tb", "rl")})
		case 10:
			g.leaf("w:pgNumType", ga{"w:start", g.num()}, ga{"w:fmt", g.enum("decimal", "lowerRoman", "upperLetter", "japaneseCounting", "chicago", "bogus")})
		case 11:
			g.leaf("w:lnNumType", ga{"w:countBy", g.num()}, ga{"w:start", g.num()}, ga{"w:distance", g.num()}, ga{"w:restart", g.enum("newPage", "newSection", "continuous")})
		case 12:
			g.leaf("w:vAlign", ga{"w:val", g.enum("top", "center", "both", "bottom")})
		case 13:
			g.borders("w:pgBorders", "w:top", "w:left", "w:bottom", "w:right")
		case 14:
			g.leaf("w:docGrid", ga{"w:type", g.enum("default", "lines", "linesAndChars", "snapToChars")}, ga{"w:linePitch", g.num()}, ga{"w:charSpace", g.num()})
		case 15:
			if g.chance(2) {
				g.open("w:sectPrChange", g.author()...)
				if g.chance(3) {
					g.open("w:sectPr")
					g.leaf("w:pgSz", ga{"w:w", g.num()}, ga{"w:h", g.num()})
					g.close("w:sectPr")
				}
				g.close("w:sectPrChange")
			}
		default:
			g.leaf("w:printerSettings", ga{"r:id", g.idRef(&g.rels)})
		}
	}
	g.close("w:sectPr")
}

func (g *docxGen) notePr(name string) {
	g.open(name)
	if g.chance(2) {
		g.leaf("w:pos", ga{"w:val", g.enum("pageBottom", "beneathText", "sectEnd", "docEnd")})
	}
	if g.chance(2) {
		g.leaf("w:numFmt", ga{"w:val", g.enum("decimal", "lowerRoman", "upperRoman", "chicago", "bogus")})
	}
	if g.chance(2) {
		g.leaf("w:numStart", ga{"w:val", g.num()})
	}
	if g.chance(2) {
		g.leaf("w:numRestart", ga{"w:val", g.enum("continuous", "eachSect", "eachPage")})
	}
	g.close(name)
}

func (g *docxGen) text(name string) {
	g.b.WriteString("<" + name)
	if g.chance(2) {
		g.b.WriteString(` xml:space="preserve"`)
	}
	g.b.WriteString(">" + textEsc(g.pick(textPool...)) + "</" + name + ">")
	g.n++
}

// run writes a w:r.
func (g *docxGen) run() {
	g.open("w:r", ga{"w:rsidR", g.from(hexPool)})
	if g.chance(2) {
		g.rPr(false, true)
	}
	for range 1 + g.rng.IntN(4) {
		switch g.rng.IntN(30) {
		case 0, 1, 2, 3, 4, 5, 6:
			g.text("w:t")
		case 7:
			g.text("w:delText")
		case 8, 9:
			g.b.WriteString("<w:instrText>" + textEsc(g.pick(instrPool...)) + "</w:instrText>")
		case 10:
			g.leaf("w:tab")
		case 11:
			g.leaf("w:ptab", ga{"w:alignment", g.enum("left", "center", "right")}, ga{"w:relativeTo", g.enum("margin", "indent")},
				ga{"w:leader", g.enum("none", "dot", "hyphen", "underscore", "middleDot")})
		case 12:
			g.leaf("w:br", ga{"w:type", g.enum("page", "column", "textWrapping")}, ga{"w:clear", g.enum("all")})
		case 13:
			g.leaf(g.pick("w:cr", "w:noBreakHyphen", "w:softHyphen", "w:lastRenderedPageBreak", "w:pgNum"))
		case 14:
			g.leaf("w:sym", ga{"w:font", g.from(fontPool)}, ga{"w:char", g.from(hexPool)})
		case 15, 16, 17:
			g.fldChar()
		case 18:
			g.leaf(g.pick("w:footnoteReference", "w:endnoteReference"), ga{"w:id", g.idRef(&g.notes, "-1", "0")}, ga{"w:customMarkFollows", g.from(onOffPool)})
		case 19:
			g.leaf(g.pick("w:footnoteRef", "w:endnoteRef", "w:separator", "w:continuationSeparator"))
		case 20:
			g.leaf("w:commentReference", ga{"w:id", g.idRef(&g.comments)})
		case 21:
			g.ruby()
		case 22:
			g.open("mc:AlternateContent")
			g.open("mc:Choice", ga{"Requires", g.enum("w14", "wps", "w15 w16", "xyz", "", "v m")})
			g.text("w:t")
			g.close("mc:Choice")
			g.open("mc:Fallback")
			g.text("w:t")
			g.close("mc:Fallback")
			g.close("mc:AlternateContent")
		case 23:
			g.drawing()
		case 24:
			if g.chance(3) {
				g.pict()
			} else if g.chance(2) {
				g.object()
			}
		default:
			g.text("w:t")
		}
	}
	g.close("w:r")
}

func (g *docxGen) fldChar() {
	g.open("w:fldChar", ga{"w:fldCharType", g.enum("begin", "separate", "end", "begin", "end")}, ga{"w:fldLock", g.from(onOffPool)}, ga{"w:dirty", g.from(onOffPool)})
	if g.chance(6) {
		g.b.WriteString(`<w:ffData><w:name w:val="Text1"/><w:enabled/></w:ffData>`)
	}
	g.close("w:fldChar")
}

func (g *docxGen) ruby() {
	g.open("w:ruby")
	if g.chance(4) {
		g.open("w:rubyPr")
		g.leaf("w:rubyAlign", ga{"w:val", g.enum("center", "distributeLetter", "left", "right", "rightVertical")})
		g.leaf("w:hps", ga{"w:val", g.num()})
		g.leaf("w:hpsRaise", ga{"w:val", g.num()})
		g.leaf("w:hpsBaseText", ga{"w:val", g.num()})
		g.leaf("w:lid", ga{"w:val", g.enum("ja-JP", "zh-CN", strings.Repeat("l", 40))})
		g.close("w:rubyPr")
	}
	for _, part := range []string{"w:rt", "w:rubyBase"} {
		if g.chance(5) {
			continue
		}
		g.open(part)
		for range g.rng.IntN(3) {
			g.open("w:r")
			if g.chance(2) {
				g.rPr(false, false)
			}
			g.text(g.pick("w:t", "w:t", "w:delText", "w:instrText"))
			g.close("w:r")
		}
		g.close(part)
	}
	g.close("w:ruby")
}

func (g *docxGen) marker() {
	switch g.rng.IntN(6) {
	case 0:
		g.leaf("w:bookmarkStart", ga{"w:id", g.idRef(&g.bookmarks, "0", "1")}, ga{"w:name", g.enum("_Toc1", "bm", " bm ", "", strings.Repeat("b", 300), "_GoBack")})
	case 1:
		g.leaf("w:bookmarkEnd", ga{"w:id", g.idRef(&g.bookmarks, "0", "1")}, ga{"w:name", g.enum("bm", "")})
	case 2:
		g.leaf(g.pick("w:commentRangeStart", "w:commentRangeEnd"), ga{"w:id", g.idRef(&g.comments)})
	case 3:
		g.leaf("w:permStart", ga{"w:id", g.enum("0", "1", "2", " 1 ")}, ga{"w:edGrp", g.enum("everyone", "", strings.Repeat("e", 300))}, ga{"w:ed", g.enum("a@b.c", "")})
	case 4:
		g.leaf("w:permEnd", ga{"w:id", g.enum("0", "1", "2", "9")})
	default:
		g.leaf(g.pick("w:moveFromRangeStart", "w:moveFromRangeEnd", "w:moveToRangeStart", "w:moveToRangeEnd"), ga{"w:id", g.enum("1", "2", "3")},
			ga{"w:name", g.enum("move1", "move2", "")}, ga{"w:author", g.enum("Ann")})
	}
}

// inline writes paragraph content.
func (g *docxGen) inline(depth int) {
	for range g.rng.IntN(6) {
		if !g.room() {
			return
		}
		switch g.rng.IntN(22) {
		case 0, 1, 2, 3, 4, 5, 6, 7:
			g.run()
		case 8:
			if depth < 3 {
				g.open("w:hyperlink", ga{"r:id", g.idRef(&g.rels)}, ga{"w:anchor", g.enum("_Toc1", "bm", "")}, ga{"w:history", g.enum("1")})
				g.inline(depth + 1)
				g.close("w:hyperlink")
			}
		case 9, 10:
			if depth < 3 {
				name := g.pick("w:ins", "w:del", "w:moveTo", "w:moveFrom")
				g.open(name, g.author()...)
				g.inline(depth + 1)
				g.close(name)
			}
		case 11:
			if depth < 3 {
				g.open("w:fldSimple", ga{"w:instr", g.from(instrPool)}, ga{"w:fldLock", g.from(onOffPool)}, ga{"w:dirty", g.from(onOffPool)})
				g.inline(depth + 1)
				g.close("w:fldSimple")
			}
		case 12, 13, 14:
			g.marker()
		case 15:
			if depth < 3 {
				name := g.pick("w:smartTag", "w:customXml", "w:dir", "w:bdo")
				g.open(name, ga{"w:val", g.enum("rtl", "ltr")})
				g.inline(depth + 1)
				g.close(name)
			}
		case 16:
			g.leaf(g.pick("w:br", "w:cr"), ga{"w:type", g.enum("page", "column")})
		case 17:
			if g.chance(g.exotic) {
				g.b.WriteString(`<m:oMath><m:r><m:t>x</m:t></m:r></m:oMath>`)
			}
		case 18:
			if depth < 3 {
				g.sdtOpen()
				g.inline(depth + 1)
				g.sdtClose()
			}
		case 19:
			g.leaf("w:proofErr", ga{"w:type", g.enum("spellStart", "spellEnd")})
		default:
			g.run()
		}
	}
}

func (g *docxGen) para(depth int) {
	g.open("w:p", ga{"w14:paraId", g.from(hexPool)}, ga{"w:rsidR", g.from(hexPool)})
	if g.chance(2) {
		g.pPr(false)
	}
	g.inline(depth)
	g.close("w:p")
}

func (g *docxGen) tblPr(name string) {
	g.open(name)
	for range g.rng.IntN(6) {
		switch g.rng.IntN(14) {
		case 0:
			g.leaf("w:tblStyle", ga{"w:val", g.styleRef()})
		case 1:
			g.leaf("w:tblW", ga{"w:type", g.enum("dxa", "pct", "auto", "nil")}, ga{"w:w", g.enum("5000", "100%", "50%", "2500", "0", "abc", "7200")})
		case 2:
			g.leaf("w:jc", ga{"w:val", g.enum("center", "right", "left", "end")})
		case 3:
			g.leaf("w:tblInd", ga{"w:w", g.num()}, ga{"w:type", g.enum("dxa")})
		case 4:
			g.borders("w:tblBorders", "w:top", "w:left", "w:start", "w:bottom", "w:right", "w:end", "w:insideH", "w:insideV")
		case 5:
			g.shd()
		case 6:
			g.leaf("w:tblLayout", ga{"w:type", g.enum("fixed", "autofit")})
		case 7:
			g.leaf("w:bidiVisual", g.onOff()...)
		case 8:
			g.margins("w:tblCellMar")
		case 9:
			g.leaf("w:tblLook", ga{"w:val", g.from(hexPool)}, ga{"w:firstRow", g.from(onOffPool)}, ga{"w:lastRow", g.from(onOffPool)},
				ga{"w:firstColumn", g.from(onOffPool)}, ga{"w:lastColumn", g.from(onOffPool)}, ga{"w:noHBand", g.from(onOffPool)}, ga{"w:noVBand", g.from(onOffPool)})
		case 10:
			g.leaf("w:tblpPr", ga{"w:leftFromText", g.num()}, ga{"w:rightFromText", g.num()}, ga{"w:topFromText", g.num()}, ga{"w:bottomFromText", g.num()},
				ga{"w:vertAnchor", g.enum("text", "margin", "page")}, ga{"w:horzAnchor", g.enum("text", "margin", "page")},
				ga{"w:tblpXSpec", g.enum("left", "center", "right", "inside", "outside", "top")}, ga{"w:tblpYSpec", g.enum("top", "center", "bottom", "inline")},
				ga{"w:tblpX", g.num()}, ga{"w:tblpY", g.num()})
		case 11:
			g.leaf("w:tblOverlap", ga{"w:val", g.enum("never", "overlap")})
			if g.chance(2) {
				g.leaf("w:tblStyleRowBandSize", ga{"w:val", g.num()})
			}
		case 12:
			g.leaf(g.pick("w:tblCaption", "w:tblDescription"), ga{"w:val", g.enum("Caption", "", strings.Repeat("c", 300), "a\tb")})
		default:
			if name == "w:tblPr" && g.chance(3) {
				g.open("w:tblPrChange", g.author()...)
				g.tblPr("w:tblPr")
				g.close("w:tblPrChange")
			}
		}
	}
	g.close(name)
}

func (g *docxGen) margins(name string) {
	g.open(name)
	for range g.rng.IntN(5) {
		g.leaf(g.pick("w:top", "w:left", "w:start", "w:bottom", "w:right", "w:end"), ga{"w:w", g.num()}, ga{"w:type", g.enum("dxa", "nil")})
	}
	g.close(name)
}

func (g *docxGen) trPr() {
	g.open("w:trPr")
	for range g.rng.IntN(4) {
		switch g.rng.IntN(7) {
		case 0:
			g.leaf("w:trHeight", ga{"w:val", g.num()}, ga{"w:hRule", g.enum("exact", "atLeast", "auto", "EXACT", "Auto")})
		case 1:
			g.leaf(g.pick("w:tblHeader", "w:cantSplit"), g.onOff()...)
		case 2:
			g.leaf(g.pick("w:gridBefore", "w:gridAfter"), ga{"w:val", g.num()})
		case 3:
			g.leaf(g.pick("w:wBefore", "w:wAfter"), ga{"w:w", g.num()}, ga{"w:type", g.enum("dxa", "pct", "auto")})
		case 4:
			if g.chance(2) {
				g.open("w:trPrChange", g.author()...)
				g.trPr()
				g.close("w:trPrChange")
			}
		default:
			g.leaf("w:jc", ga{"w:val", g.enum("center")})
		}
	}
	g.close("w:trPr")
}

func (g *docxGen) tcPr(change bool) {
	g.open("w:tcPr")
	for range g.rng.IntN(5) {
		switch g.rng.IntN(13) {
		case 0:
			g.leaf("w:tcW", ga{"w:type", g.enum("dxa", "pct", "auto", "nil")}, ga{"w:w", g.enum("2000", "50%", "2500", "0", "-5", "abc")})
		case 1, 2:
			g.leaf("w:gridSpan", ga{"w:val", g.num()})
		case 3:
			g.leaf("w:hMerge", ga{"w:val", g.enum("restart", "continue")})
		case 4:
			g.leaf("w:vMerge", ga{"w:val", g.enum("restart", "continue")})
		case 5:
			g.borders("w:tcBorders", "w:top", "w:left", "w:bottom", "w:right", "w:insideH", "w:tl2br")
		case 6:
			g.shd()
		case 7:
			g.leaf(g.pick("w:noWrap", "w:hideMark", "w:tcFitText"), g.onOff()...)
		case 8:
			g.margins("w:tcMar")
		case 9:
			g.leaf("w:textDirection", ga{"w:val", g.enum("lrTb", "tbRl", "btLr", "tbRlV")})
		case 10:
			g.leaf("w:vAlign", ga{"w:val", g.enum("top", "center", "bottom", "both")})
		case 11:
			if change && g.chance(2) {
				g.open("w:tcPrChange", g.author()...)
				g.tcPr(false)
				g.close("w:tcPrChange")
			}
		}
	}
	g.close("w:tcPr")
}

func (g *docxGen) table(depth int) {
	g.open("w:tbl")
	if g.chance(4) == false {
		g.tblPr("w:tblPr")
	}
	if !g.chance(4) {
		g.open("w:tblGrid")
		for range g.rng.IntN(5) {
			g.leaf("w:gridCol", ga{"w:w", g.num()})
		}
		g.close("w:tblGrid")
	}
	for range g.rng.IntN(4) {
		wrap := g.chance(8)
		if wrap {
			g.open("w:customXml", ga{"w:element", g.enum("row")})
		}
		if g.chance(10) {
			g.sdtOpen()
			g.sdtClose()
		}
		rowSdt := g.chance(8)
		if rowSdt {
			g.sdtOpen()
		}
		g.open("w:tr")
		if g.chance(2) {
			g.trPr()
		}
		if g.chance(6) {
			g.tblPr("w:tblPrEx")
		}
		for range g.rng.IntN(4) {
			cellSdt := g.chance(8)
			if cellSdt {
				g.sdtOpen()
			}
			g.open("w:tc")
			if g.chance(2) {
				g.tcPr(true)
			}
			g.blocks(depth+1, 1+g.rng.IntN(2))
			g.close("w:tc")
			if cellSdt {
				g.sdtClose()
			}
		}
		g.close("w:tr")
		if rowSdt {
			g.sdtClose()
		}
		if wrap {
			g.close("w:customXml")
		}
	}
	g.close("w:tbl")
}

// blocks writes up to n blocks.
func (g *docxGen) blocks(depth, n int) {
	for range n {
		if !g.room() {
			g.leaf("w:p")
			return
		}
		switch g.rng.IntN(16) {
		case 0, 1:
			if depth < 3 {
				g.table(depth)
			} else {
				g.para(depth)
			}
		case 2:
			g.marker()
		case 3:
			if depth < 3 {
				name := g.pick("w:customXml", "w:smartTag", "w:ins", "w:moveTo")
				g.open(name, g.author()...)
				g.blocks(depth+1, 2)
				g.close(name)
			}
		case 4:
			if depth < 3 {
				g.open("mc:AlternateContent")
				g.open("mc:Choice", ga{"Requires", g.enum("w14", "wps", "xyz", "")})
				g.blocks(depth+1, 1)
				g.close("mc:Choice")
				g.open("mc:Fallback")
				g.blocks(depth+1, 1)
				g.close("mc:Fallback")
				g.close("mc:AlternateContent")
			}
		case 5:
			if depth < 2 && g.chance(3) {
				g.sdtOpen()
				g.sdtOpen()
				g.table(depth + 1)
				g.sdtClose()
				g.para(depth + 1)
				g.sdtClose()
			} else if depth < 3 {
				g.sdtOpen()
				if !g.chance(8) {
					g.blocks(depth+1, 1+g.rng.IntN(3))
				}
				g.sdtClose()
			}
		case 7:
			g.special()
		case 6:
			// A drop cap paragraph followed by its text.
			g.b.WriteString(`<w:p><w:pPr><w:framePr w:dropCap="drop" w:lines="3" w:hSpace="72"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="` + g.pick("Arial", "Times") + `"/></w:rPr><w:t>` + g.pick("D", "Do", strings.Repeat("x", 20)) + `</w:t></w:r></w:p>`)
			g.para(depth)
		default:
			g.para(depth)
		}
	}
}

// drawing writes a w:drawing: usually a picture, inline or anchored, with random frame, anchoring,
// wrapping, rotation, crop and alternative text; sometimes a kind not ported yet.
func (g *docxGen) drawing() {
	g.b.WriteString(`<w:drawing>`)
	anchored := g.chance(2)
	if anchored {
		g.open("wp:anchor", ga{"distT", g.num()}, ga{"distB", g.num()}, ga{"distL", g.num()}, ga{"distR", g.num()}, ga{"behindDoc", g.from(onOffPool)},
			ga{"layoutInCell", g.from(onOffPool)}, ga{"simplePos", g.enum("0")}, ga{"relativeHeight", g.num()})
		for _, side := range []string{"H", "V"} {
			if g.chance(4) {
				continue
			}
			alt := g.chance(6)
			if alt {
				g.b.WriteString(`<mc:AlternateContent><mc:Choice Requires="wp14"><wp:position` + side + ` relativeFrom="page"><wp14:pctPosVOffset>5000</wp14:pctPosVOffset></wp:position` + side + `></mc:Choice><mc:Fallback>`)
			}
			g.open("wp:position"+side, ga{"relativeFrom", g.enum("page", "margin", "leftMargin", "rightMargin", "insideMargin", "outsideMargin", "character", "topMargin", "bottomMargin", "line", "paragraph", "column", "bogus")})
			if g.chance(2) {
				g.b.WriteString(`<wp:align>` + g.pick("left", " center ", "right", "inside", "outside", "top", "bottom", "x") + `</wp:align>`)
			} else {
				g.b.WriteString(`<wp:posOffset>` + textEsc(g.num()()) + `</wp:posOffset>`)
			}
			g.close("wp:position" + side)
			if alt {
				g.b.WriteString(`</mc:Fallback></mc:AlternateContent>`)
			}
		}
	} else {
		g.open("wp:inline", ga{"distT", g.num()})
	}
	if !g.chance(8) {
		g.leaf("wp:extent", ga{"cx", g.enum("914400", "457200", "0", "-5", "99999999999", "12700", "1in")}, ga{"cy", g.enum("914400", "685800", "1", "abc")})
	}
	if g.chance(2) {
		g.leaf("wp:effectExtent", ga{"l", g.num()}, ga{"t", g.num()}, ga{"r", g.num()}, ga{"b", g.num()})
	}
	if anchored {
		switch g.rng.IntN(6) {
		case 0:
			g.leaf("wp:wrapSquare", ga{"wrapText", g.enum("bothSides", "left", "right", "largest", "bogus")})
		case 1, 2:
			name := g.pick("wp:wrapTight", "wp:wrapThrough")
			g.open(name, ga{"wrapText", g.enum("bothSides", "largest")})
			if g.chance(4) == false {
				g.open("wp:wrapPolygon", ga{"edited", g.from(onOffPool)})
				pts := [][2]string{{"0", "0"}, {"0", "21600"}, {"21600", "21600"}, {"21600", "0"}, {"0", "0"}}
				if g.chance(4) {
					pts = [][2]string{{"5", "5"}, {"100", "9"}, {"5", "5"}}
				} else if g.chance(2) {
					pts = nil
					for range g.rng.IntN(7) {
						pts = append(pts, [2]string{g.num()(), g.num()()})
					}
				}
				for i, p := range pts {
					el := "wp:lineTo"
					if i == 0 {
						el = "wp:start"
					}
					g.b.WriteString(`<` + el + ` x="` + xmlEsc(p[0]) + `" y="` + xmlEsc(p[1]) + `"/>`)
				}
				g.close("wp:wrapPolygon")
			}
			g.close(name)
		case 3:
			g.leaf("wp:wrapTopAndBottom")
		case 4:
			g.leaf("wp:wrapNone")
		}
	}
	g.b.WriteString(`<wp:docPr id="` + g.pick("1", "2", "0", "x") + `" name="` + g.pick("Picture 1", "Picture 1", "Ink Pen 3", "Ink highlighter 4 #12", "Ink Pencil 2 #x #7",
		"Ink Bogus 1", "ink Pen", "Ink\tPen 3 #+5", "Ink Pen 3 #4294967296", "Ink Pen #9 #3", "Ink Pen 3 x #-1 #2") + `"`)
	for _, a := range []string{"descr", "title"} {
		if g.chance(2) {
			g.b.WriteString(` ` + a + `="` + xmlEsc(g.pick("A cat", "", strings.Repeat("d", 300), "line\nbreak")) + `"`)
		}
	}
	if g.chance(4) {
		g.b.WriteString(` hidden="` + g.pick(onOffPool...) + `"`)
	}
	g.b.WriteString(`>`)
	if g.chance(4) {
		g.b.WriteString(`<a:extLst><a:ext uri="` + g.pick(DecorativeExt, "{c183d7f6-b498-43b3-948b-1728b52aa6e4}", "{X}") + `"><adec:decorative xmlns:adec="http://schemas.microsoft.com/office/drawing/2017/decorative" val="` + g.pick("1", "0") + `"/></a:ext></a:extLst>`)
	}
	g.b.WriteString(`</wp:docPr>`)
	g.b.WriteString(`<a:graphic>`)
	switch {
	case g.chance(g.exotic):
		g.b.WriteString(`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="rId1"/></a:graphicData>`)
	case g.chance(3):
		g.b.WriteString(`<a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">`)
		g.wsp()
		g.b.WriteString(`</a:graphicData>`)
	case g.chance(10):
		g.b.WriteString(`<a:graphicData uri="x"/>`)
	case g.chance(6):
		g.b.WriteString(`<a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingGroup">`)
		g.group("wpg:wgp", 0)
		g.b.WriteString(`</a:graphicData>`)
	case g.chance(6):
		g.b.WriteString(`<a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas">`)
		g.canvas()
		g.b.WriteString(`</a:graphicData>`)
	default:
		g.b.WriteString(`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`)
		g.pic()
		g.b.WriteString(`</a:graphicData>`)
	}
	g.b.WriteString(`</a:graphic>`)
	if anchored {
		g.close("wp:anchor")
	} else {
		g.close("wp:inline")
	}
	g.b.WriteString(`</w:drawing>`)
}

// pic writes a picture (pic:pic) with random media, crop, rotation and frame.
func (g *docxGen) pic() {
	g.b.WriteString(`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="0" name="p"`)
	g.nvAttrs()
	g.b.WriteString(`/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill>`)
	id := "rIdMissing"
	if len(g.images) > 0 && !g.chance(8) {
		id = g.pick(g.images...)
	}
	if !g.chance(10) {
		g.b.WriteString(`<a:blip r:embed="` + id + `"/>`)
	} else {
		g.b.WriteString(`<a:blip/>`)
	}
	if g.chance(3) {
		g.b.WriteString(`<a:srcRect l="` + g.pick("0", "10000", "-5", "150000", "x", "33333") + `" t="` + g.pick("0", "500") + `" r="` + g.pick("25000", "") + `" b="7"/>`)
	}
	g.b.WriteString(`<a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr>`)
	if g.chance(2) {
		g.b.WriteString(`<a:xfrm rot="` + g.pick("0", "5400000", "-5400000", "2700000", "21600000", "99999999999", "x", "1234567", "-1234567", "-21599999") + `" flipH="` + g.pick(onOffPool...) + `" flipV="` + g.pick("0", "1") + `">`)
		g.offExt(false)
		g.b.WriteString(`</a:xfrm>`)
	}
	g.b.WriteString(`<a:prstGeom prst="rect"/></pic:spPr></pic:pic>`)
}

// emuPool are DrawingML coordinates (EMU).
var emuPool = []string{"9999999999999999", "-9999999999999999", "0", "914400", "457200", "-457200", "12700", "1", "99999999999", "-99999999999", "x", "1in", "254000", "3175", "635"}

// offExt writes a:off and a:ext, and for a group a:chOff and a:chExt, each sometimes missing.
func (g *docxGen) offExt(group bool) {
	names := [][3]string{{"a:off", "x", "y"}, {"a:ext", "cx", "cy"}}
	if group {
		names = append(names, [3]string{"a:chOff", "x", "y"}, [3]string{"a:chExt", "cx", "cy"})
	}
	for _, n := range names {
		if g.chance(5) {
			continue
		}
		g.b.WriteString(`<` + n[0])
		for _, a := range n[1:] {
			if !g.chance(8) {
				g.b.WriteString(` ` + a + `="` + g.pick(emuPool...) + `"`)
			}
		}
		g.b.WriteString(`/>`)
	}
}

// nvAttrs writes the alternative-text attributes of a cNvPr.
func (g *docxGen) nvAttrs() {
	for _, a := range []string{"descr", "title"} {
		if g.chance(3) {
			g.b.WriteString(` ` + a + `="` + xmlEsc(g.pick("A dog", "", "line\nbreak")) + `"`)
		}
	}
	if g.chance(4) {
		g.b.WriteString(` hidden="` + g.pick(onOffPool...) + `"`)
	}
}

// group writes a group (wpg:wgp, or wpg:grpSp nested in one) with random frame and members.
func (g *docxGen) group(name string, depth int) {
	g.b.WriteString(`<` + name + `><wpg:cNvGrpSpPr/>`)
	if !g.chance(6) {
		g.b.WriteString(`<wpg:grpSpPr>`)
		if !g.chance(5) {
			g.b.WriteString(`<a:xfrm`)
			if g.chance(3) {
				g.b.WriteString(` rot="` + g.pick("5400000", "-60000", "x") + `" flipH="` + g.pick("1", "0") + `"`)
			}
			g.b.WriteString(`>`)
			g.offExt(true)
			g.b.WriteString(`</a:xfrm>`)
		}
		g.b.WriteString(`</wpg:grpSpPr>`)
	}
	g.members(depth)
	g.b.WriteString(`</` + name + `>`)
}

// members writes a group's or canvas's members: pictures, shapes, groups and things skipped.
func (g *docxGen) members(depth int) {
	if depth == 0 && g.chance(20) {
		// Nested deeper than a group is read.
		n := 7 + g.rng.IntN(4)
		for range n {
			g.b.WriteString(`<wpg:grpSp><wpg:grpSpPr><a:xfrm><a:off x="12700" y="0"/><a:ext cx="12700" cy="12700"/></a:xfrm></wpg:grpSpPr><wps:wsp/>`)
		}
		g.b.WriteString(`<wps:wsp/>`)
		for range n {
			g.b.WriteString(`</wpg:grpSp>`)
		}
	}
	if g.chance(60) {
		// More than a group keeps.
		for range 1003 {
			g.b.WriteString(`<wps:wsp/>`)
		}
		return
	}
	for range g.rng.IntN(5) {
		switch g.rng.IntN(9) {
		case 0, 1:
			g.pic()
		case 2, 3, 4:
			g.wsp()
		case 5:
			if depth < 10 {
				g.group(g.pick("wpg:grpSp", "wpg:grpSp", "wpg:wgp"), depth+1)
			}
		case 6:
			g.b.WriteString(`<mc:AlternateContent><mc:Choice Requires="` + g.pick("wps", "wpg", "bogus", "wps w14") + `">`)
			if !g.chance(5) {
				g.wsp()
			}
			g.b.WriteString(`</mc:Choice>`)
			if g.chance(2) {
				g.b.WriteString(`<mc:Fallback>`)
				if g.chance(2) {
					g.pic()
				}
				g.b.WriteString(`</mc:Fallback>`)
			}
			g.b.WriteString(`</mc:AlternateContent>`)
		case 7:
			if g.chance(g.exotic / 4) {
				g.b.WriteString(g.pick(`<w14:contentPart r:id="rId1"/>`, `<mc:AlternateContent><mc:Choice Requires="w14"><w14:contentPart r:id="rId1"/></mc:Choice><mc:Fallback/></mc:AlternateContent>`))
			}
		default:
			g.b.WriteString(g.pick(`<wpg:cNvGrpSpPr/>`, `<a:junk/>`, `<wpc:bg/>`))
		}
	}
}

// canvas writes a drawing canvas (wpc:wpc) with random background, outline, spin and members.
func (g *docxGen) canvas() {
	g.b.WriteString(`<wpc:wpc>`)
	solid := func() {
		switch g.rng.IntN(4) {
		case 0:
			g.b.WriteString(`<a:solidFill><a:srgbClr val="` + g.pick(colorPool...) + `"/></a:solidFill>`)
		case 1:
			g.b.WriteString(`<a:solidFill><a:schemeClr val="accent1"/></a:solidFill>`)
		case 2:
			g.b.WriteString(`<a:noFill/>`)
		}
	}
	if g.chance(2) {
		g.b.WriteString(`<wpc:bg>`)
		solid()
		g.b.WriteString(`</wpc:bg>`)
	}
	if g.chance(2) {
		g.b.WriteString(`<wpc:whole>`)
		if !g.chance(4) {
			g.b.WriteString(`<a:ln`)
			if g.chance(2) {
				g.b.WriteString(` w="` + g.pick("9525", "0", "-5", "99999999999", "x", "12700000") + `"`)
			}
			g.b.WriteString(`>`)
			solid()
			g.b.WriteString(`</a:ln>`)
		}
		g.b.WriteString(`</wpc:whole>`)
	}
	g.members(0)
	if g.chance(3) {
		g.b.WriteString(`<wpc:extLst><a:ext uri="` + g.pick(CanvasSpinExt, "urn:wordcraft:CANVAS-xfrm", "{X}") + `"><a:xfrm rot="` + g.pick("5400000", "1234567", "x") + `" flipV="` + g.pick("1", "0") + `"/></a:ext></wpc:extLst>`)
	}
	g.b.WriteString(`</wpc:wpc>`)
}

// custGeom writes custom geometry: paths of lines, curves, arcs and closes with random sizes and
// points, sometimes more than a freeform keeps.
func (g *docxGen) custGeom() {
	g.b.WriteString(`<a:custGeom><a:avLst/><a:gdLst/><a:pathLst>`)
	coord := []string{"0", "100", "21600", "-50", "5000", "99999999999999", "-99999999999999", "x", "1e3", " 7 ", "33"}
	pt := func() {
		g.b.WriteString(`<a:pt`)
		if !g.chance(12) {
			g.b.WriteString(` x="` + g.pick(coord...) + `"`)
		}
		if !g.chance(12) {
			g.b.WriteString(` y="` + g.pick(coord...) + `"`)
		}
		g.b.WriteString(`/>`)
	}
	paths := 1 + g.rng.IntN(3)
	if g.chance(30) {
		paths = 260
	}
	for range paths {
		g.b.WriteString(`<a:path`)
		for _, a := range []string{"w", "h"} {
			if g.chance(2) {
				g.b.WriteString(` ` + a + `="` + g.pick("100", "21600", "0", "-5", "x", "99999999999999", "1") + `"`)
			}
		}
		if g.chance(4) {
			g.b.WriteString(` fill="` + g.pick("none", "norm", "darken") + `"`)
		}
		g.b.WriteString(`>`)
		if g.chance(5) {
			// Drawn back to where it started.
			if g.chance(3) {
				g.b.WriteString(`<a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:lnTo><a:pt x="0" y="0"/></a:lnTo>`)
			} else {
				g.b.WriteString(`<a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:lnTo><a:pt x="100" y="0"/></a:lnTo><a:lnTo><a:pt x="100" y="100"/></a:lnTo><a:lnTo><a:pt x="0" y="` + g.pick("0", "1") + `"/></a:lnTo>`)
			}
		}
		segs := g.rng.IntN(7)
		if paths == 1 && g.chance(40) {
			segs = 6000
		}
		for i := range segs {
			if segs > 1000 && i == segs-2 {
				// Past the points a path keeps: a new subpath after it.
				g.b.WriteString(`<a:moveTo><a:pt x="5" y="5"/></a:moveTo><a:lnTo><a:pt x="9" y="9"/></a:lnTo>`)
				break
			}
			if segs > 1000 {
				g.b.WriteString(`<a:lnTo><a:pt x="` + strconv.Itoa(i) + `" y="` + strconv.Itoa(i%97) + `"/></a:lnTo>`)
				continue
			}
			name := g.pick("a:moveTo", "a:lnTo", "a:lnTo", "a:cubicBezTo", "a:quadBezTo", "a:arcTo", "a:close", "a:junk")
			g.b.WriteString(`<` + name + `>`)
			n := map[string]int{"a:moveTo": 1, "a:lnTo": 1, "a:cubicBezTo": 3, "a:quadBezTo": 2}[name]
			if g.chance(6) {
				n = g.rng.IntN(4)
			} else if g.chance(30) {
				n = 20
			}
			for range n {
				pt()
			}
			g.b.WriteString(`</` + name + `>`)
		}
		g.b.WriteString(`</a:path>`)
	}
	g.b.WriteString(`</a:pathLst></a:custGeom>`)
}

// object writes an OLE object (w:object): its picture (VML, or a DrawingML picture) and its
// o:OLEObject, sometimes with relationships missing or more than are kept.
func (g *docxGen) object() {
	g.b.WriteString(`<w:object w:dxaOrig="1440" w:dyaOrig="960">`)
	switch g.rng.IntN(5) {
	case 0:
		g.drawing()
	case 1:
		// No picture, or two drawings (the first is shown).
		if g.chance(2) {
			g.b.WriteString(`<v:rect style="width:10pt;height:20pt" fillcolor="red"/><v:oval style="width:5pt;height:5pt"/>`)
		}
	default:
		img := "rIdMissing"
		if len(g.images) > 0 && !g.chance(6) {
			img = g.pick(g.images...)
		}
		g.b.WriteString(`<v:shape id="_x0000_i1025" type="#_x0000_t75" style="width:72pt;height:48pt" o:ole=""><v:imagedata r:id="` + img + `" o:title=""/></v:shape>`)
	}
	ole := "rIdNone"
	if len(g.oles) > 0 && !g.chance(6) {
		ole = g.pick(g.oles...)
	}
	g.b.WriteString(`<o:OLEObject Type="` + g.pick("Embed", "Link") + `" ProgID="Excel.Sheet.12" ShapeID="_x0000_i1025" DrawAspect="Content" ObjectID="_1234" r:id="` + ole + `"`)
	if g.chance(20) {
		// More relationship ids than one object may use.
		for i := range 70 {
			g.b.WriteString(` r:x` + strconv.Itoa(i) + `="` + ole + strconv.Itoa(i) + `"`)
		}
	} else if len(g.links) > 0 && g.chance(2) {
		// At the limit: with r:id, 64 ids or 65.
		for i, id := range g.links[:63+g.rng.IntN(2)] {
			g.b.WriteString(` r:l` + strconv.Itoa(i) + `="` + id + `"`)
		}
	}
	if g.chance(6) {
		g.b.WriteString(` o:relid="` + g.pick(append(g.oles, "rIdNope")...) + `"`)
	}
	g.b.WriteString(`/></w:object>`)
}

// pict writes a w:pict of legacy VML: shapetypes, shapes, presets, lines, groups, pictures, text
// boxes, WordArt, watermarks and paths, with hostile styles, colours and sizes.
func (g *docxGen) pict() {
	g.b.WriteString(`<w:pict>`)
	if g.chance(80) {
		// More drawings than one picture keeps.
		g.b.WriteString(strings.Repeat(`<v:rect style="width:1pt;height:1pt" fillcolor="red"/>`, 1003) + `</w:pict>`)
		return
	}
	for range 1 + g.rng.IntN(3) {
		g.vmlEl(0)
	}
	g.b.WriteString(`</w:pict>`)
}

var (
	vmlColorPool = []string{"#ffc000 [3207]", "ffc000", "#f00", "f00", "black", "Red", "rgb(255, 128, 0)", "rgb(100%, 0%, 0%)", "rgba(1,2,3,0.5)",
		"rgb(1,2)", "rgb(x,1,2)", "#é", "", "none", "window", "#12345", "rgb(300,-5,nan)", "rgb(50.5%, 1e2%, 0))", "rgb(2.5,0.5,1.5)", " #4472C4 "}
	vmlLenPool = []string{"100pt", "72", "1in", "2cm", "-5pt", "0", "x", "99999pt", "5.5pt", "1e3pt", "", "50%", "3mm"}
)

// vmlStyleAttr writes a random VML style attribute.
func (g *docxGen) vmlStyleAttr(sized bool) {
	if g.chance(6) && !sized {
		return
	}
	var decls []string
	if g.chance(2) {
		decls = append(decls, "position:absolute")
	}
	for range g.rng.IntN(9) {
		switch g.rng.IntN(16) {
		case 0:
			decls = append(decls, g.pick("position:absolute", "POSITION: Absolute", "position:relative", "position:"))
		case 1, 2:
			decls = append(decls, g.pick("left", "margin-left", "top", "margin-top")+":"+g.pick(vmlLenPool...))
		case 3, 4:
			decls = append(decls, g.pick("width", "height", "Width")+":"+g.pick(vmlLenPool...))
		case 5:
			decls = append(decls, "z-index:"+g.pick("-251658240", "5", "x", "-1", "0"))
		case 6:
			decls = append(decls, "visibility:"+g.pick("hidden", "visible", "HIDDEN"))
		case 7:
			decls = append(decls, "rotation:"+g.pick("90", "45.5", "-30", "5898240fd", "x", "inf", "NaN", "1e40", "720", " 2949120 fd"))
		case 8:
			decls = append(decls, "flip:"+g.pick("x", "y", "x y", "xy", ""))
		case 9:
			decls = append(decls, "mso-position-horizontal:"+g.pick("center", "left", "right", "inside", "outside", "absolute", "Center"))
		case 10:
			decls = append(decls, "mso-position-vertical:"+g.pick("center", "top", "bottom", "inside", "outside", "x"))
		case 11:
			decls = append(decls, "mso-position-horizontal-relative:"+g.pick("page", "margin", "char", "left-margin-area", "right-margin-area", "inner-margin-area", "outer-margin-area", "text", "Page"))
		case 12:
			decls = append(decls, "mso-position-vertical-relative:"+g.pick("page", "margin", "line", "top-margin-area", "bottom-margin-area", "inner-margin-area", "outer-margin-area", "text"))
		case 13:
			decls = append(decls, "mso-wrap-distance-"+g.pick("left", "right", "top", "bottom")+":"+g.pick("9pt", "0", "-3pt", "9999pt", "x"))
		case 14:
			decls = append(decls, "v-text-anchor:"+g.pick("middle", "bottom", "top", "middle-center", "x"))
		default:
			decls = append(decls, g.pick("junk", "a:b:c", " ", "mso-fit-shape-to-text:t"))
		}
	}
	if sized {
		// The last declaration wins: a size the drawing's points scale to.
		decls = append(decls, "width:"+g.pick("100pt", "7.5pt", "3pt")+";height:"+g.pick("80pt", "2pt"))
	}
	g.b.WriteString(` style="` + xmlEsc(strings.Join(decls, g.pick(";", "; ", ";;"))) + `"`)
}

// vmlPaint writes the VML stroke and fill attributes, each sometimes missing.
func (g *docxGen) vmlPaint() {
	for _, a := range []struct {
		n    string
		pool []string
	}{
		{"stroked", []string{"f", "false", "0", "t", "true", "", "F"}}, {"filled", []string{"f", "t", "0", "on"}},
		{"strokecolor", vmlColorPool}, {"fillcolor", vmlColorPool}, {"strokeweight", []string{"1pt", "2", "x", "-5pt", "500pt", ".5mm", "1.5pt"}},
	} {
		if g.chance(2) {
			g.b.WriteString(` ` + a.n + `="` + xmlEsc(g.pick(a.pool...)) + `"`)
		}
	}
}

// vmlEl writes one VML element of a picture or group.
func (g *docxGen) vmlEl(depth int) {
	if g.chance(6) {
		// Word's text watermark (in a header), with its parts sometimes missing or odd.
		g.b.WriteString(`<v:shape id="` + g.pick("PowerPlusWaterMarkObject357", "WaterMark", "x") + `" o:spid="_x0000_s2049" type="#_x0000_t136" style="` +
			g.pick("position:absolute;margin-left:0;margin-top:0;width:412.4pt;height:247.4pt;rotation:315;z-index:-251657216;mso-position-horizontal:center;mso-position-horizontal-relative:margin;mso-position-vertical:center;mso-position-vertical-relative:margin",
				"position:absolute;width:100pt;height:50pt;mso-position-horizontal:center;mso-position-vertical:center", "position:absolute;rotation:0", "",
				"position:absolute;mso-position-horizontal:center", "position:absolute;mso-position-vertical:center") +
			`" o:allowincell="f" fillcolor="` + g.pick(vmlColorPool...) + `" stroked="f">`)
		if g.chance(2) {
			g.b.WriteString(`<v:fill opacity="` + g.pick(".5", "1", "32768f", "x") + `"/>`)
		}
		g.b.WriteString(`<v:textpath style="font-family:&quot;` + g.pick("Calibri", "Times New Roman", "", " ") + `&quot;;font-size:1pt" string="` + g.pick("DRAFT", "ASAP", " ", "Confidential") + `"`)
		if g.chance(4) {
			g.b.WriteString(` on="` + g.pick("f", "t") + `"`)
		}
		g.b.WriteString(`/><w10:wrap anchorx="margin" anchory="margin"/></v:shape>`)
		return
	}
	switch g.rng.IntN(12) {
	case 0:
		g.b.WriteString(`<v:shapetype id="` + g.pick("_x0000_t202", "_x0000_t136", "_x0000_t75", "myType", "_x0000_t32") + `"`)
		if g.chance(2) {
			g.b.WriteString(` o:spt="` + g.pick("202", "136", "150", "175", "13", "0", "x", "32") + `"`)
		}
		g.vmlPaint()
		if g.chance(3) {
			g.b.WriteString(` path="m,l,21600r21600,l21600,xe"`)
		}
		g.b.WriteString(`/>`)
	case 1:
		if depth < 10 {
			g.b.WriteString(`<v:group`)
			g.vmlStyleAttr(true)
			if g.chance(2) {
				g.b.WriteString(` coordsize="` + g.pick("1000,1000", "100,50", "0,0", "-5,10", "x", "21600", "1,", "21600,21600") + `"`)
			}
			if g.chance(3) {
				g.b.WriteString(` coordorigin="` + g.pick("0,0", "-50,10", "x", "100,100") + `"`)
			}
			g.b.WriteString(`>`)
			for range g.rng.IntN(4) {
				g.vmlEl(depth + 1)
			}
			if depth == 0 && g.chance(15) {
				// Nested deeper than a group is read.
				for range 10 {
					g.b.WriteString(`<v:group style="width:10pt;height:10pt" coordsize="10,10"><v:rect style="width:5pt;height:5pt"/>`)
				}
				for range 10 {
					g.b.WriteString(`</v:group>`)
				}
			}
			g.b.WriteString(`</v:group>`)
		}
	case 2:
		g.b.WriteString(`<v:line`)
		g.vmlStyleAttr(false)
		g.vmlPaint()
		for _, a := range []string{"from", "to"} {
			if g.chance(4) == false {
				g.b.WriteString(` ` + a + `="` + g.pick("0,0", "100pt,50pt", "10,-20", "x,5", "5", "-3pt,99999pt", "1in,2cm") + `"`)
			}
		}
		g.b.WriteString(`>`)
		if g.chance(2) {
			g.b.WriteString(`<v:stroke startarrow="` + g.pick("block", "none", "") + `" endarrow="` + g.pick("classic", "none") + `"/>`)
		}
		g.vmlWrap()
		g.b.WriteString(`</v:line>`)
	default:
		name := g.pick("v:shape", "v:shape", "v:shape", "v:rect", "v:roundrect", "v:oval", "v:image", "v:junk")
		g.b.WriteString(`<` + name)
		if g.chance(4) {
			g.b.WriteString(` id="` + g.pick("PowerPlusWaterMarkObject357", "_x0000_s1026", "WordPictureWatermark1") + `"`)
		}
		path := g.chance(3)
		g.vmlStyleAttr(path && !g.chance(4))
		g.vmlPaint()
		if g.chance(2) {
			g.b.WriteString(` type="` + g.pick("#_x0000_t202", "_x0000_t136", "#_x0000_t75", "#myType", " ##_x0000_t32 ", "#_x0000_t150", "#_x0000_t13", "#_x0000_tx") + `"`)
		}
		if g.chance(4) {
			g.b.WriteString(` o:spt="` + g.pick("202", "136", "150", "175", "176", "13", "0", "24", "999", "x", "+5", "84") + `"`)
		}
		if g.chance(5) {
			g.b.WriteString(` alt="` + g.pick("A chart", "") + `"`)
		}
		if path {
			g.b.WriteString(` path="` + xmlEsc(g.pick("m0,0l100,0,100,100xe", "m,l100,xe nf", "m0,0 c 10,10 20,20 30,30 e", "M 10 10 L 20 20 R 5,5 T 3,3 E",
				"m0,0at0,0,100,100,0,0,100,100x", "qx 5,5 qy 10,10 qb 1,1,2,2", "m0,0l@1,@2e", "m 99999999999999999,0 l 1 1 e", "ns m0,0l5,5e",
				"m-5.5,3.7l+2,.5e", "", "zz", "v1,2,3,4,5,6 e", "wa0,0,10,10,1,1,5,5 wr 0,0,1,1,2,2,3,3 ae 1,2,3,4,5,6 al 1,1,1,1,1,1", "m0,0,5,5,10,0x m20,20l30,30",
				"t5,5r1,1,2,2v1,1,2,2,3,3,4,4,5,5x", "m10,10v1,2,3,4,5,6e", "m0,0qb1,1,2,2,30,40e", "ns m0,0l5,5e", "nsnf m0,0l5,5e",
				"m 999999999999,0 r 999999999999,5 e", "m10,10l20,20x r5,5e", "m0,0l5,", "m0,0 l 99999999999999999,7 e")) + `"`)

			if g.chance(2) {
				g.b.WriteString(` coordsize="` + g.pick("1000,1000", "100,50", "0,0", "-5,10", "x", "21600", "21600,21600", "999999999999999,1000") + `"`)
			}
			if g.chance(3) {
				g.b.WriteString(` coordorigin="` + g.pick("0,0", "-50,10", "x", "10") + `"`)
			}
		}
		g.b.WriteString(`>`)
		if g.chance(6) {
			g.b.WriteString(`<v:path v="m,l1,1e"/>`)
		}
		if g.chance(4) {
			g.b.WriteString(`<v:fill opacity="` + g.pick("0", ".5", "32768f", "50%", "0f", "x", "inf", "2", "-1", "1") + `"/>`)
		}
		if g.chance(4) {
			g.b.WriteString(`<v:stroke opacity="` + g.pick("0.5", "65536f", "x", "10%", "2", "-1", "131072f") + `" startarrow="block"/>`)
		}
		switch g.rng.IntN(5) {
		case 0:
			id := "rIdMissing"
			if len(g.images) > 0 && !g.chance(6) {
				id = g.pick(g.images...)
			}
			g.b.WriteString(`<v:imagedata ` + g.pick("r:id", "r:pict", "r:x") + `="` + id + `"`)
			if g.chance(2) {
				g.b.WriteString(` o:title="` + g.pick("Logo", "") + `"`)
			}
			g.b.WriteString(`/>`)
		case 1:
			g.b.WriteString(`<v:textpath`)
			if g.chance(2) {
				g.b.WriteString(` style="font-family:` + xmlEsc(g.pick(`&quot;Calibri&quot;`, `'Times New Roman'`, ` `, `""`, "Arial;font-size:1pt", strings.Repeat("f", 120))) + `"`)
			}
			if g.chance(3) {
				g.b.WriteString(` on="` + g.pick("t", "f", "false", "0", "true") + `"`)
			}
			if !g.chance(8) {
				g.b.WriteString(` string="` + xmlEsc(g.pick("DRAFT", "CONFIDENTIAL", " ", "", strings.Repeat("W", 250), "A\nB")) + `"`)
			}
			g.b.WriteString(`/>`)
		case 2:
			g.b.WriteString(`<v:textbox`)
			if g.chance(2) {
				g.b.WriteString(` inset="` + g.pick("0,0,0,0", "7.2pt,3.6pt,7.2pt,3.6pt", "91440,45720", ",,5pt", "x,-5pt,1in,", "99999pt", "") + `"`)
			}
			if g.chance(4) {
				g.b.WriteString(` o:insetmode="` + g.pick("auto", "custom", "AUTO") + `"`)
			}
			if g.chance(3) {
				g.b.WriteString(` style="mso-fit-shape-to-text:` + g.pick("t", "true", "f", "T") + `"`)
			}
			g.b.WriteString(`>`)
			if g.chance(8) {
				// Text that isn't in a v:textbox of its own.
				g.b.WriteString(`<v:junk><w:txbxContent><w:p><w:r><w:t>J</w:t></w:r></w:p></w:txbxContent></v:junk>`)
			}
			if !g.chance(6) {
				g.b.WriteString(`<w:txbxContent>`)
				if g.txbxDepth < 5 {
					g.txbxDepth++
					g.blocks(2, 1+g.rng.IntN(2))
					g.txbxDepth--
				}
				g.b.WriteString(`</w:txbxContent>`)
			}
			g.b.WriteString(`</v:textbox>`)
		}
		g.vmlWrap()
		g.b.WriteString(`</` + name + `>`)
	}
}

func (g *docxGen) vmlWrap() {
	if g.chance(3) {
		g.b.WriteString(`<w10:wrap type="` + g.pick("square", "tight", "through", "topAndBottom", "none", "x") + `"/>`)
	}
}

// customProps is a docProps/custom.xml with random properties: simple and raw values, missing
// names and values, other elements, and rarely more than are kept.
func (g *docxGen) customProps() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">`)
	n := g.rng.IntN(6)
	if g.chance(40) {
		n = 10_005
	}
	for i := range n {
		if n < 1000 && g.chance(10) {
			b.WriteString(`<other name="x"><vt:lpwstr>o</vt:lpwstr></other>`)
			continue
		}
		b.WriteString(`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="` + strconv.Itoa(i+2) + `"`)
		if !g.chance(10) {
			b.WriteString(` name="` + xmlEsc(g.pick("ZOTERO_PREF_1", "Client", "", "a&b", "Ünïcødé")) + `"`)
		}
		b.WriteString(`>`)
		if n > 1000 {
			b.WriteString(`<vt:i4>` + strconv.Itoa(i) + `</vt:i4></property>`)
			continue
		}
		switch g.rng.IntN(8) {
		case 0:
			// No value.
		case 1:
			b.WriteString(`<vt:vector size="2" baseType="lpstr"><vt:lpstr>a &amp; b</vt:lpstr><vt:lpstr attr="&quot;q&quot;" xmlnsish="2">c</vt:lpstr></vt:vector>`)
		case 2:
			b.WriteString(`<vt:blob>AAEC</vt:blob>`)
		case 3:
			k := g.pick("i4", "bool", "filetime", "r8", "clsid", "decimal")
			b.WriteString(`<vt:` + k + `>` + textEsc(g.pick("42", "true", "2024-01-01T00:00:00Z", " 1.5 ", "{X}", "")) + `</vt:` + k + `>`)
		case 4:
			b.WriteString(`<vt:empty/>`)
		case 5:
			b.WriteString(`<vt:lpwstr>` + textEsc(g.pick("pref <data/>", "", "line\nbreak", strings.Repeat("z", 300))) + `</vt:lpwstr><vt:i4>9</vt:i4>`)
		default:
			b.WriteString(`<vt:lpwstr>` + textEsc(g.pick(textPool...)) + `<x xmlns:q="urn:q" q:a="1" xmlnsish="2">mixed</x>tail</vt:lpwstr>`)
		}
		b.WriteString(`</property>`)
	}
	b.WriteString(`</Properties>`)
	return b.String()
}

// w14Color writes a w14 colour (srgb or scheme) with random modifiers.
func (g *docxGen) w14Color() {
	if g.chance(8) {
		return
	}
	name := g.pick("w14:srgbClr", "w14:schemeClr", "w14:prstClr")
	val := g.pick(colorPool...)
	if name == "w14:schemeClr" {
		val = g.pick("accent1", "tx1", "bg2", "phClr", "hlink", "x")
	}
	g.b.WriteString(`<` + name)
	if !g.chance(10) {
		g.b.WriteString(` w14:val="` + xmlEsc(val) + `"`)
	}
	g.b.WriteString(`>`)
	for range g.rng.IntN(3) {
		g.b.WriteString(`<w14:` + g.pick("lumMod", "lumOff", "alpha", "tint") + ` w14:val="` + g.pick("75000", "25000", "-30000", "2000000", "x", "100000", "50000") + `"/>`)
	}
	g.b.WriteString(`</` + name + `>`)
}

// w14Fill writes a w14 fill: none, solid, or a gradient with random stops.
func (g *docxGen) w14Fill() {
	switch g.rng.IntN(4) {
	case 0:
		g.b.WriteString(`<w14:noFill/>`)
	case 1, 2:
		g.b.WriteString(`<w14:solidFill>`)
		g.w14Color()
		g.b.WriteString(`</w14:solidFill>`)
	default:
		g.b.WriteString(`<w14:gradFill><w14:gsLst>`)
		n := g.rng.IntN(5)
		if g.chance(10) {
			n = 14
		}
		for range n {
			g.b.WriteString(`<w14:gs w14:pos="` + g.pick("0", "50000", "100000", "-5", "200000", "x", "33333") + `">`)
			g.w14Color()
			g.b.WriteString(`</w14:gs>`)
		}
		g.b.WriteString(`</w14:gsLst>`)
		if g.chance(2) {
			g.b.WriteString(`<w14:lin w14:ang="` + g.pick("5400000", "0", "-5400000", "99999999999", "x", "21600000") + `" w14:scaled="0"/>`)
		}
		g.b.WriteString(`</w14:gradFill>`)
	}
}

// textEffect writes one w14 run text effect (WordArt) with hostile values.
func (g *docxGen) textEffect() {
	emu := func() string { return g.pick("12700", "0", "-5", "99999999999", "x", "38100", "1in", "1e400") }
	switch g.rng.IntN(7) {
	case 0:
		g.b.WriteString(`<w14:textFill>`)
		if !g.chance(6) {
			g.w14Fill()
		}
		g.b.WriteString(`</w14:textFill>`)
	case 1:
		g.b.WriteString(`<w14:textOutline w14:w="` + emu() + `" w14:cap="flat">`)
		if !g.chance(4) {
			g.w14Fill()
		}
		g.b.WriteString(`</w14:textOutline>`)
	case 2:
		g.b.WriteString(`<w14:shadow w14:blurRad="` + emu() + `" w14:dist="` + emu() + `" w14:dir="` + g.pick("2700000", "-5400000", "x", "21600000") + `">`)
		g.w14Color()
		g.b.WriteString(`</w14:shadow>`)
	case 3:
		g.b.WriteString(`<w14:glow w14:rad="` + emu() + `">`)
		g.w14Color()
		g.b.WriteString(`</w14:glow>`)
	case 4:
		g.b.WriteString(`<w14:reflection w14:blurRad="` + emu() + `" w14:stA="` + g.pick("52000", "-5", "200000", "x") + `"` + g.pick(` w14:endPos="35000"`, ` w14:endPos="0"`, ` w14:endPos="100001"`, "") + ` w14:dist="` + emu() + `"/>`)
	case 5:
		g.leaf("w14:ligatures", ga{"w14:val", g.enum("standard", "all", "none")})
	default:
		g.b.WriteString(g.pick(`<w14:cntxtAlts/>`, `<w14:numForm w14:val="lining"/>`, `<w14:props3d w14:extrusionH="0"/>`))
	}
}

// colorEl writes a DrawingML colour element with random modifiers.
func (g *docxGen) colorEl() {
	name := g.pick("a:srgbClr", "a:schemeClr", "a:sysClr", "a:prstClr", "a:hslClr", "a:srgbClr", "a:scrgbClr")
	switch name {
	case "a:scrgbClr":
		g.b.WriteString(`<a:scrgbClr`)
		for _, ch := range []string{"r", "g", "b"} {
			if !g.chance(8) {
				g.b.WriteString(` ` + ch + `="` + g.pick(strconv.Itoa(g.rng.IntN(100_001)), strconv.Itoa(g.rng.IntN(400)), "0", "100000", "-5", "200000", "x", "99999999999999", "50%", "21404.5") + `"`)
			}
		}
		g.b.WriteString(`>`)
	case "a:srgbClr":
		g.b.WriteString(`<a:srgbClr val="` + g.pick(colorPool...) + `">`)
	case "a:schemeClr":
		g.b.WriteString(`<a:schemeClr val="` + g.pick("accent1", "accent6", "tx1", "bg1", "dk2", "lt2", "hlink", "folHlink", "phClr", "x") + `">`)
	case "a:sysClr":
		g.b.WriteString(`<a:sysClr val="` + g.pick("windowText", "window", "highlight", "btnFace", "x") + `"`)
		if g.chance(2) {
			g.b.WriteString(` lastClr="` + g.pick(colorPool...) + `"`)
		}
		g.b.WriteString(`>`)
	case "a:prstClr":
		g.b.WriteString(`<a:prstClr val="` + g.pick("black", "white", "red", "green", "lime", "blue", "yellow", "cyan", "aqua", "magenta", "fuchsia", "gray", "grey", "silver",
			"ltgray", "ltGrey", "lightgray", "lightgrey", "dkgray", "dkGrey", "darkgray", "darkgrey", "maroon", "navy", "olive", "purple", "teal", "orange", "brown", "pink",
			"gold", "darkBlue", "dkblue", "darkred", "dkRed", "darkgreen", "dkgreen", "lightblue", "ltblue", "coral", "salmon", "tomato", "violet", "indigo", "beige",
			"ivory", "White", "nope") + `">`)
	case "a:hslClr":
		g.b.WriteString(`<a:hslClr hue="` + g.pick("0", "14400000", "-60000", "99999999999", "-12345678", "-1") + `" sat="` + g.pick("100000", "50000", "-5", "200000") + `" lum="` + g.pick("50000", "0", "100000", "x") + `">`)
	}
	for range g.rng.IntN(3) {
		g.b.WriteString(`<a:` + g.pick("lumMod", "lumOff", "tint", "shade", "alpha", "satMod") + ` val="` + g.pick("75000", "25000", "50000", "-30000", "2000000", "x", "100000") + `"/>`)
	}
	g.b.WriteString(`</` + name + `>`)
}

func (g *docxGen) fillEl() {
	switch g.rng.IntN(5) {
	case 0:
		g.b.WriteString(`<a:noFill/>`)
	case 1:
		g.b.WriteString(`<a:gradFill><a:gsLst><a:gs pos="0">`)
		g.colorEl()
		g.b.WriteString(`</a:gs></a:gsLst></a:gradFill>`)
	case 2:
		g.b.WriteString(`<a:pattFill prst="pct5"><a:fgClr>`)
		g.colorEl()
		g.b.WriteString(`</a:fgClr></a:pattFill>`)
	default:
		g.b.WriteString(`<a:solidFill>`)
		g.colorEl()
		g.b.WriteString(`</a:solidFill>`)
	}
}

// wsp writes a Word shape: preset, fill, outline, effects, style, text box and connector.
func (g *docxGen) wsp() {
	g.b.WriteString(`<wps:wsp>`)
	if g.chance(3) {
		g.b.WriteString(`<wps:cNvPr id="` + g.pick("1", "2", "0", "x", "7") + `" name="s"`)
		g.nvAttrs()
		g.b.WriteString(`/>`)
	}
	if g.chance(4) {
		g.b.WriteString(`<wps:cNvSpPr txBox="` + g.pick("1", "true", "0") + `"/>`)
	}
	if g.chance(5) {
		g.b.WriteString(`<wps:cNvCnPr>`)
		for _, t := range []string{"a:stCxn", "a:endCxn"} {
			if g.chance(3) == false {
				g.b.WriteString(`<` + t + ` id="` + g.pick("1", "2", "0", "x", "7") + `" idx="` + g.pick("0", "3", "100", "-1") + `"/>`)
			}
		}
		g.b.WriteString(`</wps:cNvCnPr>`)
	}
	if !g.chance(6) {
		g.b.WriteString(`<wps:spPr>`)
		if g.chance(2) {
			// A content part's transform; group members read it when there is no a:xfrm.
			x := g.pick("a:xfrm", "a:xfrm", "w14:xfrm")
			g.b.WriteString(`<` + x + ` rot="` + g.pick("0", "5400000", "1234567") + `">`)
			g.offExt(false)
			g.b.WriteString(`</` + x + `>`)
		}
		if !g.chance(5) {
			g.b.WriteString(`<a:prstGeom prst="` + g.pick("rect", "roundRect", "ellipse", "triangle", "rtTriangle", "diamond", "line", "straightConnector1", "bentConnector3", "curvedConnector2",
				"rightArrow", "star5", "heart", "flowChartOr", "wedgeRectCallout", "cloud", "snipRoundRect", "bogus") + `">`)
			if g.chance(2) {
				g.b.WriteString(`<a:avLst><a:gd name="adj" fmla="` + g.pick("val 16667", " val -5 ", "val 99999999", "val x", "pin 0", "val50000") + `"/></a:avLst>`)
			}
			g.b.WriteString(`</a:prstGeom>`)
		}
		if g.chance(3) {
			g.fillEl()
		}
		if g.chance(8) {
			g.b.WriteString(`<a:blipFill/>`)
		}
		if g.chance(2) {
			g.b.WriteString(`<a:ln`)
			if g.chance(2) {
				g.b.WriteString(` w="` + xmlEsc(g.num()()) + `"`)
			}
			g.b.WriteString(`>`)
			if g.chance(3) {
				c := g.pick(`srgbClr val="FF0000"`, `schemeClr val="accent1"`)
				if g.chance(4) {
					// The alpha on a second colour, which isn't the outline's.
					g.b.WriteString(`<a:solidFill><a:srgbClr val="00FF00"/><a:srgbClr val="FF0000"><a:alpha val="50000"/></a:srgbClr></a:solidFill>`)
					c = ""
				}
				if c != "" {
					g.b.WriteString(`<a:solidFill><a:` + c + `><a:alpha val="` + g.pick("50000", "-5", "200000", "x", "0", "33333") + `"/></a:` + strings.Fields(c)[0] + `></a:solidFill>`)
				}
			} else if g.chance(2) {
				g.fillEl()
			}
			switch g.rng.IntN(3) {
			case 0:
				g.b.WriteString(`<a:prstDash val="` + g.pick("dash", "sysDot", "lgDashDotDot", "solid", "x") + `"/>`)
			case 1:
				g.b.WriteString(`<a:custDash>`)
				for range g.rng.IntN(4) {
					g.b.WriteString(`<a:ds d="` + g.pick("100000", "50%", " 3000 ", "-1", "x", "1e3%", "4294967296", "12.3456%", "0.0005%") + `" sp="` + g.pick("100000", "25.5%", "0") + `"/>`)
				}
				g.b.WriteString(`</a:custDash>`)
			}
			for _, t := range []string{"a:headEnd", "a:tailEnd"} {
				if g.chance(3) {
					g.b.WriteString(`<` + t + ` type="` + g.pick("none", "triangle", "arrow", "oval") + `"/>`)
				}
			}
			g.b.WriteString(`</a:ln>`)
		}
		if g.chance(3) {
			g.b.WriteString(`<a:effectLst>`)
			if g.chance(2) {
				g.b.WriteString(`<a:outerShdw blurRad="` + g.pick("50800", "-1", "99999999999", "x") + `" dist="` + g.pick("38100", "0", "9999999999") + `" dir="` + g.pick("2700000", "-5400000", "x", "21600000") + `"`)
				if g.chance(2) {
					g.b.WriteString(` rotWithShape="` + g.pick("0", "1", "false", "true", "on", "off", "x") + `"`)
				}
				g.b.WriteString(`>`)
				if g.chance(4) == false {
					g.colorEl()
				}
				g.b.WriteString(`</a:outerShdw>`)
			}
			if g.chance(2) {
				g.b.WriteString(`<a:glow rad="` + g.pick("63500", "0", "99999999999") + `">`)
				if g.chance(2) {
					g.colorEl()
				}
				g.b.WriteString(`</a:glow>`)
			}
			if g.chance(2) {
				g.b.WriteString(`<a:softEdge rad="` + g.pick("31750", "0", "-5", "9999999999") + `"/>`)
			}
			g.b.WriteString(`</a:effectLst>`)
		}
		if g.chance(4) {
			g.custGeom()
		}
		g.b.WriteString(`</wps:spPr>`)
	}
	if g.chance(2) {
		g.b.WriteString(`<wps:style>`)
		for _, ref := range []string{"a:lnRef", "a:fillRef", "a:effectRef", "a:fontRef"} {
			if g.chance(3) {
				continue
			}
			g.b.WriteString(`<` + ref + ` idx="` + g.pick("0", "1", "2", "3", "-99999999999", "77", "minor") + `">`)
			if !g.chance(5) {
				g.colorEl()
			}
			g.b.WriteString(`</` + ref + `>`)
		}
		g.b.WriteString(`</wps:style>`)
	}
	switch g.rng.IntN(4) {
	case 0, 1:
		g.b.WriteString(`<wps:txbx`)
		if g.chance(2) {
			g.b.WriteString(` id="` + g.pick("1", "2", " 3 ", "x") + `"`)
		}
		g.b.WriteString(`><w:txbxContent>`)
		if g.txbxDepth < 5 {
			g.txbxDepth++
			g.blocks(2, 1+g.rng.IntN(2))
			g.txbxDepth--
		}
		g.b.WriteString(`</w:txbxContent></wps:txbx>`)
	case 2:
		g.b.WriteString(`<wps:linkedTxbx id="` + g.pick("1", "2", "x") + `" seq="` + g.pick("1", "2", "0", "1") + `"/>`)
	}
	if g.chance(2) {
		g.b.WriteString(`<wps:bodyPr`)
		for _, a := range []struct{ n, v string }{{"vert", g.pick("horz", "vert", "vert270", "wordArtVert", "eaVert", "x")}, {"anchor", g.pick("t", "ctr", "b", "dist", "just")},
			{"lIns", g.pick("91440", "0", "-5", "99999999999", "x")}, {"tIns", g.pick("45720", "100")}, {"rIns", g.pick("91440", "1")}, {"bIns", g.pick("45720")}} {
			if g.chance(2) {
				g.b.WriteString(` ` + a.n + `="` + a.v + `"`)
			}
		}
		g.b.WriteString(`>`)
		if g.chance(3) {
			g.b.WriteString(`<a:prstTxWarp prst="` + g.pick("textArchUp", "textWave1", "textNoShape", "bogus") + `"><a:avLst><a:gd name="` + g.pick("adj", "adj1", "", "a b", strings.Repeat("n", 20)) + `" fmla="` + g.pick("val 10800000", "val -999999999999", "val x") + `"/><a:gd name="adj" fmla="val 5"/>`)
			for i := range g.rng.IntN(12) {
				g.b.WriteString(`<a:gd name="g` + strconv.Itoa(i) + `" fmla="val ` + strconv.Itoa(i) + `"/>`)
			}
			g.b.WriteString(`</a:avLst></a:prstTxWarp>`)
		}
		if g.chance(3) {
			g.b.WriteString(`<a:spAutoFit/>`)
		}
		g.b.WriteString(`</wps:bodyPr>`)
	}
	g.b.WriteString(`</wps:wsp>`)
}

// sdtOpen starts a content control (w:sdt with random properties and an end-properties
// element) up to its w:sdtContent; sdtClose ends it. Sometimes the content is missing.
func (g *docxGen) sdtOpen() {
	g.open("w:sdt")
	if !g.chance(6) {
		g.open("w:sdtPr")
		for range g.rng.IntN(5) {
			switch g.rng.IntN(20) {
			case 0:
				g.rPr(false, true)
			case 1:
				g.leaf("w:alias", ga{"w:val", g.enum("Title", "", "a\x01b", "c1\u0085\u009fx", strings.Repeat("t", 1100))})
			case 2:
				g.leaf("w:tag", ga{"w:val", g.enum("tag1", "")})
			case 3:
				g.leaf("w:id", ga{"w:val", g.enum(" 12 ", "-5", "99999999999999999999", "x", "7")})
			case 4:
				g.leaf("w:lock", ga{"w:val", g.enum("sdtLocked", "contentLocked", "sdtContentLocked", "unlocked", "bogus")})
			case 5:
				g.b.WriteString(`<w:placeholder><w:docPart w:val="` + g.pick("DefaultPlaceholder_1", "", "\uFFFCx") + `"/></w:placeholder>`)
			case 6, 7:
				g.leaf(g.pick("w:temporary", "w:showingPlcHdr"), ga{"w:val", g.from(onOffPool)}, ga{"w14:val", g.from(onOffPool)})
			case 8:
				g.leaf(g.pick("w:richText", "w:picture", "w15:repeatingSectionItem", "w:citation", "w:equation"))
			case 9:
				g.leaf("w:text", ga{"w:multiLine", g.from(onOffPool)})
			case 10:
				g.b.WriteString(`<w14:checkbox><w14:checked w14:val="` + g.pick(onOffPool...) + `"/><w14:checkedState w14:val="` + g.pick(hexPool...) +
					`" w14:font="` + g.pick("MS Gothic", "") + `"/><w14:uncheckedState w14:val="` + g.pick(append([]string{"FFFC", "fffc"}, hexPool...)...) + `"/></w14:checkbox>`)
			case 11, 12:
				name := g.pick("w:comboBox", "w:dropDownList")
				g.open(name, ga{"w:lastValue", g.enum("a", "")})
				for range g.rng.IntN(4) {
					g.leaf("w:listItem", ga{"w:displayText", g.enum("One", "Two", "")}, ga{"w:value", g.enum("1", "2", "")})
				}
				g.close(name)
			case 13:
				g.open("w:date", ga{"w:fullDate", g.enum("2024-01-01T00:00:00Z", "")})
				g.leaf("w:dateFormat", ga{"w:val", g.enum("M/d/yyyy", "")})
				g.leaf("w:lid", ga{"w:val", g.enum("en-US")})
				g.leaf("w:storeMappedDataAs", ga{"w:val", g.enum("dateTime")})
				g.leaf("w:calendar", ga{"w:val", g.enum("gregorian")})
				g.close("w:date")
			case 14:
				name := g.pick("w:docPartObj", "w:docPartList")
				g.open(name)
				g.leaf("w:docPartGallery", ga{"w:val", g.enum("Table of Contents", "Page Numbers (Bottom of Page)", "")})
				g.leaf("w:docPartCategory", ga{"w:val", g.enum("General")})
				g.leaf("w:docPartUnique", g.onOff()...)
				g.close(name)
			case 15:
				g.b.WriteString(`<w15:repeatingSection><w15:sectionTitle ` + g.pick("w:val", "w15:val") + `="` + g.pick("Item", "") + `"/><w15:doNotAllowInsertDeleteSection w15:val="` + g.pick(onOffPool...) + `"/></w15:repeatingSection>`)
			case 16:
				g.leaf("w:dataBinding", ga{"w:xpath", g.enum("/root/x", strings.Repeat("x", 40_000))}, ga{"w:storeItemID", g.enum("{1}")})
			default:
				g.leaf(g.pick("w15:color", "w15:appearance", "w:bibliography"), ga{"w15:val", g.enum("FF0000", "hidden")})
			}
		}
		g.close("w:sdtPr")
	}
	if g.chance(4) {
		g.b.WriteString(`<w:sdtEndPr><w:rPr><w:b/></w:rPr></w:sdtEndPr>`)
	}
	g.sdtContent = !g.chance(12)
	if g.sdtContent {
		g.b.WriteString(`<w:sdtContent>`)
	}
	g.sdtStack = append(g.sdtStack, g.sdtContent)
}

func (g *docxGen) sdtClose() {
	n := len(g.sdtStack)
	if n > 0 && g.sdtStack[n-1] {
		g.b.WriteString(`</w:sdtContent>`)
	}
	if n > 0 {
		g.sdtStack = g.sdtStack[:n-1]
	}
	g.close("w:sdt")
}

// special writes block patterns random generation rarely makes: a move with both ends, enclosed
// characters whose code runs have sizes, a TOC heading followed by a TOC range.
func (g *docxGen) special() {
	g.n += 10
	sz := func() string { return g.pick("16", "20", "24", "24.01", "24.03", "28", "32", "") }
	rpr := func() string {
		if v := sz(); v != "" {
			return `<w:rPr><w:sz w:val="` + v + `"/></w:rPr>`
		}
		return ""
	}
	switch g.rng.IntN(5) {
	case 3:
		// Linked text boxes: maybe a first box (seq 0, holding the text), repeats, gaps.
		first := g.chance(2)
		id := g.pick("5", "6")
		var seqs []string
		for range 2 + g.rng.IntN(3) {
			seqs = append(seqs, g.pick("1", "2", "3", "1"))
		}
		g.b.WriteString(`<w:p>`)
		if first {
			g.b.WriteString(`<w:r><w:drawing><wp:inline><wp:extent cx="1" cy="1"/><a:graphic><a:graphicData uri="x"><wps:wsp><wps:txbx id="` + id + `"><w:txbxContent><w:p><w:r><w:t>chain</w:t></w:r></w:p></w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`)
		}
		for _, q := range seqs {
			g.b.WriteString(`<w:r><w:drawing><wp:inline><wp:extent cx="1" cy="1"/><a:graphic><a:graphicData uri="x"><wps:wsp><wps:linkedTxbx id="` + id + `" seq="` + q + `"/></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`)
		}
		g.b.WriteString(`</w:p>`)
	case 4:
		// Text boxes nested deeper than the reader follows.
		depth := 3 + g.rng.IntN(4)
		for range depth {
			g.b.WriteString(`<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1" cy="1"/><a:graphic><a:graphicData uri="x"><wps:wsp><wps:txbx><w:txbxContent>`)
		}
		g.b.WriteString(`<w:p><w:r><w:t>deep</w:t></w:r></w:p>`)
		for range depth {
			g.b.WriteString(`</w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`)
		}
	case 0:
		name := g.pick("move1", "move2")
		for _, side := range []string{"From", "To"} {
			if g.chance(6) {
				continue
			}
			id := g.pick("1", "2", "3")
			g.b.WriteString(`<w:p><w:pPr><w:rPr><w:move` + side + ` w:id="9" w:author="Ann"/></w:rPr></w:pPr>`)
			g.b.WriteString(`<w:move` + side + `RangeStart w:id="` + id + `" w:name="` + name + `" w:author="Ann"/>`)
			g.b.WriteString(`<w:move` + side + ` w:id="4" w:author="Ann"><w:r><w:t>moved</w:t></w:r></w:move` + side + `>`)
			if !g.chance(5) {
				g.b.WriteString(`<w:move` + side + `RangeEnd w:id="` + id + `"/>`)
				if g.chance(3) {
					g.b.WriteString(`<w:move` + side + ` w:id="5" w:author="Bob"><w:r><w:t>after</w:t></w:r></w:move` + side + `>`)
				}
			}
			g.b.WriteString(`</w:p>`)
		}
	case 1:
		g.b.WriteString(`<w:p><w:r>` + rpr() + `<w:fldChar w:fldCharType="begin"/></w:r>`)
		for _, piece := range []string{g.pick(" eq \\o\\ac(", "EQ \\o\\ac("), g.pick("○", "□", "字"), ",", g.pick("字", "12", "○"), ")"} {
			g.b.WriteString(`<w:r>` + rpr() + `<w:instrText>` + piece + `</w:instrText></w:r>`)
		}
		if g.chance(2) {
			g.b.WriteString(`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r>` + rpr() + `<w:t>` + g.pick("字", "") + `</w:t></w:r>`)
		}
		g.b.WriteString(`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
	default:
		g.b.WriteString(`<w:p><w:pPr><w:pStyle w:val="TOCHeading"/></w:pPr><w:r><w:t>Contents</w:t></w:r></w:p>`)
		entries := 1 + g.rng.IntN(3)
		for i := range entries {
			g.b.WriteString(`<w:p><w:pPr><w:pStyle w:val="` + g.pick("TOC1", "TOC2", "Normal") + `"/></w:pPr>`)
			if i == 0 {
				g.b.WriteString(`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h </w:instrText></w:r>`)
				if !g.chance(4) {
					g.b.WriteString(`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`)
				}
			}
			g.b.WriteString(`<w:hyperlink w:anchor="_Toc` + strconv.Itoa(i) + `"><w:r><w:rPr><w:rStyle w:val="` + g.pick("Hyperlink", "Other") + `"/></w:rPr><w:t>Entry</w:t></w:r></w:hyperlink>`)
			if i == entries-1 && !g.chance(5) {
				g.b.WriteString(`<w:r><w:fldChar w:fldCharType="end"/></w:r>`)
			}
			g.b.WriteString(`</w:p>`)
		}
	}
}

// part starts a part's XML.
func (g *docxGen) part(limit int) {
	g.b.Reset()
	g.n = 0
	g.limit = limit
	g.b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
}

func (g *docxGen) stylesPart() string {
	g.part(150)
	g.b.WriteString(`<w:styles ` + nsDocs + `>`)
	if g.chance(4) == false {
		g.open("w:docDefaults")
		if g.chance(2) {
			g.open("w:rPrDefault")
			g.rPr(false, false)
			g.close("w:rPrDefault")
		}
		if g.chance(2) {
			g.open("w:pPrDefault")
			g.pPr(true)
			g.close("w:pPrDefault")
		}
		g.close("w:docDefaults")
	}
	if g.chance(3) {
		g.b.WriteString(`<w:latentStyles w:defLockedState="0" w:count="2"><w:lsdException w:name="Normal" w:qFormat="1"/></w:latentStyles>`)
	}
	for _, id := range g.styles {
		g.b.WriteString("<w:style")
		if !g.chance(8) {
			g.attrAlways("w:type", g.pick("paragraph", "character", "table", "numbering", "bogus"))
		}
		if !g.chance(10) {
			g.attrAlways("w:styleId", id)
		}
		if g.chance(4) {
			g.attrAlways("w:default", g.pick("1", "true", "0"))
		}
		if g.chance(4) {
			g.attrAlways("w:customStyle", g.pick("1", "true", "0"))
		}
		g.b.WriteString(">")
		if g.chance(4) == false {
			g.leaf("w:name", ga{"w:val", g.enum(strings.ToLower(id), "heading 1", "toc 2", "index 3", "caption", "heading 10", "annotation text", id, "")})
		}
		for _, r := range []string{"w:basedOn", "w:next", "w:link"} {
			if g.chance(3) {
				g.leaf(r, ga{"w:val", g.styleRef()})
			}
		}
		if g.chance(3) {
			g.leaf("w:uiPriority", ga{"w:val", g.num()})
		}
		for _, f := range []string{"w:qFormat", "w:semiHidden", "w:hidden", "w:locked", "w:unhideWhenUsed"} {
			if g.chance(4) {
				g.leaf(f, g.onOff()...)
			}
		}
		if g.chance(2) {
			g.pPr(true)
		}
		if g.chance(2) {
			g.rPr(false, false)
		}
		if g.chance(3) {
			g.tblPr("w:tblPr")
			for range g.rng.IntN(4) {
				g.open("w:tblStylePr", ga{"w:type", g.enum("wholeTable", "firstRow", "lastRow", "firstCol", "lastCol", "band1Vert", "band2Vert", "band1Horz", "band2Horz", "neCell", "nwCell", "seCell", "swCell")})
				if g.chance(2) {
					g.pPr(true)
				}
				if g.chance(2) {
					g.rPr(false, false)
				}
				if g.chance(2) {
					g.tblPr("w:tblPr")
				}
				if g.chance(2) {
					g.tcPr(false)
				}
				g.close("w:tblStylePr")
			}
			if g.chance(3) {
				g.leaf("w:tblStyleRowBandSize", ga{"w:val", g.num()})
			}
		}
		g.b.WriteString("</w:style>")
	}
	g.b.WriteString(`</w:styles>`)
	return g.b.String()
}

func (g *docxGen) numberingPart() string {
	g.part(150)
	g.b.WriteString(`<w:numbering ` + nsDocs + `>`)
	for i := range g.rng.IntN(4) {
		g.b.WriteString(`<w:abstractNum w:abstractNumId="` + g.pick(strconv.Itoa(i), strconv.Itoa(i), "x", "-1") + `">`)
		if g.chance(2) {
			g.leaf("w:nsid", ga{"w:val", g.from(hexPool)})
		}
		if g.chance(3) {
			g.leaf("w:tmpl", ga{"w:val", g.from(hexPool)})
		}
		if g.chance(3) {
			g.leaf("w:multiLevelType", ga{"w:val", g.enum("singleLevel", "hybridMultilevel", "multilevel")})
		}
		if g.chance(4) {
			g.leaf("w:name", ga{"w:val", g.enum("List", "")})
		}
		if g.chance(5) {
			g.leaf("w:numStyleLink", ga{"w:val", g.styleRef()})
		}
		for range g.rng.IntN(4) {
			g.level()
		}
		g.b.WriteString(`</w:abstractNum>`)
	}
	for range g.rng.IntN(5) {
		g.b.WriteString(`<w:num w:numId="` + g.idRef(&g.nums, "0")() + `">`)
		if !g.chance(6) {
			g.leaf("w:abstractNumId", ga{"w:val", g.enum("0", "1", "2", "3", "9")})
		}
		for range g.rng.IntN(3) {
			g.open("w:lvlOverride", ga{"w:ilvl", g.enum("0", "1", "8", "12", "x")})
			if g.chance(2) {
				g.leaf("w:startOverride", ga{"w:val", g.num()})
			}
			if g.chance(3) {
				g.level()
			}
			g.close("w:lvlOverride")
		}
		g.b.WriteString(`</w:num>`)
	}
	g.b.WriteString(`</w:numbering>`)
	return g.b.String()
}

func (g *docxGen) level() {
	g.open("w:lvl", ga{"w:ilvl", g.enum("0", "1", "2", "8", "9", "x")})
	if g.chance(2) {
		g.leaf("w:start", ga{"w:val", g.num()})
	}
	if g.chance(2) {
		g.leaf("w:numFmt", ga{"w:val", g.enum("decimal", "bullet", "lowerLetter", "upperRoman", "none", "bogus")})
	}
	if g.chance(2) {
		g.leaf("w:lvlText", ga{"w:val", g.enum("%1.", "%1.%2", "•", "", strings.Repeat("%1", 40))})
	}
	if g.chance(3) {
		g.leaf("w:lvlJc", ga{"w:val", g.enum("left", "center", "right", "end")})
	}
	if g.chance(2) {
		g.pPr(true)
	}
	if g.chance(3) {
		g.b.WriteString(`<w:pPr><w:tabs><w:tab w:val="num" w:pos="` + g.num()() + `"/></w:tabs></w:pPr>`)
	}
	if g.chance(3) {
		g.rPr(false, false)
	}
	if g.chance(3) {
		g.leaf("w:suff", ga{"w:val", g.enum("space", "nothing", "tab")})
	}
	if g.chance(4) {
		g.leaf("w:isLgl", g.onOff()...)
	}
	if g.chance(3) {
		g.leaf("w:lvlRestart", ga{"w:val", g.num()})
	}
	if g.chance(4) {
		g.leaf("w:pStyle", ga{"w:val", g.styleRef()})
	}
	g.close("w:lvl")
}

func (g *docxGen) settingsPart() string {
	g.part(80)
	g.b.WriteString(`<w:settings ` + nsDocs + `>`)
	for range g.rng.IntN(10) {
		switch g.rng.IntN(20) {
		case 0:
			g.leaf("w:zoom", ga{"w:percent", g.enum("100", "150", "150%", "5", "9999", "x")}, ga{"w:val", g.enum("fullPage", "bestFit", "textFit", "none")})
		case 1:
			g.leaf(g.pick("w:linkStyles", "w:removePersonalInformation", "w:removeDateAndTime", "w:trackRevisions", "w:doNotTrackMoves", "w:evenAndOddHeaders",
				"w:mirrorMargins", "w:autoHyphenation", "w:doNotHyphenateCaps"), g.onOff()...)
		case 2:
			g.open("w:compat")
			if g.chance(2) {
				g.leaf("w:allowSpaceOfSameStyleInTable", g.onOff()...)
			}
			if g.chance(2) {
				g.leaf("w:doNotUseHTMLParagraphAutoSpacing", g.onOff()...)
			}
			if g.chance(3) {
				g.leaf("w:useFELayout")
			}
			for range g.rng.IntN(3) {
				g.leaf("w:compatSetting", ga{"w:name", g.enum("compatibilityMode", "overrideTableStyleFontSizeAndJustification")},
					ga{"w:uri", g.enum("http://schemas.microsoft.com/office/word", "http://other")}, ga{"w:val", g.enum("15", "14", "11", "5", "200", "x")})
			}
			g.close("w:compat")
		case 3:
			g.leaf(g.pick("w:defaultTabStop", "w:drawingGridHorizontalSpacing", "w:drawingGridVerticalSpacing", "w:hyphenationZone", "w:consecutiveHyphenLimit"), ga{"w:val", g.num()})
		case 4:
			name := g.pick("w:footnotePr", "w:endnotePr")
			g.notePr(name)
		case 5:
			g.leaf("w:documentProtection", ga{"w:edit", g.enum("readOnly", "comments", "trackedChanges", "forms", "none")}, ga{"w:enforcement", g.from(onOffPool)},
				ga{"w:formatting", g.from(onOffPool)}, ga{"w:hashValue", g.enum("abc=", "", strings.Repeat("h", 1100))}, ga{"w:saltValue", g.enum("s=")},
				ga{"w:algorithmName", g.enum("SHA-512")}, ga{"w:spinCount", g.num()})
		case 6:
			g.leaf("w:attachedTemplate", ga{"r:id", g.enum("rIdT1", "rIdT2", "rIdT9")})
		case 7:
			g.leaf("w:clrSchemeMapping", ga{"w:bg1", g.enum("light1", "dark1", "bogus")}, ga{"w:t1", g.enum("dark1", "light1")}, ga{"w:accent1", g.enum("accent2", "hyperlink")},
				ga{"w:hyperlink", g.enum("accent1")})
		case 8:
			g.b.WriteString(`<w:rsids><w:rsidRoot w:val="00A1B2C3"/></w:rsids>`)
		case 9:
			g.b.WriteString(`<m:mathPr><m:mathFont m:val="Cambria Math"/>`)
			for _, c := range []struct{ name, pool string }{
				{"m:brkBin", "after|repeat|before|x|After"}, {"m:brkBinSub", "-+|+-|--|x"},
				{"m:wrapIndent", "1440|0|-5|99999999|x|1in|1e400"}, {"m:wrapRight", " off |0|1|true|FALSE|False|on|"},
			} {
				if g.chance(2) {
					g.b.WriteString(`<` + c.name)
					if !g.chance(6) {
						g.b.WriteString(` m:val="` + xmlEsc(g.pick(strings.Split(c.pool, "|")...)) + `"`)
					}
					g.b.WriteString(`/>`)
				}
			}
			g.b.WriteString(`</m:mathPr>`)
		default:
			g.leaf(g.pick("w:proofState", "w:characterSpacingControl", "w:displayBackgroundShape"), ga{"w:val", g.enum("doNotCompress", "clean")})
		}
	}
	g.b.WriteString(`</w:settings>`)
	return g.b.String()
}

func (g *docxGen) themePart() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="` + g.pick("Office Theme", "") + `"><a:themeElements>`)
	if !g.chance(4) {
		b.WriteString(`<a:clrScheme name="x">`)
		for _, n := range themeSlots {
			if g.chance(5) {
				continue
			}
			if g.chance(3) {
				b.WriteString(`<a:` + n + `><a:sysClr val="windowText" lastClr="` + g.pick(colorPool...) + `"/></a:` + n + `>`)
			} else {
				b.WriteString(`<a:` + n + `><a:srgbClr val="` + g.pick(colorPool...) + `"/></a:` + n + `>`)
			}
		}
		b.WriteString(`</a:clrScheme>`)
	}
	if !g.chance(4) {
		b.WriteString(`<a:fontScheme name="x">`)
		for _, n := range []string{"a:majorFont", "a:minorFont"} {
			b.WriteString(`<` + n + `><a:latin typeface="` + g.pick("Aptos", "Calibri Light", "") + `"/><a:ea typeface=""/><a:cs typeface="` + g.pick("Arial", "", "Times New Roman") + `"/></` + n + `>`)
		}
		b.WriteString(`</a:fontScheme>`)
	}
	b.WriteString(`</a:themeElements></a:theme>`)
	return b.String()
}

func (g *docxGen) notesPart(foot bool) string {
	tag, ref := "w:endnote", "w:endnoteRef"
	if foot {
		tag, ref = "w:footnote", "w:footnoteRef"
	}
	g.part(120)
	g.b.WriteString(`<` + tag + `s ` + nsDocs + `>`)
	for range g.rng.IntN(5) {
		t := g.pick("separator", "continuationSeparator", "continuationNotice")
		if g.chance(4) {
			g.b.WriteString(`<` + tag + ` w:type="` + t + `" w:id="-1"/>`)
		} else {
			g.b.WriteString(`<` + tag + ` w:type="` + t + `" w:id="-1"><w:p><w:r><w:` + g.pick("separator", "continuationSeparator") + `/><w:t>` + g.pick("a", "b") + `</w:t></w:r></w:p></` + tag + `>`)
		}
	}
	for _, id := range g.notes {
		g.b.WriteString(`<` + tag + ` w:id="` + id + `"`)
		if g.chance(8) {
			g.attrAlways("w:type", g.pick("normal", "bogus"))
		}
		g.b.WriteString(`><w:p><w:r><w:` + ref[2:] + `/></w:r>`)
		g.inline(1)
		g.b.WriteString(`</w:p></` + tag + `>`)
	}
	g.b.WriteString(`</` + tag + `s>`)
	return g.b.String()
}

func (g *docxGen) commentsPart() string {
	g.part(120)
	g.b.WriteString(`<w:comments ` + nsDocs + `>`)
	for _, id := range g.comments {
		g.b.WriteString(`<w:comment w:id="` + id + `" w:author="` + g.pick("Ann", "") + `" w:initials="A" w:date="2024-01-01T00:00:00Z">`)
		for range 1 + g.rng.IntN(2) {
			g.b.WriteString(`<w:p w14:paraId="` + g.pick("0000000"+id, "AAAA", "BBBB") + `">`)
			g.inline(1)
			g.b.WriteString(`</w:p>`)
		}
		g.b.WriteString(`</w:comment>`)
	}
	g.b.WriteString(`</w:comments>`)
	return g.b.String()
}

func (g *docxGen) commentsExtra(kind string) string {
	var b strings.Builder
	switch kind {
	case "ex":
		b.WriteString(`<w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml">`)
		for _, id := range g.comments {
			b.WriteString(`<w15:commentEx w15:paraId="` + g.pick("0000000"+id, "AAAA") + `" w15:paraIdParent="` + g.pick("00000000", "AAAA", "BBBB") + `" w15:done="` + g.pick("0", "1", "true") + `"/>`)
		}
		b.WriteString(`</w15:commentsEx>`)
	case "ids":
		b.WriteString(`<w16cid:commentsIds xmlns:w16cid="http://schemas.microsoft.com/office/word/2016/wordml/cid">`)
		for _, id := range g.comments {
			b.WriteString(`<w16cid:commentId w16cid:paraId="` + g.pick("0000000"+id, "AAAA") + `" w16cid:durableId="` + g.pick(hexPool...) + `"/>`)
		}
		b.WriteString(`</w16cid:commentsIds>`)
	default:
		b.WriteString(`<w16cex:commentsExtensible xmlns:w16cex="http://schemas.microsoft.com/office/word/2018/wordml/cex">`)
		for range len(g.comments) {
			b.WriteString(`<w16cex:commentExtensible w16cex:durableId="` + g.pick(hexPool...) + `" w16cex:dateUtc="` + g.pick("2024-01-01T00:00:00Z", "bad date", strings.Repeat("9", 50)) + `"/>`)
		}
		b.WriteString(`</w16cex:commentsExtensible>`)
	}
	return b.String()
}

func (g *docxGen) hf(footer bool) string {
	tag := "w:hdr"
	if footer {
		tag = "w:ftr"
	}
	g.part(80)
	g.b.WriteString(`<` + tag + ` ` + nsDocs + `>`)
	g.blocks(1, 1+g.rng.IntN(3))
	if g.chance(3) {
		// A picture in the header: often Word's watermark.
		g.b.WriteString(`<w:p><w:r>`)
		g.pict()
		g.b.WriteString(`</w:r></w:p>`)
	}
	if g.chance(5) {
		g.b.WriteString(`<w:p><w:pPr>`)
		g.sectPr()
		g.b.WriteString(`</w:pPr></w:p>`)
	}
	if g.chain || g.chance(2) {
		g.b.WriteString(`<w:p><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rIdH"/><w:footerReference w:type="first" r:id="rIdH"/></w:sectPr></w:pPr></w:p>`)
	}
	g.b.WriteString(`</` + tag + `>`)
	return g.b.String()
}

type relSpec struct{ id, kind, target, mode string }

func relsXML(rels []relSpec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range rels {
		b.WriteString(`<Relationship Id="` + xmlEsc(r.id) + `" Type="` + xmlEsc(r.kind) + `" Target="` + xmlEsc(r.target) + `"`)
		if r.mode != "" {
			b.WriteString(` TargetMode="` + r.mode + `"`)
		}
		b.WriteString(`/>`)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

// randomDocx is a random package.
func randomDocx(rng *rand.Rand) []byte {
	g := &docxGen{rng: rng, exotic: 40}
	files := map[string]string{}
	var docRels, rootRels []relSpec
	main := "word/document.xml"
	if g.chance(15) {
		main = g.pick("word/main.xml", "Word/Document.xml", "document.xml")
	}
	if !g.chance(30) {
		rootRels = append(rootRels, relSpec{"rId1", relBase + "officeDocument", "/" + main, ""})
	}
	add := func(kind, target string) string {
		id := "rId" + strconv.Itoa(len(docRels)+10)
		docRels = append(docRels, relSpec{id, relBase + kind, target, ""})
		return id
	}
	for i := range g.rng.IntN(8) {
		g.styles = append(g.styles, g.pick("Normal", "Heading1", "Heading2", "Title", "Hyperlink", "TOCHeading", "TOC1", "TableGrid", "ListPara", "Custom"+strconv.Itoa(i), "Default"))
	}
	for i := range g.rng.IntN(4) {
		g.nums = append(g.nums, strconv.Itoa(i+1))
	}
	for i := range g.rng.IntN(4) {
		g.comments = append(g.comments, g.pick(strconv.Itoa(i), strconv.Itoa(i), "x", "+"+strconv.Itoa(max(i-1, 0)), "0"+strconv.Itoa(max(i-1, 0))))
	}
	for i := range g.rng.IntN(4) {
		g.notes = append(g.notes, g.pick(strconv.Itoa(i+1), strconv.Itoa(i+1), "-1", "abc"))
	}
	for range g.rng.IntN(3) {
		g.bookmarks = append(g.bookmarks, strconv.Itoa(g.rng.IntN(5)))
	}
	// hyperlinks (external), and an internal one
	for range g.rng.IntN(3) {
		id := "rIdL" + strconv.Itoa(len(g.rels))
		docRels = append(docRels, relSpec{id, relBase + "hyperlink", g.pick("http://example.com/", " https://x.org/a?b=1 ", "", "mailto:a@b.c"), "External"})
		g.rels = append(g.rels, id)
	}
	if g.chance(3) {
		files["word/styles.xml"] = g.stylesPart()
		add("styles", "styles.xml")
	}
	if g.chance(3) {
		files["word/numbering.xml"] = g.numberingPart()
		add("numbering", "numbering.xml")
	}
	var settingsRels []relSpec
	if g.chance(2) {
		files["word/settings.xml"] = g.settingsPart()
		add("settings", "settings.xml")
		settingsRels = append(settingsRels, relSpec{"rIdT1", relBase + "attachedTemplate", g.pick("file:///C:/T.dotx", "  ", "Normal.dotm"), g.pick("External", "")})
		files["word/_rels/settings.xml.rels"] = relsXML(settingsRels)
	}
	if g.chance(3) {
		files["word/theme/theme1.xml"] = g.themePart()
		add("theme", "theme/theme1.xml")
	}
	if g.chance(3) {
		files["word/fontTable.xml"] = `<?xml version="1.0"?><w:fonts ` + nsDocs + `><w:font w:name="Times New Roman"><w:family w:val="roman"/></w:font><w:font w:name="` + g.pick("Arial", "Cambria", "") + `"><w:family w:val="` + g.pick("swiss", "roman") + `"/></w:font><w:font w:name="Calibri"/></w:fonts>`
		add("fontTable", "fontTable.xml")
		if g.chance(2) {
			files["word/_rels/fontTable.xml.rels"] = relsXML([]relSpec{{"rIdF1", relBase + "font", "fonts/font1.odttf", ""}, {"rIdF2", relBase + "font", "fonts/missing.odttf", ""},
				{"rIdF1", relBase + "font", "fonts/font2.odttf", ""}, {"", relBase + "font", "fonts/font1.odttf", ""}, {"rIdF3", "", "fonts/font1.odttf", ""},
				{"rIdF4", relBase + "font", "a\tb", ""}, {"rIdF5", relBase + "hyperlink", "http://x", "External"}, {"rIdF6", relBase + "font", "../fontTable.xml", ""},
				{"rIdF7", relBase + "font", "../_rels/document.xml.rels", ""}})
			files["word/fonts/font2.odttf"] = "\x00\x02font"
			files["word/fonts/font1.odttf"] = "\x00\x01font"
		}
	}
	if len(g.notes) > 0 && g.chance(2) {
		files["word/footnotes.xml"] = g.notesPart(true)
		add("footnotes", "footnotes.xml")
	}
	if len(g.notes) > 0 && g.chance(3) {
		files["word/endnotes.xml"] = g.notesPart(false)
		add("endnotes", "endnotes.xml")
	}
	if len(g.comments) > 0 && g.chance(2) {
		files["word/comments.xml"] = g.commentsPart()
		add("comments", "comments.xml")
		if g.chance(2) {
			files["word/commentsExtended.xml"] = g.commentsExtra("ex")
			docRels = append(docRels, relSpec{"rIdCx", "http://schemas.microsoft.com/office/2011/relationships/commentsExtended", "commentsExtended.xml", ""})
		}
		if g.chance(2) {
			files["word/commentsIds.xml"] = g.commentsExtra("ids")
			docRels = append(docRels, relSpec{"rIdCi", "http://schemas.microsoft.com/office/2016/09/relationships/commentsIds", "commentsIds.xml", ""})
		}
		if g.chance(2) {
			files["word/commentsExtensible.xml"] = g.commentsExtra("cex")
			docRels = append(docRels, relSpec{"rIdCe", "http://schemas.microsoft.com/office/2018/08/relationships/commentsExtensible", "commentsExtensible.xml", ""})
		}
	}
	headers := g.rng.IntN(7)
	// A chain of headers each referring to the next, deeper than the reader follows.
	chain := g.chance(6)
	if chain {
		headers = 6
	}
	g.chain = chain
	for i := range headers {
		footer := g.chance(2)
		kind, name := "header", "header"
		if footer {
			kind, name = "footer", "footer"
		}
		target := name + strconv.Itoa(i+1) + ".xml"
		id := add(kind, target)
		g.hfRels = append(g.hfRels, id)
		if chain || !g.chance(8) {
			files["word/"+target] = g.hf(footer)
		}
		if chain || g.chance(3) {
			next := name + strconv.Itoa(i+2) + ".xml"
			if !chain && g.chance(4) {
				next = target
			}
			files["word/_rels/"+target+".rels"] = relsXML([]relSpec{{"rIdH", relBase + kind, next, ""}})
		}
	}
	if g.chance(4) {
		add("webSettings", "webSettings.xml")
		files["word/webSettings.xml"] = `<?xml version="1.0"?><w:webSettings ` + nsW + `/>`
	}
	for range g.rng.IntN(4) {
		name := g.pick("media/image1.png", "media/.png", "media/üü", "media/a b.png", "media/noext", "media/x.y.z", "img/image1.png", "media/image1_2.png", "../image1.png")
		g.images = append(g.images, add("image", name))
		files["word/"+strings.TrimPrefix(name, "../")] = "\x89PNG fake"
	}
	if g.chance(3) {
		// OLE objects' embeddings: a file, a package with a part of its own, a missing part, a link.
		g.oles = append(g.oles, add("oleObject", "embeddings/oleObject1.bin"))
		files["word/embeddings/oleObject1.bin"] = "\xd0\xcf\x11\xe0ole"
		if g.chance(2) {
			g.oles = append(g.oles, add("package", "embeddings/Sheet1.xlsx"))
			files["word/embeddings/Sheet1.xlsx"] = "PK sheet"
			files["word/embeddings/_rels/Sheet1.xlsx.rels"] = relsXML([]relSpec{{"rIdE1", relBase + "image", "../media/preview.emf", ""}, {"rIdE2", relBase + "x", "../media/gone.bin", ""}})
			files["word/media/preview.emf"] = "emf"
		}
		if g.chance(3) {
			g.oles = append(g.oles, add("oleObject", "embeddings/missing.bin"))
		}
		if g.chance(3) {
			id := "rId" + strconv.Itoa(len(docRels)+10)
			docRels = append(docRels, relSpec{id, relBase + "oleObject", "file:///C:/data.xlsx", "External"})
			g.oles = append(g.oles, id)
		}
		if g.chance(4) {
			for i := range 65 {
				id := "rIdL" + strconv.Itoa(i)
				docRels = append(docRels, relSpec{id, relBase + "hyperlink", "https://example.com/" + strconv.Itoa(i), "External"})
				g.links = append(g.links, id)
			}
		}
	}
	if g.chance(4) {
		// A macro project and the parts it relates to.
		add2 := func(kind, target string) {
			docRels = append(docRels, relSpec{"rIdV" + strconv.Itoa(len(docRels)), kind, target, ""})
		}
		project := g.pick("vbaProject.bin", "vbaProject.bin", "missing.bin", "empty.bin", "macros/proj.bin")
		add2("http://schemas.microsoft.com/office/2006/relationships/vbaProject", project)
		files["word/vbaProject.bin"] = "\xd0\xcf\x11\xe0vba"
		files["word/macros/proj.bin"] = "\xd0\xcf\x11\xe0proj"
		// A project elsewhere relates to itself and to the usual project part.
		files["word/macros/_rels/proj.bin.rels"] = relsXML([]relSpec{{"rIdP1", relBase + "x", "proj.bin", ""}, {"rIdP2", relBase + "x", "../vbaProject.bin", ""},
			{"rIdP3", relBase + "x", "../vbaData.xml", ""}})
		files["word/empty.bin"] = ""
		var vrels []relSpec
		for i, t := range []string{"vbaData.xml", "../customXml/sig.xml", "vbaProjectSignature.bin", "missing.xml", "VBADATA.xml", "_rels/x.rels", "vbaProject.bin", "", "a\tb", "/WORD/vbaData.xml"} {
			if g.chance(2) {
				kind := g.pick("http://schemas.microsoft.com/office/2006/relationships/wordVbaData", "http://schemas.microsoft.com/office/2006/relationships/vbaProjectSignature", "", "a\tb")
				vrels = append(vrels, relSpec{"rIdW" + strconv.Itoa(i), kind, t, g.pick("", "", "External")})
			}
		}
		if g.chance(3) {
			// An external target that would name a part.
			vrels = append(vrels, relSpec{"rIdWx", relBase + "x", "word/vbaData.xml", "External"})
		}
		if g.chance(10) {
			for i := range 70 {
				vrels = append(vrels, relSpec{"rIdM" + strconv.Itoa(i), "http://example.com/vbaPart", "vbaPart" + strconv.Itoa(i) + ".bin", ""})
				files["word/vbaPart"+strconv.Itoa(i)+".bin"] = "p"
			}
		}
		files["word/_rels/vbaProject.bin.rels"] = relsXML(vrels)
		files["word/vbaData.xml"] = `<wne:vbaSuppData xmlns:wne="http://schemas.microsoft.com/office/word/2006/wordml"/>`
		files["word/vbaProjectSignature.bin"] = "sig"
		files["customXml/sig.xml"] = "<x/>"
	}
	if g.chance(g.exotic) {
		add("customXml", "../customXml/item1.xml")
		files["customXml/item1.xml"] = `<b:Sources xmlns:b="http://schemas.openxmlformats.org/officeDocument/2006/bibliography"/>`
	}
	if g.chance(g.exotic) {
		add("glossaryDocument", "glossary/document.xml")
	}
	// the main part
	g.part(250)
	g.b.WriteString(`<w:document ` + nsDocs + `>`)
	if g.chance(5) {
		g.b.WriteString(`<w:background w:color="` + g.pick(colorPool...) + `">`)
		if g.chance(2) {
			g.b.WriteString(`<v:background><v:fill r:id="` + g.pick("rId10", "rId11", "rId12", "rId13", "rId14") + `" type="tile" o:title="` + g.pick("Paper", strings.Repeat("t", 300)) + `"/></v:background>`)
		}
		g.b.WriteString(`</w:background>`)
	}
	if !g.chance(20) {
		g.b.WriteString(`<w:body>`)
		g.blocks(0, 1+g.rng.IntN(8))
		if g.chance(2) {
			g.sectPr()
		} else if g.chain && len(g.hfRels) > 0 {
			g.b.WriteString(`<w:sectPr><w:headerReference w:type="default" r:id="` + g.hfRels[0] + `"/><w:footerReference w:type="default" r:id="` + g.hfRels[0] + `"/></w:sectPr>`)
		}
		g.b.WriteString(`</w:body>`)
	}
	g.b.WriteString(`</w:document>`)
	files[main] = g.b.String()
	if g.chance(30) {
		files[main] = `<?xml version="1.0"?><w:notDocument ` + nsW + `/>`
	}
	if g.chance(40) {
		files[main] = `<w:document ` + nsW + `><w:body><w:p></w:body>`
	}
	dir := main[:max(strings.LastIndexByte(main, '/'), 0)]
	relsPath := "_rels/" + main[strings.LastIndexByte(main, '/')+1:] + ".rels"
	if dir != "" {
		relsPath = dir + "/" + relsPath
	}
	// rels are relative to the main part's folder
	for i := range docRels {
		if docRels[i].mode == "" && !strings.HasPrefix(docRels[i].target, "/") && dir != "word" && !strings.HasPrefix(docRels[i].target, "../") {
			docRels[i].target = "/word/" + docRels[i].target
		}
	}
	files[relsPath] = relsXML(docRels)
	if g.chance(2) {
		files["docProps/core.xml"] = `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"><dc:title> ` +
			textEsc(g.pick(textPool...)) + ` </dc:title><dc:creator>Ann</dc:creator><cp:revision>` + g.pick(numPool...) + `</cp:revision><dcterms:created>2024-01-01T00:00:00Z</dcterms:created><cp:category>c</cp:category><cp:keywords>k</cp:keywords></cp:coreProperties>`
		rootRels = append(rootRels, relSpec{"rId2", "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties", "docProps/core.xml", ""})
	}
	if g.chance(2) {
		files["docProps/app.xml"] = `<?xml version="1.0"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Template>Normal.dotm</Template><Pages>3</Pages><Company>` +
			textEsc(g.pick("Acme", "a\tb", "", "x\ny")) + `</Company><Manager>M</Manager></Properties>`
		rootRels = append(rootRels, relSpec{"rId3", relBase + "extended-properties", "docProps/app.xml", ""})
	}
	if g.chance(4) {
		files["docProps/thumbnail.jpeg"] = "\xff\xd8thumb"
		rootRels = append(rootRels, relSpec{"rId4", "http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail", "docProps/thumbnail.jpeg", ""})
	}
	if g.chance(3) {
		files["docProps/custom.xml"] = g.customProps()
		rootRels = append(rootRels, relSpec{"rId5", relBase + "custom-properties", "docProps/custom.xml", ""})
	}
	files["_rels/.rels"] = relsXML(rootRels)
	ct := `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`
	if g.chance(2) {
		ct += `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/fonts/font1.odttf" ContentType="application/vnd.openxmlformats-officedocument.obfuscatedFont"/>`
	}
	files["[Content_Types].xml"] = ct + `</Types>`
	if g.chance(60) {
		return []byte(g.pick("not a zip", "", "PK\x03\x04junk"))
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sortStrings(names)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			return nil
		}
		_, _ = w.Write([]byte(files[n]))
	}
	_ = zw.Close()
	return out.Bytes()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
