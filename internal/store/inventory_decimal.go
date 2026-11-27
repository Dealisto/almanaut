package store

import (
	"cmp"
	"strconv"
	"strings"
)

// compareBoundedDecimals compares decimal strings already parsed as the same
// finite float64, including distinct tiny values rounded to zero. Hexadecimal
// and extreme exponents keep their historical big.Float comparison path.
func compareBoundedDecimals(a, b string) (int, bool) {
	ad, ae, an, ok := boundedDecimal(a)
	if !ok {
		return 0, false
	}
	bd, be, bn, ok := boundedDecimal(b)
	if !ok {
		return 0, false
	}
	if an != bn {
		if an {
			return -1, true
		}
		return 1, true
	}
	order := 0
	switch {
	case ad == "" || bd == "":
		order = cmp.Compare(len(ad), len(bd)) // Zero has no significant digits.
	case ae != be:
		order = cmp.Compare(ae, be)
	default:
		for i := 0; i < max(len(ad), len(bd)); i++ {
			ac, bc := byte('0'), byte('0')
			if i < len(ad) {
				ac = ad[i]
			}
			if i < len(bd) {
				bc = bd[i]
			}
			if order = cmp.Compare(ac, bc); order != 0 {
				break
			}
		}
	}
	if an {
		order = -order
	}
	return order, true
}

// boundedDecimal returns significant digits, their decimal position and sign.
// Its input has already passed strconv.ParseFloat. Conservative bounds on both
// input length and exponent keep all intermediate/final binary exponents well
// inside big.Float's signed 32-bit range. Thus the direct path agrees with its
// successful numeric subgroup; huge underflows that big.Float rejects or rounds
// to zero retain their existing behavior. This is an optimization bound, not an
// input limit: anything outside it falls back to the existing comparator.
func boundedDecimal(raw string) (digits string, position int64, negative, ok bool) {
	const maxComponent = 1 << 26
	raw = strings.TrimSpace(raw)
	if len(raw) > maxComponent || strings.ContainsAny(raw, "xXpP") {
		return "", 0, false, false
	}
	raw = strings.ReplaceAll(raw, "_", "")
	negative = raw[0] == '-'
	if raw[0] == '+' || negative {
		raw = raw[1:]
	}
	var exponent int64
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(raw[i+1:], 10, 64)
		if err != nil || exponent < -maxComponent || exponent > maxComponent {
			return "", 0, false, false
		}
		raw = raw[:i]
	}
	fractionDigits := 0
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		fractionDigits = len(raw) - i - 1
		raw = raw[:i] + raw[i+1:]
	}
	digits = strings.TrimLeft(raw, "0")
	position = exponent + int64(len(digits)-fractionDigits)
	return strings.TrimRight(digits, "0"), position, negative && digits != "", true
}
