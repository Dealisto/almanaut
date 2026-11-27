package store

import (
	"cmp"
	"strconv"
	"strings"
)

// compareFiniteDecimals compares magnitudes of decimal strings already parsed
// as the same nonzero, finite float64. Hexadecimal notation keeps the existing
// binary comparison path. The nonzero precondition excludes unbounded underflow
// exponents, whose historical sorting subgroup is handled by the caller.
func compareFiniteDecimals(a, b string) (int, bool) {
	ad, ae, ok := finiteDecimal(a)
	if !ok {
		return 0, false
	}
	bd, be, ok := finiteDecimal(b)
	if !ok {
		return 0, false
	}
	if order := cmp.Compare(ae, be); order != 0 {
		return order, true
	}
	for i := 0; i < max(len(ad), len(bd)); i++ {
		ac, bc := byte('0'), byte('0')
		if i < len(ad) {
			ac = ad[i]
		}
		if i < len(bd) {
			bc = bd[i]
		}
		if order := cmp.Compare(ac, bc); order != 0 {
			return order, true
		}
	}
	return 0, true
}

// finiteDecimal returns significant digits and the decimal position relative to
// their first digit. Its input has already passed strconv.ParseFloat and must
// represent a nonzero finite value, so exponent plus digit count cannot overflow.
func finiteDecimal(raw string) (digits string, position int64, ok bool) {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "xXpP") {
		return "", 0, false
	}
	raw = strings.ReplaceAll(raw, "_", "")
	if raw[0] == '+' || raw[0] == '-' {
		raw = raw[1:]
	}
	var exponent int64
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(raw[i+1:], 10, 64)
		if err != nil {
			return "", 0, false
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
	return strings.TrimRight(digits, "0"), position, true
}
