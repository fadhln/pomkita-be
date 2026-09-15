package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/canonical"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
)

func TestAuditRowHash_UsesCanonicalJSONBytes(t *testing.T) {
	orgID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	eventID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	createdAt := time.Date(2026, 1, 2, 14, 30, 0, 123456000, time.FixedZone("test", 7*60*60))
	previous := bytes.Repeat([]byte{0xab}, 32)
	payload := []byte(` { "z": 1, "a": "x" } `)
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	canonicalBytes, err := canonical.Marshal([]any{1, orgID.String(), int64(1), eventID.String(), "test", value, createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"), hex.EncodeToString(previous)})
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}
	wantDigest := sha256.Sum256(canonicalBytes)
	got := auditRowHash(orgID, 1, eventID, "test", payload, createdAt, previous)
	if !bytes.Equal(got, wantDigest[:]) {
		t.Fatalf("row hash: got %x, want %x", got, wantDigest)
	}
}

func TestAuditRepository_AppendBuildsAndVerifiesChainWithOutbox(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	now := fixture.now
	repository := NewAuditRepository(fixture.store)
	first, err := repository.Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "test", Payload: []byte(`{"ok":true}`), Outcome: "success"}, now)
	if err != nil {
		t.Fatalf("append first audit event: %v", err)
	}
	second, err := repository.Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "test", Payload: []byte(`{"ok":false}`), Outcome: "success"}, now)
	if err != nil {
		t.Fatalf("append second audit event: %v", err)
	}
	if first.OrgSequence != 1 || second.OrgSequence != 2 {
		t.Fatalf("sequences: first=%d second=%d", first.OrgSequence, second.OrgSequence)
	}
	var outboxCount int64
	if err := fixture.store.DB.Model(&AuditOutboxModel{}).Where("org_id = ?", fixture.orgID).Count(&outboxCount).Error; err != nil {
		t.Fatalf("count audit outbox: %v", err)
	}
	if outboxCount != 2 {
		t.Fatalf("outbox count: got %d, want 2", outboxCount)
	}
	if err := repository.Verify(ctx, fixture.orgID); err != nil {
		t.Fatalf("verify audit chain: %v", err)
	}
	if err := fixture.store.DB.Model(&AuditLogModel{}).Where("event_id = ?", first.EventID).Update("row_hash", make([]byte, 32)).Error; err != nil {
		t.Fatalf("tamper audit row: %v", err)
	}
	if err := repository.Verify(ctx, fixture.orgID); err != appaudit.ErrChainTampered {
		t.Fatalf("tampered chain error: got %v, want %v", err, appaudit.ErrChainTampered)
	}
}

func TestAuditRepository_ConcurrentFirstAppendsAllocateContiguousSequence(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	repository := NewAuditRepository(fixture.store)
	const count = 20
	results := make(chan appaudit.Event, count)
	errors := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			event, err := repository.Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "concurrent_test", Payload: []byte(`{"ok":true}`), Outcome: "success"}, fixture.now)
			if err != nil {
				errors <- err
				return
			}
			results <- event
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent append: %v", err)
	}
	sequences := make(map[int64]bool, count)
	for event := range results {
		sequences[event.OrgSequence] = true
	}
	if len(sequences) != count {
		t.Fatalf("sequence count: got %d, want %d", len(sequences), count)
	}
	for sequence := int64(1); sequence <= count; sequence++ {
		if !sequences[sequence] {
			t.Fatalf("missing sequence %d", sequence)
		}
	}
}

func TestAuditRepository_RecordDeniedPersistsOnlySafeMetadata(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	requestID, subjectID, jti := uuid.New(), fixture.actorID, uuid.New()
	service := appaudit.NewDeniedService(NewAuditRepository(fixture.store), auditTestClock{value: fixture.now})
	if err := service.Record(ctx, appaudit.DeniedRequest{RequestID: requestID, SubjectID: &subjectID, JTI: &jti, OrgID: &fixture.orgID, StationID: &fixture.stationID, Action: "read_report", Target: "report", Reason: "station_scope_forbidden", Outcome: "denied"}); err != nil {
		t.Fatalf("record denied request: %v", err)
	}
	var row AuditDeniedModel
	if err := fixture.store.DB.First(&row, "request_id = ?", requestID).Error; err != nil {
		t.Fatalf("load denied audit row: %v", err)
	}
	if row.Action != "read_report" || row.Target != "report" || row.Reason != "station_scope_forbidden" || row.Outcome != "denied" {
		t.Fatalf("denied audit row: %+v", row)
	}
}

type auditTestClock struct{ value time.Time }

func (c auditTestClock) Now() time.Time { return c.value }
