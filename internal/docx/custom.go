package docx

// Custom document properties (docProps/custom.xml, ECMA-376 Part 1 §22.3; Rust
// crates/docx/src/custom.rs read). Citation managers keep their document preferences here
// (Zotero: ZOTERO_PREF_1…), so they must survive a round trip intact.

import (
	"slices"
	"strings"

	"github.com/computernut43/wordcraft/internal/doc"
)

// maxCustomProps is the most properties kept from one file.
const maxCustomProps = 10_000

// customSimple are the variant types kept as text.
var customSimple = []string{
	"lpwstr", "lpstr", "bstr", "i1", "i2", "i4", "i8", "int", "ui1", "ui2", "ui4", "ui8", "uint", "r4", "r8", "decimal", "bool", "date", "filetime",
	"cy", "error", "clsid",
}

// readCustomProps reads the properties of a parsed docProps/custom.xml root.
func readCustomProps(root *El) doc.Vec[doc.CustomProp] {
	out := doc.Vec[doc.CustomProp]{}
	taken := 0
	for _, p := range root.Els() {
		if p.Local() != "property" {
			continue
		}
		if taken == maxCustomProps {
			break
		}
		taken++
		name, ok := p.Attr("name")
		if !ok || name == "" {
			continue
		}
		els := p.Els()
		if len(els) == 0 {
			continue
		}
		v := els[0]
		kind := v.Local()
		if slices.Contains(customSimple, kind) {
			out = append(out, doc.CustomProp{Name: name, Kind: kind, Value: v.Text()})
			continue
		}
		var xml strings.Builder
		customElXML(v, &xml, 0)
		out = append(out, doc.CustomProp{Name: name, Kind: "raw", Value: xml.String()})
	}
	return out
}

// customElXML serialises an element with its canonical prefixes (no namespace declarations).
func customElXML(e *El, out *strings.Builder, depth int) {
	if depth > MaxDepth {
		return
	}
	out.WriteString("<" + e.Name)
	for _, a := range e.Attrs {
		if strings.HasPrefix(a.Key, "xmlns") {
			continue
		}
		out.WriteString(" " + a.Key + `="` + Esc(a.Val) + `"`)
	}
	if len(e.Kids) == 0 {
		out.WriteString("/>")
		return
	}
	out.WriteString(">")
	for _, k := range e.Kids {
		if k.El != nil {
			customElXML(k.El, out, depth+1)
		} else {
			out.WriteString(Esc(k.Text))
		}
	}
	out.WriteString("</" + e.Name + ">")
}
