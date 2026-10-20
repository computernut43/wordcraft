package docx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func zipOf(t testing.TB, entries ...ZipEntry) []byte {
	t.Helper()
	z, err := ZipEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	return z
}

// Ported from crates/docx/src/package.rs tests.

func TestDecompressionBombIsRejected(t *testing.T) {
	z := zipOf(t, ZipEntry{"word/document.xml", bytes.Repeat([]byte{' '}, 2_000_000)})
	if len(z) >= 100_000 {
		t.Fatalf("%d bytes", len(z))
	}
	var e *Error
	if _, err := OpenPackageLimited(z, 1_000_000, 10_000_000); !errors.As(err, &e) || e.Kind != ErrLimit {
		t.Fatalf("part limit: %v", err)
	}
	if _, err := OpenPackageLimited(z, 10_000_000, 1_000_000); !errors.As(err, &e) || e.Kind != ErrLimit {
		t.Fatalf("total limit: %v", err)
	}
	if _, err := OpenPackageLimited(z, 10_000_000, 10_000_000); err != nil {
		t.Fatal(err)
	}
}

func TestLyingDeclaredSizeIsRejectedOrBounded(t *testing.T) {
	z := zipOf(t, ZipEntry{"a.xml", []byte("<a/>")})
	// Patch the uncompressed size in the central directory and local header to ~4 GB.
	patched := 0
	for _, sig := range [][]byte{{0x50, 0x4B, 0x01, 0x02}, {0x50, 0x4B, 0x03, 0x04}} {
		if pos := bytes.Index(z, sig); pos >= 0 {
			off := pos + 22
			if sig[2] == 1 {
				off = pos + 24
			}
			binary.LittleEndian.PutUint32(z[off:], 0xFFFF_FFF0)
			patched++
		}
	}
	if patched != 2 {
		t.Fatal(patched)
	}
	p, err := OpenPackage(z)
	if err == nil {
		if b, ok := p.Get("a.xml"); ok && len(b) >= 1024 {
			t.Fatalf("%d bytes", len(b))
		}
	}
}

func TestLookupsIgnoreSlashAndCase(t *testing.T) {
	z := zipOf(t, ZipEntry{"word/media/pic.png", []byte("\x89PNG picture bytes")}, ZipEntry{"dir/", nil})
	p, err := OpenPackage(z)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := p.Get("/WORD/media/PIC.png"); !ok || string(b) != "\x89PNG picture bytes" {
		t.Fatal("lookup")
	}
	if _, ok := p.Get("dir/"); ok {
		t.Fatal("a directory entry is a part")
	}
}

func TestResolvesTargets(t *testing.T) {
	for _, c := range [][3]string{
		{"word", "media/image1.png", "word/media/image1.png"},
		{"word", "../customXml/item1.xml", "customXml/item1.xml"},
		{"word", "/word/styles.xml", "word/styles.xml"},
		{"", "word/document.xml", "word/document.xml"},
		{"word", "../../../x", "x"},
	} {
		if got := Resolve(c[0], c[1]); got != c[2] {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestRelativeTargetsResolveBack(t *testing.T) {
	for _, c := range [][3]string{
		{"word/document.xml", "word/charts/chart1.xml", "charts/chart1.xml"},
		{"word/charts/chart1.xml", "word/embeddings/book.xlsx", "../embeddings/book.xlsx"},
		{"word/charts/chart1.xml", "word/charts/colors1.xml", "colors1.xml"},
		{"word/document.xml", "customXml/item1.xml", "../customXml/item1.xml"},
		{"word/document.xml", "word/a:b.bin", "./a:b.bin"},
	} {
		r := Relative(c[0], c[1])
		if r != c[2] {
			t.Errorf("Relative(%q, %q) = %q, want %q", c[0], c[1], r, c[2])
		}
		dir := ""
		if i := strings.LastIndexByte(c[0], '/'); i >= 0 {
			dir = c[0][:i]
		}
		if back := Resolve(dir, r); back != c[1] {
			t.Errorf("%q resolves back to %q", r, back)
		}
	}
}

func TestRelsAndContentTypes(t *testing.T) {
	z := zipOf(t,
		ZipEntry{"_rels/.rels", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + RelOfficeDoc + `" Target="word/document.xml"/></Relationships>`)},
		ZipEntry{"word/_rels/document.xml.rels", []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://purl.oclc.org/ooxml/officeDocument/relationships/styles" Target="styles.xml#x"/><Relationship Id="rId2" Type="` + RelHyperlink + `" Target="https://example.org/a#b" TargetMode="external"/></Relationships>`)},
		ZipEntry{"[Content_Types].xml", []byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="XML" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="main"/></Types>`)},
	)
	p, err := OpenPackage(z)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := p.Rels("").ByType(RelOfficeDoc); !ok || r.Target != "word/document.xml" {
		t.Fatal(r, ok)
	}
	rels := p.Rels("word/document.xml")
	if r, ok := rels.ByType(RelStyles); !ok || r.Target != "word/styles.xml" {
		t.Fatal("strict styles relationship", r, ok)
	}
	if r, ok := rels.ByID("rId2"); !ok || !r.External || r.Target != "https://example.org/a#b" {
		t.Fatal(r, ok)
	}
	ct := ReadContentTypes(p)
	if v, _ := ct.Of("/WORD/document.xml"); v != "main" {
		t.Fatal(v)
	}
	if v, _ := ct.Of("word/styles.xml"); v != "application/xml" {
		t.Fatal(v)
	}
	if _, ok := ct.Of("noext"); ok {
		t.Fatal("no extension, no type")
	}
}

func FuzzOpenPackage(f *testing.F) {
	f.Add(zipOf(f, ZipEntry{"_rels/.rels", []byte("<Relationships/>")}, ZipEntry{"word/document.xml", []byte("<w:document/>")}))
	f.Add([]byte("PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := OpenPackageLimited(b, 1<<20, 4<<20)
		if err != nil {
			return
		}
		for _, n := range p.names {
			p.Rels(n)
		}
		ReadContentTypes(p)
	})
}
