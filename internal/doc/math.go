package doc

import (
	"fmt"

	"github.com/computernut43/craft-libs/serdejson"
)

// Equations: the Office Math object model (OMML, ECMA-376 Part 1 §22.1) as a tree (math.rs).
// An equation is a list of nodes (an argument); structures hold arguments of their own.

// MaxMathDepth is the deepest structure nesting kept (parsers stop here; layout draws nothing
// deeper).
const MaxMathDepth = 64

// Arg is a sequence of math nodes (OMML's m:e, m:num, m:sub…).
type Arg = Vec[MathNode]

// Math is an equation's structure, plus what's needed to write it back.
type Math struct {
	Nodes Arg `json:"nodes,omitempty" serde:"default"`
	// OMML is the source m:oMath element (DOCX import), written back unchanged on save.
	OMML string `json:"omml,omitempty" serde:"default"`
	// Jc is the justification of a display equation.
	Jc MathJc `json:"jc,omitempty" serde:"default"`
	// Linear: shown as its linear-format text instead of built up.
	Linear bool `json:"linear,omitempty" serde:"default"`
}

// IsZero reports an equation without structure (Rust's is_empty: no nodes, whatever else it
// holds), which a containing object leaves out of its JSON.
func (m Math) IsZero() bool { return len(m.Nodes) == 0 }

// MathJc is a display equation's justification (m:oMathParaPr/m:jc).
type MathJc uint8

// Equation justifications.
const (
	JcCenterGroup MathJc = iota
	JcCenter
	JcLeft
	JcRight
)

var mathJcNames = []string{"centerGroup", "center", "left", "right"}

func (j *MathJc) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, mathJcNames, j) }
func (j MathJc) MarshalJSON() ([]byte, error)           { return writeEnum(mathJcNames, j) }

// MSty is a math run style (m:sty).
type MSty uint8

// Math run styles.
const (
	StyPlain MSty = iota
	StyBold
	StyItalic
	StyBoldItalic
)

var mStyNames = []string{"plain", "bold", "italic", "boldItalic"}

func (s *MSty) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, mStyNames, s) }
func (s MSty) MarshalJSON() ([]byte, error)           { return writeEnum(mStyNames, s) }

// MScr is a math alphabet (m:scr).
type MScr uint8

// Math alphabets.
const (
	ScrRoman MScr = iota
	ScrScript
	ScrFraktur
	ScrDoubleStruck
	ScrSansSerif
	ScrMonospace
)

var mScrNames = []string{"roman", "script", "fraktur", "doubleStruck", "sansSerif", "monospace"}

func (s *MScr) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, mScrNames, s) }
func (s MScr) MarshalJSON() ([]byte, error)           { return writeEnum(mScrNames, s) }

// FracKind is how a fraction is drawn.
type FracKind uint8

// Fraction kinds.
const (
	// FracBar is stacked with a rule.
	FracBar FracKind = iota
	// FracSkewed is num ⁄ den.
	FracSkewed
	// FracLinear is num/den.
	FracLinear
	// FracNoBar is stacked without a rule.
	FracNoBar
)

var fracKindNames = []string{"bar", "skewed", "linear", "noBar"}

func (k *FracKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, fracKindNames, k) }
func (k FracKind) MarshalJSON() ([]byte, error)           { return writeEnum(fracKindNames, k) }

// ScriptKind is which scripts a script structure has.
type ScriptKind uint8

// Script kinds.
const (
	ScriptSup ScriptKind = iota
	ScriptSub
	ScriptSubSup
	// ScriptPre is pre-scripts (m:sPre).
	ScriptPre
)

var scriptKindNames = []string{"sup", "sub", "subSup", "pre"}

func (k *ScriptKind) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, scriptKindNames, k) }
func (k ScriptKind) MarshalJSON() ([]byte, error)           { return writeEnum(scriptKindNames, k) }

// LimLoc is where an n-ary operator's limits go.
type LimLoc uint8

// Limit locations.
const (
	// LimUndOvr puts them under and over the operator.
	LimUndOvr LimLoc = iota
	// LimSubSup puts them as sub- and superscripts.
	LimSubSup
)

var limLocNames = []string{"undOvr", "subSup"}

func (l *LimLoc) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, limLocNames, l) }
func (l LimLoc) MarshalJSON() ([]byte, error)           { return writeEnum(limLocNames, l) }

// ColJc is a matrix column's justification.
type ColJc uint8

// Column justifications.
const (
	ColCenter ColJc = iota
	ColLeft
	ColRight
)

var colJcNames = []string{"center", "left", "right"}

func (j *ColJc) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, colJcNames, j) }
func (j ColJc) MarshalJSON() ([]byte, error)           { return writeEnum(colJcNames, j) }

// MNode is a math node: one of the M… structures below.
type MNode interface{ mathNode() string }

// MathNode holds a math node; its JSON is serde's internally tagged form ({"t": "frac", …}).
type MathNode struct{ MNode }

// MRun is a run of math text (m:r).
type MRun struct {
	Text string `json:"text"`
	// Sty is nil for the math default (letters italic, the rest upright).
	Sty *MSty `json:"sty,omitempty" serde:"default"`
	Scr MScr  `json:"scr,omitempty" serde:"default"`
	// Nor is normal (non-math) text.
	Nor bool `json:"nor,omitempty" serde:"default"`
	// Lit is literal: no automatic operator handling.
	Lit bool `json:"lit,omitempty" serde:"default"`
	// Size is the font size, points.
	Size  *F32 `json:"size,omitempty" serde:"default"`
	Color *RGB `json:"color,omitempty" serde:"default"`
	// Font is the font for normal text.
	Font *string `json:"font,omitempty" serde:"default"`
	// Brk is a manual line break before this run in a display equation (m:brk): 0 wraps as
	// usual, n aligns the new line at the nth operator of the first line (m:alnAt).
	Brk *uint8 `json:"brk,omitempty" serde:"default"`
}

// MFrac is a fraction (m:f).
type MFrac struct {
	Kind FracKind `json:"kind" serde:"default"`
	Num  Arg      `json:"num"`
	Den  Arg      `json:"den"`
}

// MScript is a script structure (m:sSup, m:sSub, m:sSubSup, m:sPre).
type MScript struct {
	Kind ScriptKind `json:"kind"`
	Base Arg        `json:"base"`
	Sub  Arg        `json:"sub" serde:"default"`
	Sup  Arg        `json:"sup" serde:"default"`
}

// MRad is a radical (m:rad).
type MRad struct {
	Deg     Arg  `json:"deg" serde:"default"`
	DegHide bool `json:"deg_hide" serde:"default"`
	E       Arg  `json:"e"`
}

// MNary is an n-ary operator (m:nary): ∑ ∫ ∏ … with limits and an operand.
type MNary struct {
	Chr Char `json:"chr"`
	// LimLoc is nil for Word's default (integrals take sub/sup limits, the rest under/over).
	LimLoc  *LimLoc `json:"lim_loc" serde:"default"`
	Grow    bool    `json:"grow" serde:"default"`
	SubHide bool    `json:"sub_hide" serde:"default"`
	SupHide bool    `json:"sup_hide" serde:"default"`
	Sub     Arg     `json:"sub" serde:"default"`
	Sup     Arg     `json:"sup" serde:"default"`
	E       Arg     `json:"e"`
}

// MDelim is delimiters around elements separated by Sep (m:d).
type MDelim struct {
	// Beg and End are nil for no delimiter.
	Beg *Char `json:"beg"`
	End *Char `json:"end"`
	Sep *Char `json:"sep"`
	// Grow: the delimiters grow to the content (Word's default).
	Grow bool `json:"grow"`
	// ShpMatch: grow to match the content exactly rather than centred on the math axis.
	ShpMatch bool     `json:"shp_match"`
	Elems    Vec[Arg] `json:"elems"`
}

// MFunc is a function name applied to an argument (m:func: sin x).
type MFunc struct {
	Name Arg `json:"name"`
	E    Arg `json:"e"`
}

// MLim is a lower or upper limit (m:limLow, m:limUpp).
type MLim struct {
	Upper bool `json:"upper"`
	E     Arg  `json:"e"`
	Lim   Arg  `json:"lim"`
}

// MAcc is an accent over the base (m:acc).
type MAcc struct {
	Chr Char `json:"chr"`
	E   Arg  `json:"e"`
}

// MBar is an overbar or underbar (m:bar).
type MBar struct {
	Top bool `json:"top"`
	E   Arg  `json:"e"`
}

// MBorderBox is a border box (m:borderBox). Hide: top, bottom, left, right. Strike: horizontal,
// vertical, bottom-left→top-right, top-left→bottom-right.
type MBorderBox struct {
	Hide   [4]bool `json:"hide" serde:"default"`
	Strike [4]bool `json:"strike" serde:"default"`
	E      Arg     `json:"e"`
}

// MBox is a box (m:box).
type MBox struct {
	E Arg `json:"e"`
}

// MGroupChr is a stretched character (brace, arrow) under or over the base (m:groupChr).
type MGroupChr struct {
	Chr Char `json:"chr"`
	Top bool `json:"top"`
	E   Arg  `json:"e"`
}

// MEqArr is rows aligned at & (m:eqArr).
type MEqArr struct {
	Rows Vec[Arg] `json:"rows"`
}

// MMatrix is a matrix (m:m).
type MMatrix struct {
	Rows  Vec[Vec[Arg]] `json:"rows"`
	ColJc Vec[ColJc]    `json:"col_jc" serde:"default"`
}

// MPhant is a phantom (m:phant).
type MPhant struct {
	Show     bool `json:"show"`
	ZeroWid  bool `json:"zero_wid"`
	ZeroAsc  bool `json:"zero_asc"`
	ZeroDesc bool `json:"zero_desc"`
	E        Arg  `json:"e"`
}

func (MRun) mathNode() string       { return "run" }
func (MFrac) mathNode() string      { return "frac" }
func (MScript) mathNode() string    { return "script" }
func (MRad) mathNode() string       { return "rad" }
func (MNary) mathNode() string      { return "nary" }
func (MDelim) mathNode() string     { return "delim" }
func (MFunc) mathNode() string      { return "func" }
func (MLim) mathNode() string       { return "lim" }
func (MAcc) mathNode() string       { return "acc" }
func (MBar) mathNode() string       { return "bar" }
func (MBorderBox) mathNode() string { return "borderBox" }
func (MBox) mathNode() string       { return "boxed" }
func (MGroupChr) mathNode() string  { return "groupChr" }
func (MEqArr) mathNode() string     { return "eqArr" }
func (MMatrix) mathNode() string    { return "matrix" }
func (MPhant) mathNode() string     { return "phant" }

var mNodeNames = []string{"run", "frac", "script", "rad", "nary", "delim", "func", "lim", "acc", "bar", "borderBox", "boxed", "groupChr",
	"eqArr", "matrix", "phant"}

// delimFields and phantFields read the fields whose missing value is true (#[serde(default =
// "yes")]).
type delimFields struct {
	Beg      *Char    `json:"beg"`
	End      *Char    `json:"end"`
	Sep      *Char    `json:"sep" serde:"default"`
	Grow     *bool    `json:"grow" serde:"default"`
	ShpMatch bool     `json:"shp_match" serde:"default"`
	Elems    Vec[Arg] `json:"elems"`
}

type phantFields struct {
	Show     *bool `json:"show" serde:"default"`
	ZeroWid  bool  `json:"zero_wid" serde:"default"`
	ZeroAsc  bool  `json:"zero_asc" serde:"default"`
	ZeroDesc bool  `json:"zero_desc" serde:"default"`
	E        Arg   `json:"e"`
}

func yes(b *bool) bool { return b == nil || *b }

// DecodeSerde reads serde's internally tagged form.
func (n *MathNode) DecodeSerde(d serdejson.Decoder) error {
	i, rest, err := d.Tagged("t", mNodeNames)
	if err != nil {
		return err
	}
	var v MNode
	switch mNodeNames[i] {
	case "run":
		v, err = decodeAs[MRun](rest)
	case "frac":
		v, err = decodeAs[MFrac](rest)
	case "script":
		v, err = decodeAs[MScript](rest)
	case "rad":
		v, err = decodeAs[MRad](rest)
	case "nary":
		v, err = decodeAs[MNary](rest)
	case "delim":
		var f delimFields
		if err = rest.Decode(&f); err == nil {
			v = MDelim{f.Beg, f.End, f.Sep, yes(f.Grow), f.ShpMatch, f.Elems}
		}
	case "func":
		v, err = decodeAs[MFunc](rest)
	case "lim":
		v, err = decodeAs[MLim](rest)
	case "acc":
		v, err = decodeAs[MAcc](rest)
	case "bar":
		v, err = decodeAs[MBar](rest)
	case "borderBox":
		v, err = decodeAs[MBorderBox](rest)
	case "boxed":
		v, err = decodeAs[MBox](rest)
	case "groupChr":
		v, err = decodeAs[MGroupChr](rest)
	case "eqArr":
		v, err = decodeAs[MEqArr](rest)
	case "matrix":
		v, err = decodeAs[MMatrix](rest)
	default:
		var f phantFields
		if err = rest.Decode(&f); err == nil {
			v = MPhant{yes(f.Show), f.ZeroWid, f.ZeroAsc, f.ZeroDesc, f.E}
		}
	}
	if err != nil {
		return err
	}
	*n = MathNode{v}
	return nil
}

// decodeAs reads the rest of a variant into a T.
func decodeAs[T any](d serdejson.Decoder) (T, error) {
	var v T
	err := d.Decode(&v)
	return v, err
}

// MarshalJSON writes {"t": …} and the node's fields.
func (n MathNode) MarshalJSON() ([]byte, error) {
	if n.MNode == nil {
		return nil, fmt.Errorf("doc: empty math node")
	}
	return tagged("t", n.mathNode(), n.MNode)
}
