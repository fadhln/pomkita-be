package draft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/domain"
	appdraft "github.com/fadhln/pomkita-be/internal/service/draft"
	"github.com/google/uuid"
)

func TestDraftRepository_Heartbeat_RejectsDisabledOrganization(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken := uuid.New(), uuid.New(), uuid.New()
	if err := store.DB.Table("organizations").Create(map[string]any{"org_id": orgID, "name": "Disabled Org", "enabled": false, "created_at": now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "heartbeat@example.com", Username: "heartbeat", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	expires := now.Add(time.Minute)
	if err := store.DB.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: &expires, Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	err := NewDraftRepository(store).Heartbeat(ctx, appdraft.HeartbeatRequest{DraftID: draftID, ClaimToken: claimToken, ActorID: userID}, now)
	var scopeErr *domain.Error
	if !errors.As(err, &scopeErr) || scopeErr.Code != "org_disabled" {
		t.Fatalf("heartbeat error: got %v, want org_disabled", err)
	}
}

func TestDraftRepository_Claim_RejectsDisabledStation(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID := uuid.New(), uuid.New()
	if err := store.DB.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Table("stations").Create(map[string]any{"org_id": orgID, "station_id": stationID, "name": "Disabled Station", "enabled": false, "timezone": "UTC", "created_at": now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "disabled-station@example.com", Username: "disabled-station", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.DB.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	_, err := NewDraftRepository(store).Claim(ctx, appdraft.ClaimRequest{DraftID: draftID, ShiftID: shiftID, ActorID: userID}, now)
	if err == nil || err.Error() != "station_disabled" {
		t.Fatalf("claim error: got %v, want station_disabled", err)
	}
}

func TestDraftRepository_Claim_RejectsDisabledOrganization(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID := uuid.New(), uuid.New()
	if err := store.DB.Table("organizations").Create(map[string]any{"org_id": orgID, "name": "Disabled Org", "enabled": false, "created_at": now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "disabled@example.com", Username: "disabled", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.DB.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	_, err := NewDraftRepository(store).Claim(ctx, appdraft.ClaimRequest{DraftID: draftID, ShiftID: shiftID, ActorID: userID}, now)
	if err == nil || err.Error() != "org_disabled" {
		t.Fatalf("claim error: got %v, want org_disabled", err)
	}
}

func TestDraftRepository_WriteReading_RejectsExpiredClaim(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken := uuid.New(), uuid.New(), uuid.New()
	if err := store.DB.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "draft@example.com", Username: "draft", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	expired := now.Add(-time.Minute)
	if err := store.DB.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: &expired, Status: "editing", Revision: 3, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}

	repository := NewDraftRepository(store)
	_, err := repository.WriteReading(ctx, appdraft.WriteReadingRequest{DraftID: draftID, ClaimToken: claimToken, Revision: 3, NozzleID: uuid.New(), MeterStart: "10.0", MeterEnd: "12.0", ActorID: userID}, now)
	if !errors.Is(err, appdraft.ErrClaimExpired) {
		t.Fatalf("write reading error: got %v, want %v", err, appdraft.ErrClaimExpired)
	}
}
