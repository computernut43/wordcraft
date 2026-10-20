package doc

import (
	"testing"

	"github.com/computernut43/craft-libs/serdejson"
)

// The tests of crates/doc/src/props.rs.

func TestRGBHex(t *testing.T) {
	if got := (RGB{255, 0, 16}).Hex(); got != "FF0010" {
		t.Fatal(got)
	}
	if c, ok := ParseRGB("#FF0010"); !ok || c != (RGB{255, 0, 16}) {
		t.Fatal(c, ok)
	}
	for _, bad := range []string{"zzzzzz", "é1234"} {
		if _, ok := ParseRGB(bad); ok {
			t.Errorf("%q read", bad)
		}
	}
}

func TestOverlayKeepsUnset(t *testing.T) {
	base := CharProps{Bold: ptr(true), Size: ptr(F32(12))}
	patch := CharProps{Size: ptr(F32(14)), Italic: ptr(true)}
	r := base.Overlaid(&patch)
	if *r.Bold != true || *r.Size != 14 || *r.Italic != true {
		t.Fatalf("%+v", r)
	}
}

func TestBordersOverlayPerSide(t *testing.T) {
	thin, thick := SingleBorder(0.5), SingleBorder(2)
	nil_ := Border{}
	b := AllBorders(thin)
	b.Overlay(&Borders{Top: &thick})
	if *b.Top != thick || *b.Bottom != thin {
		t.Fatalf("%+v", b)
	}
	box := BoxBorders(nil_)
	b.Overlay(&box)
	if *b.Left != nil_ {
		t.Fatal("an explicit nil border overrides that side")
	}
	if *b.Between != thin {
		t.Fatal("untouched sides survive")
	}
	b.Overlay(&Borders{})
	if *b.Left != nil_ {
		t.Fatal("an empty patch changes nothing")
	}
}

func TestOOXMLNamesRoundTrip(t *testing.T) {
	for _, u := range Underlines() {
		if UnderlineFromOOXML(u.OOXML()) != u {
			t.Errorf("underline %d", u)
		}
	}
	for _, h := range Highlights() {
		if HighlightFromOOXML(h.OOXML()) != h {
			t.Errorf("highlight %d", h)
		}
	}
}

// The 16 w14:ligatures values of [MS-DOCX], each to its flags and back.
func TestLigatureNames(t *testing.T) {
	l := func(s, c, h, d bool) Ligatures { return Ligatures{s, c, h, d} }
	for _, c := range []struct {
		name  string
		flags Ligatures
	}{
		{"none", l(false, false, false, false)},
		{"standard", l(true, false, false, false)},
		{"contextual", l(false, true, false, false)},
		{"historical", l(false, false, true, false)},
		{"discretional", l(false, false, false, true)},
		{"standardContextual", l(true, true, false, false)},
		{"standardHistorical", l(true, false, true, false)},
		{"contextualHistorical", l(false, true, true, false)},
		{"standardDiscretional", l(true, false, false, true)},
		{"contextualDiscretional", l(false, true, false, true)},
		{"historicalDiscretional", l(false, false, true, true)},
		{"standardContextualHistorical", l(true, true, true, false)},
		{"standardContextualDiscretional", l(true, true, false, true)},
		{"standardHistoricalDiscretional", l(true, false, true, true)},
		{"contextualHistoricalDiscretional", l(false, true, true, true)},
		{"all", l(true, true, true, true)},
	} {
		if got, ok := LigaturesFromOOXML(c.name); !ok || got != c.flags {
			t.Errorf("%s: %+v", c.name, got)
		}
		if got := c.flags.OOXML(); got != c.name {
			t.Errorf("%+v: %s", c.flags, got)
		}
	}
	if len(AllLigatures()) != 16 {
		t.Fatal(len(AllLigatures()))
	}
	for _, bad := range []string{"", "Standard", "contextualStandard", "standardstandard", "allx"} {
		if _, ok := LigaturesFromOOXML(bad); ok {
			t.Errorf("%q read", bad)
		}
	}
}

func TestTextDirectionNames(t *testing.T) {
	d := Horizontal
	for range 3 {
		if TextDirectionFromOOXML(d.OOXML()) != d {
			t.Errorf("%d", d)
		}
		d = d.Next()
	}
	if d != Horizontal {
		t.Fatal("the button cycles through three directions")
	}
	// Strict names, East Asian vertical layouts and junk.
	for name, want := range map[string]TextDirection{"rl": Down, "lr": Up, "tbRlV": Down, "tb": Horizontal, "lrTbV": Horizontal, "é?": Horizontal} {
		if got := TextDirectionFromOOXML(name); got != want {
			t.Errorf("%s: %d", name, got)
		}
	}
}

// #865: a colour reads as format.color takes it (RRGGBB, #RRGGBB, auto), while what documents
// and settings already hold reads and writes exactly as before.
func TestTextColorReadsHexAndKeepsItsSerializedForms(t *testing.T) {
	read := func(j string) (TextColor, error) {
		var c TextColor
		err := serdejson.Unmarshal([]byte(j), &c)
		return c, err
	}
	ink := FixedColor(RGB{0x1B, 0x16, 0x11})
	for j, want := range map[string]TextColor{`"1B1611"`: ink, `"#1b1611"`: ink, `"auto"`: {}, `"Automatic"`: {}, `{"Rgb":"1B1611"}`: ink} {
		if got, err := read(j); err != nil || got != want {
			t.Errorf("%s: %+v %v", j, got, err)
		}
	}
	for c, j := range map[TextColor]string{{}: `"Auto"`, ink: `{"Rgb":[27,22,17]}`} {
		if got, err := serdejson.MarshalCompact(c); err != nil || string(got) != j {
			t.Errorf("written %s, want %s", got, j)
		}
		if got, err := read(j); err != nil || got != c {
			t.Errorf("%s: %+v %v", j, got, err)
		}
	}
	for _, bad := range []string{`"1B161"`, `"zzzzzz"`, `""`, `{"Rgb":"1B16"}`, `{"Rgb":[1,2]}`, `{"Hex":[1,2,3]}`, "7", "[27,22,17]"} {
		if _, err := read(bad); err == nil {
			t.Errorf("%s read", bad)
		}
	}
	var p CharProps
	if err := serdejson.Unmarshal([]byte(`{"color": "#1B1611", "bold": true}`), &p); err != nil || *p.Color != ink || !*p.Bold {
		t.Fatalf("%+v %v", p, err)
	}
}

// #865: an RGB reads RRGGBB / #RRGGBB as well as the [r, g, b] it still writes.
func TestRGBReadsHexAndKeepsItsSerializedForm(t *testing.T) {
	ink := RGB{0x1B, 0x16, 0x11}
	if got, _ := serdejson.MarshalCompact(ink); string(got) != "[27,22,17]" {
		t.Fatalf("written %s", got)
	}
	for _, j := range []string{"[27,22,17]", `"1B1611"`, `"#1b1611"`} {
		var c RGB
		if err := serdejson.Unmarshal([]byte(j), &c); err != nil || c != ink {
			t.Errorf("%s: %+v %v", j, c, err)
		}
	}
	for _, bad := range []string{"[27,22]", "[27,22,17,0]", "[27,22,300]", "[-1,0,0]", `"auto"`, `"1B161"`, `""`, "7", "null", `{"r":1}`} {
		var c RGB
		if serdejson.Unmarshal([]byte(bad), &c) == nil {
			t.Errorf("%s read", bad)
		}
	}
}

func TestPropsSerdeSkipNone(t *testing.T) {
	p := CharProps{Bold: ptr(true)}
	s, err := serdejson.MarshalCompact(p)
	if err != nil || string(s) != `{"bold":true}` {
		t.Fatalf("%s %v", s, err)
	}
	var back CharProps
	if err := serdejson.Unmarshal(s, &back); err != nil || !back.Equal(p) {
		t.Fatalf("%+v %v", back, err)
	}
}

func TestSymbolFontIsOptionalInJSONAndSurvivesFontChanges(t *testing.T) {
	// Older documents (no symFont) still load.
	var old CharProps
	if err := serdejson.Unmarshal([]byte(`{"font":"Arial"}`), &old); err != nil || old.SymFont != nil {
		t.Fatalf("%+v %v", old, err)
	}
	p := CharProps{Font: ptr("Arial"), SymFont: ptr("Wingdings")}
	s, err := serdejson.MarshalCompact(p)
	if err != nil || string(s) != `{"font":"Arial","symFont":"Wingdings"}` {
		t.Fatalf("%s %v", s, err)
	}
	// A font change keeps the symbol's own font; Clear Formatting too.
	changed := p.Overlaid(&CharProps{Font: ptr("Times New Roman")})
	if *changed.SymFont != "Wingdings" || *p.Cleared().SymFont != "Wingdings" {
		t.Fatal("the symbol's font was lost")
	}
}

func TestEqualIsRustsPartialEq(t *testing.T) {
	nan := F32(float32(0) / float32(zero()))
	if (CharProps{Size: &nan}).Equal(CharProps{Size: &nan}) {
		t.Error("NaN equals itself")
	}
	a := ParaProps{Tabs: &Vec[TabStop]{}}
	if !a.Equal(ParaProps{Tabs: &Vec[TabStop]{}}) || a.Equal(ParaProps{}) {
		t.Error("Some(vec![]) is itself and not None")
	}
	if !(TextFill{Kind: FillGradient}).equalTo(TextFill{Kind: FillGradient, Stops: Vec[GradStop]{}}) {
		t.Error("an empty Vec equals a nil one")
	}
}

func zero() float32 { return 0 }

func (f TextFill) equalTo(o TextFill) bool { return equal(f, o) }
