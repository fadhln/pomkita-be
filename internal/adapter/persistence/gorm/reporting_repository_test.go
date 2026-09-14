package gormstore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

func TestReportingRepository_ReadReportAndExportAuditUseStableOrder(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	view, err := NewReportingRepository(fixture.store).ReadReport(ctx, fixture.orgID, fixture.stationID, fixture.reportID)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if view.ReportID != fixture.reportID || view.VersionNo != 1 || view.Status != "submitted" {
		t.Fatalf("report view: %+v", view)
	}
	if _, err := NewAuditRepository(fixture.store).Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "first", Payload: []byte(`{"n":1}`), Outcome: "success"}, fixture.now); err != nil {
		t.Fatalf("append first audit: %v", err)
	}
	secondID := uuid.New()
	if _, err := NewAuditRepository(fixture.store).Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: secondID, EventType: "second", Payload: []byte(`{"n":2}`), Outcome: "success"}, fixture.now); err != nil {
		t.Fatalf("append second audit: %v", err)
	}
	rows, err := NewReportingRepository(fixture.store).ExportAudit(ctx, fixture.orgID)
	if err != nil {
		t.Fatalf("export audit: %v", err)
	}
	if len(rows) != 2 || rows[0].OrgSequence != 1 || rows[1].OrgSequence != 2 || rows[1].EventID != secondID {
		t.Fatalf("audit rows: %+v", rows)
	}
}

func TestReportingRepository_AnomaliesIncludesBreakGlassAndEvidenceExceptions(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	breakGlassID := uuid.New()
	breakGlassReason := "emergency review"
	if err := fixture.store.db.Create(&AckDecisionModel{
		AckID: breakGlassID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID,
		ReportID: fixture.reportID, VersionNo: 1, AckSeq: 1, Decision: "acked", ActorUserID: fixture.actorID,
		DecidedAt: fixture.now, IsSuperadmin: false, IsBreakGlass: true, BreakGlassReason: &breakGlassReason,
	}).Error; err != nil {
		t.Fatalf("create break-glass decision: %v", err)
	}
	lossID, lossRowID := uuid.New(), uuid.New()
	nozzleID := uuid.New()
	dispenserID := uuid.New()
	if err := fixture.store.db.Create(&DispenserModel{OrgID: fixture.orgID, StationID: fixture.stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := fixture.store.db.Create(&NozzleModel{OrgID: fixture.orgID, StationID: fixture.stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	if err := fixture.store.db.Create(&LossIdentityModel{LossID: lossID, OrgID: fixture.orgID, StationID: fixture.stationID, CreatedBy: fixture.creatorID, CreatedAt: fixture.now}).Error; err != nil {
		t.Fatalf("create loss identity: %v", err)
	}
	if err := fixture.store.db.Create(&LossEntryModel{
		RowID: lossRowID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID,
		ReportID: fixture.reportID, VersionNo: 1, LossID: lossID, NozzleID: nozzleID,
		Direction: "loss", ReasonCode: "spill", Liters: Decimal("1.00"), CreatedBy: fixture.creatorID,
	}).Error; err != nil {
		t.Fatalf("create loss entry: %v", err)
	}
	exceptionID := uuid.New()
	if err := fixture.store.db.Create(&LossExceptionModel{
		ExceptionID: exceptionID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID,
		ReportID: fixture.reportID, LossID: lossID, Reason: "evidence unavailable", ActorUserID: fixture.actorID, CreatedAt: fixture.now,
	}).Error; err != nil {
		t.Fatalf("create loss exception: %v", err)
	}

	rows, err := NewReportingRepository(fixture.store).Anomalies(ctx, fixture.orgID, &fixture.stationID)
	if err != nil {
		t.Fatalf("load anomalies: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("anomaly count: got %d, want 2 (%+v)", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.SourceKind] = true
	}
	if !seen["break_glass"] || !seen["loss_exception"] {
		t.Fatalf("anomaly sources: got %+v", seen)
	}
}

var _ = appreporting.ReportView{}
