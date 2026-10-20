package docx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// EnvXMLRust names the reference program built from testdata/xml-oracle, and EnvXMLCases how
// many inputs TestXMLMatchesRust compares (default 20,000).
const (
	EnvXMLRust  = "WORDCRAFT_XML_RUST"
	EnvXMLCases = "WORDCRAFT_XML_CASES"
)

// TestXMLMatchesRust parses random inputs (well-formed documents, documents with every kind of
// mistake, and byte-level mutations of both) with this package and with crates/docx/src/xml.rs
// and compares the trees, and that both fail on the same inputs.
func TestXMLMatchesRust(t *testing.T) {
	bin := os.Getenv(EnvXMLRust)
	if bin == "" {
		t.Skipf("set %s to testdata/xml-oracle's release binary to compare with Rust", EnvXMLRust)
	}
	if !filepath.IsAbs(bin) {
		t.Fatalf("%s must be absolute: %q", EnvXMLRust, bin)
	}
	n := 20000
	if v := os.Getenv(EnvXMLCases); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("%s=%q: want a positive count", EnvXMLCases, v)
		}
	}
	rng := rand.New(rand.NewPCG(11, 3))
	inputs := make([][]byte, n)
	for i := range inputs {
		inputs[i] = randomXML(rng)
	}
	answers := askXML(t, bin, inputs)
	failures, oks := 0, 0
	for i, in := range inputs {
		got := goTree(in)
		var a, b any
		if err := json.Unmarshal([]byte(answers[i]), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(got, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			failures++
			if failures <= 5 {
				t.Errorf("input %d %q:\nrust: %s\ngo:   %s", i, in, answers[i], got)
			}
			continue
		}
		if !strings.Contains(answers[i], `"error"`) {
			oks++
		}
	}
	t.Logf("%d inputs, %d parsed, %d failed in both, %d differ", n, oks, n-oks-failures, failures)
	if failures > 0 {
		t.Fatalf("%d of %d inputs differ", failures, n)
	}
}

func askXML(t *testing.T, bin string, inputs [][]byte) []string {
	t.Helper()
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
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != len(inputs) {
		t.Fatalf("reference answered %d of %d inputs", len(lines), len(inputs))
	}
	return lines
}

// goTree is the oracle's JSON for this package's parse.
func goTree(in []byte) []byte {
	root, err := Parse(in)
	if err != nil {
		return []byte(`{"error":true}`)
	}
	b, _ := json.Marshal(treeJSON(root))
	return b
}

func treeJSON(e *El) map[string]any {
	attrs := make([][2]string, 0, len(e.Attrs))
	for _, a := range e.Attrs {
		attrs = append(attrs, [2]string{a.Key, a.Val})
	}
	kids := make([]any, 0, len(e.Kids))
	for _, k := range e.Kids {
		if k.El != nil {
			kids = append(kids, treeJSON(k.El))
		} else {
			kids = append(kids, k.Text)
		}
	}
	return map[string]any{"name": e.Name, "attrs": attrs, "kids": kids}
}

// xmlGen writes random XML from a grammar that covers what WordprocessingML producers write and
// the mistakes the tokenizer has to handle the way quick-xml does.
type xmlGen struct {
	rng *rand.Rand
	b   strings.Builder
	// prefixes declared so far (not scoped: some uses will be undeclared, as wanted).
	prefixes []string
}

func (g *xmlGen) pick(xs ...string) string { return xs[g.rng.IntN(len(xs))] }
func (g *xmlGen) chance(n int) bool        { return g.rng.IntN(n) == 0 }

var genURIs = []string{
	"http://schemas.openxmlformats.org/wordprocessingml/2006/main",
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships",
	"http://schemas.openxmlformats.org/markup-compatibility/2006",
	"http://schemas.microsoft.com/office/word/2010/wordprocessingInk",
	"http://schemas.microsoft.com/office/drawing/2010/main",
	"http://purl.oclc.org/ooxml/wordprocessingml/main",
	"http://www.w3.org/XML/1998/namespace",
	"http://www.w3.org/2000/xmlns/",
	"urn:unknown",
	"",
	"a&amp;b",
}

func (g *xmlGen) name() string {
	local := g.pick("p", "r", "t", "document", "body", "Choice", "AlternateContent", "val", "id", "x", "é", "a-b", "_", "")
	switch {
	case g.chance(3) && len(g.prefixes) > 0:
		return g.prefixes[g.rng.IntN(len(g.prefixes))] + ":" + local
	case g.chance(8):
		return g.pick("w", "mc", "xml", "xmlns", "undeclared", "") + ":" + local
	case g.chance(30):
		return local + ":" + g.pick("a", "b:c")
	case g.chance(40):
		return "\xff" + local
	}
	return local
}

func (g *xmlGen) text() string {
	parts := []string{"hello", " ", "\t", "\n", "\r\n", "\r", "é", "😀", "&amp;", "&lt;", "&gt;", "&quot;", "&apos;",
		"&#65;", "&#x42;", "&#X43;", "&#0;", "&#xD800;", "&#1114112;", "&#+5;", "&#;", "&#x;", "&nbsp;", "&unknown;",
		"]]>", ">", "'", `"`}
	var s strings.Builder
	for range g.rng.IntN(4) {
		s.WriteString(parts[g.rng.IntN(len(parts))])
	}
	switch {
	case g.chance(60):
		s.WriteString("&")
	case g.chance(60):
		s.WriteString("\xc3")
	case g.chance(60):
		s.WriteString("&a\tb;")
	}
	return s.String()
}

func (g *xmlGen) attrs() string {
	var s strings.Builder
	for range g.rng.IntN(4) {
		s.WriteString(g.pick(" ", "  ", "\n", "\t", ""))
		switch k := g.rng.IntN(12); {
		case k == 0:
			p := g.pick("w", "mc", "r", "pen", "x", "xml", "xmlns", "")
			uri := genURIs[g.rng.IntN(len(genURIs))]
			if p == "" {
				s.WriteString(`xmlns="` + uri + `"`)
			} else {
				s.WriteString(`xmlns:` + p + `="` + uri + `"`)
				g.prefixes = append(g.prefixes, p)
			}
		case k == 1:
			s.WriteString(`Requires="` + g.pick("pen", "w mc", "undeclared", "pen  x", "a14", strings.Repeat("p ", 70), strings.Repeat("pen ", 64), strings.Repeat("pen ", 65), strings.Repeat("q", 128), strings.Repeat("q", 129)) + `"`)
		case k == 2:
			s.WriteString(g.name() + "=" + g.pick("unquoted", "'single'", `"`, "", " 'x'"))
		case k == 3:
			s.WriteString(g.name())
		case k == 4:
			s.WriteString(g.name() + " = 'spaced'")
		default:
			q := g.pick(`"`, "'")
			v := strings.ReplaceAll(g.text(), q, "")
			s.WriteString(g.name() + "=" + q + v + q)
		}
	}
	return s.String()
}

// requiresValues are mc:Choice/@Requires values around the limits on their size.
var requiresValues = []string{"pen", "q", "pen q", "w mc", "undeclared", "pen  x", "a14", "pen\u00a0x", strings.Repeat("p ", 70),
	strings.Repeat("pen ", 64), strings.Repeat("pen ", 65), strings.Repeat("q", 128), strings.Repeat("q", 129), "pen " + strings.Repeat("q", 129)}

func (g *xmlGen) element(depth int) {
	name := g.name()
	if g.chance(6) {
		name = "mc:Choice"
		g.b.WriteString(`<mc:Choice xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"` +
			` xmlns:pen="http://schemas.microsoft.com/office/word/2010/wordprocessingInk" xmlns:q="http://schemas.microsoft.com/office/drawing/2010/main"` +
			` xmlns:` + strings.Repeat("q", 128) + `="urn:x" xmlns:` + strings.Repeat("q", 129) + `="http://schemas.microsoft.com/office/word/2018/wordml"` +
			` Requires="` + g.pick(requiresValues...) + `"`)
	} else {
		g.b.WriteString("<" + name)
	}
	g.b.WriteString(g.attrs())
	if depth > 4 || g.chance(4) {
		g.b.WriteString(g.pick("/>", " />", "/ >"))
		return
	}
	g.b.WriteString(">")
	for range g.rng.IntN(4) {
		switch k := g.rng.IntN(10); {
		case k < 4:
			g.element(depth + 1)
		case k < 7:
			g.b.WriteString(g.text())
		case k == 7:
			g.b.WriteString(g.pick("<![CDATA[x<y]]>", "<![CDATA[]]>", "<![CDATA[\xff]]>", "<![CDAT[x]]>", "<![]]>"))
		case k == 8:
			g.b.WriteString(g.pick("<!-- c -->", "<!---->", "<!-->", "<!--->", "<!- x -->", "<!-- a--b -->"))
		default:
			g.b.WriteString(g.pick("<?pi x?>", "<?>", "<??>", "<?xml?>"))
		}
	}
	switch {
	case g.chance(20):
		// left open
	case g.chance(20):
		g.b.WriteString("</other>")
	case g.chance(20):
		g.b.WriteString("</" + name + "  >")
	case g.chance(40):
		g.b.WriteString("</>")
	default:
		g.b.WriteString("</" + name + ">")
	}
}

func randomXML(rng *rand.Rand) []byte {
	g := &xmlGen{rng: rng}
	if g.chance(2) {
		g.b.WriteString(g.pick(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`, `<?xml version="1.0"?>`+"\n", "<?xml?>", `<?xml version="1.0" encoding="UTF-16"?>`))
	}
	if g.chance(10) {
		g.b.WriteString(g.pick("<!DOCTYPE w>", "<!doctype w [<!ENTITY e 'x>y'><!-- ] --><?p ]?><!ELEMENT a (b)>]>", "<!DOCTYPE>",
			"<!DOCTYPE a SYSTEM 'x[>'>", "<!DOCTYPE a [<!FOO bar>]>", "<!DOCTYPE a [<!x", "<!DOCTYPE a [<!ATTLIST a b \">\">]>"))
	}
	if g.chance(15) {
		g.b.WriteString(g.pick(" ", "text", "</stray>", "&amp;", "&bad"))
	}
	g.element(0)
	if g.chance(5) {
		g.b.WriteString(g.pick("trailing", "<b/>", "</x>", "&"))
	}
	out := []byte(g.b.String())
	// Byte-level mutations: the tokenizer must agree on broken input too.
	if rng.IntN(3) == 0 {
		for range 1 + rng.IntN(3) {
			if len(out) == 0 {
				break
			}
			i := rng.IntN(len(out))
			switch rng.IntN(4) {
			case 0:
				out = out[:i]
			case 1:
				out[i] = "<>&;'\"/=!?[]- \xff"[rng.IntN(15)]
			case 2:
				out = append(out[:i:i], out[i+1:]...)
			default:
				out = append(out[:i:i], append([]byte{"<>&'\"/"[rng.IntN(6)]}, out[i:]...)...)
			}
		}
	}
	switch rng.IntN(20) {
	case 0:
		out = append([]byte{0xEF, 0xBB, 0xBF}, out...)
	case 1:
		out = append([]byte{0xEF, 0xBB, 0xBF, 0xEF, 0xBB, 0xBF}, out...)
	case 2, 3:
		le := rng.IntN(2) == 0
		units := utf16.Encode([]rune(string(out)))
		b := []byte{0xFE, 0xFF}
		if le {
			b = []byte{0xFF, 0xFE}
		}
		for _, u := range units {
			if le {
				b = append(b, byte(u), byte(u>>8))
			} else {
				b = append(b, byte(u>>8), byte(u))
			}
		}
		if rng.IntN(4) == 0 {
			b = append(b, 0x00, 0xD8, 'x')
		}
		out = b
	}
	return out
}

func TestXMLGeneratorCoversCases(t *testing.T) {
	// The generator must produce both successes and failures in quantity, or the comparison
	// proves little.
	rng := rand.New(rand.NewPCG(11, 3))
	ok := 0
	for range 2000 {
		if _, err := Parse(randomXML(rng)); err == nil {
			ok++
		}
	}
	if ok < 400 || ok > 1800 {
		t.Fatalf("%d of 2000 generated inputs parse", ok)
	}
}
