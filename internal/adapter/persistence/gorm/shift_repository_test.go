package gormstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
)

func TestShiftRepository_OpenShift_PersistsSnapshotAndDraft(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	dispenserID, nozzleID := uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "Asia/Jakarta", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "supervisor@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := store.db.Create(&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	if err := store.db.Exec(`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values(?,?,?,?,tstzrange(?,?,'[)'))`, orgID, stationID, dispenserID, nozzleID, now.Add(-time.Hour), now.Add(time.Hour)).Error; err != nil {
		t.Fatalf("create nozzle map: %v", err)
	}
	if err := store.db.Exec(`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values(?,?,?,?,tstzrange(?,?,'[)'),?)`, orgID, stationID, nozzleID, Decimal("10000"), now.Add(-time.Hour), now.Add(time.Hour), userID).Error; err != nil {
		t.Fatalf("create price: %v", err)
	}

	repository := NewShiftRepository(store)
	result, err := repository.OpenShift(ctx, appshift.OpenRequest{OrgID: orgID, StationID: stationID, ActorID: userID, OpenedAt: now})
	if err != nil {
		t.Fatalf("open shift: %v", err)
	}
	if result.StationSeq != 1 || result.BusinessDate != "2026-01-02" || result.Status != appshift.StatusOpen {
		t.Fatalf("result: %+v", result)
	}
	if len(result.PriceMapSnapshot) == 0 || len(result.PriceMapHash) != 32 {
		t.Fatalf("snapshot and hash: %s %x", result.PriceMapSnapshot, result.PriceMapHash)
	}
	var draft ShiftDraftModel
	if err := store.db.Where("shift_id = ?", result.ShiftID).First(&draft).Error; err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.Status != "editing" || draft.Revision != 1 || draft.OwnedBy == nil || *draft.OwnedBy != userID {
		t.Fatalf("draft: %+v", draft)
	}
}

func TestDecimalAddTenth_FormatsZeroMeterMaximum(t *testing.T) {
	if got := decimalAddTenth("0.0"); got != "0.1" {
		t.Fatalf("modulus: got %q, want %q", got, "0.1")
	}
}
