package scope

import (
	"context"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
	"github.com/google/uuid"
)

func TestRequireEnabled_RejectsDisabledOrganization(t *testing.T) {
	database, cleanup := testsupport.NewStore(t, context.Background())
	defer cleanup()
	orgID, stationID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	if err := database.DB.Table("organizations").Create(map[string]any{"org_id": orgID, "name": "Disabled Org", "enabled": false, "created_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Table("stations").Create(map[string]any{"org_id": orgID, "station_id": stationID, "name": "Station", "enabled": true, "timezone": "UTC", "created_at": now}).Error; err != nil {
		t.Fatal(err)
	}

	err := RequireEnabled(database.DB, orgID, stationID)
	if err != ErrOrganizationDisabled {
		t.Fatalf("scope error: got %v, want %v", err, ErrOrganizationDisabled)
	}
}
