package docx

// Content control properties (w:sdtPr, ECMA-376 §17.5.2; check boxes and repeating sections from
// the [MS-DOCX] w14/w15 extensions) (Rust crates/docx/src/read/sdt.rs).

import (
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

// maxSdtExtra is the most bytes of w:sdtPr children kept verbatim per control.
const maxSdtExtra = 64 * 1024

// sdtText keeps a control's text without object marks and control characters, bounded.
func sdtText(v string) string {
	var b strings.Builder
	n := 0
	for _, c := range v {
		if c == doc.Obj || c < 0x20 || 0x7F <= c && c <= 0x9F {
			continue
		}
		if n == doc.MaxControlText {
			break
		}
		b.WriteRune(c)
		n++
	}
	return b.String()
}

// sdtOn reads an on/off element: on unless its value (w:val, or the extension's own) says off.
func sdtOn(e *El) bool {
	v, ok := e.Attr("w:val")
	if !ok {
		v, ok = e.Attr("w14:val")
	}
	if !ok {
		v, ok = e.Attr("w15:val")
	}
	return !ok || !(v == "0" || v == "false" || v == "off")
}

func sdtSymbol(e *El, def rune) (rune, string) {
	if e == nil {
		return def, ""
	}
	ch := def
	if h, ok := e.Attr("w14:val"); ok {
		if v, ok := fromStrRadix16(rustcompat.Trim(h), 32); ok && v >= 0x20 && v <= 0x10FFFF && !(v >= 0xD800 && v <= 0xDFFF) && rune(v) != doc.Obj {
			ch = rune(v)
		}
	}
	return ch, sdtText(attr(e, "w14:font"))
}

func sdtItems(e *El) doc.Vec[doc.ListItem] {
	out := doc.Vec[doc.ListItem]{}
	for i, it := range e.Children("w:listItem") {
		if i >= doc.MaxListItems {
			break
		}
		out = append(out, doc.ListItem{Display: sdtText(attr(it, "w:displayText")), Value: sdtText(attr(it, "w:value"))})
	}
	return out
}

func childValText(e *El, name string) string {
	v, _ := e.ChildVal(name)
	return sdtText(v)
}

// sdtPr is the control a w:sdtPr describes (nil: a w:sdt without one, a rich text control).
func sdtPr(pr *El, block bool) doc.ContentControl {
	c := doc.ContentControl{Block: block}
	if pr == nil {
		return c
	}
	extraLen := 0
	for _, k := range pr.Els() {
		switch k.Name {
		case "w:rPr":
			c.RPrXML = k.ToXML()
		case "w:alias":
			c.Title = sdtText(attr(k, "w:val"))
		case "w:tag":
			c.Tag = sdtText(attr(k, "w:val"))
		case "w:id":
			c.ID = nil
			if v, ok := k.Attr("w:val"); ok {
				if n, ok := rustcompat.ParseInt(rustcompat.Trim(v), 64); ok {
					c.ID = &n
				}
			}
		case "w:lock":
			c.Lock = doc.ControlLockFromOOXML(attr(k, "w:val"))
		case "w:placeholder":
			c.Placeholder = ""
			if d := k.Child("w:docPart"); d != nil {
				c.Placeholder = sdtText(attr(d, "w:val"))
			}
		case "w:temporary":
			c.Temporary = sdtOn(k)
		case "w:showingPlcHdr":
			c.ShowingPlaceholder = sdtOn(k)
		case "w:richText":
			c.Kind = doc.ControlKind{Type: doc.RichTextControl}
		case "w:text":
			c.Kind = doc.ControlKind{Type: doc.TextControl, MultiLine: onOffAttr(k, "w:multiLine")}
		case "w14:checkbox":
			checked := false
			if e := k.Child("w14:checked"); e != nil {
				checked = sdtOn(e)
			}
			cc, cf := sdtSymbol(k.Child("w14:checkedState"), doc.Checked)
			uc, uf := sdtSymbol(k.Child("w14:uncheckedState"), doc.Unchecked)
			c.Kind = doc.ControlKind{Type: doc.CheckBoxControl, Checked: checked, CheckedChar: cc, CheckedFont: cf, UncheckedChar: uc, UncheckedFont: uf}
		case "w:comboBox":
			c.Kind = doc.ControlKind{Type: doc.ComboBoxControl, Items: sdtItems(k), LastValue: sdtText(attr(k, "w:lastValue"))}
		case "w:dropDownList":
			c.Kind = doc.ControlKind{Type: doc.DropDownControl, Items: sdtItems(k), LastValue: sdtText(attr(k, "w:lastValue"))}
		case "w:date":
			c.Kind = doc.ControlKind{Type: doc.DateControl, FullDate: sdtText(attr(k, "w:fullDate")), Format: childValText(k, "w:dateFormat"),
				Lid: childValText(k, "w:lid"), Calendar: childValText(k, "w:calendar"), StoreAs: childValText(k, "w:storeMappedDataAs")}
		case "w:picture":
			c.Kind = doc.ControlKind{Type: doc.PictureControl}
		case "w:docPartObj", "w:docPartList":
			u := k.Child("w:docPartUnique")
			c.Kind = doc.ControlKind{Type: doc.GalleryControl, List: k.Name == "w:docPartList", Gallery: childValText(k, "w:docPartGallery"),
				Category: childValText(k, "w:docPartCategory"), Unique: u != nil && sdtOn(u)}
		case "w15:repeatingSection":
			title := ""
			if t := k.Child("w15:sectionTitle"); t != nil {
				v, ok := t.Attr("w:val")
				if !ok {
					v = attr(t, "w15:val")
				}
				title = sdtText(v)
			}
			n := k.Child("w15:doNotAllowInsertDeleteSection")
			c.Kind = doc.ControlKind{Type: doc.RepeatingSectionControl, Title: title, NoInsertDelete: n != nil && sdtOn(n)}
		case "w15:repeatingSectionItem":
			c.Kind = doc.ControlKind{Type: doc.RepeatingSectionItemControl}
		default:
			x := k.ToXML()
			if x != "" && extraLen+len(x) <= maxSdtExtra {
				extraLen += len(x)
				c.Extra = append(c.Extra, x)
			}
		}
	}
	return c
}

// sdtEndPr keeps w:sdtEndPr verbatim.
func sdtEndPr(e *El) string {
	if e == nil {
		return ""
	}
	if x := e.ToXML(); len(x) <= maxSdtExtra {
		return x
	}
	return ""
}
