// Package reconciliation contains exact decimal meter and report calculations.
package reconciliation

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/pomkita/pomkita-be/internal/domain"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

var (
	// ErrInvalidDecimal identifies an invalid non-negative decimal input.
	ErrInvalidDecimal = domain.NewError(domain.CategoryValidation, "invalid_decimal")
	// ErrRolloverOverThreshold identifies a meter rollover larger than policy permits.
	ErrRolloverOverThreshold = domain.NewError(domain.CategoryValidation, "rollover_over_threshold")
	// ErrInvalidMeterRange identifies an invalid meter modulus or reading range.
	ErrInvalidMeterRange = domain.NewError(domain.CategoryValidation, "invalid_meter_range")
	// ErrNumericOverflow identifies a value outside the Rupiah numeric boundary.
	ErrNumericOverflow = domain.NewError(domain.CategoryValidation, "numeric_overflow")
)

// CalculateMeterDelta calculates a meter delta without floating-point conversion.
func CalculateMeterDelta(start, end, modulus, rolloverThreshold string) (string, error) {
	startValue, scaleStart, err := parseDecimal(start)
	if err != nil {
		return "", err
	}
	endValue, scaleEnd, err := parseDecimal(end)
	if err != nil {
		return "", err
	}
	modulusValue, scaleModulus, err := parseDecimal(modulus)
	if err != nil {
		return "", err
	}
	thresholdValue, scaleThreshold, err := parseDecimal(rolloverThreshold)
	if err != nil {
		return "", err
	}
	if modulusValue.Sign() <= 0 || startValue.Cmp(modulusValue) >= 0 || endValue.Cmp(modulusValue) >= 0 {
		return "", ErrInvalidMeterRange
	}
	delta := new(big.Rat)
	if endValue.Cmp(startValue) >= 0 {
		delta.Sub(endValue, startValue)
	} else {
		delta.Add(new(big.Rat).Sub(modulusValue, startValue), endValue)
		if delta.Cmp(thresholdValue) > 0 {
			return "", ErrRolloverOverThreshold
		}
	}
	return formatDecimal(delta, maxInt(scaleStart, maxInt(scaleEnd, maxInt(scaleModulus, scaleThreshold)))), nil
}

// CalculateExpectedSaleRupiah multiplies liters by price and rounds positive values half up.
func CalculateExpectedSaleRupiah(liters, price string) (string, error) {
	litersValue, _, err := parseDecimal(liters)
	if err != nil {
		return "", err
	}
	priceValue, _, err := parseDecimal(price)
	if err != nil {
		return "", err
	}
	product := new(big.Rat).Mul(litersValue, priceValue)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(product.Num(), product.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(product.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if quotient.Cmp(mustBigInt("99999999999999")) > 0 {
		return "", ErrNumericOverflow
	}
	return quotient.String(), nil
}

func mustBigInt(value string) *big.Int {
	result, _ := new(big.Int).SetString(value, 10)
	return result
}

func parseDecimal(value string) (*big.Rat, int, error) {
	if !decimalPattern.MatchString(value) {
		return nil, 0, ErrInvalidDecimal
	}
	ratio, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, 0, ErrInvalidDecimal
	}
	parts := strings.SplitN(value, ".", 2)
	scale := 0
	if len(parts) == 2 {
		scale = len(parts[1])
	}
	return ratio, scale, nil
}

func formatDecimal(value *big.Rat, scale int) string {
	text := value.FloatString(scale)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if text == "" {
		return "0"
	}
	return text
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
