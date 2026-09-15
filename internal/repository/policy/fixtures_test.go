package policy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
)

type AuditLogModel = store.AuditLogModel
type UserModel = store.UserModel

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
	fixture := testsupport.NewGovernanceFixture(t, ctx)
	return governanceFixture{
		store: fixture.Store, cleanup: fixture.Cleanup, now: fixture.Now,
		orgID: fixture.OrgID, stationID: fixture.StationID,
		actorID: fixture.ActorID, creatorID: fixture.CreatorID,
		shiftID: fixture.ShiftID, reportID: fixture.ReportID, policySetID: fixture.PolicySetID,
	}
}
