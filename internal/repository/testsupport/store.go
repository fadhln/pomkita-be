// Package testsupport provides shared PostgreSQL fixtures for repository tests.
package testsupport

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	migrations "github.com/pomkita/pomkita-be/internal/platform/migrations"
	"github.com/pomkita/pomkita-be/internal/repository/store"
)

// NewStore creates a database store in an isolated schema.
func NewStore(t testing.TB, ctx context.Context) (*store.Store, func()) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	schema := "repository_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `create schema `+schema); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create isolated schema: %v", err)
	}

	scopedURL, err := url.Parse(dsn)
	if err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("parse database URL: %v", err)
	}
	query := scopedURL.Query()
	query.Set("options", "-c search_path="+schema)
	scopedURL.RawQuery = query.Encode()
	runner, err := migrations.New(scopedURL.String(), filepath.Join(repositoryRoot(t), "migrations"))
	if err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create migration runner: %v", err)
	}
	if err := runner.Up(ctx); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("apply migrations: %v", err)
	}
	database, err := store.Open(ctx, scopedURL.String())
	if err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("open GORM store: %v", err)
	}
	cleanup := func() {
		_ = database.Close()
		_, _ = admin.Exec(ctx, `drop schema if exists `+schema+` cascade`)
		_ = admin.Close(ctx)
	}
	return database, cleanup
}

// GovernanceFixture contains data for governance and reporting tests.
type GovernanceFixture struct {
	Store                          *store.Store
	Cleanup                        func()
	Now                            time.Time
	OrgID, StationID               uuid.UUID
	ActorID, CreatorID             uuid.UUID
	ShiftID, ReportID, PolicySetID uuid.UUID
}

// NewGovernanceFixture creates a submitted report that needs acknowledgement.
func NewGovernanceFixture(t testing.TB, ctx context.Context) GovernanceFixture {
	t.Helper()
	database, cleanup := NewStore(t, ctx)
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	fixture := GovernanceFixture{Store: database, Cleanup: cleanup, Now: now, OrgID: uuid.New(), StationID: uuid.New(), ActorID: uuid.New(), CreatorID: uuid.New(), ShiftID: uuid.New(), ReportID: uuid.New(), PolicySetID: uuid.New()}
	if err := database.DB.Create(&store.OrganizationModel{OrgID: fixture.OrgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := database.DB.Create(&store.StationModel{Name: "Station", OrgID: fixture.OrgID, StationID: fixture.StationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	for id, email := range map[uuid.UUID]string{fixture.ActorID: "admin@example.com", fixture.CreatorID: "creator@example.com"} {
		if err := database.DB.Create(&store.UserModel{UserID: id, OrgID: fixture.OrgID, DisplayName: email, Email: email, Username: strings.TrimSuffix(email, "@example.com"), PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": fixture.OrgID, "station_id": fixture.StationID, "user_id": fixture.ActorID, "role": "Station Admin"}).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := database.DB.Create(&store.ShiftModel{ShiftID: fixture.ShiftID, OrgID: fixture.OrgID, StationID: fixture.StationID, StationSeq: 1, SupervisorID: fixture.CreatorID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "awaiting_confirmation", PriceMapSnapshot: []byte(`{}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := database.DB.Create(&store.PolicySnapshotSetModel{SetID: fixture.PolicySetID, OrgID: fixture.OrgID, StationID: fixture.StationID, ShiftID: &fixture.ShiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot: %v", err)
	}
	if err := database.DB.Create(&store.ShiftReportModel{ReportID: fixture.ReportID, OrgID: fixture.OrgID, StationID: fixture.StationID, ShiftID: fixture.ShiftID, VersionNo: 1, Status: "submitted", SubmittedBy: fixture.CreatorID, SubmittedAt: now, PolicySnapshot: fixture.PolicySetID}).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	if err := database.DB.Model(&store.ShiftModel{}).Where("shift_id = ?", fixture.ShiftID).Update("current_report_id", fixture.ReportID).Error; err != nil {
		t.Fatalf("point current report: %v", err)
	}
	if err := database.DB.Create(&store.AckHeadModel{OrgID: fixture.OrgID, StationID: fixture.StationID, ShiftID: fixture.ShiftID, ReportID: fixture.ReportID, VersionNo: 1}).Error; err != nil {
		t.Fatalf("create acknowledgement head: %v", err)
	}
	return fixture
}

func repositoryRoot(t testing.TB) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test support path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}
