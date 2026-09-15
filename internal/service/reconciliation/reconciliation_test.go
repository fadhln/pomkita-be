package reconciliation

import (
	"errors"
	"testing"
)

func TestCalculateMeterDelta_RolloverReturnsExactDecimal(t *testing.T) {
	delta, err := CalculateMeterDelta("99999.9", "0.0", "100000.0", "10.0")
	if err != nil {
		t.Fatalf("calculate rollover: %v", err)
	}
	if delta != "0.1" {
		t.Fatalf("delta: got %q, want %q", delta, "0.1")
	}
}

func TestCalculateMeterDelta_RejectsExcessiveRollover(t *testing.T) {
	_, err := CalculateMeterDelta("99999.9", "50.0", "100000.0", "10.0")
	if !errors.Is(err, ErrRolloverOverThreshold) {
		t.Fatalf("error: got %v, want %v", err, ErrRolloverOverThreshold)
	}
}

func TestCalculateExpectedSale_UsesHalfUpAndRejectsOverflow(t *testing.T) {
	amount, err := CalculateExpectedSaleRupiah("1.25", "10000")
	if err != nil {
		t.Fatalf("calculate sale: %v", err)
	}
	if amount != "12500" {
		t.Fatalf("amount: got %q, want %q", amount, "12500")
	}
	if _, err := CalculateExpectedSaleRupiah("10000000000000.0", "10000"); !errors.Is(err, ErrNumericOverflow) {
		t.Fatalf("overflow: got %v, want %v", err, ErrNumericOverflow)
	}
}

func TestValidateMeterStart_RequiresTheSelectedContinuitySource(t *testing.T) {
	if err := ValidateMeterStart("20.0", "20.0", "", ""); err != nil {
		t.Fatalf("predecessor continuity: %v", err)
	}
	if err := ValidateMeterStart("21.0", "20.0", "", ""); !errors.Is(err, ErrMeterChainConflict) {
		t.Fatalf("predecessor conflict: got %v, want %v", err, ErrMeterChainConflict)
	}
	if err := ValidateMeterStart("30.0", "20.0", "30.0", ""); err != nil {
		t.Fatalf("approved reset: %v", err)
	}
	if err := ValidateMeterStart("30.0", "", "", "30.0"); err != nil {
		t.Fatalf("baseline: %v", err)
	}
}
