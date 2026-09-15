package recovery

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	apprecovery "github.com/pomkita/pomkita-be/internal/service/recovery"
)

func TestRecoveryRepository_RecoverStale_ReopensDraftWithoutReport(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID, shiftID, draftID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.DB.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "recovery@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now.Add(-time.Hour), TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "submitting", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.DB.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, Status: "submitting", Revision: 2, RecoveryCount: 0, UpdatedAt: now.Add(-11 * time.Minute)}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	service := apprecovery.NewService(NewRecoveryRepository(store))
	count, err := service.RecoverStale(ctx, now, 10*time.Minute)
	if err != nil {
		t.Fatalf("recover stale: %v", err)
	}
	if count != 1 {
		t.Fatalf("recovered count: got %d, want 1", count)
	}
	var draft ShiftDraftModel
	if err := store.DB.First(&draft, "draft_id = ?", draftID).Error; err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.Status != "editing" || draft.RecoveryCount != 1 {
		t.Fatalf("draft after recovery: %+v", draft)
	}
	var shift ShiftModel
	if err := store.DB.First(&shift, "shift_id = ?", shiftID).Error; err != nil {
		t.Fatalf("load shift: %v", err)
	}
	if shift.Status != "open" {
		t.Fatalf("shift status: got %q, want open", shift.Status)
	}
	var auditCount int64
	if err := store.DB.Model(&AuditLogModel{}).Where("org_id = ? and event_id = ? and event_type = ?", orgID, draftID, "shift.recovered").Count(&auditCount).Error; err != nil {
		t.Fatalf("count recovery audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("recovery audit events: got %d, want 1", auditCount)
	}
}
