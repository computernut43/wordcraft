package docx

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// EnvPackageRust names the reference program built from testdata/package-oracle, and
// EnvPackageCases how many archives TestPackageMatchesRust compares (default 5,000).
const (
	EnvPackageRust  = "WORDCRAFT_PACKAGE_RUST"
	EnvPackageCases = "WORDCRAFT_PACKAGE_CASES"
)

type packageCase struct {
	maxPart, maxTotal uint64
	zip               []byte
}

// TestPackageMatchesRust opens random archives (packages as producers write them, with
// repeated, odd and case-varied names, directories, stored and deflated parts, and broken ones:
// bad checksums, unsupported methods, encryption flags, truncation, byte damage) with this
// package and crates/docx/src/package.rs, and compares the parts, each part's relationships and
// content type, and the failures.
func TestPackageMatchesRust(t *testing.T) {
	bin := os.Getenv(EnvPackageRust)
	if bin == "" {
		t.Skipf("set %s to testdata/package-oracle's release binary to compare with Rust", EnvPackageRust)
	}
	if !filepath.IsAbs(bin) {
		t.Fatalf("%s must be absolute: %q", EnvPackageRust, bin)
	}
	n := 5000
	if v := os.Getenv(EnvPackageCases); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			t.Fatalf("%s=%q: want a positive count", EnvPackageCases, v)
		}
	}
	rng := rand.New(rand.NewPCG(5, 8))
	cases := make([]packageCase, n)
	for i := range cases {
		cases[i] = randomPackage(rng)
	}
	answers := askPackages(t, bin, cases)
	failures, opened := 0, 0
	for i, c := range cases {
		got := goPackage(c)
		var a, b any
		if err := json.Unmarshal([]byte(answers[i]), &a); err != nil {
			t.Fatal(err)
		}
		msg := ""
		if m, ok := a.(map[string]any); ok {
			msg, _ = m["msg"].(string)
			delete(m, "msg")
		}
		if err := json.Unmarshal(got, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			failures++
			if failures <= 5 {
				t.Errorf("case %d (limits %d/%d, %d bytes, rust says %q):\nrust: %.2000s\ngo:   %.2000s", i, c.maxPart, c.maxTotal, len(c.zip), msg, answers[i], got)
			}
			continue
		}
		if m, ok := a.(map[string]any); ok && m["error"] == nil {
			opened++
		}
	}
	t.Logf("%d archives, %d opened, %d failed in both, %d differ", n, opened, n-opened-failures, failures)
	if failures > 0 {
		t.Fatalf("%d of %d archives differ", failures, n)
	}
}

func askPackages(t *testing.T, bin string, cases []packageCase) []string {
	t.Helper()
	var in bytes.Buffer
	for _, c := range cases {
		in.Write(binary.LittleEndian.AppendUint64(nil, c.maxPart))
		in.Write(binary.LittleEndian.AppendUint64(nil, c.maxTotal))
		in.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(c.zip))))
		in.Write(c.zip)
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
	if len(lines) != len(cases) {
		t.Fatalf("reference answered %d of %d archives", len(lines), len(cases))
	}
	return lines
}

func fnv(b []byte) string {
	h := uint64(0xcbf29ce484222325)
	for _, x := range b {
		h = (h ^ uint64(x)) * 0x100000001b3
	}
	return fmt.Sprintf("%016x", h)
}

// goPackage is the oracle's JSON for this package's reading.
func goPackage(c packageCase) []byte {
	p, err := OpenPackageLimited(c.zip, c.maxPart, c.maxTotal)
	if err != nil {
		kind := "zip"
		if e, ok := err.(*Error); ok && e.Kind == ErrLimit {
			kind = "limit"
		}
		b, _ := json.Marshal(map[string]any{"error": kind})
		return b
	}
	// The archive's entry names, as the oracle lists them with the zip crate.
	var names []string
	if entries, err := openZip(c.zip); err == nil {
		for _, e := range entries {
			names = append(names, e.name)
		}
	}
	files := make([]any, 0, len(names))
	for _, n := range names {
		if b, ok := p.Get(n); ok {
			files = append(files, []any{n, len(b), fnv(b)})
		} else {
			files = append(files, []any{n, nil, nil})
		}
	}
	rels := map[string]any{}
	cts := map[string]any{}
	ct := ReadContentTypes(p)
	queries := append(append([]string{""}, names...), "word/document.xml", "/WORD/Document.XML", "x.PNG", "noext")
	for _, n := range queries {
		list := make([]any, 0)
		for _, r := range p.Rels(n) {
			list = append(list, []any{r.ID, r.Kind, r.Target, r.External})
		}
		rels[n] = list
		if t, ok := ct.Of(n); ok {
			cts[n] = t
		} else {
			cts[n] = nil
		}
	}
	b, _ := json.Marshal(map[string]any{"files": files, "rels": rels, "ct": cts})
	return b
}

const testRelsNS = `xmlns="http://schemas.openxmlformats.org/package/2006/relationships"`

func randomPackage(rng *rand.Rand) packageCase {
	pick := func(xs ...string) string { return xs[rng.IntN(len(xs))] }
	c := packageCase{maxPart: MaxPart, maxTotal: MaxTotal}
	if rng.IntN(4) == 0 {
		c.maxPart, c.maxTotal = uint64(rng.IntN(3000)), uint64(rng.IntN(6000))
	}
	type entry struct {
		name   string
		data   []byte
		method uint16
		flags  uint16
		extra  []byte
	}
	var entries []entry
	add := func(name, data string) {
		m := zip.Deflate
		if rng.IntN(3) == 0 {
			m = zip.Store
		}
		entries = append(entries, entry{name: name, data: []byte(data), method: m})
	}
	rel := func(id, typ, target string, external bool) string {
		mode := ""
		if external {
			mode = ` TargetMode="` + pick("External", "external", "Internal") + `"`
		}
		return `<Relationship Id="` + id + `" Type="` + typ + `" Target="` + target + `"` + mode + `/>`
	}
	targets := []string{"word/document.xml", "/word/document.xml", "document.xml", "../word/x.xml", "media/a.png#frag", "./styles.xml", "https://example.org/", "a:b.xml", ""}
	types := []string{RelOfficeDoc, RelStyles, "http://purl.oclc.org/ooxml/officeDocument/relationships/styles", RelImage, "odd"}
	var rs bytes.Buffer
	for i := range rng.IntN(5) {
		rs.WriteString(rel("rId"+strconv.Itoa(i), types[rng.IntN(len(types))], targets[rng.IntN(len(targets))], rng.IntN(4) == 0))
	}
	if rng.IntN(5) == 0 {
		rs.WriteString(`<Relationship Target="x"/><Other Id="r" Target="y"/>`)
	}
	add(pick("_rels/.rels", "_rels/.rels", "_RELS/.RELS", "/_rels/.rels"), `<Relationships `+testRelsNS+`>`+rs.String()+`</Relationships>`)
	if rng.IntN(2) == 0 {
		add(pick("word/_rels/document.xml.rels", "word/_rels/document.xml.rels", "word/_rels/Document.xml.rels"), `<Relationships `+testRelsNS+`>`+
			rel("rId1", RelStyles, pick("styles.xml", "../word/styles.xml", "/x/y.xml"), false)+rel("rId2", RelHyperlink, "http://a/b#c", true)+`</Relationships>`)
	}
	if rng.IntN(2) == 0 {
		add("[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
			`<Default Extension="`+pick("xml", "XML", "rels")+`" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>`+
			`<Override PartName="`+pick("/word/document.xml", "/WORD/DOCUMENT.XML", "word/document.xml")+`" ContentType="main"/>`+
			`<Override PartName="/word/document.xml" ContentType="second"/><Default ContentType="nokey"/></Types>`)
	}
	add(pick("word/document.xml", "word/document.xml", "Word/Document.xml", "/word/document.xml"), `<w:document xmlns:w="`+Namespaces[0].URI+`"/>`)
	for range rng.IntN(4) {
		name := pick("word/media/a.png", "word/styles.xml", "word/", "dir/", "word\\x.xml", "café.xml", "x/../y.xml", "word/document.xml", "a.bin", "")
		size := rng.IntN(5000)
		if rng.IntN(10) == 0 {
			size = 70000
		}
		data := bytes.Repeat([]byte{byte('a' + rng.IntN(3))}, size)
		add(name, string(data))
	}
	if rng.IntN(6) == 0 && len(entries) > 0 {
		e := &entries[rng.IntN(len(entries))]
		switch rng.IntN(3) {
		case 0:
			e.method = 12 // bzip2: not supported by either
		case 1:
			e.flags |= 1 // "encrypted"
		default:
			e.flags |= 0x800 // UTF-8 names
		}
	}
	if rng.IntN(8) == 0 {
		entries = append(entries, entry{name: "caf\x82.xml", data: []byte("<a/>"), method: zip.Store})
	}
	if rng.IntN(3) == 0 && len(entries) > 0 {
		e := &entries[rng.IntN(len(entries))]
		e.extra = randomExtra(rng, e.name)
		if rng.IntN(10) == 0 {
			e.method = zipMethodAES
		}
	}
	var buf bytes.Buffer
	if rng.IntN(20) == 0 {
		buf.WriteString("#!prefix junk\n")
	}
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		fh := &zip.FileHeader{Name: e.name, Method: e.method, Flags: e.flags, Extra: e.extra}
		if e.method != zip.Store && e.method != zip.Deflate {
			// Write it stored, then relabel the method in both headers below.
			fh.Method = zip.Store
		}
		w, err := zw.CreateHeader(fh)
		if err != nil {
			continue
		}
		_, _ = w.Write(e.data)
	}
	_ = zw.Close()
	z := buf.Bytes()
	for _, e := range entries {
		if e.method == 12 || e.method == zipMethodAES {
			relabelMethod(z, e.name, e.method)
		}
	}
	switch rng.IntN(9) {
	case 8:
		// An empty archive, possibly after prepended data.
		var e bytes.Buffer
		if rng.IntN(2) == 0 {
			e.WriteString("junk before")
		}
		ew := zip.NewWriter(&e)
		_ = ew.Close()
		z = e.Bytes()
	case 0:
		z = toZip64(rng, z)
	case 1:
		// An archive comment, sometimes holding something that looks like an end record.
		z = withComment(rng, z)
	case 2:
		// Two archives back to back: the later end record wins unless it's broken.
		z = append(append([]byte{}, z...), z...)
	}
	// Damage.
	switch rng.IntN(10) {
	case 0:
		// A bad CRC in the central directory.
		if i := bytes.LastIndex(z, []byte{0x50, 0x4B, 0x01, 0x02}); i >= 0 && i+20 <= len(z) {
			z[i+16] ^= 0xFF
		}
	case 1:
		if len(z) > 0 {
			z = z[:rng.IntN(len(z))]
		}
	case 2:
		for range 1 + rng.IntN(4) {
			if len(z) > 0 {
				z[rng.IntN(len(z))] ^= byte(1 + rng.IntN(255))
			}
		}
	case 3:
		// Damage inside the compressed data of the first entry.
		if len(z) > 60 {
			z[30+rng.IntN(30)] ^= 0x55
		}
	}
	c.zip = z
	return c
}

// relabelMethod sets an entry's compression method in its local and central headers.
func relabelMethod(z []byte, name string, method uint16) {
	for _, sig := range [][]byte{{0x50, 0x4B, 0x03, 0x04}, {0x50, 0x4B, 0x01, 0x02}} {
		nameOff := 30
		methodOff := 8
		if sig[2] == 1 {
			nameOff, methodOff = 46, 10
		}
		for i := 0; ; {
			j := bytes.Index(z[i:], sig)
			if j < 0 {
				break
			}
			i += j
			if i+nameOff <= len(z) && bytes.HasPrefix(z[i+nameOff:], []byte(name)) {
				binary.LittleEndian.PutUint16(z[i+methodOff:], method)
			}
			i += 4
		}
	}
}

// randomExtra is an extra field block with the fields the zip crate parses, valid or not.
func randomExtra(rng *rand.Rand, name string) []byte {
	var b []byte
	field := func(kind uint16, data []byte) {
		b = binary.LittleEndian.AppendUint16(b, kind)
		b = binary.LittleEndian.AppendUint16(b, uint16(len(data)))
		b = append(b, data...)
	}
	for range 1 + rng.IntN(2) {
		switch rng.IntN(9) {
		case 0: // NTFS, valid or not
			d := make([]byte, 32)
			binary.LittleEndian.PutUint16(d[4:], 1)
			binary.LittleEndian.PutUint16(d[6:], 24)
			switch rng.IntN(4) {
			case 0:
				d = d[:30]
			case 1:
				d[4] = 2
			}
			field(0x000a, d)
		case 1: // extended timestamp
			flags := byte(rng.IntN(9))
			n := 1 + 4*rng.IntN(4)
			d := make([]byte, n)
			d[0] = flags
			field(0x5455, d)
		case 2, 3: // Info-ZIP Unicode path
			content := []byte(pick(rng, "unicode-é.xml", "word/document.xml", "\xff.xml", ""))
			crc := crc32.ChecksumIEEE([]byte(name))
			if rng.IntN(3) == 0 {
				crc++
			}
			d := append([]byte{1}, binary.LittleEndian.AppendUint32(nil, crc)...)
			if rng.IntN(8) == 0 {
				d = d[:3]
			}
			field(0x7075, append(d, content...))
		case 4: // Unicode comment (the entries have no comment: its CRC is of "")
			crc := uint32(0)
			if rng.IntN(2) == 0 {
				crc = 7
			}
			field(0x6375, append(append([]byte{1}, binary.LittleEndian.AppendUint32(nil, crc)...), "c"...))
		case 5: // AES
			d := []byte{byte(1 + rng.IntN(2)), 0, 'A', 'E', byte(1 + rng.IntN(3)), 8, 0}
			if rng.IntN(3) == 0 {
				d[2] = 'X'
			}
			field(0x9901, d)
		case 6: // ZIP64 with a wrong length
			field(0x0001, make([]byte, rng.IntN(30)))
		case 7: // unknown
			field(0xCAFE, []byte("xyz"))
		default: // cut short
			b = append(b, 0x0a, 0x00, 0x20)
		}
	}
	return b
}

func pick(rng *rand.Rand, xs ...string) string { return xs[rng.IntN(len(xs))] }

// cdEntry is a central directory entry as written, for rewriting archives.
type cdEntry struct {
	head  []byte // 46 bytes
	name  []byte
	extra []byte
	tail  []byte // comment
}

func parseCD(z []byte) (entries []cdEntry, cdOff, eocd int, ok bool) {
	eocd = bytes.LastIndex(z, []byte{0x50, 0x4B, 0x05, 0x06})
	if eocd < 0 || eocd+22 > len(z) {
		return nil, 0, 0, false
	}
	n := int(binary.LittleEndian.Uint16(z[eocd+10:]))
	cdOff = int(binary.LittleEndian.Uint32(z[eocd+16:]))
	p := cdOff
	for range n {
		if p+46 > len(z) {
			return nil, 0, 0, false
		}
		nl := int(binary.LittleEndian.Uint16(z[p+28:]))
		el := int(binary.LittleEndian.Uint16(z[p+30:]))
		cl := int(binary.LittleEndian.Uint16(z[p+32:]))
		if p+46+nl+el+cl > len(z) {
			return nil, 0, 0, false
		}
		entries = append(entries, cdEntry{
			head:  append([]byte{}, z[p:p+46]...),
			name:  z[p+46 : p+46+nl],
			extra: z[p+46+nl : p+46+nl+el],
			tail:  z[p+46+nl+el : p+46+nl+el+cl],
		})
		p += 46 + nl + el + cl
	}
	return entries, cdOff, eocd, true
}

// toZip64 rewrites an archive's directory in ZIP64 form: ZIP64 extra fields on some entries,
// an end-of-directory record and locator, and the 32-bit end record's fields set to "see ZIP64".
// Some rewrites are left inconsistent on purpose.
func toZip64(rng *rand.Rand, z []byte) []byte {
	entries, cdOff, _, ok := parseCD(z)
	if !ok {
		return z
	}
	out := append([]byte{}, z[:cdOff]...)
	newCD := len(out)
	for _, e := range entries {
		head := e.head
		extra := e.extra
		if rng.IntN(2) == 0 {
			var f []byte
			usize := binary.LittleEndian.Uint32(head[24:])
			csize := binary.LittleEndian.Uint32(head[20:])
			off := binary.LittleEndian.Uint32(head[42:])
			f = binary.LittleEndian.AppendUint64(f, uint64(usize))
			f = binary.LittleEndian.AppendUint64(f, uint64(csize))
			f = binary.LittleEndian.AppendUint64(f, uint64(off))
			binary.LittleEndian.PutUint32(head[24:], 0xFFFFFFFF)
			binary.LittleEndian.PutUint32(head[20:], 0xFFFFFFFF)
			binary.LittleEndian.PutUint32(head[42:], 0xFFFFFFFF)
			ext := binary.LittleEndian.AppendUint16(nil, 1)
			if rng.IntN(6) == 0 {
				// Declared shorter than the three values it must hold: they're read past it.
				ext = binary.LittleEndian.AppendUint16(ext, 16)
			} else {
				ext = binary.LittleEndian.AppendUint16(ext, 24)
			}
			extra = append(append(ext, f...), extra...)
		}
		binary.LittleEndian.PutUint16(head[30:], uint16(len(extra)))
		out = append(out, head...)
		out = append(out, e.name...)
		out = append(out, extra...)
		out = append(out, e.tail...)
	}
	cdSize := len(out) - newCD
	end64 := len(out)
	n := uint64(len(entries))
	rec := []byte{0x50, 0x4B, 0x06, 0x06}
	rec = binary.LittleEndian.AppendUint64(rec, 44)
	rec = binary.LittleEndian.AppendUint16(rec, 45)
	rec = binary.LittleEndian.AppendUint16(rec, 45)
	rec = binary.LittleEndian.AppendUint32(rec, 0)
	rec = binary.LittleEndian.AppendUint32(rec, 0)
	rec = binary.LittleEndian.AppendUint64(rec, n)
	rec = binary.LittleEndian.AppendUint64(rec, n)
	rec = binary.LittleEndian.AppendUint64(rec, uint64(cdSize))
	rec = binary.LittleEndian.AppendUint64(rec, uint64(newCD))
	switch rng.IntN(8) {
	case 0:
		binary.LittleEndian.PutUint64(rec[4:], 40) // record too small
	case 1:
		binary.LittleEndian.PutUint64(rec[24:], n+5) // files on this disk > files
	case 2:
		binary.LittleEndian.PutUint16(rec[14:], 63) // needs a newer version than made it
	case 3:
		binary.LittleEndian.PutUint64(rec[32:], n+1000) // more files than fit
	}
	out = append(out, rec...)
	loc := []byte{0x50, 0x4B, 0x06, 0x07}
	loc = binary.LittleEndian.AppendUint32(loc, 0)
	loc = binary.LittleEndian.AppendUint64(loc, uint64(end64))
	disks := uint32(1)
	if rng.IntN(10) == 0 {
		disks = 3
	}
	loc = binary.LittleEndian.AppendUint32(loc, disks)
	out = append(out, loc...)
	eocd := []byte{0x50, 0x4B, 0x05, 0x06, 0, 0, 0, 0}
	files := uint16(0xFFFF)
	if rng.IntN(3) == 0 {
		files = uint16(n)
	}
	eocd = binary.LittleEndian.AppendUint16(eocd, files)
	eocd = binary.LittleEndian.AppendUint16(eocd, files)
	eocd = binary.LittleEndian.AppendUint32(eocd, 0xFFFFFFFF)
	eocd = binary.LittleEndian.AppendUint32(eocd, 0xFFFFFFFF)
	eocd = binary.LittleEndian.AppendUint16(eocd, 0)
	return append(out, eocd...)
}

// withComment gives the archive a comment, sometimes one with a fake end record inside, or a
// comment length that runs past the end.
func withComment(rng *rand.Rand, z []byte) []byte {
	eocd := bytes.LastIndex(z, []byte{0x50, 0x4B, 0x05, 0x06})
	if eocd < 0 || eocd+22 > len(z) {
		return z
	}
	comment := []byte(pick(rng, "a comment", ""))
	if rng.IntN(2) == 0 {
		fake := append([]byte{}, z[eocd:eocd+22]...)
		binary.LittleEndian.PutUint32(fake[16:], uint32(rng.IntN(len(z)+1)))
		comment = append(comment, fake...)
	}
	out := append([]byte{}, z[:eocd+22]...)
	n := len(comment)
	if rng.IntN(5) == 0 {
		n += 10
	}
	binary.LittleEndian.PutUint16(out[eocd+20:], uint16(n))
	return append(out, comment...)
}
