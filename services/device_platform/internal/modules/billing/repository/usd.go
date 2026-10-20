package repository

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ErrInvalidUSD reports a provider quota amount that this package cannot store
// or compare exactly.
var ErrInvalidUSD = errors.New("billing repository: invalid provider quota amount")

// quotaScale is the number of decimal places of the provider's NUMERIC(18, 6)
// quota unit, which Go must parse itself because every amount here is kept as a
// decimal string and never as a float64.
const quotaScale = 6

// quotaScaleFactor is 10^quotaScale, the divisor that turns micro units back
// into whole USD.
var quotaScaleFactor = big.NewInt(1_000_000)

// ParseUSDMicroUnits parses a plain decimal provider amount into exact 1e-6 USD
// units.
//
// The column is NUMERIC(18, 6), so PostgreSQL would silently round a seventh
// decimal. Rejecting more than six decimals keeps the value written and the
// value read back identical.
func ParseUSDMicroUnits(text string) (*big.Int, error) {
	return usdMicroUnits(text)
}

// FormatUSDMicroUnits renders exact 1e-6 USD units as plain decimal text.
func FormatUSDMicroUnits(units *big.Int) string {
	return formatUSDMicroUnits(units)
}

// USDMicroUnitsAsFloat converts exact micro units to float64, reporting false
// when the conversion would lose precision.
//
// The AI gateway's grant API takes a float64, so the value must cross that
// boundary; a lossy conversion is refused because a rounded grant would make
// the ledger disagree with the provider balance it is meant to explain.
func USDMicroUnitsAsFloat(units *big.Int) (float64, bool) {
	whole, remainder := new(big.Int).QuoRem(units, quotaScaleFactor, new(big.Int))
	fraction := float64(remainder.Int64()) / float64(quotaScaleFactor.Int64())
	wholeFloat, accuracy := new(big.Float).SetInt(whole).Float64()
	if accuracy != big.Exact {
		return 0, false
	}
	return wholeFloat + fraction, true
}

// usdMicroUnits parses a plain decimal provider amount into an exact integer
// count of 1e-6 USD.
//
// The column is NUMERIC(18, 6), so PostgreSQL would silently round a seventh
// decimal. Rejecting more than six decimals keeps the value written and the
// value read back identical.
func usdMicroUnits(text string) (*big.Int, error) {
	value := strings.TrimSpace(text)
	if value == "" {
		return nil, fmt.Errorf("%w: empty amount", ErrInvalidUSD)
	}
	if strings.ContainsAny(value, "eE") {
		return nil, fmt.Errorf("%w: %q must be a plain decimal", ErrInvalidUSD, text)
	}
	negative := strings.HasPrefix(value, "-")
	if negative || strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	whole, fraction, hasSeparator := strings.Cut(value, ".")
	if whole == "" || !isASCIIDigits(whole) {
		return nil, fmt.Errorf("%w: %q has a non-numeric integer part", ErrInvalidUSD, text)
	}
	if len(fraction) > quotaScale {
		return nil, fmt.Errorf("%w: %q has more than %d decimals", ErrInvalidUSD, text, quotaScale)
	}
	if hasSeparator && fraction != "" && !isASCIIDigits(fraction) {
		return nil, fmt.Errorf("%w: %q has a non-numeric decimal part", ErrInvalidUSD, text)
	}
	scaled, ok := new(big.Int).SetString(whole+padFraction(fraction), 10)
	if !ok {
		return nil, fmt.Errorf("%w: %q is out of range", ErrInvalidUSD, text)
	}
	if negative {
		scaled.Neg(scaled)
	}
	return scaled, nil
}

// formatUSDMicroUnits renders exact 1e-6 USD units back into the plain decimal
// text PostgreSQL NUMERIC accepts, trimmed to the minimum six decimals.
func formatUSDMicroUnits(units *big.Int) string {
	if units == nil {
		return "0"
	}
	negative := units.Sign() < 0
	magnitude := new(big.Int).Abs(units)
	whole, remainder := new(big.Int).QuoRem(magnitude, quotaScaleFactor, new(big.Int))
	fraction := fmt.Sprintf("%06d", remainder.Int64())
	fraction = strings.TrimRight(fraction, "0")
	rendered := whole.String()
	if fraction != "" {
		rendered += "." + fraction
	}
	if negative {
		rendered = "-" + rendered
	}
	return rendered
}

// addUSDMicroUnits returns left + right in exact micro units, used to compute a
// new balance after an entry without any floating point step.
func addUSDMicroUnits(left, right *big.Int) *big.Int {
	return new(big.Int).Add(left, right)
}

// negateUSDMicroUnits returns -value, used when a clawback removes quota.
func negateUSDMicroUnits(value *big.Int) *big.Int {
	return new(big.Int).Neg(value)
}

// isASCIIDigits reports whether text is non-empty and every byte is 0-9.
func isASCIIDigits(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return false
		}
	}
	return true
}

// padFraction renders fraction as an exactly six-digit string.
func padFraction(fraction string) string {
	if len(fraction) >= quotaScale {
		return fraction
	}
	return fraction + strings.Repeat("0", quotaScale-len(fraction))
}
