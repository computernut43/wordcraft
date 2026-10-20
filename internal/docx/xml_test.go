package docx

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

// Ported from crates/docx/src/xml.rs tests.

func TestUTF16PartsParseAndOddOnesNeverPanic(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-16"?><r:Relationships xmlns:r="urn:x"><r:Relationship Id="rId1" Target="café😀.xml"/></r:Relationships>`
	for _, le := range []bool{true, false} {
		b := []byte{0xFE, 0xFF}
		if le {
			b = []byte{0xFF, 0xFE}
		}
		for _, u := range utf16.Encode([]rune(src)) {
			if le {
				b = append(b, byte(u), byte(u>>8))
			} else {
				b = append(b, byte(u>>8), byte(u))
			}
		}
		root, err := Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := root.Els()[0].Attr("Target"); v != "café😀.xml" {
			t.Fatalf("le %v: %q", le, v)
		}
	}
	// Odd length, unpaired surrogates, a bare BOM: an error or a tree, never a panic.
	for _, b := range [][]byte{{0xFF, 0xFE}, {0xFE, 0xFF, 0x00}, {0xFF, 0xFE, '<', 0, 0x00, 0xD8, 'a', 0, '/', 0, '>', 0}, {0xFF, 0xFE, 0x00, 0xDC, 0x00}} {
		_, _ = Parse(b)
	}
	if s, ok := utf16ToUTF8([]byte{0xFF, 0xFE, 'a', 0, 0x00, 0xD8, 'b'}); !ok || s != "a�" {
		t.Fatalf("%q %v", s, ok)
	}
	if _, ok := utf16ToUTF8([]byte("<a/>")); ok {
		t.Fatal("UTF-8 taken for UTF-16")
	}
}

func TestParsesWithNamespaces(t *testing.T) {
	x := `<?xml version="1.0"?><x:document xmlns:x="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:rr="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><x:p rr:id="r1" x:val="a&amp;b"><x:t xml:space="preserve"> hi &#65;&lt; </x:t></x:p></x:document>`
	e, err := Parse([]byte(x))
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "w:document" {
		t.Fatal(e.Name)
	}
	p := e.Child("w:p")
	if v, _ := p.Attr("r:id"); v != "r1" {
		t.Fatal(v)
	}
	if v, _ := p.Attr("w:val"); v != "a&b" {
		t.Fatal(v)
	}
	tx := p.Child("w:t")
	if tx.Text() != " hi A< " {
		t.Fatalf("%q", tx.Text())
	}
	if v, _ := tx.Attr("xml:space"); v != "preserve" {
		t.Fatal(v)
	}
}

func TestChoiceRequirementsResolveNamespaceAliasesAndRebindings(t *testing.T) {
	x := `<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:pen="http://schemas.microsoft.com/office/word/2010/wordprocessingInk"><mc:Choice Requires="pen"/><mc:Choice xmlns:pen="urn:future" Requires="pen"/><mc:Choice Requires="undeclared"/></mc:AlternateContent>`
	e, err := Parse([]byte(x))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range e.Children("mc:Choice") {
		v, _ := c.Attr("Requires")
		got = append(got, v)
	}
	if strings.Join(got, ",") != "wpi,?,?" {
		t.Fatal(got)
	}
}

func TestDeepNestingIsError(t *testing.T) {
	if _, err := Parse([]byte(strings.Repeat("<a>", 1000))); err == nil {
		t.Fatal("parsed")
	}
}

// A start tag with thousands of xmlns declarations is refused instead of allocated
// (RUSTSEC-2026-0195: unbounded namespace bindings in the resolver).
func TestTooManyNamespaceDeclarationsIsError(t *testing.T) {
	var s strings.Builder
	s.WriteString(`<a xmlns:a0="urn:a0"`)
	for i := 1; i < 10_000; i++ {
		s.WriteString(` xmlns:a` + strconv.Itoa(i) + `="urn:a` + strconv.Itoa(i) + `"`)
	}
	s.WriteString("><b/></a>")
	if _, err := Parse([]byte(s.String())); err == nil {
		t.Fatal("parsed")
	}
}

func TestGarbageIsErrorNotPanic(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("empty parsed")
	}
	if _, err := Parse([]byte("\xff\xfe<")); err == nil {
		t.Fatal("garbage parsed")
	}
	_, _ = Parse([]byte("<a><b></a>"))
	_, _ = Parse([]byte("<a x='1' x='2'/>"))
}

func TestEscapes(t *testing.T) {
	if got := Esc("a<b&\"\u0001"); got != "a&lt;b&amp;&quot;" {
		t.Fatal(got)
	}
}

// Go additions.

func TestLossyMatchesRust(t *testing.T) {
	// Rust's from_utf8_lossy: one U+FFFD per maximal invalid subsequence.
	for in, want := range map[string]string{
		"a\xffb":           "a�b",
		"\xe2\x82":         "�",
		"\xe2\x82a":        "�a",
		"\xf0\x90\x80":     "�",
		"\xed\xa0\x80":     "���", // a surrogate: ED A0 isn't a valid start
		"\xc0\xaf":         "��",
		"\xf4\x90\x80\x80": "����",
		"\xe0\x80":         "��",
		"é😀":               "é😀",
	} {
		if got := lossy([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestToXMLAndWriter(t *testing.T) {
	e, err := Parse([]byte(`<w:p xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:u="urn:u" w:a="1&quot;" u:b="x"><u:x/>t&lt;<w:r/></w:p>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.ToXML(); got != `<w:p w:a="1&quot;">t&lt;<w:r/></w:p>` {
		t.Fatal(got)
	}
	if e.Local() != "p" || e.Find("w:r") == nil || e.DeepText() != "t<" {
		t.Fatal("helpers")
	}
	w := NewW()
	w.Open("w:p", Attr{"a", "<"})
	w.Val("w:b", "1")
	w.Leaf("w:t", "x&y")
	w.Close("w:p")
	if got := string(w.Bytes()); !strings.HasSuffix(got, `<w:p a="&lt;"><w:b w:val="1"/><w:t>x&amp;y</w:t></w:p>`) {
		t.Fatal(got)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`<a xmlns="urn:x" b='&amp;'><![CDATA[x]]><!--c--><?p?>t&#65;</a>`))
	f.Add([]byte("<!DOCTYPE a [<!ENTITY e 'x>'>]><a/>"))
	f.Add([]byte{0xFF, 0xFE, '<', 0, 'a', 0, '/', 0, '>', 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		e, err := Parse(b)
		if err != nil {
			return
		}
		// What the tree writes back parses again, when its names are XML names. (Like Rust's
		// to_xml, ToXML writes names as they were read, even ones that aren't.)
		if namesOK(e) {
			if _, err := Parse([]byte(NewW().S.String() + e.ToXML())); err != nil {
				t.Fatalf("ToXML of %q doesn't parse: %v", b, err)
			}
		}
	})
}

func namesOK(e *El) bool {
	name := func(s string) bool {
		if s == "" || strings.HasPrefix(s, "?:") {
			return false
		}
		for _, c := range s {
			if !(c == ':' || c == '-' || c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80 && c != 0xFFFD) {
				return false
			}
		}
		return true
	}
	if !name(e.Name) {
		return false
	}
	for _, a := range e.Attrs {
		if !name(a.Key) {
			return false
		}
	}
	for _, k := range e.Kids {
		if k.El != nil && !namesOK(k.El) {
			return false
		}
	}
	return true
}
