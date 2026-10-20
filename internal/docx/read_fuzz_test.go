package docx

import (
	"archive/zip"
	"bytes"
	"math/rand/v2"
	"testing"
)

// FuzzRead reads arbitrary bytes as a package, and arbitrary bytes as the main part of an
// otherwise valid package: never a panic, and a document read is always writable as JSON. The
// seeds are whole generated packages, so run it with -fuzzminimizetime=0: minimizing a 30 KB input
// a byte at a time takes minutes.
func FuzzRead(f *testing.F) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		f.Add(randomDocx(rng), false)
	}
	f.Add([]byte(`<w:document `+nsDocs+`><w:body><w:p><w:r><w:t>x</w:t></w:r></w:p></w:body></w:document>`), true)
	f.Fuzz(func(t *testing.T, b []byte, asMain bool) {
		if asMain {
			var out bytes.Buffer
			zw := zip.NewWriter(&out)
			w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Store})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write(b)
			_ = zw.Close()
			b = out.Bytes()
		}
		d, err := Read(b)
		if err != nil {
			return
		}
		if _, err := d.Marshal(); err != nil {
			t.Fatalf("read a document that can't be written: %v", err)
		}
	})
}
