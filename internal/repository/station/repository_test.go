package station

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
	appstation "github.com/fadhln/pomkita-be/internal/service/station"
	"github.com/google/uuid"
)

func TestRepository_StationLifecycleWritesAuditAndKeepsDisabledStationReadable(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	orgID, actorID := uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	created, err := NewRepository(database).Create(ctx, orgID, appstation.CreateRequest{Name: "Main", Code: "JKT-01", Address: "Jakarta", Timezone: "Asia/Jakarta"}, actorID, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "Main" || created.Code != "JKT-01" || created.Address != "Jakarta" || created.Timezone != "Asia/Jakarta" || !created.Enabled {
		t.Fatalf("created station: %+v", created)
	}
	name, address := "Main Updated", "Bandung"
	updated, err := NewRepository(database).Update(ctx, orgID, created.StationID, appstation.UpdateRequest{Name: &name, Address: &address}, actorID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != name || updated.Address != address || updated.Code != created.Code {
		t.Fatalf("updated station: %+v", updated)
	}
	if err := NewRepository(database).Disable(ctx, orgID, created.StationID, actorID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("disable: %v", err)
	}
	read, err := NewRepository(database).Read(ctx, orgID, created.StationID)
	if err != nil {
		t.Fatalf("read disabled station: %v", err)
	}
	if read.Name != name || read.Enabled {
		t.Fatalf("disabled station: %+v", read)
	}
	enabled := true
	reenabled, err := NewRepository(database).Update(ctx, orgID, created.StationID, appstation.UpdateRequest{Enabled: &enabled}, actorID, now.Add(3*time.Minute))
	if err != nil || !reenabled.Enabled {
		t.Fatalf("re-enable station: result=%+v err=%v", reenabled, err)
	}
	var events []store.AuditLogModel
	if err := database.DB.Where("org_id = ?", orgID).Order("org_sequence").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("audit event count: got %d, want 4", len(events))
	}
	for _, event := range events {
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["actor_user_id"] != actorID.String() || payload["target_station_id"] != created.StationID.String() {
			t.Fatalf("audit target: %+v", payload)
		}
		if _, ok := payload["after"]; !ok {
			t.Fatalf("audit after value missing: %+v", payload)
		}
	}
}

func TestRepository_CreateRejectsCaseInsensitiveDuplicateCode(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	orgID := uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(database)
	request := appstation.CreateRequest{Name: "Main", Code: "abc", Timezone: "UTC"}
	if _, err := repository.Create(ctx, orgID, request, uuid.New(), now); err != nil {
		t.Fatalf("create first station: %v", err)
	}
	request.Code = "ABC"
	if _, err := repository.Create(ctx, orgID, request, uuid.New(), now); err != appstation.ErrCodeConflict {
		t.Fatalf("duplicate code error: got %v, want %v", err, appstation.ErrCodeConflict)
	}
}

func TestRepository_DisableSucceedsWhenStationHasReportAndRole(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.StationModel{OrgID: orgID, StationID: stationID, Name: "Main", Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.UserModel{UserID: userID, OrgID: orgID, DisplayName: "Owner", Email: "owner@example.test", Username: "owner", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": userID, "role": "Owner"}).Error; err != nil {
		t.Fatal(err)
	}
	shiftID, reportID, setID := uuid.New(), uuid.New(), uuid.New()
	if err := database.DB.Create(&store.ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-09-15", Status: "awaiting_confirmation", PriceMapSnapshot: []byte(`{}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.PolicySnapshotSetModel{SetID: setID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.ShiftReportModel{ReportID: reportID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, VersionNo: 1, Status: "submitted", SubmittedBy: userID, SubmittedAt: now, PolicySnapshot: setID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewRepository(database).Disable(ctx, orgID, stationID, userID, now.Add(time.Minute)); err != nil {
		t.Fatalf("disable station with references: %v", err)
	}
	var row store.StationModel
	if err := database.DB.Where("org_id = ? and station_id = ?", orgID, stationID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Enabled {
		t.Fatal("station remains enabled")
	}
}

func TestRepository_ListDoesNotReturnAnotherOrganization(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Now().UTC()
	orgID, otherID := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{orgID, otherID} {
		if err := database.DB.Create(&store.OrganizationModel{OrgID: id, Name: strings.ToUpper(id.String()[:8]), CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.DB.Create(&store.StationModel{OrgID: id, StationID: uuid.New(), Name: "Station", Timezone: "UTC", CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	stations, err := NewRepository(database).List(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stations) != 1 || stations[0].OrgID != orgID {
		t.Fatalf("stations: %+v", stations)
	}
}

func TestRepository_ReadMissingStationReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Now().UTC()
	orgID := uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(database).Read(ctx, orgID, uuid.New()); err != appstation.ErrStationNotFound {
		t.Fatalf("missing station error: got %v, want %v", err, appstation.ErrStationNotFound)
	}
}
