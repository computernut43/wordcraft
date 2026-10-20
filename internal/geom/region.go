package geom

// The conventions of the user's region that new documents and the measurement fields follow
// (#838): the paper a new blank document starts on, the unit lengths are shown in, and the
// decimal separator. Word takes them from the system region, not from the interface language.
//
// The territory lists come from Unicode CLDR's supplemental measurementData (Unicode License):
// paperSize US-Letter and measurementSystem US. The decimal separators follow CLDR's number
// symbols for the common locales. The front end detects the region; the engine only sees this
// plain value.

import (
	"slices"
	"strings"
)

// letterTerritories default to US Letter (CLDR paperSize type="US-Letter").
var letterTerritories = []string{"BZ", "CA", "CL", "CO", "CR", "GT", "MX", "NI", "PA", "PH", "PR", "SV", "US", "VE"}

// inchTerritories measure in inches (CLDR measurementSystem type="US").
var inchTerritories = []string{"LR", "MM", "US"}

// commaLanguages use a decimal comma in most of their territories.
var commaLanguages = []string{
	"af", "az", "be", "bg", "bs", "ca", "cs", "da", "de", "el", "es", "et", "eu", "fi", "fo", "fr", "gl", "hr", "hu", "hy", "id", "is", "it", "ka",
	"kk", "ky", "lb", "lt", "lv", "mk", "nb", "nl", "nn", "no", "pl", "pt", "ro", "ru", "sk", "sl", "sq", "sr", "sv", "tr", "uk", "uz", "vi",
}

// pointExceptions are territories where those languages use a decimal point after all.
var pointExceptions = map[string][]string{
	"de": {"CH", "LI"},
	"it": {"CH"},
	"es": {"CU", "DO", "GT", "HN", "MX", "NI", "PA", "PE", "PR", "SV", "US", "419"},
}

// commaTerritories write English (and other decimal-point languages) with a decimal comma.
var commaTerritories = []string{
	"AR", "AT", "BE", "BG", "BO", "BR", "CL", "CO", "CZ", "DE", "DK", "EC", "EE", "ES", "FI", "FR", "GR", "HR", "HU", "ID", "IS", "IT", "LT", "LU",
	"LV", "NL", "NO", "PL", "PT", "PY", "RO", "RS", "RU", "SE", "SI", "SK", "TR", "UA", "UY", "VE", "VN", "ZA",
}

// Region is a language and a territory, read from a locale tag like pl-PL, de_AT.UTF-8 or
// en-US. The default is en-US (Letter, inches, decimal point).
type Region struct {
	// Language is the lower-case language subtag (pl); empty when the tag has none.
	Language string `json:"language"`
	// Territory is upper-case (PL, or a UN M.49 number like 419); empty when the tag has none.
	Territory string `json:"territory"`
	// Letter is US Letter or not when a separate paper locale says so (Unix LC_PAPER);
	// otherwise the territory decides.
	Letter *bool `json:"letter,omitempty"`
	// Inches is inches or not when a separate measurement locale says so (Unix
	// LC_MEASUREMENT); otherwise the territory decides.
	Inches *bool `json:"inches,omitempty"`
}

// DefaultRegion is en-US.
func DefaultRegion() Region { return Region{Language: "en", Territory: "US"} }

func isAlphaASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func isDigitASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'a' <= c && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}

func isTerritory(s string) bool {
	return (len(s) == 2 && isAlphaASCII(s)) || (len(s) == 3 && isDigitASCII(s))
}

// RegionFromTag reads a BCP 47 or POSIX locale tag (pl-PL, pl_PL.UTF-8@euro, zh-Hans-CN,
// en-US-u-ms-metric). An empty or unreadable tag, C and POSIX are en-US; a bare en is the United
// States, any other bare language has no territory.
func RegionFromTag(tag string) Region {
	// Locale tags are short; anything long is junk and only its first 64 characters are read.
	n := 0
	for i := range tag {
		if n == 64 {
			tag = tag[:i]
			break
		}
		n++
	}
	if i := strings.IndexAny(tag, ".@"); i >= 0 {
		tag = tag[:i]
	}
	tag = strings.TrimSpace(tag)
	if tag == "" || eqFoldASCII(tag, "c") || eqFoldASCII(tag, "posix") {
		return DefaultRegion()
	}
	parts := splitKeep(tag)
	language := parts[0]
	if len(language) < 2 || len(language) > 3 || !isAlphaASCII(language) {
		return DefaultRegion()
	}
	language = lowerASCII(language)
	territory := ""
	for _, p := range parts[1:] {
		// A singleton starts the extensions (-u-…, -x-…): no territory after it.
		if len(p) == 1 {
			break
		}
		if isTerritory(p) {
			territory = upperASCII(p)
			break
		}
	}
	if territory == "" && language == "en" {
		territory = "US"
	}
	return Region{Language: language, Territory: territory}
}

// splitKeep splits at '-' and '_', keeping empty parts like Rust's split(['-', '_']); it always
// returns at least one part.
func splitKeep(tag string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(tag); i++ {
		if tag[i] == '-' || tag[i] == '_' {
			parts = append(parts, tag[start:i])
			start = i + 1
		}
	}
	return append(parts, tag[start:])
}

// WithTerritory returns r with another territory (from a platform setting naming only the
// territory, like Windows' home location). A value that isn't a territory code is ignored.
func (r Region) WithTerritory(territory string) Region {
	if t := strings.TrimSpace(territory); isTerritory(t) {
		r.Territory = upperASCII(t)
	}
	return r
}

// WithPaperAndMeasurement takes the paper and the measurement system from their own locale tags
// when the platform sets them apart (Unix LC_PAPER, LC_MEASUREMENT); nil leaves them to the
// territory. LANG=pl_PL.UTF-8 with LC_PAPER=en_US.UTF-8 writes 2,54 cm on Letter.
func (r Region) WithPaperAndMeasurement(paper, measurement *string) Region {
	if paper != nil {
		v := RegionFromTag(*paper).UsesLetter()
		r.Letter = &v
	}
	if measurement != nil {
		v := RegionFromTag(*measurement).Units() == Inches
		r.Inches = &v
	}
	return r
}

// RegionFromLocaleVars is the system region from the locale variables lookup reads (the process
// environment) and, when none is set, the platform's first preferred locale. One tag for
// everything first (WORDCRAFT_REGION, then LC_ALL); otherwise Unix sets each convention on its
// own: numbers (LC_NUMERIC, else LANG), paper (LC_PAPER) and measurement (LC_MEASUREMENT).
// Empty, C and POSIX values count as unset.
func RegionFromLocaleVars(lookup func(string) (string, bool), platform func() (string, bool)) Region {
	get := func(name string) (string, bool) {
		v, ok := lookup(name)
		if !ok || !usableLocale(v) {
			return "", false
		}
		return v, true
	}
	for _, name := range []string{"WORDCRAFT_REGION", "LC_ALL"} {
		if v, ok := get(name); ok {
			return RegionFromTag(v)
		}
	}
	base, ok := get("LC_NUMERIC")
	if !ok {
		base, ok = get("LANG")
	}
	if !ok {
		base, _ = platform()
	}
	var paper, measurement *string
	if v, ok := get("LC_PAPER"); ok {
		paper = &v
	}
	if v, ok := get("LC_MEASUREMENT"); ok {
		measurement = &v
	}
	return RegionFromTag(base).WithPaperAndMeasurement(paper, measurement)
}

// UsesLetter reports whether a new document starts on US Letter here (otherwise A4).
func (r Region) UsesLetter() bool {
	if r.Letter != nil {
		return *r.Letter
	}
	return slices.Contains(letterTerritories, r.Territory)
}

// Paper is the default paper's name and portrait size in points: Letter (612 × 792) or A4.
func (r Region) Paper() (string, float32, float32) {
	if r.UsesLetter() {
		return "Letter", 612, 792
	}
	return "A4", 595.28, 841.89
}

// Units is the unit lengths are shown in by default: inches in the US, centimetres elsewhere.
func (r Region) Units() Unit {
	inches := slices.Contains(inchTerritories, r.Territory)
	if r.Inches != nil {
		inches = *r.Inches
	}
	if inches {
		return Inches
	}
	return Centimeters
}

// Decimal is the decimal separator numbers are written with here (',' or '.').
func (r Region) Decimal() rune {
	if slices.Contains(commaLanguages, r.Language) {
		if slices.Contains(pointExceptions[r.Language], r.Territory) {
			return '.'
		}
		return ','
	}
	if r.Language == "en" && slices.Contains(commaTerritories, r.Territory) {
		return ','
	}
	return '.'
}

// Format writes pt in unit the way this region writes numbers (2,54 cm in Poland).
func (r Region) Format(unit Unit, pt float32) string { return unit.FormatWith(pt, r.Decimal()) }

// usableLocale: a locale variable that names a locale (not empty, C or POSIX).
func usableLocale(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && !eqFoldASCII(v, "c") && !eqFoldASCII(v, "posix") && !strings.HasPrefix(v, "C.")
}
