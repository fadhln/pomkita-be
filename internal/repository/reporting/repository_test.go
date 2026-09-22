package reporting

import (
	"context"
	"crypto/sha256"
	"testing"

	appaudit "github.com/fadhln/pomkita-be/internal/service/audit"
	appreporting "github.com/fadhln/pomkita-be/internal/service/reporting"
	"github.com/google/uuid"
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
	if err := fixture.store.DB.Create(&AckDecisionModel{
		AckID: breakGlassID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID,
		ReportID: fixture.reportID, VersionNo: 1, AckSeq: 1, Decision: "acked", ActorUserID: fixture.actorID,
		DecidedAt: fixture.now, IsSuperadmin: false, IsBreakGlass: true, BreakGlassReason: &breakGlassReason,
	}).Error; err != nil {
		t.Fatalf("create break-glass decision: %v", err)
	}
	lossID, lossRowID := uuid.New(), uuid.New()
	nozzleID := uuid.New()
	dispenserID := uuid.New()
	if err := fixture.store.DB.Create(&DispenserModel{OrgID: fixture.orgID, StationID: fixture.stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := fixture.store.DB.Create(&NozzleModel{OrgID: fixture.orgID, StationID: fixture.stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	if err := fixture.store.DB.Create(&LossIdentityModel{LossID: lossID, OrgID: fixture.orgID, StationID: fixture.stationID, CreatedBy: fixture.creatorID, CreatedAt: fixture.now}).Error; err != nil {
		t.Fatalf("create loss identity: %v", err)
	}
	if err := fixture.store.DB.Create(&LossEntryModel{
		RowID: lossRowID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID,
		ReportID: fixture.reportID, VersionNo: 1, LossID: lossID, NozzleID: nozzleID,
		Direction: "loss", ReasonCode: "spill", Liters: Decimal("1.00"), CreatedBy: fixture.creatorID,
	}).Error; err != nil {
		t.Fatalf("create loss entry: %v", err)
	}
	exceptionID := uuid.New()
	if err := fixture.store.DB.Create(&LossExceptionModel{
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

func TestReportingRepository_AnomaliesIncludesLossGainAndVariance(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	dispenserID, nozzleID := uuid.New(), uuid.New()
	if err := fixture.store.DB.Create(&DispenserModel{OrgID: fixture.orgID, StationID: fixture.stationID, DispenserID: dispenserID}).Error; err != nil {
		t.Fatalf("create dispenser: %v", err)
	}
	if err := fixture.store.DB.Create(&NozzleModel{OrgID: fixture.orgID, StationID: fixture.stationID, NozzleID: nozzleID, DispenserID: dispenserID, MeterMax: Decimal("99999.9")}).Error; err != nil {
		t.Fatalf("create nozzle: %v", err)
	}
	payload := []byte(`{"hash_version":1,"loss_liter_threshold":"10.00","gain_liter_threshold":"1.00","loss_rupiah_threshold":"100","gain_rupiah_threshold":"50","variance_rupiah_threshold":"0","rollover_threshold":"0"}`)
	payloadHash := sha256.Sum256(payload)
	if err := fixture.store.DB.Create(&PolicySnapshotItemModel{ItemID: uuid.New(), OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, SetID: fixture.policySetID, PolicyKind: "threshold", PolicyID: uuid.New(), RevID: uuid.New(), Scope: "organization", Payload: payload, PayloadHash: payloadHash[:]}).Error; err != nil {
		t.Fatalf("create threshold snapshot: %v", err)
	}
	if err := fixture.store.DB.Create(&DispenserReadingModel{ReadingID: uuid.New(), OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, NozzleID: nozzleID, MeterStart: Decimal("10.0"), MeterEnd: Decimal("11.0"), PriceUsed: Decimal("120"), ExpectedSale: Decimal("120"), Observed: true, IsCarriedForward: false}).Error; err != nil {
		t.Fatalf("create report reading: %v", err)
	}
	if err := fixture.store.DB.Create(&SalesDeclaredModel{SalesID: uuid.New(), OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, DispenserID: dispenserID, CashAmount: Decimal("100"), CashlessAmount: Decimal("0"), CreatedBy: fixture.creatorID}).Error; err != nil {
		t.Fatalf("create declared sale: %v", err)
	}
	for _, entry := range []struct {
		lossID, rowID uuid.UUID
		direction     string
		liters        string
		cash          string
	}{
		{uuid.New(), uuid.New(), "loss", "11.00", "101"},
		{uuid.New(), uuid.New(), "gain", "2.00", "51"},
	} {
		if err := fixture.store.DB.Create(&LossIdentityModel{LossID: entry.lossID, OrgID: fixture.orgID, StationID: fixture.stationID, CreatedBy: fixture.creatorID, CreatedAt: fixture.now}).Error; err != nil {
			t.Fatalf("create loss identity: %v", err)
		}
		cash := Decimal(entry.cash)
		if err := fixture.store.DB.Create(&LossEntryModel{RowID: entry.rowID, OrgID: fixture.orgID, StationID: fixture.stationID, ShiftID: fixture.shiftID, ReportID: fixture.reportID, VersionNo: 1, LossID: entry.lossID, NozzleID: nozzleID, Direction: entry.direction, ReasonCode: "test", Liters: Decimal(entry.liters), CashAmount: &cash, CreatedBy: fixture.creatorID}).Error; err != nil {
			t.Fatalf("create %s entry: %v", entry.direction, err)
		}
	}

	rows, err := NewReportingRepository(fixture.store).Anomalies(ctx, fixture.orgID, &fixture.stationID)
	if err != nil {
		t.Fatalf("load anomalies: %v", err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.SourceKind] = true
	}
	for _, source := range []string{"loss_threshold", "gain_threshold", "variance"} {
		if !seen[source] {
			t.Fatalf("missing %s anomaly in %+v", source, rows)
		}
	}
}

var _ = appreporting.ReportView{}
