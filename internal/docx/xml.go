// Package docx reads and writes WordprocessingML packages (.docx): the Go port of crates/docx,
// growing in steps (MIGRATION.md §6).
package docx

// A small, bounded XML tree, plus a string-building XML writer (Rust crates/docx/src/xml.rs).
//
// Element and attribute names are rewritten to canonical prefixes by namespace URI (w:p, r:id,
// a:blip…), so the reader doesn't care which prefixes a producer chose. Names in unknown
// namespaces keep their local name behind a "?:" prefix.
//
// Rust builds the tree on quick-xml 0.41 (MIT, © 2016 Johann Tuffe). The tokenizer here is
// written to give the same tree, and the same failures, for any input: tokens split where
// quick-xml splits them, references and attribute values resolved and normalized as it does,
// and namespace scopes kept as its NsReader keeps them. TestXMLMatchesRust checks this against
// the Rust code itself.

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Limits of the parser (Rust MAX_DEPTH, MAX_ELEMENTS).
const (
	MaxDepth    = 256
	MaxElements = 8_000_000
	// maxDeclarations is quick-xml's limit on namespace declarations in one start tag.
	maxDeclarations = 256
)

// NS pairs a canonical prefix with its namespace URI.
type NS struct{ Prefix, URI string }

// Namespaces are the known namespaces of body parts and the canonical prefix used for each,
// read and written.
var Namespaces = []NS{
	{"w", "http://schemas.openxmlformats.org/wordprocessingml/2006/main"},
	{"r", "http://schemas.openxmlformats.org/officeDocument/2006/relationships"},
	{"wp", "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"},
	{"a", "http://schemas.openxmlformats.org/drawingml/2006/main"},
	{"c", "http://schemas.openxmlformats.org/drawingml/2006/chart"},
	{"pic", "http://schemas.openxmlformats.org/drawingml/2006/picture"},
	{"mc", "http://schemas.openxmlformats.org/markup-compatibility/2006"},
	{"w14", "http://schemas.microsoft.com/office/word/2010/wordml"},
	{"wpi", "http://schemas.microsoft.com/office/word/2010/wordprocessingInk"},
	{"w15", "http://schemas.microsoft.com/office/word/2012/wordml"},
	{"wp14", "http://schemas.microsoft.com/office/word/2010/wordprocessingDrawing"},
	{"wps", "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"},
	{"wpg", "http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"},
	{"wpc", "http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"},
	{"dgm", "http://schemas.openxmlformats.org/drawingml/2006/diagram"},
	{"dsp", "http://schemas.microsoft.com/office/drawing/2008/diagram"},
	{"m", "http://schemas.openxmlformats.org/officeDocument/2006/math"},
	{"v", "urn:schemas-microsoft-com:vml"},
	{"o", "urn:schemas-microsoft-com:office:office"},
	{"w10", "urn:schemas-microsoft-com:office:word"},
	// Office 2016 charts (cx:chart), and the namespaces their mc:Choice requires.
	{"cx", "http://schemas.microsoft.com/office/drawing/2014/chartex"},
	{"cx1", "http://schemas.microsoft.com/office/drawing/2015/9/8/chartex"},
	{"cx2", "http://schemas.microsoft.com/office/drawing/2015/10/21/chartex"},
	{"cx3", "http://schemas.microsoft.com/office/drawing/2016/5/9/chartex"},
	{"cx4", "http://schemas.microsoft.com/office/drawing/2016/5/10/chartex"},
	{"cx5", "http://schemas.microsoft.com/office/drawing/2016/5/11/chartex"},
	{"cx6", "http://schemas.microsoft.com/office/drawing/2016/5/12/chartex"},
	{"cx7", "http://schemas.microsoft.com/office/drawing/2016/5/13/chartex"},
	{"cx8", "http://schemas.microsoft.com/office/drawing/2016/5/14/chartex"},
}

// DecorativeExt is the a:ext URI under which a drawing is marked decorative ([MS-ODRAWXML]),
// and DecorativeNS the namespace of its adec:decorative element.
const (
	DecorativeExt = "{C183D7F6-B498-43B3-948B-1728B52AA6E4}"
	DecorativeNS  = "http://schemas.microsoft.com/office/drawing/2017/decorative"
)

// otherNamespaces only appear in non-body parts.
var otherNamespaces = []NS{
	{"b", "http://schemas.openxmlformats.org/officeDocument/2006/bibliography"},
	{"rel", "http://schemas.openxmlformats.org/package/2006/relationships"},
	{"ct", "http://schemas.openxmlformats.org/package/2006/content-types"},
	{"cp", "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"},
	{"dc", "http://purl.org/dc/elements/1.1/"},
	{"dcterms", "http://purl.org/dc/terms/"},
	{"xsi", "http://www.w3.org/2001/XMLSchema-instance"},
	{"xml", "http://www.w3.org/XML/1998/namespace"},
	{"ep", "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"},
	{"op", "http://schemas.openxmlformats.org/officeDocument/2006/custom-properties"},
	{"vt", "http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"},
}

// strictNamespaces are the ISO/IEC 29500 Strict namespaces, mapped onto the transitional
// prefixes.
var strictNamespaces = []NS{
	{"w", "http://purl.oclc.org/ooxml/wordprocessingml/main"},
	{"r", "http://purl.oclc.org/ooxml/officeDocument/relationships"},
	{"wp", "http://purl.oclc.org/ooxml/drawingml/wordprocessingDrawing"},
	{"a", "http://purl.oclc.org/ooxml/drawingml/main"},
	{"pic", "http://purl.oclc.org/ooxml/drawingml/picture"},
	{"m", "http://purl.oclc.org/ooxml/officeDocument/math"},
}

func prefixFor(uri string, extra []NS) string {
	for _, list := range [][]NS{extra, Namespaces, otherNamespaces, strictNamespaces} {
		for _, n := range list {
			if n.URI == uri {
				return n.Prefix
			}
		}
	}
	return "?"
}

// requirementPrefix is the canonical prefix of a compatibility feature namespace named by
// mc:Choice/@Requires, including some the tree doesn't otherwise handle.
func requirementPrefix(uri string) string {
	if p := prefixFor(uri, nil); p != "?" {
		return p
	}
	switch uri {
	case "http://schemas.microsoft.com/office/drawing/2010/main":
		return "a14"
	case "http://schemas.microsoft.com/office/word/2015/wordml/symex":
		return "w16se"
	case "http://schemas.microsoft.com/office/word/2016/wordml/cid":
		return "w16cid"
	case "http://schemas.microsoft.com/office/word/2018/wordml":
		return "w16"
	case "http://schemas.microsoft.com/office/word/2018/wordml/cex":
		return "w16cex"
	case "http://schemas.microsoft.com/office/word/2020/wordml/sdtdatahash":
		return "w16sdtdh"
	}
	return "?"
}

// Node is a child of an element: an element, or text when El is nil.
type Node struct {
	El   *El
	Text string
}

// Attr is an attribute with its canonical name.
type Attr struct{ Key, Val string }

// El is an element with canonical-prefixed name and attributes.
type El struct {
	Name  string
	Attrs []Attr
	Kids  []Node
}

// Attr is the attribute by canonical name (w:val), falling back to an unqualified attribute
// with the same local name (some producers omit the prefix).
func (e *El) Attr(name string) (string, bool) {
	for _, a := range e.Attrs {
		if a.Key == name {
			return a.Val, true
		}
	}
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		local := name[i+1:]
		for _, a := range e.Attrs {
			if a.Key == local {
				return a.Val, true
			}
		}
	}
	return "", false
}

// Els are the child elements.
func (e *El) Els() []*El {
	var out []*El
	for _, n := range e.Kids {
		if n.El != nil {
			out = append(out, n.El)
		}
	}
	return out
}

// Child is the first child element named name, or nil.
func (e *El) Child(name string) *El {
	for _, n := range e.Kids {
		if n.El != nil && n.El.Name == name {
			return n.El
		}
	}
	return nil
}

// Children are the child elements named name.
func (e *El) Children(name string) []*El {
	var out []*El
	for _, n := range e.Kids {
		if n.El != nil && n.El.Name == name {
			out = append(out, n.El)
		}
	}
	return out
}

// ChildVal is the w:val of the first child named name.
func (e *El) ChildVal(name string) (string, bool) {
	if c := e.Child(name); c != nil {
		return c.Attr("w:val")
	}
	return "", false
}

// Text is the element's direct text content.
func (e *El) Text() string {
	var s strings.Builder
	for _, n := range e.Kids {
		if n.El == nil {
			s.WriteString(n.Text)
		}
	}
	return s.String()
}

// DeepText is all descendant text, depth-bounded.
func (e *El) DeepText() string {
	var s strings.Builder
	var walk func(*El, int)
	walk = func(e *El, depth int) {
		if depth > MaxDepth {
			return
		}
		for _, n := range e.Kids {
			if n.El == nil {
				s.WriteString(n.Text)
			} else {
				walk(n.El, depth+1)
			}
		}
	}
	walk(e, 0)
	return s.String()
}

// Find is the first descendant named name (depth first, bounded), or nil.
func (e *El) Find(name string) *El {
	var walk func(*El, int) *El
	walk = func(e *El, depth int) *El {
		if depth > MaxDepth {
			return nil
		}
		for _, n := range e.Kids {
			if n.El == nil {
				continue
			}
			if n.El.Name == name {
				return n.El
			}
			if f := walk(n.El, depth+1); f != nil {
				return f
			}
		}
		return nil
	}
	return walk(e, 0)
}

// ToXML writes the element as XML with canonical prefixes (for re-emitting kept content).
// Elements and attributes in unknown namespaces are dropped.
func (e *El) ToXML() string {
	var s strings.Builder
	var walk func(*El, int)
	walk = func(e *El, depth int) {
		if depth > MaxDepth || strings.HasPrefix(e.Name, "?:") {
			return
		}
		s.WriteByte('<')
		s.WriteString(e.Name)
		for _, a := range e.Attrs {
			if strings.HasPrefix(a.Key, "?:") {
				continue
			}
			s.WriteByte(' ')
			s.WriteString(a.Key)
			s.WriteString(`="`)
			s.WriteString(Esc(a.Val))
			s.WriteByte('"')
		}
		if len(e.Kids) == 0 {
			s.WriteString("/>")
			return
		}
		s.WriteByte('>')
		for _, n := range e.Kids {
			if n.El == nil {
				s.WriteString(Esc(n.Text))
			} else {
				walk(n.El, depth+1)
			}
		}
		s.WriteString("</")
		s.WriteString(e.Name)
		s.WriteByte('>')
	}
	walk(e, 0)
	return s.String()
}

// Local is the local part of the name.
func (e *El) Local() string {
	if i := strings.LastIndexByte(e.Name, ':'); i >= 0 {
		return e.Name[i+1:]
	}
	return e.Name
}

// utf16ToUTF8 transcodes UTF-16 LE or BE text that starts with a byte-order mark (ok false
// without one). Lossy: an unpaired surrogate becomes U+FFFD and a trailing odd byte is dropped.
func utf16ToUTF8(b []byte) (string, bool) {
	if len(b) < 2 {
		return "", false
	}
	var le bool
	switch {
	case b[0] == 0xFF && b[1] == 0xFE:
		le = true
	case b[0] == 0xFE && b[1] == 0xFF:
	default:
		return "", false
	}
	rest := b[2:]
	units := make([]uint16, len(rest)/2)
	for i := range units {
		lo, hi := rest[2*i], rest[2*i+1]
		if le {
			units[i] = uint16(lo) | uint16(hi)<<8
		} else {
			units[i] = uint16(lo)<<8 | uint16(hi)
		}
	}
	return string(utf16.Decode(units)), true
}

// lossy is Rust's String::from_utf8_lossy: each maximal invalid subsequence becomes one U+FFFD.
func lossy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var s strings.Builder
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r != utf8.RuneError || n > 1 {
			s.Write(b[:n])
			b = b[n:]
			continue
		}
		s.WriteRune(utf8.RuneError)
		b = b[invalidPrefix(b):]
	}
	return s.String()
}

// invalidPrefix is the length of the maximal subpart of an ill-formed sequence at the start of
// b: a lead byte and the continuation bytes that could still have completed it (at least 1).
func invalidPrefix(b []byte) int {
	lead := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead == 0xE0:
		need, lo = 2, 0xA0
	case lead == 0xED:
		need, hi = 2, 0x9F
	case lead >= 0xE1 && lead <= 0xEF:
		need = 2
	case lead == 0xF0:
		need, lo = 3, 0x90
	case lead == 0xF4:
		need, hi = 3, 0x8F
	case lead >= 0xF1 && lead <= 0xF3:
		need = 3
	default:
		return 1
	}
	i := 1
	for ; i <= need && i < len(b); i++ {
		c := b[i]
		if i == 1 && (c < lo || c > hi) || i > 1 && (c < 0x80 || c > 0xBF) {
			break
		}
	}
	return i
}

// xmlError is a parse failure. The wording isn't Rust's (quick-xml's messages); that a part
// fails, and where it's read leniently, matches.
func xmlError(what string) error { return errors.New("docx: XML: " + what) }

// Parse parses XML bytes into the root element.
func Parse(b []byte) (*El, error) { return ParseNS(b, nil) }

// ParseNS is Parse with extra namespaces (prefix, URI) mapped to canonical prefixes first, so a
// related vocabulary (WordprocessingML 2003) reads with the prefixes the reader knows.
func ParseNS(b []byte, extra []NS) (*El, error) {
	// UTF-16 (with a BOM) is transcoded to UTF-8; a UTF-8 BOM is stripped. quick-xml then strips
	// one more UTF-8 BOM at its start.
	if s, ok := utf16ToUTF8(b); ok {
		b = []byte(s)
	} else {
		b = bytes.TrimPrefix(b, utf8BOM)
	}
	b = bytes.TrimPrefix(b, utf8BOM)
	p := &parser{data: b, extra: extra, ns: newResolver()}
	return p.run()
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// event kinds of the tokenizer (quick-xml's Event, those the tree uses).
type eventKind int

const (
	evNone eventKind = iota // a token the tree ignores (declaration, PI, comment, DOCTYPE)
	evStart
	evEmpty
	evEnd
	evText
	evCData
	evRef
	evEOF
)

type event struct {
	kind eventKind
	// start/empty: the tag content after "<" (name and attributes); end: the name; text, CDATA
	// and references: their bytes.
	raw     []byte
	nameLen int
}

// tokenizer states (quick-xml ParseState).
type state int

const (
	inText state = iota
	inRef
	inMarkup
	done
)

type parser struct {
	data   []byte
	pos    int
	st     state
	opened int // open start tags (quick-xml's opened_starts)
	extra  []NS
	ns     *resolver
	popNS  bool // the last event was an empty or end tag: its scope ends before the next read
}

func isSpace(c byte) bool { return c == ' ' || c == '\r' || c == '\n' || c == '\t' }

// next reads one event (quick-xml read_event_impl). Any error ends the parse, as Rust's parse
// returns on the first one.
func (p *parser) next() (event, error) {
	switch p.st {
	case inText:
		rest := p.data[p.pos:]
		i := bytes.IndexAny(rest, "<&")
		switch {
		case i == 0 && rest[0] == '<':
			p.st = inMarkup
			return p.markup()
		case i == 0:
			p.st = inRef
			return p.ref()
		case i > 0:
			p.pos += i
			if rest[i] == '<' {
				p.st = inMarkup
			} else {
				p.st = inRef
			}
			return event{kind: evText, raw: rest[:i]}, nil
		}
		p.pos = len(p.data)
		p.st = done
		if len(rest) == 0 {
			return event{kind: evEOF}, nil
		}
		return event{kind: evText, raw: rest}, nil
	case inRef:
		return p.ref()
	case inMarkup:
		return p.markup()
	}
	return event{kind: evEOF}, nil
}

// ref reads a reference at "&": up to ";", or fails at the next "&", "<" or the end.
func (p *parser) ref() (event, error) {
	rest := p.data[p.pos:]
	i := bytes.IndexAny(rest[1:], ";&<")
	if i >= 0 && rest[1+i] == ';' {
		p.pos += i + 2
		p.st = inText
		return event{kind: evRef, raw: rest[1 : 1+i]}, nil
	}
	return event{}, xmlError("unclosed reference")
}

// markup reads the markup at "<".
func (p *parser) markup() (event, error) {
	p.st = inText
	rest := p.data[p.pos:]
	if len(rest) < 2 {
		return event{}, xmlError("unclosed tag")
	}
	switch rest[1] {
	case '!':
		return p.bang(rest)
	case '/':
		n, ok := elementEnd(rest)
		if !ok {
			return event{}, xmlError("unclosed tag")
		}
		p.pos += n
		// The name, without trailing whitespace (unless that's all there is).
		name := rest[2 : n-1]
		for j := len(name) - 1; j >= 0; j-- {
			if !isSpace(name[j]) {
				name = name[:j+1]
				break
			}
		}
		if p.opened == 0 {
			return event{}, xmlError("end tag without a start tag")
		}
		p.opened--
		return event{kind: evEnd, raw: name}, nil
	case '?':
		n, ok := piEnd(rest)
		if !ok {
			return event{}, xmlError("unclosed processing instruction")
		}
		p.pos += n
		if n <= 3 {
			return event{}, xmlError("unclosed processing instruction")
		}
		return event{kind: evNone}, nil
	}
	n, ok := elementEnd(rest)
	if !ok {
		return event{}, xmlError("unclosed tag")
	}
	p.pos += n
	content := rest[1:n]
	if c, empty := bytes.CutSuffix(content, []byte("/>")); empty {
		return event{kind: evEmpty, raw: c, nameLen: nameLen(c)}, nil
	}
	c := content[:len(content)-1]
	p.opened++
	return event{kind: evStart, raw: c, nameLen: nameLen(c)}, nil
}

func nameLen(b []byte) int {
	for i, c := range b {
		if isSpace(c) {
			return i
		}
	}
	return len(b)
}

// elementEnd is the length of a tag starting at b[0] == '<' up to its ">" outside quotes.
func elementEnd(b []byte) (int, bool) {
	var quote byte
	for i, c := range b {
		switch {
		case quote == 0 && c == '>':
			return i + 1, true
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
		case quote != 0 && c == quote:
			quote = 0
		}
	}
	return 0, false
}

// piEnd is the length of a processing instruction or declaration starting at "<?" up to "?>".
func piEnd(b []byte) (int, bool) {
	for i := 1; i < len(b); i++ {
		if b[i] == '>' && b[i-1] == '?' {
			return i + 1, true
		}
	}
	return 0, false
}

// bang reads a comment, CDATA section or DOCTYPE at "<!".
func (p *parser) bang(rest []byte) (event, error) {
	if len(rest) < 3 {
		return event{}, xmlError("invalid markup after <!")
	}
	switch rest[2] {
	case '[':
		for i := 0; i < len(rest); i++ {
			if rest[i] == '>' && bytes.HasSuffix(rest[:i], []byte("]]")) {
				p.pos += i + 1
				b := rest[:i+1]
				if !bytes.HasPrefix(b, []byte("<![CDATA[")) {
					return event{}, xmlError("invalid CDATA")
				}
				return event{kind: evCData, raw: b[9 : len(b)-3]}, nil
			}
		}
		return event{}, xmlError("unclosed CDATA")
	case '-':
		for i := 6; i < len(rest); i++ {
			if rest[i] == '>' && bytes.HasSuffix(rest[:i], []byte("--")) {
				p.pos += i + 1
				if !bytes.HasPrefix(rest, []byte("<!--")) {
					return event{}, xmlError("invalid comment")
				}
				return event{kind: evNone}, nil
			}
		}
		return event{}, xmlError("unclosed comment")
	case 'D', 'd':
		n, ok := doctypeEnd(rest)
		if !ok {
			return event{}, xmlError("unclosed DOCTYPE")
		}
		p.pos += n
		b := rest[:n]
		if len(b) < 9 || !bytes.EqualFold(b[:9], []byte("<!DOCTYPE")) {
			return event{}, xmlError("invalid DOCTYPE")
		}
		for _, c := range b[9 : len(b)-1] {
			if !isSpace(c) {
				return event{kind: evNone}, nil
			}
		}
		return event{}, xmlError("DOCTYPE without a name")
	}
	return event{}, xmlError("invalid markup after <!")
}

// doctypeEnd is the length of a DOCTYPE declaration at "<!D" up to its closing ">", skipping
// quoted literals and the markup of an internal subset (quick-xml's DtdParser on one chunk).
func doctypeEnd(b []byte) (int, bool) {
	i := 0
	// Before the internal subset: quoted literals hide "[" and ">".
	for i < len(b) {
		c := b[i]
		switch c {
		case '\'', '"':
			j := bytes.IndexByte(b[i+1:], c)
			if j < 0 {
				return 0, false
			}
			i += j + 2
			continue
		case '>':
			return i + 1, true
		case '[':
			end, ok := internalSubsetEnd(b, i+1)
			if !ok {
				return 0, false
			}
			j := bytes.IndexByte(b[end:], '>')
			if j < 0 {
				return 0, false
			}
			return end + j + 1, true
		}
		i++
	}
	return 0, false
}

// internalSubsetEnd skips a DOCTYPE's internal subset from i to just after its "]".
func internalSubsetEnd(b []byte, i int) (int, bool) {
	for i < len(b) {
		j := bytes.IndexAny(b[i:], "]<")
		if j < 0 {
			return 0, false
		}
		i += j
		if b[i] == ']' {
			return i + 1, true
		}
		m := b[i+1:]
		switch {
		case bytes.HasPrefix(m, []byte("?")):
			n, ok := piBody(m[1:])
			if !ok {
				return 0, false
			}
			i += 2 + n
		case bytes.HasPrefix(m, []byte("!--")):
			n, ok := commentBody(m[3:])
			if !ok {
				return 0, false
			}
			i += 4 + n
		case bytes.HasPrefix(m, []byte("!ELEMENT")):
			k := bytes.IndexByte(m[8:], '>')
			if k < 0 {
				return 0, false
			}
			i += 1 + 8 + k + 1
		case bytes.HasPrefix(m, []byte("!ENTITY")), bytes.HasPrefix(m, []byte("!ATTLIST")), bytes.HasPrefix(m, []byte("!NOTATION")):
			skip := 7
			if !bytes.HasPrefix(m, []byte("!ENTITY")) {
				skip = 8
				if bytes.HasPrefix(m, []byte("!NOTATION")) {
					skip = 9
				}
			}
			n, ok := elementEnd(m[skip:])
			if !ok {
				return 0, false
			}
			i += 1 + skip + n
		default:
			k := bytes.IndexByte(m, '>')
			switch {
			case k >= 0:
				i += 1 + k + 1
			case len(m) >= 9:
				// Not a declaration keyword: skip to the next ">".
				k = bytes.IndexByte(m, '>')
				if k < 0 {
					return 0, false
				}
				i += 1 + k + 1
			default:
				return 0, false
			}
		}
	}
	return 0, false
}

// piBody finds the end of a processing instruction inside a DTD, after "<?": the length up to
// and including "?>" (quick-xml PiParser).
func piBody(b []byte) (int, bool) {
	for i := 0; i < len(b); i++ {
		if b[i] == '>' && i > 0 && b[i-1] == '?' {
			return i + 1, true
		}
	}
	return 0, false
}

// commentBody finds the end of a comment inside a DTD, after "<!--": the length up to and
// including "-->" (quick-xml CommentParser from its initial state).
func commentBody(b []byte) (int, bool) {
	for i := 0; i < len(b); i++ {
		if b[i] == '>' && bytes.HasSuffix(b[:i], []byte("--")) {
			return i + 1, true
		}
	}
	return 0, false
}

// resolver keeps namespace bindings per scope (quick-xml NamespaceResolver).
type resolver struct {
	bindings []binding
	level    int
}

type binding struct {
	prefix, uri string // prefix "" is the default namespace
	level       int
}

const (
	xmlNS   = "http://www.w3.org/XML/1998/namespace"
	xmlnsNS = "http://www.w3.org/2000/xmlns/"
)

func newResolver() *resolver {
	return &resolver{bindings: []binding{{"xml", xmlNS, 0}, {"xmlns", xmlnsNS, 0}}}
}

// push opens a scope for a start or empty tag with its namespace declarations.
func (r *resolver) push(content []byte, nameLen int) error {
	r.level++
	count := 0
	it := attrIter{b: content, pos: nameLen}
	for {
		a, ok, more := it.next()
		if !more {
			return nil
		}
		if !ok {
			// quick-xml stops scanning declarations at the first malformed attribute.
			return nil
		}
		key := content[a.key[0]:a.key[1]]
		if !bytes.HasPrefix(key, []byte("xmlns")) {
			continue
		}
		var prefix string
		switch {
		case len(key) == 5:
		case key[5] == ':':
			prefix = string(key[6:])
		default:
			continue
		}
		if count >= maxDeclarations {
			return xmlError("too many namespace declarations")
		}
		count++
		uri := string(content[a.val[0]:a.val[1]])
		named := len(key) > 5
		switch {
		case named && prefix == "xml":
			if uri != xmlNS {
				return xmlError("xml prefix bound to another namespace")
			}
			continue
		case named && prefix == "xmlns":
			return xmlError("xmlns prefix bound")
		case named && uri == xmlNS:
			return xmlError("prefix bound to the xml namespace")
		case named && uri == xmlnsNS:
			return xmlError("prefix bound to the xmlns namespace")
		}
		r.bindings = append(r.bindings, binding{prefix, uri, r.level})
	}
}

// pop closes the innermost scope.
func (r *resolver) pop() {
	if r.level > 0 {
		r.level--
	}
	n := len(r.bindings)
	for n > 0 && r.bindings[n-1].level > r.level {
		n--
	}
	r.bindings = r.bindings[:n]
}

// resolve is a qualified name's namespace: bound (ok, uri), unbound (no prefix, no default:
// ok false, prefixed false) or unknown (an undeclared or reset prefix: ok false).
func (r *resolver) resolve(qname []byte, element bool) (uri string, bound bool) {
	i := bytes.IndexByte(qname, ':')
	if i < 0 {
		if !element {
			return "", false
		}
		for j := len(r.bindings) - 1; j >= 0; j-- {
			if r.bindings[j].prefix == "" {
				return r.bindings[j].uri, r.bindings[j].uri != ""
			}
		}
		return "", false
	}
	prefix := string(qname[:i])
	for j := len(r.bindings) - 1; j >= 0; j-- {
		b := r.bindings[j]
		if b.prefix == prefix && prefix != "" {
			return b.uri, b.uri != ""
		}
	}
	return "", false
}

func localName(qname []byte) []byte {
	if i := bytes.IndexByte(qname, ':'); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

// attrSpan is an attribute's key and value as offsets into the tag content.
type attrSpan struct{ key, val [2]int }

// attrIter walks a tag's attributes as quick-xml's Attributes iterator in XML mode with
// duplicate checks off: next returns more false at the end, ok false for a malformed attribute
// (after which it resumes where quick-xml does).
type attrIter struct {
	b    []byte
	pos  int
	done bool
	// skip is how to resume after an error: 0 none, 1 skip an unquoted value.
	skip int
}

func (it *attrIter) next() (a attrSpan, ok, more bool) {
	if it.done {
		return a, false, false
	}
	b := it.b
	i := it.pos
	if it.skip == 1 {
		it.skip = 0
		for i < len(b) && !isSpace(b[i]) {
			i++
		}
		if i == len(b) {
			it.done = true
			return a, false, false
		}
	}
	for i < len(b) && isSpace(b[i]) {
		i++
	}
	if i == len(b) {
		it.done = true
		return a, false, false
	}
	start := i
	// The key runs from its first character (even "=") to the next "=" or whitespace.
	i++
	for i < len(b) && b[i] != '=' && !isSpace(b[i]) {
		i++
	}
	if i == len(b) {
		it.done = true
		return a, false, true
	}
	keyEnd := i
	if b[i] != '=' {
		for i < len(b) && isSpace(b[i]) {
			i++
		}
		if i == len(b) {
			it.done = true
			return a, false, true
		}
		if b[i] != '=' {
			it.pos = i
			return a, false, true
		}
	}
	i++ // past "="
	for i < len(b) && isSpace(b[i]) {
		i++
	}
	if i == len(b) {
		it.done = true
		return a, false, true
	}
	q := b[i]
	if q != '"' && q != '\'' {
		it.pos, it.skip = i, 1
		return a, false, true
	}
	j := bytes.IndexByte(b[i+1:], q)
	if j < 0 {
		it.done = true
		return a, false, true
	}
	it.pos = i + 1 + j + 1
	return attrSpan{key: [2]int{start, keyEnd}, val: [2]int{i + 1, i + 1 + j}}, true, true
}

// predefined are XML's predefined entities.
func predefined(name string) (string, bool) {
	switch name {
	case "lt":
		return "<", true
	case "gt":
		return ">", true
	case "amp":
		return "&", true
	case "apos":
		return "'", true
	case "quot":
		return `"`, true
	}
	return "", false
}

// charRef is the character a character reference's number (after "#") names, as quick-xml's
// parse_number: decimal, or hex after a lowercase "x"; no sign, not 0, a valid scalar value.
func charRef(num string) (rune, bool) {
	base, digits := 10, num
	if h, ok := strings.CutPrefix(num, "x"); ok {
		base, digits = 16, h
	}
	if digits == "" || digits[0] == '+' || digits[0] == '-' {
		return 0, false
	}
	n, err := strconv.ParseUint(digits, base, 32)
	if err != nil || n == 0 || n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) {
		return 0, false
	}
	return rune(n), true
}

// attrValue is the attribute value normalized as XML 1.0 requires (quick-xml normalized_value):
// references resolved, tab, CR, LF and CRLF each a space. ok false for invalid UTF-8, an
// unterminated or unknown reference or a bad character reference.
func attrValue(raw []byte) (string, bool) {
	if !utf8.Valid(raw) {
		return "", false
	}
	s := string(raw)
	if !strings.ContainsAny(s, "\t\r\n&") {
		return s, true
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '&':
			j := strings.IndexByte(s[i+1:], ';')
			if j < 0 {
				return "", false
			}
			pat := s[i+1 : i+1+j]
			if num, ok := strings.CutPrefix(pat, "#"); ok {
				r, ok := charRef(num)
				if !ok {
					return "", false
				}
				out.WriteRune(r)
			} else if v, ok := predefined(pat); ok {
				out.WriteString(v)
			} else {
				return "", false
			}
			i += j + 2
		case '\t', '\n':
			out.WriteByte(' ')
			i++
		case '\r':
			out.WriteByte(' ')
			i++
			if i < len(s) && s[i] == '\n' {
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String(), true
}

// requires rewrites mc:Choice/@Requires: its namespace prefixes become canonical feature
// prefixes, resolved in the element's scope.
func (p *parser) requires(val string) string {
	// Rust splits on Unicode White_Space, as strings.Fields does.
	fields := strings.Fields(val)
	if len(fields) > 64 {
		return "?"
	}
	out := make([]string, len(fields))
	for i, prefix := range fields {
		if len(prefix) > 128 {
			return "?"
		}
		uri, bound := p.ns.resolve([]byte(prefix+":requirement"), false)
		if bound {
			out[i] = requirementPrefix(uri)
		} else {
			out[i] = "?"
		}
	}
	return strings.Join(out, " ")
}

func (p *parser) run() (*El, error) {
	var stack []*El
	var root *El
	count := 0
	attach := func(el *El) {
		if len(stack) > 0 {
			top := stack[len(stack)-1]
			top.Kids = append(top.Kids, Node{El: el})
		} else if root == nil {
			root = el
		}
	}
	pushText := func(s string) {
		top := stack[len(stack)-1]
		if n := len(top.Kids); n > 0 && top.Kids[n-1].El == nil {
			top.Kids[n-1].Text += s
		} else {
			top.Kids = append(top.Kids, Node{Text: s})
		}
	}
	for {
		if p.popNS {
			p.ns.pop()
			p.popNS = false
		}
		ev, err := p.next()
		if err != nil {
			return nil, err
		}
		switch ev.kind {
		case evStart, evEmpty:
			if err := p.ns.push(ev.raw, ev.nameLen); err != nil {
				return nil, err
			}
			if ev.kind == evEmpty {
				p.popNS = true
			}
			count++
			if count > MaxElements {
				return nil, xmlError("too many XML elements")
			}
			qname := ev.raw[:ev.nameLen]
			name := lossy(localName(qname))
			if uri, bound := p.ns.resolve(qname, true); bound {
				name = prefixFor(uri, p.extra) + ":" + name
			}
			el := &El{Name: name}
			it := attrIter{b: ev.raw, pos: ev.nameLen}
			for {
				a, ok, more := it.next()
				if !more {
					break
				}
				if !ok {
					continue
				}
				key := ev.raw[a.key[0]:a.key[1]]
				if bytes.Equal(key, []byte("xmlns")) || bytes.HasPrefix(key, []byte("xmlns:")) {
					continue
				}
				aname := lossy(localName(key))
				if uri, bound := p.ns.resolve(key, false); bound {
					aname = prefixFor(uri, p.extra) + ":" + aname
				}
				raw := ev.raw[a.val[0]:a.val[1]]
				val, ok := attrValue(raw)
				if !ok {
					val = lossy(raw)
				}
				if el.Name == "mc:Choice" && aname == "Requires" {
					val = p.requires(val)
				}
				el.Attrs = append(el.Attrs, Attr{aname, val})
			}
			if ev.kind == evStart {
				if len(stack) >= MaxDepth {
					return nil, xmlError("XML nesting too deep")
				}
				stack = append(stack, el)
			} else {
				attach(el)
			}
		case evEnd:
			p.popNS = true
			if n := len(stack); n > 0 {
				el := stack[n-1]
				stack = stack[:n-1]
				attach(el)
			}
		case evText:
			if len(stack) > 0 {
				if !utf8.Valid(ev.raw) {
					return nil, xmlError("text is not UTF-8")
				}
				pushText(string(ev.raw))
			}
		case evCData:
			if len(stack) > 0 {
				pushText(lossy(ev.raw))
			}
		case evRef:
			if len(stack) > 0 && utf8.Valid(ev.raw) {
				name := string(ev.raw)
				if num, ok := strings.CutPrefix(name, "#"); ok {
					if r, ok := charRef(num); ok {
						pushText(string(r))
					}
				} else if v, ok := predefined(name); ok {
					pushText(v)
				}
			}
		case evEOF:
			for n := len(stack); n > 0; n = len(stack) {
				el := stack[n-1]
				stack = stack[:n-1]
				attach(el)
			}
			if root == nil {
				return nil, xmlError("no root element")
			}
			return root, nil
		}
		if root != nil && len(stack) == 0 {
			return root, nil
		}
	}
}

// xmlCharOK reports whether c may appear in XML 1.0 text.
func xmlCharOK(c rune) bool {
	return c == '\t' || c == '\n' || c == '\r' || (c >= 0x20 && c <= 0xD7FF) || (c >= 0xE000 && c <= 0xFFFD) || (c >= 0x10000 && c <= 0x10FFFF)
}

// Esc escapes text for element content or attribute values, dropping characters XML can't
// carry.
func Esc(s string) string {
	var o strings.Builder
	o.Grow(len(s))
	for _, c := range s {
		switch c {
		case '&':
			o.WriteString("&amp;")
		case '<':
			o.WriteString("&lt;")
		case '>':
			o.WriteString("&gt;")
		case '"':
			o.WriteString("&quot;")
		case '\'':
			o.WriteString("&apos;")
		case '\t':
			o.WriteString("&#9;")
		case '\n':
			o.WriteString("&#10;")
		case '\r':
			o.WriteString("&#13;")
		default:
			if xmlCharOK(c) {
				o.WriteRune(c)
			}
		}
	}
	return o.String()
}

// W builds an XML document as a string.
type W struct{ S strings.Builder }

// NewW starts a document with the XML declaration.
func NewW() *W {
	w := &W{}
	w.S.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n")
	return w
}

func (w *W) attrs(attrs []Attr) {
	for _, a := range attrs {
		w.S.WriteByte(' ')
		w.S.WriteString(a.Key)
		w.S.WriteString(`="`)
		w.S.WriteString(Esc(a.Val))
		w.S.WriteByte('"')
	}
}

// Open writes a start tag.
func (w *W) Open(name string, attrs ...Attr) {
	w.S.WriteByte('<')
	w.S.WriteString(name)
	w.attrs(attrs)
	w.S.WriteByte('>')
}

// Empty writes an empty-element tag.
func (w *W) Empty(name string, attrs ...Attr) {
	w.S.WriteByte('<')
	w.S.WriteString(name)
	w.attrs(attrs)
	w.S.WriteString("/>")
}

// Close writes an end tag.
func (w *W) Close(name string) {
	w.S.WriteString("</")
	w.S.WriteString(name)
	w.S.WriteByte('>')
}

// Text writes escaped text.
func (w *W) Text(t string) { w.S.WriteString(Esc(t)) }

// Leaf writes <name attrs>text</name>.
func (w *W) Leaf(name string, t string, attrs ...Attr) {
	w.Open(name, attrs...)
	w.Text(t)
	w.Close(name)
}

// Val writes <name w:val="v"/>.
func (w *W) Val(name, v string) { w.Empty(name, Attr{"w:val", v}) }

// Raw writes s as it is.
func (w *W) Raw(s string) { w.S.WriteString(s) }

// Bytes is the document.
func (w *W) Bytes() []byte { return []byte(w.S.String()) }

// BodyNS are the xmlns declarations for the body-part namespaces, plus mc:Ignorable.
func BodyNS() []Attr {
	out := make([]Attr, 0, len(Namespaces)+1)
	for _, n := range Namespaces {
		out = append(out, Attr{"xmlns:" + n.Prefix, n.URI})
	}
	return append(out, Attr{"mc:Ignorable", "w14 w15 wp14"})
}
