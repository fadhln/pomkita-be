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

var _ = appreporting.ReportView{}
