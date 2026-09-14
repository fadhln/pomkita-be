package gormstore

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
)

var decimalTextPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Decimal stores a PostgreSQL numeric value without converting it to a float.
type Decimal string

// NewDecimal validates and creates an exact decimal value.
func NewDecimal(value string) (Decimal, error) {
	if !decimalTextPattern.MatchString(value) {
		return "", fmt.Errorf("invalid decimal value")
	}
	return Decimal(value), nil
}

// String returns the original decimal text.
func (d Decimal) String() string {
	return string(d)
}

// Value returns the decimal text for a PostgreSQL numeric parameter.
func (d Decimal) Value() (driver.Value, error) {
	if _, err := NewDecimal(d.String()); err != nil {
		return nil, err
	}
	return d.String(), nil
}

// Scan reads a PostgreSQL numeric value without loss of precision.
func (d *Decimal) Scan(value any) error {
	if d == nil {
		return fmt.Errorf("scan decimal into nil receiver")
	}
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case []byte:
		text = string(typed)
	default:
		return fmt.Errorf("unsupported decimal source type %T", value)
	}
	parsed, err := NewDecimal(text)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// MarshalJSON encodes decimal values as JSON strings.
func (d Decimal) MarshalJSON() ([]byte, error) {
	if _, err := NewDecimal(d.String()); err != nil {
		return nil, err
	}
	return json.Marshal(d.String())
}
