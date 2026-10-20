package docx

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// vmlFixtures are the hand-written VML snippets of crates/docx's tests (tests/vml_shapes.rs,
// tests/fixtures.rs, tests/text_boxes.rs), each a w:pict's content or whole paragraphs.
//
//go:embed testdata/vml_fixtures.json
var vmlFixtures []byte

// TestVMLFixturesMatchRust reads the Rust tests' VML fixtures with Read and with crates/docx's
// read, each in the body and in a header (where a text watermark is taken out), and compares the
// documents: what those tests assert, checked against the reference itself.
func TestVMLFixturesMatchRust(t *testing.T) {
	bin := os.Getenv(EnvReadRust)
	if bin == "" {
		t.Skipf("set %s to testdata/read-oracle's release binary to compare with Rust", EnvReadRust)
	}
	var snippets []string
	if err := json.Unmarshal(vmlFixtures, &snippets); err != nil {
		t.Fatal(err)
	}
	// Paths at the reader's limits: more numbers than are read (the subpath after them gets none),
	// a path longer than is read, and coordinates past the largest kept, which a huge coordsize
	// keeps apart once scaled.
	big := `<v:shape style="width:100pt;height:80pt" coordsize="999999999999999,1000" path="`
	snippets = append(snippets,
		`<v:shape style="width:100pt;height:80pt" path="m0,0l`+strings.Repeat("1,2,", 25_000)+`e m5,5 l9,9 e"/>`,
		`<v:shape style="width:100pt;height:80pt" path="m0,0l5,5`+strings.Repeat(" ", 1<<20)+`l9,9e"/>`,
		big+`m 999999999999,0 r 999999999999,5 e"/>`,
		big+`m0,0 l 99999999999999999,7 e"/>`)
	var cases [][]byte
	for _, s := range snippets {
		body := s
		if !strings.Contains(s, "<w:p") {
			body = `<w:p><w:r><w:pict>` + s + `</w:pict></w:r></w:p>`
		}
		cases = append(cases, vmlPackage(body, ""), vmlPackage(`<w:p><w:r><w:t>Body</w:t></w:r></w:p>`, body))
	}
	answers := askRead(t, bin, cases)
	compared := 0
	for i, c := range cases {
		got := goRead(c)
		if strings.HasPrefix(got, "REFUSED ") {
			// OLE objects (w:object) show a VML picture but aren't ported yet.
			t.Logf("fixture %d (%s): %s", i/2, []string{"body", "header"}[i%2], got)
			continue
		}
		compared++
		if got != answers[i] {
			a, b := answers[i], got
			at := 0
			for at < len(a) && at < len(b) && a[at] == b[at] {
				at++
			}
			from := max(at-300, 0)
			t.Errorf("fixture %d (%s) differs at byte %d:\nrust: …%.800s\ngo:   …%.800s", i/2, []string{"body", "header"}[i%2], at, a[from:], b[from:])
		}
	}
	if compared < len(cases)-4 {
		t.Fatalf("only %d of %d fixtures compared", compared, len(cases))
	}
}

// vmlPackage is a .docx whose body is body and, when header isn't empty, whose default header
// holds header. The fixtures' picture ids (rId8, rId9, rIdImg, rIdP) are images.
func vmlPackage(body, header string) []byte {
	rels := []relSpec{{"rId8", relBase + "image", "media/image1.png", ""}, {"rId9", relBase + "image", "media/image2.png", ""},
		{"rIdImg", relBase + "image", "media/image1.png", ""}, {"rIdP", relBase + "image", "media/image2.png", ""}}
	files := map[string]string{
		"word/media/image1.png": "\x89PNG\r\n\x1a\nimage1",
		"word/media/image2.png": "\x89PNG\r\n\x1a\nimage2",
	}
	if header != "" {
		rels = append(rels, relSpec{"rIdH", relBase + "header", "header1.xml", ""})
		files["word/header1.xml"] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:hdr ` + nsDocs + `>` + header + `</w:hdr>`
		files["word/_rels/header1.xml.rels"] = relsXML(rels[:4])
		body += `<w:sectPr><w:headerReference w:type="default" r:id="rIdH"/></w:sectPr>`
	}
	files["_rels/.rels"] = relsXML([]relSpec{{"rId1", relBase + "officeDocument", "word/document.xml", ""}})
	files["word/document.xml"] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + nsDocs + `><w:body>` + body + `</w:body></w:document>`
	files["word/_rels/document.xml.rels"] = relsXML(rels)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			return nil
		}
		_, _ = w.Write([]byte(c))
	}
	_ = zw.Close()
	return out.Bytes()
}
