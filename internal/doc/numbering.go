package doc

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/computernut43/craft-libs/serdejson"
)

// Lists: abstract numbering definitions (9 levels each), numbering instances, and the counter
// state used to compute list labels (crates/doc/src/numbering.rs).

// LevelSuffix is what follows a list label.
type LevelSuffix uint8

// Level suffixes.
const (
	SuffixTab LevelSuffix = iota
	SuffixSpace
	SuffixNothing
)

var levelSuffixNames = []string{"tab", "space", "nothing"}

func (s *LevelSuffix) DecodeSerde(d serdejson.Decoder) error { return readEnum(d, levelSuffixNames, s) }
func (s LevelSuffix) MarshalJSON() ([]byte, error)           { return writeEnum(levelSuffixNames, s) }

// Level is one level of a list definition.
type Level struct {
	Start  uint32    `json:"start"`
	Format NumFormat `json:"format"`
	// Text is the label template: `%1.`, `%1.%2.`, or the bullet character.
	Text  string `json:"text"`
	Align Align  `json:"align"`
	// Indent is the left indent of the text, points.
	Indent F32 `json:"indent"`
	// Hanging is the hanging indent (the label sits this far left of Indent), points.
	Hanging F32         `json:"hanging"`
	Suffix  LevelSuffix `json:"suffix"`
	// Chr is the label formatting (bullet font etc.).
	Chr CharProps `json:"chr"`
	// Restart: restart after a higher level (true = after any higher level, the default).
	Restart bool `json:"restart"`
	// RestartAfter restarts only after this level (1-based, Word's w:lvlRestart): a level 3 that
	// restarts after level 1 keeps counting across level 2 items. nil restarts after the level
	// just above. Ignored when Restart is false.
	RestartAfter *uint8 `json:"restartAfter,omitempty"`
	// Tab is the tab stop that follows the number (Word's "Add tab stop at"), points from the
	// paragraph's left edge.
	Tab *F32 `json:"tab,omitempty"`
	// Legal shows all levels as decimal (legal numbering).
	Legal bool `json:"legal"`
	// Style is the paragraph style linked to this level (e.g. Heading 1 for outline numbering).
	Style *string `json:"style"`
}

func (Level) SerdeDefault() {}

// SetDefaults is Rust's Level::default(): starting at 1, "%1.", indent 36, hanging 18.
func (l *Level) SetDefaults() { *l = NewLevel() }

// NewLevel is the default level.
func NewLevel() Level {
	return Level{Start: 1, Format: FmtDecimal, Text: "%1.", Indent: 36, Hanging: 18, Restart: true}
}

// AbstractNum is a list definition. Its JSON field names are Rust's own (snake case).
type AbstractNum struct {
	ID     uint32     `json:"id"`
	Name   *string    `json:"name"`
	Levels Vec[Level] `json:"levels"`
	// NSID is Word's list identity (w:nsid, 8 hex digits), which it uses to match lists across
	// documents (paste, compare); kept from the file.
	NSID *string `json:"nsid,omitempty"`
	// Tmpl is the list template the definition came from (w:tmpl); kept from the file.
	Tmpl *string `json:"tmpl,omitempty"`
	// MultiLevelType is w:multiLevelType from the file (singleLevel, multilevel,
	// hybridMultilevel); nil: derived from the levels.
	MultiLevelType *string `json:"multi_level_type,omitempty"`
}

func (AbstractNum) SerdeDefault() {}

// Pair is a Rust tuple (A, B): a JSON array of two.
type Pair[A, B any] struct {
	A A
	B B
}

// DecodeSerde reads a two-element sequence, as serde reads a tuple.
func (p *Pair[A, B]) DecodeSerde(d serdejson.Decoder) error {
	if d.Peek() != serdejson.KindArray {
		return d.Fail("invalid type, expected a tuple of size 2")
	}
	var v Pair[A, B]
	n := 0
	err := d.Array(func(i int) error {
		n = i + 1
		switch i {
		case 0:
			return d.Decode(&v.A)
		case 1:
			return d.Decode(&v.B)
		}
		return d.Fail("trailing characters (expected a tuple of size 2)")
	})
	if err != nil {
		return err
	}
	if n != 2 {
		return d.Fail("invalid length " + strconv.Itoa(n) + ", expected a tuple of size 2")
	}
	*p = v
	return nil
}

// MarshalJSON writes [a, b].
func (p Pair[A, B]) MarshalJSON() ([]byte, error) {
	return serdejson.MarshalCompact([2]any{p.A, p.B})
}

// Num is a list: an instance of an abstract list, with its own start values and levels.
type Num struct {
	ID         uint32 `json:"id"`
	AbstractID uint32 `json:"abstractId"`
	// StartOverrides are per-level start overrides (level, start).
	StartOverrides Vec[Pair[uint8, uint32]] `json:"startOverrides"`
	// LevelOverrides are the levels this list defines itself instead of taking them from its
	// abstract list (level, definition): Word's w:lvlOverride/w:lvl.
	LevelOverrides Vec[Pair[uint8, Level]] `json:"levelOverrides,omitempty"`
}

func (Num) SerdeDefault() {}

// Numbering is a document's lists.
type Numbering struct {
	Abstracts Vec[AbstractNum] `json:"abstracts"`
	Nums      Vec[Num]         `json:"nums"`
}

func (Numbering) SerdeDefault() {}

// SetDefaults is the default: no lists, written as empty sequences.
func (n *Numbering) SetDefaults() { *n = Numbering{Abstracts: Vec[AbstractNum]{}, Nums: Vec[Num]{}} }

// ListKind is a built-in list kind of the Bullets / Numbering / Multilevel galleries.
type ListKind struct {
	Kind ListKindName
	// Char is the bullet of ListBulletChar.
	Char rune
}

// ListKindName names a list kind.
type ListKindName uint8

// List kinds.
const (
	// ListBullet is • ○ ▪ repeating.
	ListBullet ListKindName = iota
	// ListBulletChar is a specific bullet character.
	ListBulletChar
	// ListNumbered is 1. a. i.
	ListNumbered
	// ListNumberedParen is 1) a) i)
	ListNumberedParen
	// ListOutline is I. A. 1.
	ListOutline
	// ListUpperLetter is A. B. C.
	ListUpperLetter
	// ListLowerLetter is a) b) c)
	ListLowerLetter
	// ListLowerLetterDot is a. b. c.
	ListLowerLetterDot
	// ListLowerRoman is i. ii. iii.
	ListLowerRoman
	// ListLegal is 1. 1.1. 1.1.1.
	ListLegal
)

var listKindNames = []string{"bullet", "bulletChar", "numbered", "numberedParen", "outline", "upperLetter", "lowerLetter", "lowerLetterDot",
	"lowerRoman", "legal"}

// DecodeSerde reads serde's externally tagged form: "numbered", or {"bulletChar": "★"}.
func (k *ListKind) DecodeSerde(d serdejson.Decoder) error {
	var c rune
	i, err := serdejson.ExternallyTagged(d, listKindNames, func(i int) bool { return ListKindName(i) != ListBulletChar },
		func(_ int, d serdejson.Decoder) error {
			s, err := d.String()
			if err != nil {
				return err
			}
			r, n := utf8.DecodeRuneInString(s)
			if n == 0 || n != len(s) {
				return d.Fail("invalid value: string, expected a character")
			}
			c = r
			return nil
		})
	if err != nil {
		return err
	}
	*k = ListKind{Kind: ListKindName(i), Char: c}
	return nil
}

// MarshalJSON writes the kind's name, or {"bulletChar": "c"}.
func (k ListKind) MarshalJSON() ([]byte, error) {
	if k.Kind == ListBulletChar {
		return serdejson.MarshalCompact(map[string]string{"bulletChar": string(k.Char)})
	}
	return writeEnum(listKindNames, k.Kind)
}

// ParseListKind reads a list kind's name (a single character is that bullet).
func ParseListKind(s string) (ListKind, bool) {
	switch s {
	case "bullet":
		return ListKind{Kind: ListBullet}, true
	case "numbered", "number":
		return ListKind{Kind: ListNumbered}, true
	case "numberedParen":
		return ListKind{Kind: ListNumberedParen}, true
	case "outline":
		return ListKind{Kind: ListOutline}, true
	case "upperLetter":
		return ListKind{Kind: ListUpperLetter}, true
	case "lowerLetter":
		return ListKind{Kind: ListLowerLetter}, true
	case "lowerLetterDot":
		return ListKind{Kind: ListLowerLetterDot}, true
	case "lowerRoman":
		return ListKind{Kind: ListLowerRoman}, true
	case "legal", "multilevel":
		return ListKind{Kind: ListLegal}, true
	}
	if utf8.RuneCountInString(s) == 1 {
		r, _ := utf8.DecodeRuneInString(s)
		return ListKind{Kind: ListBulletChar, Char: r}, true
	}
	return ListKind{}, false
}

// IsBullet reports a bulleted kind.
func (k ListKind) IsBullet() bool { return k.Kind == ListBullet || k.Kind == ListBulletChar }

// DefaultBullet is the bullet a level of a plain bulleted list shows: • ○ ▪ repeating (level is
// zero-based).
func DefaultBullet(level int) rune { return []rune{'•', '○', '▪'}[level%3] }

func bulletLevel(i int, c rune) Level {
	l := NewLevel()
	l.Format, l.Text = FmtBullet, string(c)
	l.Indent, l.Hanging = f32(36*(float64(i)+1)), 18
	switch {
	case c == '•' || c == '▪' || c == '○':
		l.Chr.Font = ptr("Arial")
	case c < utf8.RuneSelf || c == '–':
		// Plain text characters (a dash bullet) use the paragraph's font.
	default:
		l.Chr.Font = ptr("Segoe UI Symbol")
	}
	return l
}

// LevelsFor is the nine levels of a list kind.
func LevelsFor(kind ListKind) Vec[Level] {
	out := make(Vec[Level], 0, 9)
	for i := range 9 {
		ind := F32(float32(36) * (float32(i) + 1))
		cyc := func(a, b, c NumFormat) NumFormat { return [3]NumFormat{a, b, c}[i%3] }
		lvl := func(format NumFormat, text string) Level {
			l := NewLevel()
			l.Format, l.Text, l.Indent, l.Hanging = format, text, ind, 18
			return l
		}
		n := strconv.Itoa(i + 1)
		var l Level
		switch kind.Kind {
		case ListBullet:
			l = bulletLevel(i, DefaultBullet(i))
		case ListBulletChar:
			c := DefaultBullet(i)
			if i == 0 {
				c = kind.Char
			}
			l = bulletLevel(i, c)
		case ListNumbered:
			l = lvl(cyc(FmtDecimal, FmtLowerLetter, FmtLowerRoman), "%"+n+".")
		case ListNumberedParen:
			l = lvl(cyc(FmtDecimal, FmtLowerLetter, FmtLowerRoman), "%"+n+")")
		case ListUpperLetter:
			l = lvl(cyc(FmtUpperLetter, FmtLowerLetter, FmtLowerRoman), "%"+n+".")
		case ListLowerLetter:
			l = lvl(cyc(FmtLowerLetter, FmtLowerRoman, FmtDecimal), "%"+n+")")
		case ListLowerLetterDot:
			l = lvl(cyc(FmtLowerLetter, FmtLowerRoman, FmtDecimal), "%"+n+".")
		case ListLowerRoman:
			l = lvl(cyc(FmtLowerRoman, FmtLowerLetter, FmtDecimal), "%"+n+".")
		case ListOutline:
			f := FmtDecimal
			if outline := []NumFormat{FmtUpperRoman, FmtUpperLetter, FmtDecimal, FmtLowerLetter, FmtLowerRoman}; i < len(outline) {
				f = outline[i]
			}
			l = lvl(f, "%"+n+".")
		default: // ListLegal
			var text strings.Builder
			for k := 1; k <= i+1; k++ {
				text.WriteString("%" + strconv.Itoa(k) + ".")
			}
			l = NewLevel()
			l.Format, l.Text = FmtDecimal, text.String()
			l.Indent, l.Hanging = F32(18+25*float32(i)), F32(18+7*float32(i))
		}
		out = append(out, l)
	}
	return out
}

// Num is list id's instance; false when there is none.
func (n *Numbering) Num(id uint32) (Num, bool) {
	for _, x := range n.Nums {
		if x.ID == id {
			return x, true
		}
	}
	return Num{}, false
}

// AbstractOf is the definition list num uses.
func (n *Numbering) AbstractOf(num uint32) (AbstractNum, bool) {
	x, ok := n.Num(num)
	if !ok {
		return AbstractNum{}, false
	}
	for _, a := range n.Abstracts {
		if a.ID == x.AbstractID {
			return a, true
		}
	}
	return AbstractNum{}, false
}

// ShowsLabel reports whether a paragraph in list num at level gets a label (Counters.NextLabel
// gives one), without counting it.
func (n *Numbering) ShowsLabel(num uint32, level uint8) bool {
	if _, ok := n.AbstractOf(num); !ok {
		return false
	}
	d, ok := n.Level(num, min(level, 8))
	return ok && d.Format != FmtNone
}

// Level is level of list num: the list's own definition, else its abstract list's.
func (n *Numbering) Level(num uint32, level uint8) (Level, bool) {
	x, ok := n.Num(num)
	if !ok {
		return Level{}, false
	}
	for _, o := range x.LevelOverrides {
		if o.A == level {
			return o.B, true
		}
	}
	a, ok := n.AbstractOf(num)
	if !ok || int(level) >= len(a.Levels) {
		return Level{}, false
	}
	return a.Levels[level], true
}

// AddList creates a new list of kind and returns its num id. List ids run out only past
// MaxUint32 lists, more than memory holds; then nothing is added and the id is 0, "no list".
func (n *Numbering) AddList(kind ListKind) uint32 {
	id, err := n.TryAddList(kind)
	if err != nil {
		return 0
	}
	return id
}

// TryAddList is AddList, or an error, adding nothing, when list ids are all taken.
func (n *Numbering) TryAddList(kind ListKind) (uint32, error) {
	return n.TryAddLevels(nil, LevelsFor(kind))
}

// TryAddLevels adds a list definition (name, levels) and a num for it, and returns the num id.
// Both ids are FreeIDs, so the largest ids a file can hold don't make them collide with its
// lists; an error, adding nothing, when they are all taken.
func (n *Numbering) TryAddLevels(name *string, levels Vec[Level]) (uint32, error) {
	ids := make([]uint32, len(n.Abstracts))
	for i, a := range n.Abstracts {
		ids[i] = a.ID
	}
	aid, aok := FreeID(ids, 0)
	nid, nok := n.freeNumID()
	if !aok || !nok {
		return 0, &Error{Kind: Invalid, Detail: "no free list id"}
	}
	n.Abstracts = append(n.Abstracts, AbstractNum{ID: aid, Name: name, Levels: levels})
	n.Nums = append(n.Nums, Num{ID: nid, AbstractID: aid, StartOverrides: Vec[Pair[uint8, uint32]]{}})
	return nid, nil
}

// freeNumID is a num id no list has (num 0 means "no list"); false when they are all taken.
func (n *Numbering) freeNumID() (uint32, bool) {
	ids := make([]uint32, len(n.Nums))
	for i, x := range n.Nums {
		ids[i] = x.ID
	}
	return FreeID(ids, 1)
}

// Restart adds a num that restarts the numbering of list num (false: no such list, or list ids
// are all taken).
func (n *Numbering) Restart(num uint32) (uint32, bool) {
	source, ok := n.Num(num)
	if !ok {
		return 0, false
	}
	a := source.AbstractID
	levelOverrides := append(Vec[Pair[uint8, Level]](nil), source.LevelOverrides...)
	nid, ok := n.freeNumID()
	if !ok {
		return 0, false
	}
	// Every level starts again at its own effective start, so a level this list defines itself
	// keeps its start.
	levels := 0
	for _, x := range n.Abstracts {
		if x.ID == a {
			levels = len(x.Levels)
			break
		}
	}
	starts := Vec[Pair[uint8, uint32]]{}
	for i := range levels {
		if l, ok := n.Level(num, uint8(i)); ok {
			starts = append(starts, Pair[uint8, uint32]{uint8(i), l.Start})
		}
	}
	n.Nums = append(n.Nums, Num{ID: nid, AbstractID: a, StartOverrides: starts, LevelOverrides: levelOverrides})
	return nid, true
}

// FindKind finds an existing list whose first level matches kind, so consecutive bullet clicks
// reuse it.
func (n *Numbering) FindKind(kind ListKind) (uint32, bool) {
	first := LevelsFor(kind)[0]
	for _, x := range n.Nums {
		a, ok := n.AbstractOf(x.ID)
		if ok && len(a.Levels) > 0 && a.Levels[0].Format == first.Format && a.Levels[0].Text == first.Text {
			return x.ID, true
		}
	}
	return 0, false
}

// FreeID is a free id, min or more, for something new (a part, a list, a comment): one past the
// largest id in used, so ids keep growing; once that would pass MaxUint32 (a file can use any
// id), the lowest one used doesn't hold. False when every id from min up is taken.
func FreeID(used []uint32, minID uint32) (uint32, bool) {
	if len(used) == 0 {
		return minID, true
	}
	largest := used[0]
	for _, i := range used[1:] {
		largest = max(largest, i)
	}
	if largest < ^uint32(0) {
		return max(largest+1, minID), true
	}
	taken := map[uint32]bool{}
	for _, i := range used {
		taken[i] = true
	}
	want := minID
	for taken[want] {
		if want == ^uint32(0) {
			return 0, false
		}
		want++
	}
	return want, true
}

// Counters are the running list counters while walking a document in order.
type Counters struct {
	// state is, per abstract list, the current value per level.
	state map[uint32]*[9]uint32
	// seen holds the nums that have started (to apply start overrides once).
	seen map[uint32]bool
}

// Values is the current value of each level of num's list (0 for a level not counted yet).
func (c *Counters) Values(numbering *Numbering, num uint32) ([9]uint32, bool) {
	abs, ok := numbering.AbstractOf(num)
	if !ok {
		return [9]uint32{}, false
	}
	if st := c.state[abs.ID]; st != nil {
		return *st, true
	}
	return [9]uint32{}, true
}

// NextLabel advances the counter for num/level and returns the label ("1.", "a)", "•") and the
// level's definition.
func (c *Counters) NextLabel(numbering *Numbering, num uint32, level uint8) (string, Level, bool) {
	n, ok := numbering.Num(num)
	if !ok {
		return "", Level{}, false
	}
	abs, ok := numbering.AbstractOf(num)
	if !ok {
		return "", Level{}, false
	}
	lv := min(int(level), 8)
	// This list's own definition of a level wins over its abstract list's.
	levelDef := func(k int) (Level, bool) { return numbering.Level(num, uint8(k)) }
	def, ok := levelDef(lv)
	if !ok {
		return "", Level{}, false
	}
	// A "none" level shows no label at all (Word's numFmt none) — not even its text.
	if def.Format == FmtNone {
		return "", Level{}, false
	}
	if c.state == nil {
		c.state, c.seen = map[uint32]*[9]uint32{}, map[uint32]bool{}
	}
	st := c.state[abs.ID]
	if st == nil {
		st = new([9]uint32)
		c.state[abs.ID] = st
	}
	if !c.seen[num] {
		c.seen[num] = true
		for _, o := range n.StartOverrides {
			if int(o.A) < len(st) {
				st[o.A] = o.B - min(o.B, 1)
			}
		}
	}
	if v := &st[lv]; *v == 0 {
		*v = def.Start
		if def.Start != 0 {
			*v = max(def.Start, 1)
		}
	} else if *v < ^uint32(0) {
		*v++
	}
	// Deeper levels restart, unless they never restart or only restart after a level above this
	// one (Word's lvlRestart).
	for d := lv + 1; d < len(st); d++ {
		l, ok := levelDef(d)
		if !ok || l.Restart && (l.RestartAfter == nil || lv < int(*l.RestartAfter)) {
			st[d] = 0
		}
	}
	vals := *st
	if def.Format == FmtBullet {
		return def.Text, def, true
	}
	var label strings.Builder
	text := []rune(def.Text)
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '%' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9' {
			i++
			k := min(max(int(text[i]-'0')-1, 0), 8)
			val := vals[k]
			if val == 0 {
				val = 1
				if l, ok := levelDef(k); ok {
					val = l.Start
				}
			}
			// Legal numbering (w:isLgl) shows every level in Arabic digits, whatever its own
			// format (二 of a Chinese outline becomes 2 in "2.1").
			fmt := FmtDecimal
			if l, ok := levelDef(k); ok {
				fmt = l.Format
			}
			if fmt != FmtDecimalZero && def.Legal {
				fmt = FmtDecimal
			}
			label.WriteString(fmt.Format(val))
		} else {
			label.WriteRune(ch)
		}
	}
	return label.String(), def, true
}
