package organization

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
	apporg "github.com/pomkita/pomkita-be/internal/service/organization"
)

func TestRepository_CreateOrganizationAndFirstStationWritesAuditAtomically(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	request := apporg.CreateRequest{Name: "Pom Org", LegalName: "Pom Org PT", Address: "Jakarta", ContactEmail: "admin@example.test", Timezone: "Asia/Jakarta", FirstStation: &apporg.FirstStationRequest{Name: "Main", Timezone: "Asia/Jakarta"}}
	created, err := NewRepository(database).Create(ctx, request, uuid.New(), now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var stationCount, auditCount int64
	if err := database.DB.Model(&store.StationModel{}).Where("org_id = ?", created.OrgID).Count(&stationCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Table("audit_log").Where("org_id = ?", created.OrgID).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if stationCount != 1 || auditCount != 1 {
		t.Fatalf("station/audit count: %d/%d", stationCount, auditCount)
	}
	name := "Updated Org"
	updated, err := NewRepository(database).Update(ctx, created.OrgID, apporg.UpdateRequest{Name: &name}, uuid.New(), now.Add(time.Minute))
	if err != nil || updated.Name != name {
		t.Fatalf("update: %v, result=%+v", err, updated)
	}
	if err := NewRepository(database).Disable(ctx, created.OrgID, uuid.New(), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("disable: %v", err)
	}
	read, err := NewRepository(database).Read(ctx, created.OrgID)
	if err != nil || read.Enabled {
		t.Fatalf("disabled read: %v, enabled=%v", err, read.Enabled)
	}
	if err := database.DB.Table("audit_log").Where("org_id = ?", created.OrgID).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("audit count after changes: got %d, want 3", auditCount)
	}
}

func TestRepository_ReadOrganizationAcceptsNullDetailFields(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	orgID := uuid.New()
	if err := database.DB.Table("organizations").Create(map[string]any{"org_id": orgID, "name": "Legacy Org", "created_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := NewRepository(database).Read(ctx, orgID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result.Name != "Legacy Org" || result.LegalName != "" || result.Timezone != "" {
		t.Fatalf("result: %+v", result)
	}
}

func TestRepository_CreateOrganizationWithoutStationFails(t *testing.T) {
	database, cleanup := testsupport.NewStore(t, context.Background())
	defer cleanup()
	request := apporg.CreateRequest{Name: "Pom Org"}
	if _, err := NewRepository(database).Create(context.Background(), request, uuid.New(), time.Now()); err != apporg.ErrNoStation {
		t.Fatalf("error: got %v, want %v", err, apporg.ErrNoStation)
	}
}
