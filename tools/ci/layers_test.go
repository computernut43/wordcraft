package main

import (
	"reflect"
	"strings"
	"testing"
)

func n(name string, deps ...dep) unit { return unit{name: name, deps: deps} }
func ws(name string) dep              { return dep{name: name, workspace: true} }
func wsTest(name string) dep          { return dep{name: name, kind: testDep, workspace: true} }
func ext(path string) dep             { return dep{name: path} }

// The cases of xtask/src/layers.rs's tests, in Go terms.
func TestCheckLayers(t *testing.T) {
	cases := []struct {
		name  string
		units []unit
		want  []string
	}{
		{"downward is fine", []unit{n("docx", ws("doc"), ws("geom")), n("engine", ws("docx"), ws("layout"))}, nil},
		{"upward", []unit{n("doc", ws("layout"))}, []string{"doc (L1) -> layout (L2) [normal]: may only depend on strictly lower layers"}},
		{"same layer", []unit{n("docx", ws("formats"))}, []string{"docx (L2) -> formats (L2) [normal]: may only depend on strictly lower layers"}},
		{"intra-layer chain allows fonts -> doc", []unit{n("fonts", ws("doc"))}, nil},
		{"but not doc -> fonts", []unit{n("doc", ws("fonts"))}, []string{"doc (L1) -> fonts (L1) [normal]: may only depend on strictly lower layers"}},
		{"upward in tests too", []unit{n("geom", wsTest("doc"))}, []string{"geom (L0) -> doc (L1) [test]: may only depend on strictly lower layers"}},
		{"standalone imports nothing", []unit{n("controlkey", ws("geom"))}, []string{"controlkey: standalone unit must not import unit geom"}},
		{"standalone may be imported", []unit{n("docx", ws("cfb")), n("cfb")}, nil},
		{"testkit only from tests", []unit{n("docx", wsTest("rustref")), n("engine", ws("rustref"))}, []string{"engine: rustref may only be imported from tests"}},
		{"testkit itself may import up to L5", []unit{n("rustref", ws("mcp"), ws("engine"))}, nil},
		{"no importing the apps", []unit{n("engine", ws("cmd/wordcraft-cli"))}, []string{"engine (L4) -> cmd/wordcraft-cli (L7) [normal]: may only depend on strictly lower layers"}},
		{"apps are exempt", []unit{n("cmd/wordcraft", ws("ui"), ext("gioui.org/app")), n("tools", ws("engine"))}, nil},
		{"unregistered", []unit{n("mystery", ws("geom"))}, []string{"mystery: unknown unit; register it in tools/ci/layers.go layerTable (see MIGRATION.md §2)"}},
		{"UI below L6", []unit{n("engine", ext("gioui.org/widget")), n("render", ext("fyne.io/fyne/v2"))}, []string{
			"engine (L4) -> gioui.org/widget: UI toolkits are only allowed from L6",
			"render (L3) -> fyne.io/fyne/v2: UI toolkits are only allowed from L6",
		}},
		{"UI at L6", []unit{n("ui", ext("gioui.org"), ws("engine"))}, nil},
		{"not a UI prefix", []unit{n("engine", ext("gioui.orgx/thing"))}, nil},
		{"self import", []unit{n("doc", ws("doc"))}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkLayers(c.units)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestUnitOf(t *testing.T) {
	const m = "github.com/computernut43/wordcraft"
	cases := map[string]string{
		m + "/internal/doc":      "doc",
		m + "/internal/doc/math": "doc",
		m + "/cmd/wordcraft-cli": "cmd/wordcraft-cli",
		m + "/tools/ci":          "tools",
		m:                        "(module root)",
		m + "/stray":             "stray",
		m + "x/internal/doc":     "",
		"github.com/computernut43/craft-libs/pdf": "",
		"fmt": "",
	}
	for path, want := range cases {
		if got := unitOf(m, path); got != want {
			t.Errorf("unitOf(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestUnitsFromGoList(t *testing.T) {
	const m = "github.com/computernut43/wordcraft"
	input := `{"ImportPath":"` + m + `/internal/docx","Imports":["fmt","` + m + `/internal/doc"],"XTestImports":["` + m + `/internal/rustref"]}
{"ImportPath":"` + m + `/internal/docx/xml","Imports":["encoding/xml","` + m + `/internal/doc"]}
{"ImportPath":"` + m + `/internal/doc"}`
	got, err := unitsFromGoList(m, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []unit{
		{name: "docx", deps: []dep{ext("fmt"), ws("doc"), wsTest("rustref"), ext("encoding/xml")}},
		{name: "doc"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if _, err := unitsFromGoList(m, strings.NewReader("{not json")); err == nil {
		t.Fatal("bad JSON: want an error")
	}
}
