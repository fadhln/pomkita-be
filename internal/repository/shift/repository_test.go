package shift

import (
	"context"
	"errors"
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
	if err := store.DB.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "Asia/Jakarta", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "supervisor@example.com", Username: "supervisor", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Create(&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := store.DB.Create(&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	if err := store.DB.Exec(`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values(?,?,?,?,tstzrange(?,?,'[)'))`, orgID, stationID, dispenserID, nozzleID, now.Add(-time.Hour), now.Add(time.Hour)).Error; err != nil {
		t.Fatalf("create nozzle map: %v", err)
	}
	if err := store.DB.Exec(`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values(?,?,?,?,tstzrange(?,?,'[)'),?)`, orgID, stationID, nozzleID, Decimal("10000"), now.Add(-time.Hour), now.Add(time.Hour), userID).Error; err != nil {
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
	if err := store.DB.Where("shift_id = ?", result.ShiftID).First(&draft).Error; err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.Status != "editing" || draft.Revision != 1 || draft.OwnedBy == nil || *draft.OwnedBy != userID {
		t.Fatalf("draft: %+v", draft)
	}
	var auditCount int64
	if err := store.DB.Model(&AuditLogModel{}).Where("org_id = ? and event_id = ?", orgID, result.ShiftID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count shift audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("shift audit count: got %d, want 1", auditCount)
	}
}

func TestDecimalAddTenth_FormatsZeroMeterMaximum(t *testing.T) {
	if got := decimalAddTenth("0.0"); got != "0.1" {
		t.Fatalf("modulus: got %q, want %q", got, "0.1")
	}
}

func TestShiftRepository_OpenShift_PersistsApprovedBackfillMetadata(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, actorID, approverID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dispenserID, nozzleID := uuid.New(), uuid.New()
	if err := store.DB.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	for _, user := range []UserModel{
		{UserID: actorID, OrgID: orgID, DisplayName: "Supervisor", Email: "backfill-supervisor@example.com", Username: "backfill-supervisor", PasswordHash: "hash", Enabled: true, CreatedAt: now},
		{UserID: approverID, OrgID: orgID, DisplayName: "Owner", Email: "backfill-owner@example.com", Username: "backfill-owner", PasswordHash: "hash", Enabled: true, CreatedAt: now},
	} {
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := store.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": approverID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create approver role: %v", err)
	}
	if err := store.DB.Create(&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := store.DB.Create(&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	if err := store.DB.Exec(`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values(?,?,?,?,tstzrange(?,?,'[)'))`, orgID, stationID, dispenserID, nozzleID, now.Add(-time.Hour), now.Add(time.Hour)).Error; err != nil {
		t.Fatalf("create nozzle map: %v", err)
	}
	if err := store.DB.Exec(`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values(?,?,?,?,tstzrange(?,?,'[)'),?)`, orgID, stationID, nozzleID, Decimal("10000"), now.Add(-time.Hour), now.Add(time.Hour), actorID).Error; err != nil {
		t.Fatalf("create price: %v", err)
	}

	result, err := NewShiftRepository(store).OpenShift(ctx, appshift.OpenRequest{OrgID: orgID, StationID: stationID, ActorID: actorID, Role: "Supervisor", OpenedAt: now, Backfilled: true, OriginalEventDate: "2025-12-31", ShiftKE: 2, BackfillApprover: approverID, BackfillReason: "late paper report"})
	if err != nil {
		t.Fatalf("open backfill: %v", err)
	}
	var shift ShiftModel
	if err := store.DB.First(&shift, "shift_id = ?", result.ShiftID).Error; err != nil {
		t.Fatalf("load backfill shift: %v", err)
	}
	if !shift.Backfilled || shift.OriginalEventDate == nil || shift.ShiftKE == nil || *shift.ShiftKE != 2 || shift.BackfillApprover == nil || *shift.BackfillApprover != approverID || shift.BackfillReason == nil || *shift.BackfillReason != "late paper report" {
		t.Fatalf("backfill metadata: %+v", shift)
	}
}

func TestShiftRepository_OpenShift_RejectsBackfillBeforeLaterChainedReport(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, actorID, approverID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	baseShiftID, laterShiftID := uuid.New(), uuid.New()
	baseReportID, laterReportID := uuid.New(), uuid.New()
	baseSetID, laterSetID := uuid.New(), uuid.New()
	dispenserID, nozzleID, baseReadingID, laterReadingID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, value := range []any{
		&OrganizationModel{OrgID: orgID, Name: "Backfill Order", CreatedAt: now},
		&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now},
		&UserModel{UserID: actorID, OrgID: orgID, DisplayName: "Supervisor", Email: "backfill-order-supervisor@example.com", Username: "backfill-order-supervisor", PasswordHash: "hash", Enabled: true, CreatedAt: now},
		&UserModel{UserID: approverID, OrgID: orgID, DisplayName: "Owner", Email: "backfill-order-owner@example.com", Username: "backfill-order-owner", PasswordHash: "hash", Enabled: true, CreatedAt: now},
		&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID},
		&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")},
	} {
		if err := store.DB.Create(value).Error; err != nil {
			t.Fatalf("create fixture row: %v", err)
		}
	}
	if err := store.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": approverID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create approver role: %v", err)
	}
	if err := store.DB.Exec(`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values(?,?,?,?,tstzrange(?,?,'[)'))`, orgID, stationID, dispenserID, nozzleID, now.Add(-time.Hour), now.Add(time.Hour)).Error; err != nil {
		t.Fatalf("create nozzle map: %v", err)
	}
	if err := store.DB.Exec(`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values(?,?,?,?,tstzrange(?,?,'[)'),?)`, orgID, stationID, nozzleID, Decimal("10000"), now.Add(-time.Hour), now.Add(time.Hour), actorID).Error; err != nil {
		t.Fatalf("create price: %v", err)
	}
	snapshot := []byte(`{"hash_version":1,"items":[{"nozzle_id":"` + nozzleID.String() + `","dispenser_id":"` + dispenserID.String() + `","meter_max":"99999.9","modulus":"100000.0","price":"10000"}]}`)
	for _, shift := range []ShiftModel{
		{ShiftID: baseShiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: actorID, OpenedAt: now.Add(-48 * time.Hour), TimezoneSnapshot: "UTC", BusinessDate: "2025-12-31", Status: "locked", Backfilled: false, PriceMapSnapshot: snapshot, PriceMapHash: make([]byte, 32)},
		{ShiftID: laterShiftID, OrgID: orgID, StationID: stationID, StationSeq: 2, SupervisorID: actorID, OpenedAt: now.Add(-24 * time.Hour), TimezoneSnapshot: "UTC", BusinessDate: "2026-01-01", Status: "locked", Backfilled: false, PriceMapSnapshot: snapshot, PriceMapHash: make([]byte, 32)},
	} {
		if err := store.DB.Create(&shift).Error; err != nil {
			t.Fatalf("create locked shift: %v", err)
		}
	}
	for _, set := range []PolicySnapshotSetModel{
		{SetID: baseSetID, OrgID: orgID, StationID: stationID, ShiftID: &baseShiftID, CreatedAt: now},
		{SetID: laterSetID, OrgID: orgID, StationID: stationID, ShiftID: &laterShiftID, CreatedAt: now},
	} {
		if err := store.DB.Create(&set).Error; err != nil {
			t.Fatalf("create policy set: %v", err)
		}
	}
	if err := store.DB.Create(&ShiftReportModel{ReportID: baseReportID, OrgID: orgID, StationID: stationID, ShiftID: baseShiftID, VersionNo: 1, Status: "locked", SubmittedBy: actorID, SubmittedAt: now.Add(-48 * time.Hour), PolicySnapshot: baseSetID}).Error; err != nil {
		t.Fatalf("create base report: %v", err)
	}
	if err := store.DB.Create(&ShiftReportModel{ReportID: laterReportID, OrgID: orgID, StationID: stationID, ShiftID: laterShiftID, VersionNo: 1, Status: "locked", SubmittedBy: actorID, SubmittedAt: now.Add(-24 * time.Hour), PolicySnapshot: laterSetID}).Error; err != nil {
		t.Fatalf("create later report: %v", err)
	}
	if err := store.DB.Model(&ShiftModel{}).Where("shift_id = ?", baseShiftID).Update("current_report_id", baseReportID).Error; err != nil {
		t.Fatalf("point base report: %v", err)
	}
	if err := store.DB.Model(&ShiftModel{}).Where("shift_id = ?", laterShiftID).Update("current_report_id", laterReportID).Error; err != nil {
		t.Fatalf("point later report: %v", err)
	}
	if err := store.DB.Create(&DispenserReadingModel{ReadingID: baseReadingID, OrgID: orgID, StationID: stationID, ShiftID: baseShiftID, ReportID: baseReportID, NozzleID: nozzleID, MeterStart: Decimal("10.0"), MeterEnd: Decimal("20.0"), PriceUsed: Decimal("10000"), ExpectedSale: Decimal("100000"), Observed: true, IsCarriedForward: false}).Error; err != nil {
		t.Fatalf("create base reading: %v", err)
	}
	if err := store.DB.Create(&DispenserReadingModel{ReadingID: laterReadingID, OrgID: orgID, StationID: stationID, ShiftID: laterShiftID, ReportID: laterReportID, NozzleID: nozzleID, MeterStart: Decimal("20.0"), MeterEnd: Decimal("30.0"), PriceUsed: Decimal("10000"), ExpectedSale: Decimal("100000"), Observed: false, IsCarriedForward: true, SourceShiftID: &baseShiftID, SourceReportID: &baseReportID, SourceReadingID: &baseReadingID}).Error; err != nil {
		t.Fatalf("create later reading: %v", err)
	}

	_, err := NewShiftRepository(store).OpenShift(ctx, appshift.OpenRequest{OrgID: orgID, StationID: stationID, ActorID: actorID, Role: "Supervisor", OpenedAt: now, Backfilled: true, OriginalEventDate: "2025-12-30", ShiftKE: 1, BackfillApprover: approverID, BackfillReason: "late paper report"})
	if !errors.Is(err, appshift.ErrBackfillOutOfOrder) {
		t.Fatalf("open backfill error: got %v, want %v", err, appshift.ErrBackfillOutOfOrder)
	}
}
