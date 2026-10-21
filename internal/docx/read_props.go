package docx

// w:rPr, w:pPr, table, row, cell and section property readers (Rust
// crates/docx/src/read/props.rs).

import (
	"math"
	"strconv"
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

func ptr[T any](v T) *T { return &v }

func f32p(v float32) *doc.F32 { return ptr(doc.F32(v)) }

// fclamp is Rust's f32::clamp.
func fclamp(v, lo, hi float32) float32 { return rustcompat.Clamp(v, lo, hi) }

// attr is an attribute's value, "" when absent (Rust attr(..).unwrap_or("")).
func attr(e *El, name string) string {
	v, _ := e.Attr(name)
	return v
}

// attrIs reports whether attribute name is present with value v.
func attrIs(e *El, name, v string) bool {
	a, ok := e.Attr(name)
	return ok && a == v
}

// takeChars is s.chars().take(n).collect().
func takeChars(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// textFrame reads a text frame (w:framePr without a drop cap, ECMA-376 §17.3.1.11): sizes and
// offsets in points (clamped), names kept as OOXML writes them.
func textFrame(k *El) doc.TextFrame {
	length := func(name string) (float32, bool) {
		v, ok := tw(k, name)
		if !ok || math.IsInf(float64(v), 0) || v != v {
			return 0, false
		}
		return fclamp(v, -1584, 1584), true
	}
	word := func(name string, allowed []string, def string) string {
		if v, ok := k.Attr(name); ok && contains(allowed, v) {
			return v
		}
		return def
	}
	align := func(name string, allowed []string) *string {
		if v, ok := k.Attr(name); ok && contains(allowed, v) {
			return ptr(v)
		}
		return nil
	}
	f := doc.TextFrame{
		HExact:  attrIs(k, "w:hRule", "exact"),
		XAlign:  align("w:xAlign", []string{"left", "center", "right", "inside", "outside"}),
		YAlign:  align("w:yAlign", []string{"top", "center", "bottom", "inside", "outside"}),
		HAnchor: word("w:hAnchor", []string{"text", "margin", "page"}, "page"),
		VAnchor: word("w:vAnchor", []string{"text", "margin", "page"}, "page"),
		Wrap:    word("w:wrap", []string{"around", "auto", "notBeside", "none", "tight", "through"}, "auto"),
	}
	if v, ok := length("w:w"); ok && v > 0 {
		f.W = f32p(v)
	}
	if v, ok := length("w:h"); ok && float32(math.Abs(float64(v))) > 0 {
		f.H = f32p(float32(math.Abs(float64(v))))
	}
	if v, ok := length("w:x"); ok {
		f.X = doc.F32(v)
	}
	if v, ok := length("w:y"); ok {
		f.Y = doc.F32(v)
	}
	if v, ok := length("w:hSpace"); ok {
		f.HSpace = doc.F32(rustcompat.Max(v, 0))
	}
	if v, ok := length("w:vSpace"); ok {
		f.VSpace = doc.F32(rustcompat.Max(v, 0))
	}
	return f
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// PropCtx is the context for property reading: theme fonts and colours, and style id aliases.
type PropCtx struct {
	MajorFont, MinorFont string
	// MajorCS and MinorCS are the theme's complex-script fonts (a:cs), which the …Bidi theme
	// slots name.
	MajorCS, MinorCS string
	// ThemeColors is the theme's colour scheme (dk1, lt1, dk2, lt2, accent1..6, hlink,
	// folHlink); empty when the package has none, and then w:themeColor falls back to the fixed
	// colour.
	ThemeColors []doc.RGB
	// ColorMap is w:clrSchemeMapping: the scheme slot each of text1, background1, text2,
	// background2, accent1..6, hyperlink and followedHyperlink uses. Nil = the default.
	ColorMap *[12]int
	// Alias renames style ids (e.g. a localized default paragraph style id → Normal).
	Alias map[string]string
	// Theme are the theme's colours (for WordArt effects in theme colours).
	Theme []doc.RGB
}

// levelOnly marks a w:numPr that gives a level (w:ilvl) and no list (w:numId): the list is the one
// the paragraph's style chain uses (ECMA-376 §17.3.1.19). A placeholder while reading, never left
// in a document.
const levelOnly uint32 = math.MaxUint32

// chainList is the list a level-only numPr refers to: the nearest numbering in style's based-on
// chain (its own included) that names a list.
func chainList(sheet *doc.StyleSheet, style string) (uint32, bool) {
	chain := sheet.Chain(style)
	for i := len(chain) - 1; i >= 0; i-- {
		if n := chain[i].Para.Numbering; n != nil && n.Num != levelOnly {
			return n.Num, true
		}
	}
	return 0, false
}

// resolveLevelOnly resolves level-only numbering in p against its style chain (style when it has
// none).
func resolveLevelOnly(sheet *doc.StyleSheet, p *doc.ParaProps, style string) {
	if p.Numbering == nil || p.Numbering.Num != levelOnly {
		return
	}
	if p.Style != nil {
		style = *p.Style
	}
	if num, ok := chainList(sheet, style); ok {
		p.Numbering = &doc.NumRef{Num: num, Level: p.Numbering.Level}
	} else {
		p.Numbering = nil
	}
}

// StyleID is id through the alias table.
func (pc *PropCtx) StyleID(id string) string {
	if a, ok := pc.Alias[id]; ok {
		return a
	}
	return id
}

func (pc *PropCtx) themeFont(t string) (string, bool) {
	latin, cs := pc.MinorFont, pc.MinorCS
	if strings.HasPrefix(t, "major") {
		latin, cs = pc.MajorFont, pc.MajorCS
	}
	// Measured in Word: minorBidi/majorBidi name the complex-script font, and an empty one names
	// nothing, so the font beside it or the inherited one applies.
	f := latin
	if strings.HasSuffix(t, "Bidi") {
		f = cs
	}
	return f, f != ""
}

// themeColor is the theme colour e names in attr (w:themeColor, w:themeFill), through the colour
// scheme mapping, with its tint and shade (hex bytes) applied to the HSL luminance as Word does.
func (pc *PropCtx) themeColor(e *El, attrName, tint, shade string) (doc.RGB, bool) {
	logical := func(i int) int {
		if pc.ColorMap != nil {
			return pc.ColorMap[i]
		}
		return i
	}
	v, ok := e.Attr(attrName)
	if !ok {
		return doc.RGB{}, false
	}
	var slot int
	switch v {
	case "dark1":
		slot = 0
	case "light1":
		slot = 1
	case "dark2":
		slot = 2
	case "light2":
		slot = 3
	case "text1":
		slot = logical(0)
	case "background1":
		slot = logical(1)
	case "text2":
		slot = logical(2)
	case "background2":
		slot = logical(3)
	case "accent1":
		slot = logical(4)
	case "accent2":
		slot = logical(5)
	case "accent3":
		slot = logical(6)
	case "accent4":
		slot = logical(7)
	case "accent5":
		slot = logical(8)
	case "accent6":
		slot = logical(9)
	case "hyperlink":
		slot = logical(10)
	case "followedHyperlink":
		slot = logical(11)
	default:
		return doc.RGB{}, false
	}
	if slot < 0 || slot >= len(pc.ThemeColors) {
		return doc.RGB{}, false
	}
	c := pc.ThemeColors[slot]
	byteOf := func(a string) (float64, bool) {
		v, ok := e.Attr(a)
		if !ok {
			return 0, false
		}
		n, ok := fromStrRadix16(rustcompat.Trim(v), 8)
		return float64(n) / 255.0, ok
	}
	if t, ok := byteOf(tint); ok {
		c = adjustLum(c, func(l float64) float64 { return float64(l*t) + (1.0 - t) })
	}
	if s, ok := byteOf(shade); ok {
		c = adjustLum(c, func(l float64) float64 { return l * s })
	}
	return c, true
}

// fromStrRadix16 is Rust's uN::from_str_radix(s, 16) for an unsigned type of bits bits: an
// optional +, then one or more hex digits, in range.
func fromStrRadix16(s string, bits int) (uint64, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, bits)
	return n, err == nil
}

// ShdFill is w:shd's fill colour (a theme fill first), or a solid pattern's colour.
func (pc *PropCtx) ShdFill(e *El) (doc.RGB, bool) {
	if c, ok := pc.themeColor(e, "w:themeFill", "w:themeFillTint", "w:themeFillShade"); ok {
		return c, true
	}
	// A solid pattern without a fill is painted in its pattern colour, a theme one first.
	if attrIs(e, "w:val", "solid") {
		if _, ok := parseRGBAttr(e, "w:fill"); !ok {
			if c, ok := pc.themeColor(e, "w:themeColor", "w:themeTint", "w:themeShade"); ok {
				return c, true
			}
		}
	}
	return shdFill(e)
}

func (pc *PropCtx) shdFillPtr(e *El) *doc.RGB {
	if c, ok := pc.ShdFill(e); ok {
		return &c
	}
	return nil
}

func parseRGBAttr(e *El, name string) (doc.RGB, bool) {
	v, ok := e.Attr(name)
	if !ok {
		return doc.RGB{}, false
	}
	return doc.ParseRGB(v)
}

func onOffPtr(k *El) *bool { return ptr(onOff(k)) }

// RPr reads w:rPr.
func (pc *PropCtx) RPr(e *El) doc.CharProps {
	var c doc.CharProps
	for _, k := range e.Els() {
		switch k.Name {
		case "w:rStyle":
			if v, ok := k.Attr("w:val"); ok && v != "" {
				c.Style = ptr(pc.StyleID(v))
			} else {
				c.Style = nil
			}
		case "w:rFonts":
			// A theme font supersedes the fixed font beside it (w:asciiTheme over w:ascii,
			// w:hAnsiTheme over w:hAnsi).
			named := func(a string) (string, bool) {
				v, ok := k.Attr(a)
				return v, ok && v != ""
			}
			themed := func(a string) (string, bool) {
				v, ok := k.Attr(a)
				if !ok {
					return "", false
				}
				return pc.themeFont(v)
			}
			c.Font = nil
			for _, f := range []func() (string, bool){
				func() (string, bool) { return themed("w:asciiTheme") },
				func() (string, bool) { return named("w:ascii") },
				func() (string, bool) { return themed("w:hAnsiTheme") },
				func() (string, bool) { return named("w:hAnsi") },
				func() (string, bool) {
					// w:eastAsia and w:cs name fonts for other scripts; they stand in for the
					// Latin font only when the run is hinted as that script.
					switch attr(k, "w:hint") {
					case "eastAsia":
						return named("w:eastAsia")
					case "cs":
						return named("w:cs")
					}
					return "", false
				},
			} {
				if v, ok := f(); ok {
					c.Font = ptr(v)
					break
				}
			}
			// The complex-script font. A w:cstheme font is left to inherit.
			c.FontCS = nil
			if v, ok := k.Attr("w:cs"); ok && v != "" && len(v) < 256 {
				c.FontCS = ptr(v)
			}
			// The East Asian font. A w:eastAsiaTheme font is left to inherit.
			c.FontEA = nil
			if v, ok := k.Attr("w:eastAsia"); ok && v != "" && len(v) < 256 {
				c.FontEA = ptr(v)
			}
		case "w:b":
			c.Bold = onOffPtr(k)
		case "w:bCs":
			c.BoldCS = onOffPtr(k)
		case "w:i":
			c.Italic = onOffPtr(k)
		case "w:iCs":
			c.ItalicCS = onOffPtr(k)
		case "w:cs":
			c.CS = onOffPtr(k)
		case "w:caps":
			c.Caps = onOffPtr(k)
		case "w:smallCaps":
			c.SmallCaps = onOffPtr(k)
		case "w:strike":
			c.Strike = onOffPtr(k)
		case "w:dstrike":
			c.DoubleStrike = onOffPtr(k)
		case "w:outline":
			c.Outline = onOffPtr(k)
		case "w:shadow":
			c.Shadow = onOffPtr(k)
		case "w:emboss":
			c.Emboss = onOffPtr(k)
		case "w:imprint":
			c.Engrave = onOffPtr(k)
		case "w:noProof":
			c.NoProof = onOffPtr(k)
		case "w:vanish":
			c.Hidden = onOffPtr(k)
		case "w:rtl":
			c.RTL = onOffPtr(k)
		case "w:color":
			c.Color = nil
			if t, ok := pc.themeColor(k, "w:themeColor", "w:themeTint", "w:themeShade"); ok {
				c.Color = ptr(doc.FixedColor(t))
			} else if v, ok := k.Attr("w:val"); ok {
				if asciiEqualFold(v, "auto") {
					c.Color = ptr(doc.TextColor{})
				} else if rgb, ok := doc.ParseRGB(v); ok {
					c.Color = ptr(doc.FixedColor(rgb))
				}
			}
		case "w:spacing":
			c.Spacing = nil
			if v, ok := tw(k, "w:val"); ok {
				c.Spacing = f32p(fclamp(v, -1584, 1584))
			}
		case "w:w":
			c.Scale = nil
			if v, ok := k.Attr("w:val"); ok {
				if m, ok := measure(strings.TrimRight(v, "%"), 1); ok {
					c.Scale = f32p(fclamp(m, 1, 600))
				}
			}
		case "w:kern":
			c.Kern = nil
			if v, ok := attrMeasure(k, "w:val", 2); ok {
				c.Kern = f32p(fclamp(v, 0, 1638))
			}
		case "w:position":
			c.Position = nil
			if v, ok := attrMeasure(k, "w:val", 2); ok {
				c.Position = f32p(fclamp(v, -1584, 1584))
			}
		case "w:sz":
			c.Size = nil
			if v, ok := attrMeasure(k, "w:val", 2); ok && v > 0 {
				c.Size = f32p(fclamp(v, 1, 1638))
			}
		case "w:szCs":
			c.SizeCS = nil
			if v, ok := attrMeasure(k, "w:val", 2); ok && v > 0 {
				c.SizeCS = f32p(fclamp(v, 1, 1638))
			}
		case "w:highlight":
			c.Highlight = nil
			if v, ok := k.Attr("w:val"); ok {
				c.Highlight = ptr(doc.HighlightFromOOXML(v))
			}
		case "w:u":
			// w:val has no default in the schema: without it Word draws no underline.
			c.Underline = nil
			if v, ok := k.Attr("w:val"); ok {
				c.Underline = ptr(doc.UnderlineFromOOXML(v))
			}
			c.UnderlineColor = nil
			if t, ok := pc.themeColor(k, "w:themeColor", "w:themeTint", "w:themeShade"); ok {
				c.UnderlineColor = &t
			} else if rgb, ok := parseRGBAttr(k, "w:color"); ok {
				c.UnderlineColor = &rgb
			}
		case "w:bdr":
			c.Border = ptr(border(k))
		case "w:shd":
			c.Shading = pc.shdFillPtr(k)
		case "w:vertAlign":
			c.VertAlign = nil
			if v, ok := k.Attr("w:val"); ok {
				va := doc.Baseline
				switch v {
				case "superscript":
					va = doc.Superscript
				case "subscript":
					va = doc.Subscript
				}
				c.VertAlign = &va
			}
		case "w:eastAsianLayout":
			l := eastAsianLayout(k)
			c.EastAsianLayout = nil
			if l != (doc.EastAsianLayout{}) {
				c.EastAsianLayout = &l
			}
		case "w:lang":
			c.Lang, c.LangBidi = nil, nil
			if v, ok := k.Attr("w:val"); ok && v != "" && len(v) < 64 {
				c.Lang = ptr(v)
			}
			if v, ok := k.Attr("w:bidi"); ok && v != "" && len(v) < 64 {
				c.LangBidi = ptr(v)
			}
		case "w14:ligatures":
			c.Ligatures = nil
			if v, ok := k.Attr("w14:val"); ok {
				if l, ok := doc.LigaturesFromOOXML(v); ok {
					c.Ligatures = &l
				}
			}
		default:
			if strings.HasPrefix(k.Name, "w14:") {
				if c.TextEffects == nil {
					c.TextEffects = &doc.TextEffects{}
				}
				readTextEffect(k, pc.Theme, c.TextEffects)
				if c.TextEffects.IsEmpty() {
					c.TextEffects = nil
				}
			}
		}
	}
	return c
}

// PPr reads w:pPr (without w:sectPr; w:rPr is the paragraph mark, returned separately). A
// w:numPr with a level and no list sets levelOnly, resolved by the caller.
func (pc *PropCtx) PPr(e *El) (doc.ParaProps, doc.CharProps) {
	var p doc.ParaProps
	var mark doc.CharProps
	for _, k := range e.Els() {
		switch k.Name {
		case "w:pStyle":
			p.Style = nil
			if v, ok := k.Attr("w:val"); ok && v != "" {
				p.Style = ptr(pc.StyleID(v))
			}
		case "w:keepNext":
			p.KeepNext = onOffPtr(k)
		case "w:keepLines":
			p.KeepLines = onOffPtr(k)
		case "w:pageBreakBefore":
			p.PageBreakBefore = onOffPtr(k)
		case "w:widowControl":
			p.WidowControl = onOffPtr(k)
		case "w:contextualSpacing":
			p.ContextualSpacing = onOffPtr(k)
		case "w:mirrorIndents":
			p.MirrorIndents = onOffPtr(k)
		case "w:suppressAutoHyphens":
			p.SuppressHyphens = onOffPtr(k)
		case "w:suppressLineNumbers":
			p.SuppressLineNumbers = onOffPtr(k)
		case "w:bidi":
			p.Bidi = onOffPtr(k)
		case "w:kinsoku":
			p.Kinsoku = onOffPtr(k)
		case "w:wordWrap":
			p.WordWrap = onOffPtr(k)
		case "w:overflowPunct":
			p.OverflowPunct = onOffPtr(k)
		case "w:topLinePunct":
			p.TopLinePunct = onOffPtr(k)
		case "w:autoSpaceDE":
			p.AutoSpaceDE = onOffPtr(k)
		case "w:autoSpaceDN":
			p.AutoSpaceDN = onOffPtr(k)
		case "w:snapToGrid":
			p.SnapToGrid = onOffPtr(k)
		case "w:framePr":
			if kind, ok := k.Attr("w:dropCap"); ok && (kind == "drop" || kind == "margin") {
				lines := uint32(3)
				if v, ok := k.Attr("w:lines"); ok {
					if n, ok := u32Of(v); ok {
						lines = n
					}
				}
				p.DropCap = ptr(uint8(min(max(lines, 1), 10)))
				p.DropCapMargin = nil
				if kind == "margin" {
					p.DropCapMargin = ptr(true)
				}
				p.DropCapDistance = nil
				if d, ok := tw(k, "w:hSpace"); ok && !math.IsInf(float64(d), 0) && d == d && d > 0 {
					p.DropCapDistance = f32p(rustcompat.Min(d, maxDropCapDistance))
				}
			} else {
				p.Frame = ptr(textFrame(k))
			}
		case "w:numPr":
			num, hasNum := childU32(k, "w:numId")
			ilvl, hasLvl := childU32(k, "w:ilvl")
			level := uint8(min(ilvl, 8))
			switch {
			case hasNum:
				p.Numbering = &doc.NumRef{Num: num, Level: level}
			case hasLvl:
				// A level without a list: the list comes from the based-on style, as in a
				// Heading 3 that only says "level 3" of Heading 2's list.
				p.Numbering = &doc.NumRef{Num: levelOnly, Level: level}
			}
		case "w:pBdr":
			p.Borders = ptr(borders(k))
		case "w:shd":
			p.Shading = pc.shdFillPtr(k)
		case "w:tabs":
			tabs := doc.Vec[doc.TabStop]{}
			for i, t := range k.Children("w:tab") {
				if i >= 64 {
					break
				}
				if s, ok := tabStop(t); ok {
					tabs = append(tabs, s)
				}
			}
			p.Tabs = &tabs
		case "w:spacing":
			if v, ok := tw(k, "w:before"); ok {
				p.SpaceBefore = f32p(rustcompat.Max(v, 0))
			}
			if v, ok := tw(k, "w:after"); ok {
				p.SpaceAfter = f32p(rustcompat.Max(v, 0))
			}
			// HTML-style automatic spacing; "0" switches an inherited one off.
			auto := func(name string) (bool, bool) {
				v, ok := k.Attr(name)
				if !ok {
					return false, false
				}
				switch rustcompat.Trim(v) {
				case "1", "true", "on":
					return true, true
				}
				return false, true
			}
			if a, ok := auto("w:beforeAutospacing"); ok {
				p.SpaceBeforeAuto = &a
			}
			if a, ok := auto("w:afterAutospacing"); ok {
				p.SpaceAfterAuto = &a
			}
			if line, ok := k.Attr("w:line"); ok {
				// Word reads the rule case-insensitively (atleast is seen in the wild).
				rule := "auto"
				if v, ok := k.Attr("w:lineRule"); ok {
					rule = toASCIILower(v)
				}
				p.LineSpacing = nil
				switch rule {
				case "exact":
					if v, ok := measure(line, 20); ok {
						p.LineSpacing = &doc.LineSpacing{Rule: doc.LineExactly, Value: doc.F32(abs32(v))}
					}
				case "atleast":
					if v, ok := measure(line, 20); ok {
						p.LineSpacing = &doc.LineSpacing{Rule: doc.LineAtLeast, Value: doc.F32(abs32(v))}
					}
				default:
					// Strict OOXML writes the height with a unit (13.90pt).
					if v, ok := lineMultiple(line); ok {
						p.LineSpacing = &doc.LineSpacing{Rule: doc.LineMultiple, Value: doc.F32(fclamp(abs32(v), 0.06, 132))}
					}
				}
			}
		case "w:ind":
			if v, ok := tw(k, "w:left"); ok {
				p.IndentLeft = f32p(v)
			} else if v, ok := tw(k, "w:start"); ok {
				p.IndentLeft = f32p(v)
			}
			if v, ok := tw(k, "w:right"); ok {
				p.IndentRight = f32p(v)
			} else if v, ok := tw(k, "w:end"); ok {
				p.IndentRight = f32p(v)
			}
			if v, ok := tw(k, "w:hanging"); ok {
				p.IndentFirst = f32p(-v)
			} else if v, ok := tw(k, "w:firstLine"); ok {
				p.IndentFirst = f32p(v)
			}
			// Character units (hundredths of a character), which win when non-zero.
			chars := func(a string) (float32, bool) {
				v, ok := k.Attr(a)
				if !ok {
					return 0, false
				}
				n, ok := intOf(v)
				if !ok {
					return 0, false
				}
				return float32(min(max(n, -158_400), 158_400)) / 100, true
			}
			if v, ok := chars("w:leftChars"); ok {
				p.IndentLeftChars = f32p(v)
			} else if v, ok := chars("w:startChars"); ok {
				p.IndentLeftChars = f32p(v)
			}
			if v, ok := chars("w:rightChars"); ok {
				p.IndentRightChars = f32p(v)
			} else if v, ok := chars("w:endChars"); ok {
				p.IndentRightChars = f32p(v)
			}
			h, hOK := chars("w:hangingChars")
			f, fOK := chars("w:firstLineChars")
			switch {
			case hOK && (h != 0 || !fOK):
				p.IndentFirstChars = f32p(-h)
			case fOK:
				p.IndentFirstChars = f32p(f)
			}
		case "w:jc":
			p.Align = nil
			if v, ok := k.Attr("w:val"); ok {
				p.Align = ptr(align(v))
			}
		case "w:outlineLvl":
			p.OutlineLevel = nil
			if v, ok := k.Attr("w:val"); ok {
				if n, ok := u32Of(v); ok {
					p.OutlineLevel = ptr(uint8(min(n, 9)))
				}
			}
		case "w:rPr":
			mark = pc.RPr(k)
		}
	}
	return p, mark
}

// maxDropCapDistance is Rust resolve::MAX_DROP_CAP_DISTANCE.
const maxDropCapDistance float32 = 1584.0

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

// childU32 is u32Of of a child's w:val.
func childU32(e *El, name string) (uint32, bool) {
	v, ok := e.ChildVal(name)
	if !ok {
		return 0, false
	}
	return u32Of(v)
}

func childInt(e *El, name string) (int64, bool) {
	v, ok := e.ChildVal(name)
	if !ok {
		return 0, false
	}
	return intOf(v)
}

func align(v string) doc.Align {
	switch v {
	case "center":
		return doc.AlignCenter
	case "right", "end":
		return doc.AlignRight
	case "both", "justify", "lowKashida", "mediumKashida", "highKashida":
		return doc.AlignJustify
	case "distribute", "thaiDistribute":
		return doc.AlignDistribute
	}
	return doc.AlignLeft
}

func tabStop(t *El) (doc.TabStop, bool) {
	pos, ok := tw(t, "w:pos")
	if !ok {
		return doc.TabStop{}, false
	}
	a := doc.TabLeft
	v, ok := t.Attr("w:val")
	if !ok {
		v = "left"
	}
	switch v {
	case "center":
		a = doc.TabCenter
	case "right", "end":
		a = doc.TabRight
	case "decimal":
		a = doc.TabDecimal
	case "bar":
		a = doc.TabBar
	case "clear":
		a = doc.TabClear
	}
	leader, ok := t.Attr("w:leader")
	if !ok {
		leader = "none"
	}
	return doc.TabStop{Pos: doc.F32(pos), Align: a, Leader: tabLeader(leader)}, true
}

// tabLeader reads a tab stop's or positional tab's w:leader (ST_TabTlc, ST_PTabLeader).
func tabLeader(v string) doc.TabLeader {
	switch v {
	case "dot":
		return doc.LeaderDot
	case "hyphen":
		return doc.LeaderHyphen
	case "underscore", "heavy":
		return doc.LeaderUnderscore
	case "middleDot":
		return doc.LeaderMiddleDot
	}
	return doc.LeaderNone
}

// shdFill is w:shd/@w:fill (a solid fill colour; auto = none), ignoring theme fills.
func shdFill(e *El) (doc.RGB, bool) {
	if c, ok := parseRGBAttr(e, "w:fill"); ok {
		return c, true
	}
	// A solid pattern uses the pattern colour.
	if attrIs(e, "w:val", "solid") {
		return parseRGBAttr(e, "w:color")
	}
	return doc.RGB{}, false
}

func border(e *El) doc.Border {
	style, ok := e.Attr("w:val")
	if !ok {
		style = "single"
	}
	b := doc.Border{Style: doc.BorderStyleFromOOXML(style), Width: 0.5}
	if v, ok := attrMeasure(e, "w:sz", 8); ok {
		b.Width = doc.F32(v)
	}
	b.Width = doc.F32(fclamp(float32(b.Width), 0, 12))
	if c, ok := parseRGBAttr(e, "w:color"); ok {
		b.Color = &c
	}
	if v, ok := attrMeasure(e, "w:space", 1); ok {
		b.Space = doc.F32(v)
	}
	b.Space = doc.F32(fclamp(float32(b.Space), 0, 31))
	return b
}

// borders reads w:pBdr, w:tblBorders, w:tcBorders, w:pgBorders.
func borders(e *El) doc.Borders {
	var b doc.Borders
	for _, k := range e.Els() {
		switch k.Local() {
		case "top":
			b.Top = ptr(border(k))
		case "left", "start":
			b.Left = ptr(border(k))
		case "bottom":
			b.Bottom = ptr(border(k))
		case "right", "end":
			b.Right = ptr(border(k))
		case "between", "insideH":
			b.Between = ptr(border(k))
		case "insideV":
			b.InsideV = ptr(border(k))
		}
	}
	return b
}

// margins reads a margins element (w:tblCellMar, w:tcMar) as [top, left, bottom, right].
func margins(e *El) [4]doc.F32 {
	var out [4]doc.F32
	for i, s := range marginSides(e) {
		if s != nil {
			out[i] = *s
		}
	}
	return out
}

func marginSides(e *El) [4]*doc.F32 {
	get := func(names ...string) *doc.F32 {
		for _, n := range names {
			if c := e.Child(n); c != nil {
				if v, ok := tw(c, "w:w"); ok {
					return f32p(fclamp(v, 0, 1584))
				}
				return nil
			}
		}
		return nil
	}
	return [4]*doc.F32{get("w:top"), get("w:left", "w:start"), get("w:bottom"), get("w:right", "w:end")}
}

// pctWidth reads a w:w in fiftieths of a percent, or a percentage with a % sign.
func pctWidth(w string, hi float32) *doc.F32 {
	var v float32
	var ok bool
	if p, cut := strings.CutSuffix(w, "%"); cut {
		v, ok = measure(p, 1)
	} else {
		v, ok = measure(w, 50)
	}
	if !ok {
		return nil
	}
	v = fclamp(v, 0, hi)
	if v > 0 {
		return f32p(v)
	}
	return nil
}

// TblPr reads w:tblPr.
func (pc *PropCtx) TblPr(e *El) doc.TableProps {
	var t doc.TableProps
	t.SetDefaults()
	for _, k := range e.Els() {
		switch k.Name {
		case "w:tblStyle":
			t.Style = nil
			if v, ok := k.Attr("w:val"); ok && v != "" {
				t.Style = ptr(pc.StyleID(v))
			}
		case "w:tblW":
			ty, ok := k.Attr("w:type")
			if !ok {
				ty = "dxa"
			}
			w, ok := k.Attr("w:w")
			if !ok {
				w = "0"
			}
			switch ty {
			case "pct":
				t.WidthPct = pctWidth(w, 1000)
			case "auto", "nil":
			default:
				t.Width = nil
				if v, ok := measure(w, 20); ok && v > 0 {
					t.Width = f32p(v)
				}
			}
		case "w:jc":
			t.Align = nil
			if v, ok := k.Attr("w:val"); ok {
				t.Align = ptr(align(v))
			}
		case "w:tblInd":
			t.Indent = nil
			if v, ok := tw(k, "w:w"); ok {
				t.Indent = f32p(v)
			}
		case "w:tblBorders":
			t.Borders = ptr(borders(k))
		case "w:shd":
			t.Shading = pc.shdFillPtr(k)
		case "w:tblLayout":
			t.Fixed = attrIs(k, "w:type", "fixed")
		case "w:bidiVisual":
			t.BidiVisual = onOff(k)
		case "w:tblCellMar":
			t.CellMargins = ptr(margins(k))
		case "w:tblLook":
			t.Look = look(k)
		case "w:tblpPr":
			t.Float = ptr(tableFloat(k))
		case "w:tblOverlap":
			if t.Float != nil {
				t.Float.Overlap = !attrIs(k, "w:val", "never")
			}
		case "w:tblCaption":
			t.Caption = nil
			if v, ok := k.Attr("w:val"); ok {
				t.Caption = ptr(doc.CapAlt(v))
			}
		case "w:tblDescription":
			t.Description = nil
			if v, ok := k.Attr("w:val"); ok {
				t.Description = ptr(doc.CapAlt(v))
			}
		}
	}
	return t
}

// look reads w:tblLook in either the attribute form or the legacy hex bitmask form.
func look(e *El) doc.TableLook {
	l := doc.DefaultTableLook
	if v, ok := e.Attr("w:val"); ok {
		bits := uint32(0x04A0)
		if n, ok := fromStrRadix16(rustcompat.Trim(v), 32); ok {
			bits = uint32(n)
		}
		l.HeaderRow = bits&0x0020 != 0
		l.TotalRow = bits&0x0040 != 0
		l.FirstColumn = bits&0x0080 != 0
		l.LastColumn = bits&0x0100 != 0
		l.BandedRows = bits&0x0200 == 0
		l.BandedColumns = bits&0x0400 == 0
	}
	b := func(name string) (bool, bool) {
		v, ok := e.Attr(name)
		if !ok {
			return false, false
		}
		return !(v == "0" || v == "false" || v == "off"), true
	}
	if v, ok := b("w:firstRow"); ok {
		l.HeaderRow = v
	}
	if v, ok := b("w:lastRow"); ok {
		l.TotalRow = v
	}
	if v, ok := b("w:firstColumn"); ok {
		l.FirstColumn = v
	}
	if v, ok := b("w:lastColumn"); ok {
		l.LastColumn = v
	}
	if v, ok := b("w:noHBand"); ok {
		l.BandedRows = !v
	}
	if v, ok := b("w:noVBand"); ok {
		l.BandedColumns = !v
	}
	return l
}

// trPr reads w:trPr.
func trPr(e *El) doc.RowProps {
	var r doc.RowProps
	for _, k := range e.Els() {
		switch k.Name {
		case "w:trHeight":
			r.Height = nil
			if v, ok := tw(k, "w:val"); ok {
				r.Height = f32p(abs32(v))
			}
			r.HeightRule = doc.HeightAtLeast
			if v, ok := k.Attr("w:hRule"); ok {
				switch toASCIILower(v) {
				case "exact":
					r.HeightRule = doc.HeightExact
				case "auto":
					r.HeightRule = doc.HeightAuto
				}
			}
		case "w:tblHeader":
			r.Header = onOff(k)
		case "w:cantSplit":
			r.CantSplit = onOff(k)
		case "w:gridBefore":
			r.GridBefore = gridCount(k)
		case "w:gridAfter":
			r.GridAfter = gridCount(k)
		case "w:wBefore":
			r.WBefore = gapWidth(k)
		case "w:wAfter":
			r.WAfter = gapWidth(k)
		}
	}
	return r
}

func gridCount(k *El) uint32 {
	n := uint32(0)
	if v, ok := k.Attr("w:val"); ok {
		if m, ok := u32Of(v); ok {
			n = m
		}
	}
	return min(n, 63)
}

// gapWidth reads w:wBefore/w:wAfter in points: only a width in twips (dxa) is kept.
func gapWidth(k *El) *doc.F32 {
	if ty, ok := k.Attr("w:type"); ok && ty != "dxa" {
		return nil
	}
	if v, ok := tw(k, "w:w"); ok && v >= 0 {
		return f32p(rustcompat.Min(v, MaxLenPt))
	}
	return nil
}

// tcPr reads w:tcPr; it returns the props and whether the cell is a legacy hMerge continuation.
func tcPr(pc *PropCtx, e *El) (doc.CellProps, bool) {
	c := doc.CellProps{Span: 1}
	hcont := false
	for _, k := range e.Els() {
		switch k.Name {
		case "w:tcW":
			ty, ok := k.Attr("w:type")
			switch {
			case !ok || ty == "dxa":
				c.Width = nil
				if v, ok := tw(k, "w:w"); ok && v > 0 {
					c.Width = f32p(v)
				}
			case ty == "pct":
				w, ok := k.Attr("w:w")
				if !ok {
					w = "0"
				}
				c.WidthPct = pctWidth(w, 100)
			}
		case "w:gridSpan":
			n := uint32(1)
			if v, ok := k.Attr("w:val"); ok {
				if m, ok := u32Of(v); ok {
					n = m
				}
			}
			c.Span = min(max(n, 1), 63)
		case "w:hMerge":
			hcont = !attrIs(k, "w:val", "restart")
		case "w:vMerge":
			c.VMerge = doc.VMergeContinue
			if attrIs(k, "w:val", "restart") {
				c.VMerge = doc.VMergeRestart
			}
		case "w:tcBorders":
			c.Borders = ptr(borders(k))
		case "w:shd":
			c.Shading = pc.shdFillPtr(k)
		case "w:noWrap":
			c.NoWrap = onOff(k)
		case "w:hideMark":
			c.HideMark = onOff(k)
		case "w:tcMar":
			sides := marginSides(k)
			if sides[0] != nil && sides[1] != nil && sides[2] != nil && sides[3] != nil {
				c.Margins = &[4]doc.F32{*sides[0], *sides[1], *sides[2], *sides[3]}
			} else {
				c.PartialMargins = &sides
			}
		case "w:textDirection":
			c.TextDirection = doc.TextDirectionFromOOXML(attr(k, "w:val"))
			c.TextDirectionSet = attrIs(k, "w:val", "lrTb")
		case "w:vAlign":
			va := doc.VAlignTop
			switch attr(k, "w:val") {
			case "center":
				va = doc.VAlignCenter
			case "bottom":
				va = doc.VAlignBottom
			}
			c.VAlign = &va
		}
	}
	return c, hcont
}

// hfRef is a header/footer reference found in a w:sectPr.
type hfRef struct {
	footer    bool
	kind, rid string
}

// sectPr reads w:sectPr (header/footer references are returned for the caller to resolve).
func sectPr(e *El) (doc.SectionProps, []hfRef) {
	s := doc.NewSectionProps()
	var refs []hfRef
	for _, k := range e.Els() {
		switch k.Name {
		case "w:headerReference", "w:footerReference":
			if rid, ok := k.Attr("r:id"); ok {
				kind, ok := k.Attr("w:type")
				if !ok {
					kind = "default"
				}
				refs = append(refs, hfRef{footer: k.Name == "w:footerReference", kind: kind, rid: rid})
			}
		case "w:type":
			s.Start = doc.StartNextPage
			switch attr(k, "w:val") {
			case "continuous":
				s.Start = doc.StartContinuous
			case "evenPage":
				s.Start = doc.StartEvenPage
			case "oddPage":
				s.Start = doc.StartOddPage
			case "nextColumn":
				s.Start = doc.StartNextColumn
			}
		case "w:pgSz":
			if w, ok := tw(k, "w:w"); ok && w >= 36 {
				s.PageW = doc.F32(w)
			}
			if h, ok := tw(k, "w:h"); ok && h >= 36 {
				s.PageH = doc.F32(h)
			}
			if o, ok := k.Attr("w:orient"); ok {
				s.Landscape = o == "landscape"
			} else {
				s.Landscape = s.PageW > s.PageH
			}
		case "w:pgMar":
			m := func(n string, d doc.F32) doc.F32 {
				v := float32(d)
				if t, ok := tw(k, n); ok {
					v = abs32(t)
				}
				return doc.F32(rustcompat.Min(v, rustcompat.Max(float32(s.PageW), float32(s.PageH))))
			}
			s.MarginTop = m("w:top", s.MarginTop)
			s.MarginBottom = m("w:bottom", s.MarginBottom)
			s.MarginLeft = m("w:left", s.MarginLeft)
			s.MarginRight = m("w:right", s.MarginRight)
			s.Header = m("w:header", s.Header)
			s.Footer = m("w:footer", s.Footer)
			s.Gutter = m("w:gutter", s.Gutter)
		case "w:cols":
			var c doc.Columns
			c.SetDefaults()
			n := uint32(1)
			if v, ok := k.Attr("w:num"); ok {
				if m, ok := u32Of(v); ok {
					n = m
				}
			}
			c.Count = min(max(n, 1), 45)
			if sp, ok := tw(k, "w:space"); ok {
				c.Space = doc.F32(rustcompat.Max(sp, 0))
			}
			if v, ok := k.Attr("w:sep"); ok {
				c.Separator = !(v == "0" || v == "false" || v == "off")
			}
			equal := true
			if v, ok := k.Attr("w:equalWidth"); ok {
				equal = !(v == "0" || v == "false" || v == "off")
			}
			if !equal {
				c.Widths = doc.Vec[[2]doc.F32]{}
				for i, col := range k.Children("w:col") {
					if i >= 45 {
						break
					}
					w, _ := tw(col, "w:w")
					sp, _ := tw(col, "w:space")
					c.Widths = append(c.Widths, [2]doc.F32{doc.F32(rustcompat.Max(w, 0)), doc.F32(rustcompat.Max(sp, 0))})
				}
				if len(c.Widths) > 0 {
					c.Count = uint32(len(c.Widths))
				}
			}
			s.Columns = c
		case "w:footnotePr":
			s.FootnotePr = notePr(k, true)
		case "w:endnotePr":
			s.EndnotePr = notePr(k, true)
		case "w:formProt":
			s.FormProt = onOffPtr(k)
		case "w:titlePg":
			s.TitlePage = onOff(k)
		case "w:bidi":
			s.RTL = onOff(k)
		case "w:textDirection":
			s.TextDirection = doc.TextDirectionFromOOXML(attr(k, "w:val"))
			s.TextDirectionSet = attrIs(k, "w:val", "lrTb")
		case "w:pgNumType":
			s.PageNumStart = nil
			if v, ok := k.Attr("w:start"); ok {
				if n, ok := u32Of(v); ok {
					s.PageNumStart = &n
				}
			}
			if f, ok := k.Attr("w:fmt"); ok {
				s.PageNumFormat = doc.NumFormatFromOOXML(f)
			}
			s.PageNumSet = s.PageNumStart == nil && s.PageNumFormat == doc.FmtDecimal
		case "w:lnNumType":
			var l doc.LineNumbering
			l.SetDefaults()
			if v, ok := k.Attr("w:countBy"); ok {
				if n, ok := u32Of(v); ok {
					l.CountBy = min(max(n, 1), 100)
				}
			}
			if v, ok := k.Attr("w:start"); ok {
				if n, ok := intOf(v); ok {
					l.Start = uint32(min(max(n, 0), 32_767) + 1)
				}
			}
			d, _ := tw(k, "w:distance")
			l.Distance = doc.F32(rustcompat.Max(d, 0))
			switch attr(k, "w:restart") {
			case "newSection":
				l.Restart = doc.LinesSection
			case "continuous":
				l.Restart = doc.LinesContinuous
			default:
				l.Restart = doc.LinesPage
			}
			s.LineNumbers = &l
		case "w:vAlign":
			switch attr(k, "w:val") {
			case "center":
				s.VAlign = doc.PageCenter
			case "both":
				s.VAlign = doc.PageJustify
			case "bottom":
				s.VAlign = doc.PageBottom
			default:
				s.VAlign = doc.PageTop
			}
		case "w:pgBorders":
			s.PageBorders = ptr(borders(k))
		case "w:docGrid":
			var g doc.DocGrid
			g.SetDefaults()
			if v, ok := k.Attr("w:type"); ok {
				if t, ok := doc.DocGridTypeFromOOXML(v); ok {
					g.Kind = t
				}
			}
			if p, ok := tw(k, "w:linePitch"); ok && !math.IsInf(float64(p), 0) && p == p && p > 0 {
				g.LinePitch = doc.F32(rustcompat.Min(p, doc.MaxGridPitch))
			}
			if v, ok := k.Attr("w:charSpace"); ok {
				if c, ok := intOf(v); ok {
					g.CharSpace = int32(min(max(c, -doc.MaxCharSpace), doc.MaxCharSpace))
				}
			}
			s.DocGrid = &g
		}
	}
	return s, refs
}

// notePr reads w:footnotePr / w:endnotePr (ECMA-376 §17.11): placement, number format (only kept
// when fmt: the document's goes to its own setting), start and restart. A start out of range is
// clamped; unknown values are left unset.
func notePr(e *El, fmt bool) doc.NoteProps {
	var n doc.NoteProps
	if v, ok := e.ChildVal("w:pos"); ok {
		if p, ok := doc.NotePosFromOOXML(v); ok {
			n.Pos = &p
		}
	}
	if fmt {
		if v, ok := e.ChildVal("w:numFmt"); ok {
			n.NumFmt = ptr(doc.NumFormatFromOOXML(v))
		}
	}
	if v, ok := childInt(e, "w:numStart"); ok {
		n.NumStart = ptr(uint32(min(max(v, 1), doc.MaxNoteStart)))
	}
	if v, ok := e.ChildVal("w:numRestart"); ok {
		if r, ok := doc.NoteRestartFromOOXML(v); ok {
			n.NumRestart = &r
		}
	}
	return n
}

// tableFloat reads w:tblpPr: a floating table's anchors, offsets or alignments and distances
// from text.
func tableFloat(e *El) doc.TableFloat {
	pt := func(n string) float32 {
		if v, ok := attrMeasure(e, n, 20); ok {
			return fclamp(v, -31_680, 31_680)
		}
		return 0
	}
	dist := func(n string) doc.F32 { return doc.F32(fclamp(pt(n), 0, 1584)) }
	rel := func(n string, text doc.Anchor) doc.Anchor {
		switch attr(e, n) {
		case "page":
			return doc.AnchorPage
		case "margin":
			return doc.AnchorMargin
		}
		return text
	}
	alignOf := func(n string) *doc.FloatAlign {
		if v, ok := e.Attr(n); ok {
			if a, ok := doc.FloatAlignFromOOXML(v); ok {
				return &a
			}
		}
		return nil
	}
	return doc.TableFloat{
		HRel:    rel("w:horzAnchor", doc.AnchorColumn),
		VRel:    rel("w:vertAnchor", doc.AnchorParagraph),
		X:       doc.F32(pt("w:tblpX")),
		Y:       doc.F32(pt("w:tblpY")),
		HAlign:  alignOf("w:tblpXSpec"),
		VAlign:  alignOf("w:tblpYSpec"),
		Dist:    [4]doc.F32{dist("w:leftFromText"), dist("w:topFromText"), dist("w:rightFromText"), dist("w:bottomFromText")},
		Overlap: true,
	}
}

// eastAsianLayout reads w:eastAsianLayout (ISO/IEC 29500-1 §17.3.2.10). Attributes the schema
// doesn't allow (an id out of xsd:int, an unknown bracket, an on/off that isn't one) are left
// unset.
func eastAsianLayout(e *El) doc.EastAsianLayout {
	flagOf := func(name string) *bool {
		v, ok := e.Attr(name)
		if !ok {
			return nil
		}
		switch rustcompat.Trim(v) {
		case "1", "true", "on":
			return ptr(true)
		case "0", "false", "off":
			return ptr(false)
		}
		return nil
	}
	var l doc.EastAsianLayout
	if v, ok := e.Attr("w:id"); ok {
		if n, ok := intOf(v); ok && n >= math.MinInt32 && n <= math.MaxInt32 {
			l.ID = ptr(int32(n))
		}
	}
	l.Combine = flagOf("w:combine")
	if v, ok := e.Attr("w:combineBrackets"); ok {
		if b, ok := doc.CombineBracketsFromOOXML(rustcompat.Trim(v)); ok {
			l.CombineBrackets = &b
		}
	}
	l.Vert = flagOf("w:vert")
	l.VertCompress = flagOf("w:vertCompress")
	return l
}

// toASCIILower is Rust's to_ascii_lowercase.
func toASCIILower(s string) string { return toLowerASCII(s) }
