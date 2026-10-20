package doc

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// East Asian list and page number formats (crates/doc/src/cjk_numbers.rs; ECMA-376 §17.18.59
// ST_NumberFormat): Chinese, Japanese and Korean counting systems (with units 十/百/千 and
// myriads 万/億), legal (financial) numerals, digit-by-digit ideographs, the heavenly stems and
// earthly branches, kana and hangul sequences and enclosed numbers. Written from the number
// systems' rules; a value outside a sequence's range falls back to plain decimal.

// omitOne is whether a counting system writes the "one" before a unit.
type omitOne uint8

const (
	// omitNever: always written: legal numerals (壹拾, 壱百).
	omitNever omitOne = iota
	// omitLeadingTen: dropped only before a leading ten: 十, 十一, 十万 but 一百一十 (Chinese).
	omitLeadingTen
	// omitUnits: dropped before 十, 百 and 千 inside every group of four: 十, 百, 千 but 一万
	// (Japanese, Korean).
	omitUnits
)

// counting is a myriad-based counting system (groups of four digits, like 1|2345|6789).
type counting struct {
	// digits are 0 to 9 (index 0 is only used by digit-by-digit formats).
	digits [10]rune
	// units are ten, hundred, thousand.
	units [3]rune
	// myriads are ten thousand, hundred million.
	myriads [2]rune
	// zero is written once for a run of zeros between two digits (Chinese 零); 0 skips zeros
	// (Japanese, Korean).
	zero    rune
	omitOne omitOne
}

func (c *counting) format(n uint32) string {
	if n == 0 {
		if c.zero != 0 {
			return string(c.zero)
		}
		return "〇"
	}
	groups := [3]uint32{n / 100_000_000, (n / 10_000) % 10_000, n % 10_000}
	var out strings.Builder
	zeroPending := false
	for gi, g := range groups {
		if g == 0 {
			zeroPending = zeroPending || out.Len() > 0
			continue
		}
		ds := [4]uint32{g / 1000, (g / 100) % 10, (g / 10) % 10, g % 10}
		for pos, d := range ds {
			if d == 0 {
				zeroPending = zeroPending || out.Len() > 0
				continue
			}
			if zeroPending && c.zero != 0 {
				out.WriteRune(c.zero)
			}
			zeroPending = false
			// pos 0 = thousands, 1 = hundreds, 2 = tens, 3 = ones.
			var unit rune
			switch pos {
			case 0:
				unit = c.units[2]
			case 1:
				unit = c.units[1]
			case 2:
				unit = c.units[0]
			}
			omit := d == 1 && unit != 0
			switch c.omitOne {
			case omitNever:
				omit = false
			case omitLeadingTen:
				omit = omit && pos == 2 && out.Len() == 0
			}
			if !omit {
				out.WriteRune(c.digits[d])
			}
			if unit != 0 {
				out.WriteRune(unit)
			}
		}
		switch gi {
		case 0:
			out.WriteRune(c.myriads[1])
		case 1:
			out.WriteRune(c.myriads[0])
		}
	}
	return out.String()
}

var cnDigits = [10]rune{'〇', '一', '二', '三', '四', '五', '六', '七', '八', '九'}

var (
	chinese = counting{cnDigits, [3]rune{'十', '百', '千'}, [2]rune{'万', '亿'}, '零', omitLeadingTen}
	taiwan  = counting{cnDigits, [3]rune{'十', '百', '千'}, [2]rune{'萬', '億'}, '零', omitLeadingTen}
	cnLegal = counting{[10]rune{'零', '壹', '贰', '叁', '肆', '伍', '陆', '柒', '捌', '玖'}, [3]rune{'拾', '佰', '仟'}, [2]rune{'万', '亿'}, '零',
		omitNever}
	twLegal = counting{[10]rune{'零', '壹', '貳', '參', '肆', '伍', '陸', '柒', '捌', '玖'}, [3]rune{'拾', '佰', '仟'}, [2]rune{'萬', '億'}, '零',
		omitNever}
	japanese = counting{cnDigits, [3]rune{'十', '百', '千'}, [2]rune{'万', '億'}, 0, omitUnits}
	jpLegal  = counting{[10]rune{'〇', '壱', '弐', '参', '四', '伍', '六', '七', '八', '九'}, [3]rune{'拾', '百', '阡'}, [2]rune{'萬', '億'}, 0,
		omitNever}
	korean = counting{[10]rune{'영', '일', '이', '삼', '사', '오', '육', '칠', '팔', '구'}, [3]rune{'십', '백', '천'}, [2]rune{'만', '억'}, 0, omitUnits}
)

// chineseCounting: 一, 二 … 十, 十一 … 九十九; from 100 on, digit by digit (一〇〇).
func chineseCounting(n uint32) string {
	if n < 100 {
		return chinese.format(n)
	}
	return digitsOf(n, &cnDigits)
}

// chineseCountingThousand: Chinese counting with every unit: 一百零一, 一千, 一万.
func chineseCountingThousand(n uint32) string { return chinese.format(n) }

// chineseLegalSimplified: simplified Chinese legal (financial) numerals: 壹, 贰, 叁 … 壹拾.
func chineseLegalSimplified(n uint32) string { return cnLegal.format(n) }

// ideographLegalTraditional: traditional Chinese legal numerals: 壹, 貳, 參 … 壹拾.
func ideographLegalTraditional(n uint32) string { return twLegal.format(n) }

// taiwaneseCounting: like Chinese counting below 100, digit by digit above.
func taiwaneseCounting(n uint32) string {
	if n < 100 {
		return taiwan.format(n)
	}
	return digitsOf(n, &cnDigits)
}

// taiwaneseCountingThousand: Taiwanese counting with every unit: 一百零一, 一千, 一萬.
func taiwaneseCountingThousand(n uint32) string { return taiwan.format(n) }

// japaneseCounting: 一 … 十, 十一, 百, 千, 一万.
func japaneseCounting(n uint32) string { return japanese.format(n) }

// japaneseLegal: Japanese legal numerals: 壱, 弐, 参 … 壱拾.
func japaneseLegal(n uint32) string { return jpLegal.format(n) }

// japaneseDigitalTenThousand: Japanese digits with 万/億 marking each group of four: 一二,
// 一万二三四五.
func japaneseDigitalTenThousand(n uint32) string {
	groups := [3]struct {
		g    uint32
		mark rune
	}{{n / 100_000_000, '億'}, {(n / 10_000) % 10_000, '万'}, {n % 10_000, 0}}
	var out strings.Builder
	for _, x := range groups {
		s := digitsOf(x.g, &cnDigits)
		if out.Len() == 0 {
			if x.g == 0 && x.mark != 0 {
				continue
			}
		} else {
			// Inner groups keep their four places: 一万〇〇〇一.
			out.WriteString(strings.Repeat("〇", max(4-utf8.RuneCountInString(s), 0)))
		}
		out.WriteString(s)
		if x.mark != 0 {
			out.WriteRune(x.mark)
		}
	}
	return out.String()
}

// koreanCounting: Sino-Korean numbers in hangul: 일, 이, 삼 … 십, 십일, 백.
func koreanCounting(n uint32) string { return korean.format(n) }

// koreanDigital: Sino-Korean digits in hangul, one per digit: 일, 일영, 일일.
func koreanDigital(n uint32) string { return digitsOf(n, &korean.digits) }

var (
	koOnes = [10]string{"", "하나", "둘", "셋", "넷", "다섯", "여섯", "일곱", "여덟", "아홉"}
	koTens = [10]string{"", "열", "스물", "서른", "마흔", "쉰", "예순", "일흔", "여든", "아흔"}
)

// koreanLegal: native Korean numbers: 하나, 둘, 셋 … 열, 열하나 … 아흔아홉 (decimal from 100 on).
func koreanLegal(n uint32) string {
	if n == 0 || n > 99 {
		return strconv.FormatUint(uint64(n), 10)
	}
	return koTens[n/10] + koOnes[n%10]
}

// ideographDigital: one ideograph per decimal digit: 一, 一〇, 一〇一.
func ideographDigital(n uint32) string { return digitsOf(n, &cnDigits) }

func digitsOf(n uint32, set *[10]rune) string {
	var out strings.Builder
	for _, b := range []byte(strconv.FormatUint(uint64(n), 10)) {
		out.WriteRune(set[b-'0'])
	}
	return out.String()
}

var (
	stems    = []rune{'甲', '乙', '丙', '丁', '戊', '己', '庚', '辛', '壬', '癸'}
	branches = []rune{'子', '丑', '寅', '卯', '辰', '巳', '午', '未', '申', '酉', '戌', '亥'}
)

// ideographTraditional: the ten heavenly stems: 甲, 乙, 丙 … 癸.
func ideographTraditional(n uint32) string { return nthRune(stems, n) }

// ideographZodiac: the twelve earthly branches: 子, 丑, 寅 … 亥.
func ideographZodiac(n uint32) string { return nthRune(branches, n) }

// ideographZodiacTraditional: the sixty-year cycle: 甲子, 乙丑, 丙寅 … 癸亥.
func ideographZodiacTraditional(n uint32) string {
	if n < 1 || n > 60 {
		return strconv.FormatUint(uint64(n), 10)
	}
	i := n - 1
	return string(stems[i%10]) + string(branches[i%12])
}

// nthRune is the nth (1-based) item of a sequence, or decimal past its end.
func nthRune(set []rune, n uint32) string {
	if n >= 1 && uint64(n) <= uint64(len(set)) {
		return string(set[n-1])
	}
	return strconv.FormatUint(uint64(n), 10)
}

// enclosed is the code point first+n-1 for n in 1..=count, decimal beyond.
func enclosed(n, first, count uint32) string {
	if n >= 1 && n <= count {
		return string(rune(first + n - 1))
	}
	return strconv.FormatUint(uint64(n), 10)
}

// decimalEnclosedCircle: ① ② ③ … ⑳.
func decimalEnclosedCircle(n uint32) string { return enclosed(n, 0x2460, 20) }

// decimalEnclosedCircleChinese: ① … ⑩ (the Chinese set stops at ten).
func decimalEnclosedCircleChinese(n uint32) string { return enclosed(n, 0x2460, 10) }

// decimalEnclosedFullstop: ⒈ ⒉ ⒊ … ⒛.
func decimalEnclosedFullstop(n uint32) string { return enclosed(n, 0x2488, 20) }

// decimalEnclosedParen: ⑴ ⑵ ⑶ … ⒇.
func decimalEnclosedParen(n uint32) string { return enclosed(n, 0x2474, 20) }

// ideographEnclosedCircle: ㊀ ㊁ ㊂ … ㊉ (circled ideographs).
func ideographEnclosedCircle(n uint32) string { return enclosed(n, 0x3280, 10) }

// decimalFullWidth: full-width digits: １, ２ … １０.
func decimalFullWidth(n uint32) string {
	var out strings.Builder
	for _, b := range []byte(strconv.FormatUint(uint64(n), 10)) {
		out.WriteRune(rune(0xFF10 + int(b-'0')))
	}
	return out.String()
}

var (
	aiueoFull = []rune("アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン")
	aiueoHalf = []rune("ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜｦﾝ")
	irohaFull = []rune("イロハニホヘトチリヌルヲワカヨタレソツネナラムウヰノオクヤマケフコエテアサキユメミシヱヒモセス")
	irohaHalf = []rune("ｲﾛﾊﾆﾎﾍﾄﾁﾘﾇﾙｦﾜｶﾖﾀﾚｿﾂﾈﾅﾗﾑｳヰﾉｵｸﾔﾏｹﾌｺｴﾃｱｻｷﾕﾒﾐｼヱﾋﾓｾｽ")
	ganadaSet = []rune("가나다라마바사아자차카타파하")
	chosung   = []rune("ㄱㄴㄷㄹㅁㅂㅅㅇㅈㅊㅋㅌㅍㅎ")
)

// aiueo: half-width katakana in gojūon order: ｱ, ｲ, ｳ …
func aiueo(n uint32) string { return nthRune(aiueoHalf, n) }

// aiueoFullWidth: full-width katakana in gojūon order: ア, イ, ウ …
func aiueoFullWidth(n uint32) string { return nthRune(aiueoFull, n) }

// iroha: half-width katakana in iroha order: ｲ, ﾛ, ﾊ …
func iroha(n uint32) string { return nthRune(irohaHalf, n) }

// irohaFullWidth: full-width katakana in iroha order: イ, ロ, ハ …
func irohaFullWidth(n uint32) string { return nthRune(irohaFull, n) }

// ganada: hangul syllables: 가, 나, 다 …
func ganada(n uint32) string { return nthRune(ganadaSet, n) }

// chosungOf: hangul initial consonants: ㄱ, ㄴ, ㄷ …
func chosungOf(n uint32) string { return nthRune(chosung, n) }
