package db

import (
	"encoding/json"
	"testing"
)

func TestMapAuditTrailRow_ExtractsActorAndStationFromPayload(t *testing.T) {
	payload := json.RawMessage(`{"station_id":"22222222-2222-4222-8222-222222222222","actor_user_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}`)

	row, err := mapAuditTrailRow(
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"7",
		"shift_transition",
		payload,
		"success",
		"2026-09-13T08:00:00.123456Z",
	)
	if err != nil {
		t.Fatalf("map audit trail row: %v", err)
	}

	if row.EventID != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" ||
		row.Sequence != "7" ||
		row.EventType != "shift_transition" ||
		row.StationID == nil || *row.StationID != "22222222-2222-4222-8222-222222222222" ||
		row.ActorUserID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" ||
		row.OccurredAt != "2026-09-13T08:00:00.123456Z" ||
		row.Outcome != "success" {
		t.Fatalf("mapped row: %+v", row)
	}
}
