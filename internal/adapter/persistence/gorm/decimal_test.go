package gormstore

import (
	"encoding/json"
	"testing"
)

func TestDecimal_PreservesExactTextAndJSONString(t *testing.T) {
	value, err := NewDecimal("10000000000000.00")
	if err != nil {
		t.Fatalf("create decimal: %v", err)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal decimal: %v", err)
	}
	if string(encoded) != `"10000000000000.00"` {
		t.Fatalf("JSON value: got %s, want %s", encoded, `"10000000000000.00"`)
	}
	if got := value.String(); got != "10000000000000.00" {
		t.Fatalf("decimal text: got %q, want %q", got, "10000000000000.00")
	}
}

func TestDecimal_RejectsInvalidText(t *testing.T) {
	for _, input := range []string{"", "1.2.3", "NaN", "1e3"} {
		t.Run(input, func(t *testing.T) {
			if _, err := NewDecimal(input); err == nil {
				t.Fatalf("NewDecimal(%q) accepted invalid text", input)
			}
		})
	}
}
