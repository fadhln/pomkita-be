package canonical

import (
	"bytes"
	"testing"
)

func TestMarshal_SortsObjectKeysAndKeepsDecimalStrings(t *testing.T) {
	got, err := Marshal(map[string]any{
		"b": "100.00",
		"a": map[string]any{"z": true, "y": nil},
	})
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}
	want := []byte(`{"a":{"y":null,"z":true},"b":"100.00"}`)
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical JSON: got %s, want %s", got, want)
	}
}

func TestHashVersionedPayload_UsesPayloadAndVersion(t *testing.T) {
	hash, err := HashVersionedPayload(map[string]any{"amount": "10"}, 2)
	if err != nil {
		t.Fatalf("hash payload: %v", err)
	}
	if len(hash) != 32 {
		t.Fatalf("hash length: got %d, want 32", len(hash))
	}
	other, err := HashVersionedPayload(map[string]any{"amount": "10"}, 3)
	if err != nil {
		t.Fatalf("hash other version: %v", err)
	}
	if bytes.Equal(hash, other) {
		t.Fatal("different versions have the same hash")
	}
}
