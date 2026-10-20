package doc

import (
	"testing"

	"github.com/computernut43/craft-libs/serdejson"
)

// Ports of crates/doc/src/styles.rs's tests.

func TestBuiltinsPresent(t *testing.T) {
	s := BuiltinStyles()
	for _, id := range []string{"Normal", "Heading1", "Heading9", "Title", "Strong", "TableGrid", "TOC3", "Hyperlink", "ListParagraph"} {
		if _, ok := s.Get(id); !ok {
			t.Error(id)
		}
	}
	if st, ok := s.Find("heading 2"); !ok || st.ID != "Heading2" {
		t.Error("find by name")
	}
	g := s.Gallery()
	if len(g) == 0 || g[0].ID != "Normal" {
		t.Error("gallery starts with Normal")
	}
	if !containsID(g, "Heading1") {
		t.Error("Heading1 in the gallery")
	}
}

func containsID(v []Style, id string) bool {
	for _, s := range v {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestDefaultGalleryMarksAndAddsQuickBuiltins(t *testing.T) {
	b := BuiltinStyles()
	s := EmptyStyleSheet()
	plain := func(id, name string) Style { return Style{ID: id, Name: name, Builtin: true} }
	h2 := plain("Heading2", "Heading 2")
	h2.Priority = ptr[uint32](7)
	h3 := plain("Heading3", "Heading 3")
	h3.Builtin = false
	custom := plain("Custom1", "Subtitle")
	custom.Para.SpaceAfter = ptr[F32](99)
	s.Styles = append(s.Styles, plain("Normal", "Normal"), plain("Heading1", "heading 1"), h2, h3, custom)
	s.AddDefaultGallery()
	g := s.Gallery()
	for _, id := range []string{"Normal", "NoSpacing", "Heading1", "Heading2", "Title"} {
		if !containsID(g, id) {
			t.Errorf("%s in the gallery", id)
		}
	}
	bare := EmptyStyleSheet()
	bare.AddDefaultGallery()
	if st, ok := bare.Get("Heading1"); !ok || !st.Quick {
		t.Error("missing quick built-ins added")
	}
	if _, ok := bare.Get("Title"); !ok {
		t.Error("Title added")
	}
	if st, _ := s.Get("Heading2"); *st.Priority != 7 {
		t.Error("file priority kept")
	}
	got, _ := s.Get("Heading1")
	want, _ := b.Get("Heading1")
	if *got.Priority != *want.Priority {
		t.Error("built-in priority taken")
	}
	if st, _ := s.Get("Heading3"); st.Quick {
		t.Error("custom style untouched")
	}
	if _, ok := s.Get("Subtitle"); ok {
		t.Error("a style with that name exists: not duplicated")
	}
	if st, _ := s.Get("Custom1"); !st.Quick || *st.Para.SpaceAfter != 99 {
		t.Error("found by name only, built in: quick, own formatting kept")
	}
}

func TestOlderSerializedStylesKeepExplicitDefaults(t *testing.T) {
	var sheet StyleSheet
	if err := serdejson.Unmarshal([]byte(`{"defaultChr":{},"defaultPara":{},"styles":[]}`), &sheet); err != nil {
		t.Fatal(err)
	}
	if !sheet.DefaultParaPresent {
		t.Error("missing defaultParaPresent is true")
	}
	sheet.DefaultParaPresent = false
	js, err := serdejson.Marshal(sheet)
	if err != nil {
		t.Fatal(err)
	}
	var again StyleSheet
	if err := serdejson.Unmarshal(js, &again); err != nil || !equal(again, sheet) {
		t.Errorf("round trip: %v", err)
	}
}

func TestChainIsCycleSafe(t *testing.T) {
	s := BuiltinStyles()
	s.Styles[s.index("Normal")].BasedOn = ptr("Heading1")
	if c := s.Chain("Heading1"); len(c) > 2 {
		t.Errorf("chain %d long", len(c))
	}
}

// A table style based on another inherits what it doesn't set itself; borders merge edge by edge.
func TestTableStyleMergesBasedOnChain(t *testing.T) {
	s := BuiltinStyles()
	red := RGB{0xC0, 0, 0}
	thick := SingleBorder(2)
	s.Upsert(Style{ID: "RedGrid", Kind: StyleTable, BasedOn: ptr("TableGrid"), Chr: CharProps{Bold: ptr(true), Color: color(red)},
		Table: &TableStyleParts{Borders: &Borders{Top: &thick}, HeaderFill: &red}})
	ts, ok := s.TableStyle("RedGrid")
	if !ok {
		t.Fatal("RedGrid")
	}
	if *ts.Para.SpaceAfter != 0 || !equal(ts.Para.LineSpacing, multiple(1)) {
		t.Error("from Table Grid")
	}
	if !*ts.Chr.Bold || !equal(ts.Chr.Color, color(red)) {
		t.Error("own character formatting")
	}
	if !equal(ts.Parts.Borders.Top, &thick) || !equal(ts.Parts.Borders.Left, ptr(SingleBorder(0.5))) {
		t.Error("borders merge edge by edge")
	}
	if !equal(ts.Parts.HeaderFill, &red) {
		t.Error("header fill")
	}
	// Not a table style, unknown, or a based-on loop: no panic.
	if _, ok := s.TableStyle("Normal"); ok {
		t.Error("Normal is no table style")
	}
	if _, ok := s.TableStyle("Nope"); ok {
		t.Error("unknown")
	}
	s.Styles[s.index("TableGrid")].BasedOn = ptr("RedGrid")
	if _, ok := s.TableStyle("RedGrid"); !ok {
		t.Error("loop")
	}
}

func TestNewIDsUnique(t *testing.T) {
	s := BuiltinStyles()
	for name, want := range map[string]string{"My Style": "MyStyle", "Normal": "Normal1", "!!!": "Style"} {
		if got := s.NewID(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestStyleEditsCopyOnWrite(t *testing.T) {
	s := BuiltinStyles()
	snapshot := s
	s.Upsert(Style{ID: "Normal", Name: "Changed"})
	s.Remove("Heading1")
	s.AddDefaultGallery()
	if n, _ := snapshot.Get("Normal"); n.Name != "Normal" {
		t.Error("Upsert changed a shared sheet")
	}
	if _, ok := snapshot.Get("Heading1"); !ok {
		t.Error("Remove changed a shared sheet")
	}
}
