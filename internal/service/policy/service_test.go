package policy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_CreateRevision_RequiresTombstoneReason(t *testing.T) {
	service := NewService(&policyRepositorySpy{}, policyClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := PolicyRevisionRequest{OrgID: uuid.New(), StationID: uuid.New(), ActorID: uuid.New(), Role: "Owner", PolicyKind: "threshold", PolicyID: uuid.New(), ValidFrom: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Disabled: true}
	if _, err := service.CreateRevision(context.Background(), request); err != ErrTombstoneReasonRequired {
		t.Fatalf("create revision error: got %v, want %v", err, ErrTombstoneReasonRequired)
	}
}

func TestService_CreateRevision_RejectsFloatingPointText(t *testing.T) {
	service := NewService(&policyRepositorySpy{}, policyClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := validThresholdRequest()
	request.LossLiterThreshold = "0.1e1"
	if _, err := service.CreateRevision(context.Background(), request); err != ErrInvalidRequest {
		t.Fatalf("create revision error: got %v, want %v", err, ErrInvalidRequest)
	}
}

func validThresholdRequest() PolicyRevisionRequest {
	return PolicyRevisionRequest{OrgID: uuid.New(), StationID: uuid.New(), ActorID: uuid.New(), Role: "Owner", PolicyKind: "threshold", PolicyID: uuid.New(), ValidFrom: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), LossLiterThreshold: "1.00", GainLiterThreshold: "1.00", LossRupiahThreshold: "100", GainRupiahThreshold: "100", VarianceRupiahThreshold: "100", RolloverThreshold: "100000.0"}
}

type policyRepositorySpy struct{}

func (policyRepositorySpy) CreateRevision(context.Context, PolicyRevisionRequest, time.Time) (PolicyRevision, error) {
	return PolicyRevision{}, nil
}

type policyClock struct{ value time.Time }

func (c policyClock) Now() time.Time { return c.value }
