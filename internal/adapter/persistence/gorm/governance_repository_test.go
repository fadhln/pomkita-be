package gormstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

func TestGovernanceRepository_Acknowledge_LocksTheReportAndShift(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	store, now := fixture.store, fixture.now
	orgID, stationID := fixture.orgID, fixture.stationID
	actorID, reportID, shiftID := fixture.actorID, fixture.reportID, fixture.shiftID

	result, err := NewGovernanceRepository(store).Acknowledge(ctx, appgovernance.AcknowledgeRequest{
		OrgID: orgID, StationID: stationID, ShiftID: shiftID, ReportID: reportID, ActorID: actorID, VersionNo: 1, Role: "Station Admin", Decision: "acked",
	}, now)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if result.AckID == uuid.Nil || result.ShiftStatus != "locked" {
		t.Fatalf("result: %+v", result)
	}
	var head AckHeadModel
	if err := store.db.Where("report_id = ?", reportID).First(&head).Error; err != nil {
		t.Fatalf("load head: %v", err)
	}
	if head.ActiveAckID == nil || *head.ActiveAckID != result.AckID {
		t.Fatalf("active acknowledgement: got %v, want %v", head.ActiveAckID, result.AckID)
	}
	var shift ShiftModel
	if err := store.db.Where("shift_id = ?", shiftID).First(&shift).Error; err != nil {
		t.Fatalf("load shift: %v", err)
	}
	if shift.Status != "locked" {
		t.Fatalf("shift status: got %q, want locked", shift.Status)
	}
	var auditCount int64
	if err := store.db.Model(&AuditLogModel{}).Where("org_id = ? and event_id = ?", orgID, result.AckID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count acknowledgement audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("acknowledgement audit count: got %d, want 1", auditCount)
	}
}

func TestGovernanceRepository_Acknowledge_RejectsWrongRoleAndReportCreator(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	repository := NewGovernanceRepository(fixture.store)
	request := appgovernance.AcknowledgeRequest{OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, ActorID: fixture.actorID, VersionNo: 1, Role: "Operator", Decision: "acked"}
	if _, err := repository.Acknowledge(ctx, request, fixture.now); err != appgovernance.ErrAckRoleRequired {
		t.Fatalf("wrong role error: got %v, want %v", err, appgovernance.ErrAckRoleRequired)
	}
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.creatorID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create creator role: %v", err)
	}
	request.ActorID = fixture.creatorID
	request.Role = "Owner"
	if _, err := repository.Acknowledge(ctx, request, fixture.now); err != appgovernance.ErrAckSeparationRequired {
		t.Fatalf("creator error: got %v, want %v", err, appgovernance.ErrAckSeparationRequired)
	}
}

func TestGovernanceRepository_Acknowledge_RejectionNeedsCorrection(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	result, err := NewGovernanceRepository(fixture.store).Acknowledge(ctx, appgovernance.AcknowledgeRequest{
		OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, ActorID: fixture.actorID, VersionNo: 1, Role: "Station Admin", Decision: "rejected", RejectionReason: "meter evidence is missing",
	}, fixture.now)
	if err != nil {
		t.Fatalf("reject acknowledgement: %v", err)
	}
	if result.ShiftStatus != "needs_correction" {
		t.Fatalf("shift status in result: got %q, want needs_correction", result.ShiftStatus)
	}
	var shift ShiftModel
	if err := fixture.store.db.Where("shift_id = ?", fixture.shiftID).First(&shift).Error; err != nil {
		t.Fatalf("load shift: %v", err)
	}
	if shift.Status != "needs_correction" {
		t.Fatalf("shift status: got %q, want needs_correction", shift.Status)
	}
	var ack AckDecisionModel
	if err := fixture.store.db.Where("ack_id = ?", result.AckID).First(&ack).Error; err != nil {
		t.Fatalf("load acknowledgement: %v", err)
	}
	if ack.RejectionReason == nil || *ack.RejectionReason != "meter evidence is missing" {
		t.Fatalf("rejection reason: got %v", ack.RejectionReason)
	}
}

func TestGovernanceRepository_RequestAmendment_CreatesPendingAllowlistedItems(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Supervisor"}).Error; err != nil {
		t.Fatalf("create supervisor role: %v", err)
	}
	result, err := NewGovernanceRepository(fixture.store).RequestAmendment(ctx, appgovernance.AmendmentRequest{
		OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, BaseReportID: fixture.reportID, RequesterID: fixture.actorID, Role: "Supervisor", Reason: "correct cash amount", StaleCheckHash: make([]byte, 32),
		Items: []appgovernance.AmendmentItem{{TargetKind: "sales_declared", TargetLogicalID: uuid.New(), Field: "cash_amount", OldValue: []byte(`"100"`), NewValue: []byte(`"110"`)}},
	}, fixture.now)
	if err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	if result.AmendmentID == uuid.Nil || result.Status != "pending" || len(result.StaleCheckHash) != 32 {
		t.Fatalf("result: %+v", result)
	}
	var amendment AmendmentModel
	if err := fixture.store.db.Where("amendment_id = ?", result.AmendmentID).First(&amendment).Error; err != nil {
		t.Fatalf("load amendment: %v", err)
	}
	if amendment.Status != "pending" || amendment.RequesterUserID != fixture.actorID {
		t.Fatalf("amendment: %+v", amendment)
	}
	var itemCount int64
	if err := fixture.store.db.Model(&AmendmentItemModel{}).Where("amendment_id = ?", result.AmendmentID).Count(&itemCount).Error; err != nil {
		t.Fatalf("count amendment items: %v", err)
	}
	if itemCount != 1 {
		t.Fatalf("amendment item count: got %d, want 1", itemCount)
	}
	var auditCount int64
	if err := fixture.store.db.Model(&AuditLogModel{}).Where("org_id = ? and event_id = ?", fixture.orgID, result.AmendmentID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count amendment audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("amendment audit count: got %d, want 1", auditCount)
	}
}

func TestGovernanceRepository_RejectAmendment_EnforcesSeparationAndState(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.creatorID, "role": "Supervisor"}).Error; err != nil {
		t.Fatalf("create requester role: %v", err)
	}
	requested, err := NewGovernanceRepository(fixture.store).RequestAmendment(ctx, appgovernance.AmendmentRequest{
		OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, BaseReportID: fixture.reportID, RequesterID: fixture.creatorID, Role: "Supervisor", Reason: "correct cash amount", StaleCheckHash: make([]byte, 32),
		Items: []appgovernance.AmendmentItem{{TargetKind: "sales_declared", TargetLogicalID: uuid.New(), Field: "cash_amount", OldValue: []byte(`"100"`), NewValue: []byte(`"110"`)}},
	}, fixture.now)
	if err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create approver role: %v", err)
	}
	if err := NewGovernanceRepository(fixture.store).RejectAmendment(ctx, appgovernance.RejectAmendmentRequest{OrgID: fixture.orgID, StationID: fixture.stationID, AmendmentID: requested.AmendmentID, ApproverID: fixture.creatorID, Role: "Supervisor", RejectionReason: "same requester"}, fixture.now); err != appgovernance.ErrAmendmentRoleRequired {
		t.Fatalf("requester role error: got %v, want %v", err, appgovernance.ErrAmendmentRoleRequired)
	}
	if err := NewGovernanceRepository(fixture.store).RejectAmendment(ctx, appgovernance.RejectAmendmentRequest{OrgID: fixture.orgID, StationID: fixture.stationID, AmendmentID: requested.AmendmentID, ApproverID: fixture.actorID, Role: "Owner", RejectionReason: "same requester"}, fixture.now); err != nil {
		t.Fatalf("reject amendment: %v", err)
	}
	var amendment AmendmentModel
	if err := fixture.store.db.Where("amendment_id = ?", requested.AmendmentID).First(&amendment).Error; err != nil {
		t.Fatalf("load amendment: %v", err)
	}
	if amendment.Status != "rejected" || amendment.ApproverUserID == nil || *amendment.ApproverUserID != fixture.actorID {
		t.Fatalf("rejected amendment: %+v", amendment)
	}
}

func TestGovernanceRepository_ApproveAmendment_RejectsStaleBase(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.creatorID, "role": "Supervisor"}).Error; err != nil {
		t.Fatalf("create requester role: %v", err)
	}
	requested, err := NewGovernanceRepository(fixture.store).RequestAmendment(ctx, appgovernance.AmendmentRequest{
		OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, BaseReportID: fixture.reportID, RequesterID: fixture.creatorID, Role: "Supervisor", Reason: "correct cash amount", StaleCheckHash: make([]byte, 32),
		Items: []appgovernance.AmendmentItem{{TargetKind: "sales_declared", TargetLogicalID: uuid.New(), Field: "cash_amount", OldValue: []byte(`"100"`), NewValue: []byte(`"110"`)}},
	}, fixture.now)
	if err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	if _, err := NewGovernanceRepository(fixture.store).ApproveAmendment(ctx, appgovernance.ApproveAmendmentRequest{OrgID: fixture.orgID, StationID: fixture.stationID, AmendmentID: requested.AmendmentID, ApproverID: fixture.actorID, Role: "Station Admin", StaleCheckHash: make([]byte, 32)}, fixture.now); err != appgovernance.ErrAmendmentStale {
		t.Fatalf("stale approval error: got %v, want %v", err, appgovernance.ErrAmendmentStale)
	}
}

func TestGovernanceRepository_ApproveAmendment_CreatesNewVersionAndSupersedesAck(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	_, err := NewGovernanceRepository(fixture.store).Acknowledge(ctx, appgovernance.AcknowledgeRequest{OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, ActorID: fixture.actorID, VersionNo: 1, Role: "Station Admin", Decision: "acked"}, fixture.now)
	if err != nil {
		t.Fatalf("acknowledge base report: %v", err)
	}
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Supervisor"}).Error; err != nil {
		t.Fatalf("create requester role: %v", err)
	}
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.creatorID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create approver role: %v", err)
	}
	dispenserID, salesID := uuid.New(), uuid.New()
	if err := fixture.store.db.Create(&DispenserModel{OrgID: fixture.orgID, StationID: fixture.stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := fixture.store.db.Create(&SalesDeclaredModel{SalesID: salesID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, DispenserID: dispenserID, CashAmount: Decimal("100"), CashlessAmount: Decimal("0"), CreatedBy: fixture.actorID}).Error; err != nil {
		t.Fatalf("create sale: %v", err)
	}
	requested, err := NewGovernanceRepository(fixture.store).RequestAmendment(ctx, appgovernance.AmendmentRequest{
		OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, BaseReportID: fixture.reportID, RequesterID: fixture.actorID, Role: "Supervisor", Reason: "correct cash amount", StaleCheckHash: make([]byte, 32),
		Items: []appgovernance.AmendmentItem{{TargetKind: "sales_declared", TargetLogicalID: salesID, Field: "cash_amount", OldValue: []byte(`"100"`), NewValue: []byte(`"110"`)}},
	}, fixture.now)
	if err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	result, err := NewGovernanceRepository(fixture.store).ApproveAmendment(ctx, appgovernance.ApproveAmendmentRequest{OrgID: fixture.orgID, StationID: fixture.stationID, AmendmentID: requested.AmendmentID, ApproverID: fixture.creatorID, Role: "Owner", StaleCheckHash: requested.StaleCheckHash}, fixture.now)
	if err != nil {
		t.Fatalf("approve amendment: %v", err)
	}
	if result.AppliedReportID == uuid.Nil || result.VersionNo != 2 {
		t.Fatalf("approval result: %+v", result)
	}
	var shift ShiftModel
	if err := fixture.store.db.Where("shift_id = ?", fixture.shiftID).First(&shift).Error; err != nil {
		t.Fatalf("load shift: %v", err)
	}
	if shift.Status != "awaiting_confirmation" || shift.CurrentReportID == nil || *shift.CurrentReportID != result.AppliedReportID {
		t.Fatalf("amended shift: %+v", shift)
	}
	var oldHead AckHeadModel
	if err := fixture.store.db.Where("report_id = ?", fixture.reportID).First(&oldHead).Error; err != nil {
		t.Fatalf("load old head: %v", err)
	}
	if oldHead.ActiveAckID != nil {
		t.Fatalf("old acknowledgement head: %v", oldHead.ActiveAckID)
	}
	var sale SalesDeclaredModel
	if err := fixture.store.db.Where("report_id = ? and dispenser_id = ?", result.AppliedReportID, dispenserID).First(&sale).Error; err != nil {
		t.Fatalf("load amended sale: %v", err)
	}
	if sale.CashAmount.String() != "110" {
		t.Fatalf("amended cash amount: got %q, want 110", sale.CashAmount.String())
	}
	var supersessionCount int64
	if err := fixture.store.db.Model(&AckSupersessionModel{}).Where("old_report_id = ?", fixture.reportID).Count(&supersessionCount).Error; err != nil {
		t.Fatalf("count supersessions: %v", err)
	}
	if supersessionCount != 1 {
		t.Fatalf("supersession count: got %d, want 1", supersessionCount)
	}
}

type governanceFixture struct {
	store                          *Store
	cleanup                        func()
	now                            time.Time
	orgID, stationID               uuid.UUID
	actorID, creatorID             uuid.UUID
	shiftID, reportID, policySetID uuid.UUID
}

func newGovernanceFixture(t *testing.T, ctx context.Context) governanceFixture {
	t.Helper()
	store, cleanup := newAuthTestStore(t, ctx)
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	fixture := governanceFixture{store: store, cleanup: cleanup, now: now, orgID: uuid.New(), stationID: uuid.New(), actorID: uuid.New(), creatorID: uuid.New(), shiftID: uuid.New(), reportID: uuid.New(), policySetID: uuid.New()}
	if err := store.db.Create(&OrganizationModel{OrgID: fixture.orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: fixture.orgID, StationID: fixture.stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	for id, email := range map[uuid.UUID]string{fixture.actorID: "admin@example.com", fixture.creatorID: "creator@example.com"} {
		if err := store.db.Create(&UserModel{UserID: id, OrgID: fixture.orgID, DisplayName: email, Email: email, PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Station Admin"}).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := store.db.Create(&ShiftModel{ShiftID: fixture.shiftID, OrgID: fixture.orgID, StationID: fixture.stationID, StationSeq: 1, SupervisorID: fixture.creatorID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "awaiting_confirmation", PriceMapSnapshot: []byte(`{}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: fixture.policySetID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: &fixture.shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot: %v", err)
	}
	if err := store.db.Create(&ShiftReportModel{ReportID: fixture.reportID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, VersionNo: 1, Status: "submitted", SubmittedBy: fixture.creatorID, SubmittedAt: now, PolicySnapshot: fixture.policySetID}).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	if err := store.db.Model(&ShiftModel{}).Where("shift_id = ?", fixture.shiftID).Update("current_report_id", fixture.reportID).Error; err != nil {
		t.Fatalf("point current report: %v", err)
	}
	if err := store.db.Create(&AckHeadModel{OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, VersionNo: 1}).Error; err != nil {
		t.Fatalf("create acknowledgement head: %v", err)
	}
	return fixture
}
