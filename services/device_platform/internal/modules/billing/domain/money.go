package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidAmount reports a malformed money string or an arithmetic overflow.
var ErrInvalidAmount = errors.New("billing: invalid amount")

// Cents is a CNY amount in 分 (1/100 yuan), stored as an integer.
//
// The type exists so an amount cannot be silently passed where a discount, a
// permission count, or a USD quota is expected. It deliberately has no
// float64 conversion: see design doc §2.
type Cents int64

const (
	centsPerYuan     int64 = 100
	yuanSeparator          = "¥"
	permillePerUnit  int64 = 1000
	twoDecimalPlaces int   = 2
)

// Format renders the amount as "¥9.90", "-¥9.90" when negative, and "¥0.00"
// for zero.
//
// Neither Go %f nor float64 is used: the yuan and cent parts are formatted
// separately so no amount can drift through binary floating point. The
// magnitude is taken as a uint64 first so the most negative int64, whose
// negation overflows, still formats correctly.
func (component Cents) Format() string {
	magnitude := uint64(component)
	sign := ""
	if component < 0 {
		sign = "-"
		magnitude = uint64(-int64(component))
	}
	yuan := magnitude / uint64(centsPerYuan)
	fen := magnitude % uint64(centsPerYuan)
	return fmt.Sprintf("%s%s%d.%02d", sign, yuanSeparator, yuan, fen)
}

// String implements fmt.Stringer with the same rendering as Format.
func (component Cents) String() string {
	return component.Format()
}

// MarshalJSON encodes the amount as an integer cent count, matching the wire
// contract in design doc §2.4 ("amount_cny_cents": 990). The display string is
// a rendering concern and never the transport format.
func (component Cents) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(component), 10)), nil
}

// UnmarshalJSON accepts only an integer cent count.
//
// A decimal such as 9.90, a quoted string, or null is rejected: accepting any
// of them would either reintroduce floating point or silently disagree with
// the unit rule.
func (component *Cents) UnmarshalJSON(data []byte) error {
	if component == nil {
		return fmt.Errorf("%w: nil receiver", ErrInvalidAmount)
	}
	raw := string(data)
	if raw == "" {
		return fmt.Errorf("%w: empty amount", ErrInvalidAmount)
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %q is not an integer cent count", ErrInvalidAmount, raw)
	}
	*component = Cents(parsed)
	return nil
}

// ParseCents parses "9.90", "¥9.90", "-¥9.90", or a bare integer such as
// "990", reading a bare integer as yuan (990 -> ¥990.00).
//
// It rejects empty input, non-numeric input, a bare or doubled sign, a value
// with no digits, more than two decimal places, and any value whose cent
// equivalent does not fit in int64. Parsing is decimal-exact: both parts stay
// digit strings and are never converted through a float. For an already-scaled
// integer cent count, decode JSON instead.
func ParseCents(text string) (Cents, error) {
	value := strings.TrimSpace(text)
	negative := false
	if trimmed := strings.TrimPrefix(value, "-"); trimmed != value {
		negative = true
		value = trimmed
	}
	// A minus sign alone, or a second sign after it, is not an amount.
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return 0, fmt.Errorf("%w: %q has an invalid sign", ErrInvalidAmount, text)
	}
	value = strings.TrimPrefix(value, yuanSeparator)

	whole, fraction, hasSeparator := strings.Cut(value, ".")
	if whole == "" && fraction == "" {
		return 0, fmt.Errorf("%w: %q has no digits", ErrInvalidAmount, text)
	}
	if whole != "" && !isDigits(whole) {
		return 0, fmt.Errorf("%w: %q has a non-numeric integer part", ErrInvalidAmount, text)
	}
	if len(fraction) > twoDecimalPlaces {
		return 0, fmt.Errorf("%w: %q has more than two decimal places", ErrInvalidAmount, text)
	}
	if hasSeparator && fraction != "" && !isDigits(fraction) {
		return 0, fmt.Errorf("%w: %q has a non-numeric decimal part", ErrInvalidAmount, text)
	}

	wholeValue := strings.TrimLeft(whole, "0")
	if wholeValue == "" {
		wholeValue = "0"
	}
	yuan, ok := parseDigits(wholeValue)
	if !ok {
		return 0, fmt.Errorf("%w: %q exceeds the supported range", ErrInvalidAmount, text)
	}
	fen, ok := parseDigits(padFen(fraction))
	if !ok {
		return 0, fmt.Errorf("%w: %q exceeds the supported range", ErrInvalidAmount, text)
	}

	// Check the scaled value against the int64 range before multiplying, so a
	// huge input is rejected rather than wrapping around. The negative range is
	// one wider than the positive one.
	limit := uint64(math.MaxInt64)
	if negative {
		limit = uint64(math.MaxInt64) + 1
	}
	if uint64(yuan) > limit/uint64(centsPerYuan) {
		return 0, fmt.Errorf("%w: %q exceeds the supported range", ErrInvalidAmount, text)
	}
	total := uint64(yuan)*uint64(centsPerYuan) + uint64(fen)
	if total > limit {
		return 0, fmt.Errorf("%w: %q exceeds the supported range", ErrInvalidAmount, text)
	}
	if negative {
		return Cents(-int64(total)), nil
	}
	return Cents(total), nil
}

// CentsFromYuanString is ParseCents for a value that must not be negative.
//
// Signup forms and plan editors send yuan prices, which are never negative;
// routing them through the same parser keeps one accepted format.
func CentsFromYuanString(text string) (Cents, error) {
	amount, err := ParseCents(text)
	if err != nil {
		return 0, err
	}
	if amount < 0 {
		return 0, fmt.Errorf("%w: %q must not be negative", ErrInvalidAmount, text)
	}
	return amount, nil
}

// PercentOff returns the discount of permille thousandths of total, rounded
// half up.
//
// permille is a thousandth (250 = 25%) because a percentage cannot express a
// precise discount on small totals. Rounding is (total*permille + 500) / 1000
// in integer math, where the added half unit is what makes an exact half round
// up instead of truncating.
func PercentOff(total Cents, permille int) Cents {
	if total <= 0 || permille <= 0 {
		return 0
	}
	if int64(permille) >= permillePerUnit {
		return total
	}
	const halfUnit = permillePerUnit / 2
	discount, ok := mulInt64(int64(total), int64(permille))
	if ok {
		if sum, addOK := addInt64(discount, halfUnit); addOK {
			return Cents(sum / permillePerUnit)
		}
	}
	// Overflow path: divide first and round the remainder separately, so a
	// large total still rounds exactly instead of being clamped.
	quotient := int64(total) / permillePerUnit
	remainder := int64(total) % permillePerUnit
	scaledQuotient, _ := mulInt64(quotient, int64(permille))
	scaledRemainder, ok := mulInt64(remainder, int64(permille))
	if !ok {
		return total
	}
	scaledRemainder, ok = addInt64(scaledRemainder, halfUnit)
	if !ok {
		return total
	}
	sum, ok := addInt64(scaledQuotient, scaledRemainder/permillePerUnit)
	if !ok {
		return total
	}
	return Cents(sum)
}

// Add returns total plus other, or ErrInvalidAmount on int64 overflow.
func (component Cents) Add(other Cents) (Cents, error) {
	sum, ok := addInt64(int64(component), int64(other))
	if !ok {
		return 0, fmt.Errorf("%w: addition overflow", ErrInvalidAmount)
	}
	return Cents(sum), nil
}

// Sub returns total minus other, or ErrInvalidAmount on int64 overflow.
func (component Cents) Sub(other Cents) (Cents, error) {
	difference, ok := subInt64(int64(component), int64(other))
	if !ok {
		return 0, fmt.Errorf("%w: subtraction overflow", ErrInvalidAmount)
	}
	return Cents(difference), nil
}

// isDigits reports whether text is non-empty and every byte is ASCII 0-9.
func isDigits(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

// parseDigits converts an ASCII digit string, reporting failure instead of
// saturating so a caller can never persist a clamped amount.
func parseDigits(digits string) (int64, bool) {
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// padFen normalises the decimal part to exactly two digits: "9" -> "90",
// "" -> "00", "90" -> "90".
func padFen(fen string) string {
	switch len(fen) {
	case 0:
		return "00"
	case 1:
		return fen + "0"
	default:
		return fen
	}
}

// addInt64 returns left + right, reporting whether the result fits.
func addInt64(left, right int64) (int64, bool) {
	sum := left + right
	if (left > 0 && right > 0 && sum < 0) || (left < 0 && right < 0 && sum > 0) {
		return 0, false
	}
	return sum, true
}

// subInt64 returns left - right, reporting whether the result fits.
func subInt64(left, right int64) (int64, bool) {
	difference := left - right
	if (right > 0 && difference > left) || (right < 0 && difference < left) {
		return 0, false
	}
	return difference, true
}

// mulInt64 returns left * right, reporting whether the result fits.
func mulInt64(left, right int64) (int64, bool) {
	if left == 0 || right == 0 {
		return 0, true
	}
	if (left == math.MinInt64 && right == -1) || (right == math.MinInt64 && left == -1) {
		return 0, false
	}
	product := left * right
	if product/right != left {
		return 0, false
	}
	return product, true
}
