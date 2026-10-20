package docx

// The scan of a file's word/settings.xml for settings the model doesn't hold (Rust
// crates/docx/src/write/settings_merge.rs: has_unowned and the scanner it uses; the merge itself
// belongs to the writer, not ported yet).

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/computernut43/craft-libs/rustcompat"
)

// settingsOwned are the children of w:settings the model writes.
var settingsOwned = []string{
	"w:removePersonalInformation",
	"w:removeDateAndTime",
	"w:displayBackgroundShape",
	"w:mirrorMargins",
	"w:attachedTemplate",
	"w:linkStyles",
	"w:trackRevisions",
	"w:doNotTrackMoves",
	"w:documentProtection",
	"w:defaultTabStop",
	"w:autoHyphenation",
	"w:consecutiveHyphenLimit",
	"w:hyphenationZone",
	"w:doNotHyphenateCaps",
	"w:evenAndOddHeaders",
	"w:drawingGridHorizontalSpacing",
	"w:drawingGridVerticalSpacing",
	"w:characterSpacingControl",
	"w:footnotePr",
	"w:endnotePr",
	"m:mathPr",
}

// scanChild is a child element of the root: its qualified name and byte span.
type scanChild struct {
	name       string
	start, end int
}

// isCharBoundary is Rust's str::is_char_boundary.
func isCharBoundary(s string, i int) bool {
	return i == 0 || i == len(s) || i > 0 && i < len(s) && utf8.RuneStart(s[i])
}

// scanChildren is the root start tag's span end and the root's direct children; false for
// anything the scanner doesn't expect (CDATA, DOCTYPE, a different root).
func scanChildren(xml, root string) (openEnd, closeAt int, out []scanChild, ok bool) {
	open := strings.Index(xml, "<"+root)
	if open < 0 {
		return
	}
	gt := strings.IndexByte(xml[open:], '>')
	if gt < 0 {
		return
	}
	openEnd = open + gt + 1
	if openEnd < 2 || !isCharBoundary(xml, openEnd-2) || xml[openEnd-2:openEnd] == "/>" {
		return
	}
	closeAt = strings.LastIndex(xml, "</"+root+">")
	if closeAt < 0 {
		return
	}
	depth := 0
	i := openEnd
	var cur *scanChild
	for i < closeAt {
		if !isCharBoundary(xml, i) {
			return
		}
		lt := strings.IndexByte(xml[i:closeAt], '<')
		if lt < 0 {
			break
		}
		at := i + lt
		tail := xml[at:closeAt]
		if strings.HasPrefix(tail, "<!--") {
			e := strings.Index(tail, "-->")
			if e < 0 {
				return
			}
			i = at + e + 3
			continue
		}
		if strings.HasPrefix(tail, "<![CDATA[") || strings.HasPrefix(tail, "<!") {
			return
		}
		if strings.HasPrefix(tail, "<?") {
			e := strings.Index(tail, "?>")
			if e < 0 {
				return
			}
			i = at + e + 2
			continue
		}
		g := strings.IndexByte(tail, '>')
		if g < 0 {
			return
		}
		end := at + g + 1
		tag := xml[at:end]
		if strings.HasPrefix(tag, "</") {
			if depth == 0 {
				return
			}
			depth--
			if depth == 0 {
				if cur == nil {
					return
				}
				out = append(out, scanChild{cur.name, cur.start, end})
				cur = nil
			}
		} else {
			name := tag[1:]
			if j := strings.IndexFunc(name, func(c rune) bool { return rustcompat.IsWhitespace(c) || c == '>' || c == '/' }); j >= 0 {
				name = name[:j]
			}
			selfClose := strings.HasSuffix(tag, "/>")
			switch {
			case depth == 0 && selfClose:
				out = append(out, scanChild{name, at, end})
			case depth == 0:
				cur = &scanChild{name: name, start: at}
				depth = 1
			case !selfClose:
				depth++
			}
		}
		i = end
	}
	if depth != 0 {
		return
	}
	return openEnd, closeAt, out, true
}

// settingsHasUnowned reports whether original (a settings part) holds anything the model doesn't
// write; the reader then keeps it for the writer's merge. False for parts WordCraft wrote itself,
// and for anything it can't scan.
func settingsHasUnowned(original []byte) bool {
	if !utf8.Valid(original) {
		return false
	}
	xml := string(original)
	_, _, kids, ok := scanChildren(xml, "w:settings")
	if !ok {
		return false
	}
	for _, k := range kids {
		switch k.name {
		case "w:zoom":
		case "w:compat":
			// Anything in w:compat beyond its own tag and the compatibility mode.
			c := xml[k.start:k.end]
			mode := 0
			if strings.Contains(c, `w:name="compatibilityMode"`) {
				mode = 1
			}
			if strings.Count(c, "<w:")-(1+mode) > 0 {
				return true
			}
		default:
			if !slices.Contains(settingsOwned, k.name) {
				return true
			}
		}
	}
	return false
}
