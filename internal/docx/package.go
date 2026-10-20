package docx

// OPC package access: bounded zip reading, relationships, part names (Rust
// crates/docx/src/package.rs).

import (
	"archive/zip"
	"bytes"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// Package limits (Rust MAX_PART, MAX_TOTAL, MAX_ENTRIES).
const (
	// MaxPart is the largest single part decompressed.
	MaxPart = 256 * 1024 * 1024
	// MaxTotal is the largest total of all decompressed parts.
	MaxTotal = 512 * 1024 * 1024
	// MaxEntries is the most zip entries looked at.
	MaxEntries = 20_000
)

// Relationship types.
const (
	RelOfficeDoc          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	RelCore               = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	RelApp                = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"
	RelCustom             = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/custom-properties"
	RelCustomXMLProps     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/customXmlProps"
	RelStyles             = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	RelNumbering          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	RelSettings           = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings"
	RelAttachedTemplate   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/attachedTemplate"
	RelFontTable          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable"
	RelTheme              = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"
	RelFootnotes          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes"
	RelEndnotes           = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes"
	RelComments           = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	RelCommentsEx         = "http://schemas.microsoft.com/office/2011/relationships/commentsExtended"
	RelCommentsIDs        = "http://schemas.microsoft.com/office/2016/09/relationships/commentsIds"
	RelCommentsExtensible = "http://schemas.microsoft.com/office/2018/08/relationships/commentsExtensible"
	RelHeader             = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	RelFooter             = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	RelImage              = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	RelHyperlink          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	RelVBAProject         = "http://schemas.microsoft.com/office/2006/relationships/vbaProject"
	RelChart              = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"
	RelDiagramData        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramData"
	RelDiagramDrawing     = "http://schemas.microsoft.com/office/2007/relationships/diagramDrawing"
	RelDiagramLayout      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramLayout"
	RelDiagramQuickStyle  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramQuickStyle"
	RelDiagramColors      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramColors"
	RelCustomXML          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/customXml"
	RelGlossary           = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/glossaryDocument"
)

// ErrKind classifies package errors (the Rust DocxError variants used here).
type ErrKind int

// Error kinds.
const (
	// ErrZip: the bytes aren't a zip archive this reader can open.
	ErrZip ErrKind = iota + 1
	// ErrLimit: a size or count limit was hit.
	ErrLimit
)

// Error is a package error.
type Error struct {
	Kind ErrKind
	Msg  string
}

func (e *Error) Error() string {
	switch e.Kind {
	case ErrZip:
		return "zip: " + e.Msg
	case ErrLimit:
		return "limit exceeded: " + e.Msg
	}
	return e.Msg
}

// RelIs reports whether relationship type t is full, or ends with the same last segment
// (ignoring the transitional or strict prefix).
func RelIs(t, full string) bool {
	tail := func(s string) string {
		if i := strings.LastIndexByte(s, '/'); i >= 0 {
			return s[i+1:]
		}
		return s
	}
	return t == full || tail(t) == tail(full)
}

// Package holds the decompressed parts of a package, by part name without a leading "/".
type Package struct {
	files map[string][]byte
	// names are the part names in sorted order (Rust keeps them in a BTreeMap), for the
	// case-insensitive fallback.
	names []string
}

// OpenPackage reads a zip package with the default limits.
func OpenPackage(b []byte) (*Package, error) { return OpenPackageLimited(b, MaxPart, MaxTotal) }

// OpenPackageLimited reads a zip package, failing when a part declares or decompresses past
// maxPart bytes or all of them past maxTotal. An entry that can't be read is skipped.
func OpenPackageLimited(b []byte, maxPart, maxTotal uint64) (*Package, error) {
	entries, err := openZip(b)
	if err != nil {
		return nil, &Error{ErrZip, err.Error()}
	}
	if len(entries) > MaxEntries {
		return nil, &Error{ErrLimit, fmt.Sprintf("%d zip entries", len(entries))}
	}
	p := &Package{files: map[string][]byte{}}
	var total uint64
	for i := range entries {
		e := &entries[i]
		// As the zip crate's by_index: an entry that can't be opened is skipped before anything
		// else is looked at.
		if _, err := e.open(b); err != nil {
			slog.Warn("docx: skipping unreadable zip entry", "entry", e.name, "err", err)
			continue
		}
		if strings.HasSuffix(e.name, "/") || strings.HasSuffix(e.name, "\\") {
			continue
		}
		part := strings.TrimLeft(e.name, "/")
		if e.usize > maxPart {
			return nil, &Error{ErrLimit, fmt.Sprintf("part %s declares %d bytes", part, e.usize)}
		}
		room := min(maxPart, maxTotal-min(total, maxTotal))
		data, err := e.read(b, room)
		if err != nil {
			slog.Warn("docx: zip entry unreadable", "part", part, "err", err)
			continue
		}
		if uint64(len(data)) > room {
			return nil, &Error{ErrLimit, fmt.Sprintf("part %s decompresses past the size limit", part)}
		}
		total += uint64(len(data))
		p.files[part] = data
	}
	for n := range p.files {
		p.names = append(p.names, n)
	}
	sort.Strings(p.names)
	return p, nil
}

// cp437high are code page 437's characters 0x80–0xFF.
const cp437high = "ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜ¢£¥₧ƒáíóúñÑªº¿⌐¬½¼¡«»░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀αßΓπΣσµτΦΘΩδ∞φε∩≡±≥≤⌠⌡÷≈°∙·√ⁿ²■ "

var cp437Runes = []rune(cp437high)

func cp437(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x80 {
			b.WriteByte(c)
		} else if int(c-0x80) < len(cp437Runes) {
			b.WriteRune(cp437Runes[c-0x80])
		}
	}
	return b.String()
}

// PackageFromParts is a package from parts already in memory (a Flat OPC file, a converted
// document), by part name without the leading "/".
func PackageFromParts(files map[string][]byte) *Package {
	p := &Package{files: files}
	for n := range files {
		p.names = append(p.names, n)
	}
	sort.Strings(p.names)
	return p
}

// Get is a part's bytes by name (leading "/" ignored; ASCII case-insensitive fallback, the
// first match in name order).
func (p *Package) Get(name string) ([]byte, bool) {
	n := strings.TrimLeft(name, "/")
	if b, ok := p.files[n]; ok {
		return b, true
	}
	for _, k := range p.names {
		if asciiEqualFold(k, n) {
			return p.files[k], true
		}
	}
	return nil, false
}

// asciiEqualFold is Rust's eq_ignore_ascii_case.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		b[i] = lowerASCII(c)
	}
	return string(b)
}

// XML parses a part; nil, nil when there is no such part.
func (p *Package) XML(name string) (*El, error) {
	b, ok := p.Get(name)
	if !ok {
		return nil, nil
	}
	return Parse(b)
}

// Rel is a relationship: an internal target is a package path, an external one a URL.
type Rel struct {
	ID, Kind, Target string
	External         bool
}

// Rels are a part's relationships, in file order.
type Rels []Rel

// ByID is the relationship with id, if any.
func (rs Rels) ByID(id string) (Rel, bool) {
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return Rel{}, false
}

// ByType is the first relationship of type full (see RelIs), if any.
func (rs Rels) ByType(full string) (Rel, bool) {
	for _, r := range rs {
		if RelIs(r.Kind, full) {
			return r, true
		}
	}
	return Rel{}, false
}

// Rels reads part's relationships (dir/_rels/name.rels); "" is the package's own. An unreadable
// relationships part counts as none.
func (p *Package) Rels(part string) Rels {
	dir, file := splitDir(part)
	path := "_rels/" + file + ".rels"
	if dir != "" {
		path = dir + "/" + path
	}
	root, err := p.XML(path)
	if err != nil {
		slog.Warn("docx: ignoring unreadable relationships", "part", path, "err", err)
		return nil
	}
	if root == nil {
		return nil
	}
	var rels Rels
	for _, r := range root.Els() {
		if r.Local() != "Relationship" {
			continue
		}
		id, ok1 := r.Attr("Id")
		target, ok2 := r.Attr("Target")
		if !ok1 || !ok2 {
			continue
		}
		mode, _ := r.Attr("TargetMode")
		external := asciiEqualFold(mode, "External")
		if !external {
			target = Resolve(dir, target)
		}
		kind, _ := r.Attr("Type")
		rels = append(rels, Rel{ID: id, Kind: kind, Target: target, External: external})
	}
	return rels
}

// ContentTypes are [Content_Types].xml: Override by part name, then Default by extension (both
// ASCII case-insensitive; the first entry for a key wins).
type ContentTypes struct {
	overrides, defaults map[string]string
}

// ReadContentTypes reads the package's content types; an unreadable part counts as none.
func ReadContentTypes(p *Package) ContentTypes {
	ct := ContentTypes{overrides: map[string]string{}, defaults: map[string]string{}}
	root, err := p.XML("[Content_Types].xml")
	if err != nil {
		slog.Warn("docx: ignoring unreadable [Content_Types].xml", "err", err)
		return ct
	}
	if root == nil {
		return ct
	}
	for _, e := range root.Els() {
		key, ok := e.Attr("PartName")
		if !ok {
			key, ok = e.Attr("Extension")
		}
		t, ok2 := e.Attr("ContentType")
		if !ok || !ok2 {
			continue
		}
		m := ct.defaults
		if e.Local() == "Override" {
			m = ct.overrides
		}
		k := toLowerASCII(strings.TrimLeft(key, "/"))
		if _, exists := m[k]; !exists {
			m[k] = t
		}
	}
	return ct
}

// Of is the content type of part (a package path; a leading "/" is ignored).
func (ct ContentTypes) Of(part string) (string, bool) {
	part = toLowerASCII(strings.TrimLeft(part, "/"))
	if t, ok := ct.overrides[part]; ok {
		return t, true
	}
	i := strings.LastIndexByte(part, '.')
	if i < 0 {
		return "", false
	}
	t, ok := ct.defaults[part[i+1:]]
	return t, ok
}

func splitDir(part string) (dir, file string) {
	p := strings.TrimLeft(part, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

// Resolve resolves a relative target against a source directory into a package path.
func Resolve(dir, target string) string {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	var parts []string
	if !strings.HasPrefix(target, "/") {
		for _, s := range strings.Split(dir, "/") {
			if s != "" {
				parts = append(parts, s)
			}
		}
	}
	for _, seg := range strings.Split(target, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// Relative is the relative reference from part from to part to (both package paths), as a
// relationship target: word/document.xml → word/charts/chart1.xml is charts/chart1.xml.
func Relative(from, to string) string {
	nonEmpty := func(s string) []string {
		var out []string
		for _, x := range strings.Split(s, "/") {
			if x != "" {
				out = append(out, x)
			}
		}
		return out
	}
	fromDir := nonEmpty(from)
	if len(fromDir) > 0 {
		fromDir = fromDir[:len(fromDir)-1]
	}
	toParts := nonEmpty(to)
	toDir := toParts
	if len(toDir) > 0 {
		toDir = toDir[:len(toDir)-1]
	}
	common := 0
	for common < len(fromDir) && common < len(toDir) && fromDir[common] == toDir[common] {
		common++
	}
	var out []string
	for range len(fromDir) - common {
		out = append(out, "..")
	}
	if common <= len(toParts) {
		out = append(out, toParts[common:]...)
	}
	s := strings.Join(out, "/")
	// A relative reference whose first segment has a colon would read as a URI scheme.
	if first, _, _ := strings.Cut(s, "/"); strings.Contains(first, ":") {
		return "./" + s
	}
	return s
}

// ZipEntry is a part to write.
type ZipEntry struct {
	Name string
	Data []byte
}

// ZipEntries builds a zip from entries: deflated, except already-compressed pictures, stored.
func ZipEntries(entries []ZipEntry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		lower := toLowerASCII(e.Name)
		method := zip.Deflate
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
			if strings.HasSuffix(lower, ext) {
				method = zip.Store
			}
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.Name, Method: method})
		if err != nil {
			return nil, &Error{ErrZip, err.Error()}
		}
		if _, err := w.Write(e.Data); err != nil {
			return nil, &Error{ErrZip, err.Error()}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, &Error{ErrZip, err.Error()}
	}
	return buf.Bytes(), nil
}
