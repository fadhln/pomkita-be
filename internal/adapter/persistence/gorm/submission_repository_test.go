package gormstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	appreconciliation "github.com/pomkita/pomkita-be/internal/service/reconciliation"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

func TestSubmissionRepository_Submit_IsIdempotentByRequestHash(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken, policySetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "submit@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: policySetID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot set: %v", err)
	}

	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: claimToken, ExpectedRevision: 1, ActorID: userID, IdempotencyKey: "submit-1", Payload: []byte(`{"readings":[],"sales":[],"losses":[]}`)}
	first, err := service.Submit(ctx, request)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := service.Submit(ctx, request)
	if err != nil {
		t.Fatalf("replay submit: %v", err)
	}
	if !second.Replay || first.ReportID != second.ReportID {
		t.Fatalf("replay result: first=%+v second=%+v", first, second)
	}
	request.Payload = []byte(`{"readings":[{"nozzle_id":"different"}],"sales":[],"losses":[]}`)
	if _, err := service.Submit(ctx, request); !errors.Is(err, appsubmission.ErrIdempotencyConflict) {
		t.Fatalf("hash conflict: got %v, want %v", err, appsubmission.ErrIdempotencyConflict)
	}
	var reportCount int64
	if err := store.db.Model(&ShiftReportModel{}).Where("shift_id = ?", shiftID).Count(&reportCount).Error; err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if reportCount != 1 {
		t.Fatalf("report count: got %d, want 1", reportCount)
	}
}

func TestSubmissionRepository_Submit_PromotesDraftChildrenToReport(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken, policySetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dispenserID, nozzleID, draftReadingID, draftSalesID, lossID, lossRowID, evidenceRowID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "promote@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := store.db.Create(&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	snapshot := []byte(`{"hash_version":1,"items":[{"nozzle_id":"` + nozzleID.String() + `","dispenser_id":"` + dispenserID.String() + `","meter_max":"99999.9","modulus":"100000.0","price":"10000","price_id":"` + uuid.NewString() + `","dispenser_nozzle_map_id":"` + uuid.NewString() + `","nozzle_tank_map_id":null}]}`)
	if err := store.db.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: snapshot, PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 3, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: policySetID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot set: %v", err)
	}
	if err := store.db.Create(&DraftReadingModel{RowID: draftReadingID, OrgID: orgID, StationID: stationID, DraftID: draftID, NozzleID: nozzleID, MeterStart: Decimal("10.0"), MeterEnd: Decimal("12.0"), CreatedBy: userID}).Error; err != nil {
		t.Fatalf("create draft reading: %v", err)
	}
	if err := store.db.Create(&DraftSalesModel{RowID: draftSalesID, OrgID: orgID, StationID: stationID, DraftID: draftID, DispenserID: dispenserID, CashAmount: Decimal("20000"), CashlessAmount: Decimal("0"), CreatedBy: userID}).Error; err != nil {
		t.Fatalf("create draft sales: %v", err)
	}
	if err := store.db.Create(&DraftLossModel{RowID: lossRowID, OrgID: orgID, StationID: stationID, DraftID: draftID, LossID: lossID, Direction: "loss", ReasonCode: "test", Liters: Decimal("0.10"), CreatedBy: userID}).Error; err != nil {
		t.Fatalf("create draft loss: %v", err)
	}
	if err := store.db.Create(&DraftEvidenceStagingModel{RowID: evidenceRowID, OrgID: orgID, StationID: stationID, DraftID: draftID, LossRowID: lossRowID, EvidenceType: "photo", ObjectKey: "evidence/object", ContentHash: make([]byte, 32), SizeBytes: 10, MIME: "image/jpeg", Status: "finalized", UploadedBy: userID}).Error; err != nil {
		t.Fatalf("create staged evidence: %v", err)
	}

	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: claimToken, ExpectedRevision: 3, ActorID: userID, IdempotencyKey: "submit-promote", Payload: []byte(`{"hash_version":1,"readings":[{"nozzle_id":"` + nozzleID.String() + `","observed":true}],"sales":[],"losses":[{"loss_id":"` + lossID.String() + `","nozzle_id":"` + nozzleID.String() + `"}]}`)}
	result, err := service.Submit(ctx, request)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	var readingCount, salesCount, lossCount, evidenceCount int64
	if err := store.db.Model(&DispenserReadingModel{}).Where("report_id = ?", result.ReportID).Count(&readingCount).Error; err != nil {
		t.Fatalf("count report readings: %v", err)
	}
	if err := store.db.Model(&SalesDeclaredModel{}).Where("report_id = ?", result.ReportID).Count(&salesCount).Error; err != nil {
		t.Fatalf("count report sales: %v", err)
	}
	if err := store.db.Model(&LossEntryModel{}).Where("report_id = ?", result.ReportID).Count(&lossCount).Error; err != nil {
		t.Fatalf("count report losses: %v", err)
	}
	if err := store.db.Model(&EvidenceEventModel{}).Where("report_id = ?", result.ReportID).Count(&evidenceCount).Error; err != nil {
		t.Fatalf("count report evidence: %v", err)
	}
	if readingCount != 1 || salesCount != 1 || lossCount != 1 || evidenceCount != 1 {
		t.Fatalf("report children: readings=%d sales=%d losses=%d evidence=%d", readingCount, salesCount, lossCount, evidenceCount)
	}
}

func TestSubmissionRepository_Submit_RejectsBrokenMeterChain(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	previousShiftID, currentShiftID, previousReportID, currentDraftID, claimToken := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	previousSetID, currentSetID, nozzleID, dispenserID, previousReadingID, currentDraftRowID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "chain@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := store.db.Create(&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	snapshot := []byte(`{"hash_version":1,"items":[{"nozzle_id":"` + nozzleID.String() + `","dispenser_id":"` + dispenserID.String() + `","meter_max":"99999.9","modulus":"100000.0","price":"10000","price_id":"` + uuid.NewString() + `","dispenser_nozzle_map_id":"` + uuid.NewString() + `","nozzle_tank_map_id":null}]}`)
	for _, shift := range []ShiftModel{
		{ShiftID: previousShiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now.Add(-time.Hour), TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "locked", Backfilled: false, PriceMapSnapshot: snapshot, PriceMapHash: make([]byte, 32)},
		{ShiftID: currentShiftID, OrgID: orgID, StationID: stationID, StationSeq: 2, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: snapshot, PriceMapHash: make([]byte, 32)},
	} {
		if err := store.db.Create(&shift).Error; err != nil {
			t.Fatalf("create shift: %v", err)
		}
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: previousSetID, OrgID: orgID, StationID: stationID, ShiftID: &previousShiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create previous policy set: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: currentSetID, OrgID: orgID, StationID: stationID, ShiftID: &currentShiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create current policy set: %v", err)
	}
	if err := store.db.Create(&ShiftReportModel{ReportID: previousReportID, OrgID: orgID, StationID: stationID, ShiftID: previousShiftID, VersionNo: 1, Status: "locked", SubmittedBy: userID, SubmittedAt: now.Add(-time.Hour), PolicySnapshot: previousSetID}).Error; err != nil {
		t.Fatalf("create previous report: %v", err)
	}
	if err := store.db.Model(&ShiftModel{}).Where("shift_id = ?", previousShiftID).Updates(map[string]any{"current_report_id": previousReportID}).Error; err != nil {
		t.Fatalf("point previous report: %v", err)
	}
	if err := store.db.Create(&DispenserReadingModel{ReadingID: previousReadingID, OrgID: orgID, StationID: stationID, ShiftID: previousShiftID, ReportID: previousReportID, NozzleID: nozzleID, MeterStart: Decimal("10.0"), MeterEnd: Decimal("20.0"), PriceUsed: Decimal("10000"), ExpectedSale: Decimal("100000"), Observed: true, IsCarriedForward: false}).Error; err != nil {
		t.Fatalf("create previous reading: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: currentDraftID, OrgID: orgID, StationID: stationID, ShiftID: currentShiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 2, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create current draft: %v", err)
	}
	if err := store.db.Create(&DraftReadingModel{RowID: currentDraftRowID, OrgID: orgID, StationID: stationID, DraftID: currentDraftID, NozzleID: nozzleID, MeterStart: Decimal("21.0"), MeterEnd: Decimal("22.0"), CreatedBy: userID}).Error; err != nil {
		t.Fatalf("create current reading: %v", err)
	}

	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: currentShiftID, DraftID: currentDraftID, ClaimToken: claimToken, ExpectedRevision: 2, ActorID: userID, IdempotencyKey: "submit-chain", Payload: []byte(`{"readings":[{"nozzle_id":"` + nozzleID.String() + `","meter_start":"21.0","meter_end":"22.0"}],"sales":[],"losses":[]}`)}
	if _, err := service.Submit(ctx, request); !errors.Is(err, appreconciliation.ErrMeterChainConflict) {
		t.Fatalf("chain error: got %v, want %v", err, appreconciliation.ErrMeterChainConflict)
	}
}

func TestSubmissionRepository_Submit_UsesSnapshotRolloverThreshold(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken, policySetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dispenserID, nozzleID, draftReadingID := uuid.New(), uuid.New(), uuid.New()
	policyID, revisionID := uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Policy Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "policy-submit@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&DispenserModel{OrgID: orgID, StationID: stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := store.db.Create(&NozzleModel{OrgID: orgID, StationID: stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	snapshot := []byte(`{"hash_version":1,"items":[{"nozzle_id":"` + nozzleID.String() + `","dispenser_id":"` + dispenserID.String() + `","meter_max":"99999.9","modulus":"100000.0","price":"10000","price_id":"` + uuid.NewString() + `","dispenser_nozzle_map_id":"` + uuid.NewString() + `","nozzle_tank_map_id":null}]}`)
	if err := store.db.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: snapshot, PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: policySetID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot set: %v", err)
	}
	if err := store.db.Create(&ThresholdPolicyRevisionModel{RevID: revisionID, PolicyID: policyID, OrgID: orgID, ValidFrom: now.Add(-time.Hour), LossLiterThreshold: Decimal("10.00"), GainLiterThreshold: Decimal("10.00"), LossRupiahThreshold: Decimal("0"), GainRupiahThreshold: Decimal("0"), VarianceThreshold: Decimal("0"), RolloverThreshold: Decimal("40.0"), CreatedBy: userID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create threshold policy: %v", err)
	}
	if err := store.db.Create(&DraftReadingModel{RowID: draftReadingID, OrgID: orgID, StationID: stationID, DraftID: draftID, NozzleID: nozzleID, MeterStart: Decimal("99999.9"), MeterEnd: Decimal("30.0"), CreatedBy: userID}).Error; err != nil {
		t.Fatalf("create draft reading: %v", err)
	}

	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: claimToken, ExpectedRevision: 1, ActorID: userID, IdempotencyKey: "policy-rollover", Payload: []byte(`{"readings":[{"nozzle_id":"` + nozzleID.String() + `"}],"sales":[],"losses":[]}`)}
	if _, err := service.Submit(ctx, request); err != nil {
		t.Fatalf("submit with policy threshold: %v", err)
	}
}

func TestSubmissionRepository_Submit_RejectsExpiredTakeoverWithoutPreviousClaim(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken, policySetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Takeover Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "takeover@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: policySetID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot set: %v", err)
	}
	payload := []byte(`{"losses":[],"readings":[],"sales":[]}`)
	hash := sha256.Sum256(payload)
	if err := store.db.Create(&SubmitIdempotencyModel{IdemID: uuid.New(), OrgID: orgID, StationID: stationID, ShiftID: shiftID, IdempotencyKey: "missing-old-claim", RequestHash: hash[:], Status: "in_progress", AttemptCount: 1, LeaseStartedAt: ptrTime(now.Add(-time.Hour)), LeaseExpiresAt: ptrTime(now.Add(-time.Minute)), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}).Error; err != nil {
		t.Fatalf("create stale idempotency row: %v", err)
	}

	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: claimToken, ExpectedRevision: 1, ActorID: userID, IdempotencyKey: "missing-old-claim", Payload: payload}
	if _, err := service.Submit(ctx, request); !errors.Is(err, appsubmission.ErrIdempotencyConflict) {
		t.Fatalf("takeover error: got %v, want %v", err, appsubmission.ErrIdempotencyConflict)
	}
}

func TestSubmissionRepository_Submit_TakeoverRaceCreatesOneReport(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	service := NewSubmissionRepository(store)

	type outcome struct {
		result appsubmission.Result
		err    error
	}
	for iteration := 0; iteration < 3; iteration++ {
		orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
		shiftID, draftID, currentClaim, oldClaim, policySetID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
		for _, value := range []any{
			&OrganizationModel{OrgID: orgID, Name: "Race Org", CreatedAt: now},
			&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now},
			&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "race-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now},
			&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)},
			&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &currentClaim, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now},
			&PolicySnapshotSetModel{SetID: policySetID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now},
		} {
			if err := store.db.Create(value).Error; err != nil {
				t.Fatalf("iteration %d create fixture row: %v", iteration, err)
			}
		}
		payload := []byte(`{"losses":[],"readings":[],"sales":[]}`)
		hash := sha256.Sum256(payload)
		if err := store.db.Create(&SubmitIdempotencyModel{IdemID: uuid.New(), OrgID: orgID, StationID: stationID, ShiftID: shiftID, IdempotencyKey: "race", RequestHash: hash[:], Status: "in_progress", ClaimToken: &oldClaim, AttemptCount: 1, LeaseStartedAt: ptrTime(now.Add(-time.Hour)), LeaseExpiresAt: ptrTime(now.Add(-time.Minute)), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}).Error; err != nil {
			t.Fatalf("iteration %d create stale row: %v", iteration, err)
		}
		request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: currentClaim, ExpectedRevision: 1, ActorID: userID, IdempotencyKey: "race", Payload: payload}
		start := make(chan struct{})
		results := make(chan outcome, 2)
		for worker := 0; worker < 2; worker++ {
			go func() {
				<-start
				result, err := appsubmission.NewService(service, submissionClock{value: now}).Submit(ctx, request)
				results <- outcome{result: result, err: err}
			}()
		}
		close(start)
		var reports, replays int
		for worker := 0; worker < 2; worker++ {
			result := <-results
			if result.err != nil {
				t.Fatalf("iteration %d concurrent submit: %v", iteration, result.err)
			}
			if result.result.Replay {
				replays++
			} else {
				reports++
			}
		}
		if reports != 1 || replays != 1 {
			t.Fatalf("iteration %d outcomes: reports=%d replays=%d, want one each", iteration, reports, replays)
		}
		var reportCount int64
		if err := store.db.Model(&ShiftReportModel{}).Where("shift_id = ?", shiftID).Count(&reportCount).Error; err != nil {
			t.Fatalf("iteration %d count reports: %v", iteration, err)
		}
		if reportCount != 1 {
			t.Fatalf("iteration %d report count: got %d, want 1", iteration, reportCount)
		}
	}
}

func TestSubmissionRepository_Submit_FailureForcesANewIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID, shiftID, draftID, claimToken := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "failed-submit@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}
	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: claimToken, ExpectedRevision: 1, ActorID: userID, IdempotencyKey: "failed-key", Payload: []byte(`{"readings":"not-an-array","sales":[],"losses":[]}`)}
	if _, err := service.Submit(ctx, request); err == nil {
		t.Fatal("invalid submission succeeded")
	}
	var idem SubmitIdempotencyModel
	if err := store.db.Where("idempotency_key = ?", request.IdempotencyKey).First(&idem).Error; err != nil {
		t.Fatalf("load failed idempotency row: %v", err)
	}
	if idem.Status != "failed" {
		t.Fatalf("idempotency status: got %q, want failed", idem.Status)
	}
	if _, err := service.Submit(ctx, request); !errors.Is(err, appsubmission.ErrIdempotencyConflict) {
		t.Fatalf("failed-key replay: got %v, want %v", err, appsubmission.ErrIdempotencyConflict)
	}
}

type submissionClock struct{ value time.Time }

func (c submissionClock) Now() time.Time { return c.value }

func ptrTime(value time.Time) *time.Time { return &value }
