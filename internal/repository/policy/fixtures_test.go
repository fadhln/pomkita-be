package policy

import (
	"context"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
	"github.com/google/uuid"
)

type AuditLogModel = store.AuditLogModel
type UserModel = store.UserModel
type Store = store.Store
type Decimal = store.Decimal
type EvidencePolicyRevisionModel = store.EvidencePolicyRevisionModel
type StationModel = store.StationModel
type ThresholdPolicyRevisionModel = store.ThresholdPolicyRevisionModel

type governanceFixture struct {
	store                          *store.Store
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
