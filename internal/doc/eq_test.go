package doc

import (
	"reflect"
	"testing"
)

// Rust eq.rs parses_word_enclosures_and_general_overstrike.
func TestParsesWordEnclosuresAndGeneralOverstrike(t *testing.T) {
	e, ok := ParseEnclosure(" eq \\o\\ac(○,字)")
	if !ok || e != (Enclosure{EncloseCircle, EncloseShrink, "字"}) {
		t.Fatalf("%v %v", e, ok)
	}
	// Nested switches flatten to their text; the list separator may be ;.
	e, ok = ParseEnclosure("EQ \\o\\ac(□;\\s\\up 1(12))")
	if !ok || e.Shape != EncloseSquare || e.Text != "12" {
		t.Fatalf("%v %v", e, ok)
	}
	e = Enclosure{EncloseDiamond, EncloseEnlarge, "a,"}
	if back, ok := ParseEnclosure(e.Instr()); !ok || back != e {
		t.Fatalf("%v %v", back, ok)
	}
	if got := e.WordInstr(); got != "eq \\o\\ac(◇,a\\,)" {
		t.Fatal(got)
	}
	o, ok := ParseOverstrike("eq \\o \\al(=,/)")
	if !ok || o.Align != OAlignLeft || !reflect.DeepEqual(o.Args, []string{"=", "/"}) {
		t.Fatalf("%v %v", o, ok)
	}
	for _, s := range []string{"eq \\f(1,2)", "PAGE", "eq \\o(unclosed"} {
		if _, ok := ParseOverstrike(s); ok {
			t.Error(s)
		}
	}
	if _, ok := ParseEnclosure("eq \\o(a,b)"); ok {
		t.Error("eq \\o(a,b)")
	}
	if !FitsEnclosure("字") || !FitsEnclosure("12") || FitsEnclosure("字字") || FitsEnclosure("123") || FitsEnclosure("") {
		t.Error("fits")
	}
}
