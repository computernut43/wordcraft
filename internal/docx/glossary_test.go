package docx

import (
	"regexp"
	"testing"

	"github.com/computernut43/wordcraft/internal/doc"
)

// The glossary fingerprint tells a save whether the entries changed since they were read: equal
// entries give the same 16 hex digits, any change another.
func TestGlossaryFingerprint(t *testing.T) {
	entry := func(name, text string) doc.BuildingBlock {
		return doc.BuildingBlock{Name: name, Blocks: doc.Vec[doc.Block]{{Para: doc.ParagraphWithText(text, doc.CharProps{})}}}
	}
	a := doc.Vec[doc.BuildingBlock]{entry("Sig", "Hi")}
	b := doc.Vec[doc.BuildingBlock]{entry("Sig", "Hi")}
	if fa, fb := glossaryFingerprint(a), glossaryFingerprint(b); fa != fb {
		t.Fatalf("equal entries: %s, %s", fa, fb)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(glossaryFingerprint(a)) {
		t.Fatalf("not 16 hex digits: %q", glossaryFingerprint(a))
	}
	for _, c := range []doc.Vec[doc.BuildingBlock]{
		{entry("Sig", "Ho")}, {entry("Sig2", "Hi")}, {entry("Sig", "Hi"), entry("X", "y")}, {},
	} {
		if glossaryFingerprint(c) == glossaryFingerprint(a) {
			t.Errorf("changed entries %v give the same fingerprint", c)
		}
	}
	if glossaryFingerprint(nil) != glossaryFingerprint(doc.Vec[doc.BuildingBlock]{}) {
		t.Error("no entries: nil and empty differ")
	}
}
