// Package canonical provides the versioned JSON byte rules used by hashes.
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// Marshal returns deterministic RFC 8785 compatible JSON for supported values.
func Marshal(value any) ([]byte, error) {
	var out bytes.Buffer
	if err := appendValue(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// HashVersionedPayload hashes the canonical pair [payload, version].
func HashVersionedPayload(payload any, version int) ([]byte, error) {
	if version < 1 {
		return nil, errors.New("canonical version must be positive")
	}
	encoded, err := Marshal([]any{payload, version})
	if err != nil {
		return nil, fmt.Errorf("marshal versioned payload: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func appendValue(out *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if typed {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		return appendString(out, typed)
	case json.Number:
		return appendNumber(out, typed.String())
	case int:
		out.WriteString(strconv.Itoa(typed))
	case int8:
		out.WriteString(strconv.FormatInt(int64(typed), 10))
	case int16:
		out.WriteString(strconv.FormatInt(int64(typed), 10))
	case int32:
		out.WriteString(strconv.FormatInt(int64(typed), 10))
	case int64:
		out.WriteString(strconv.FormatInt(typed, 10))
	case uint:
		out.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint8:
		out.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint16:
		out.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint32:
		out.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint64:
		out.WriteString(strconv.FormatUint(typed, 10))
	case float32, float64:
		return errors.New("floating point values are not supported")
	case []any:
		out.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := appendValue(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := appendString(out, key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := appendValue(out, typed[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func appendString(out *bytes.Buffer, value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal JSON string: %w", err)
	}
	out.Write(encoded)
	return nil
}

func appendNumber(out *bytes.Buffer, value string) error {
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return fmt.Errorf("invalid JSON number: %w", err)
	}
	if len(value) > 0 && value[0] == '+' {
		return errors.New("invalid JSON number")
	}
	out.WriteString(value)
	return nil
}
