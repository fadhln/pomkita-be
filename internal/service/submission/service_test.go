package submission

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_Submit_CanonicalizesPayloadBeforeRepositoryCall(t *testing.T) {
	repository := &submissionRepositoryStub{result: Result{ReportID: uuid.New()}}
	service := NewService(repository, fixedClock{value: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	result, err := service.Submit(context.Background(), Request{
		OrgID:            uuid.New(),
		StationID:        uuid.New(),
		ShiftID:          uuid.New(),
		DraftID:          uuid.New(),
		ClaimToken:       uuid.New(),
		ExpectedRevision: 1,
		ActorID:          uuid.New(),
		IdempotencyKey:   "submit-1",
		Payload:          []byte(`{"sales":{"cash":"10","cashless":"0"},"readings":[]}`),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.ReportID == uuid.Nil || len(repository.requestHash) != 32 {
		t.Fatalf("result or hash: %+v %x", result, repository.requestHash)
	}
	if string(repository.payload) != `{"readings":[],"sales":{"cash":"10","cashless":"0"}}` {
		t.Fatalf("canonical payload: got %s", repository.payload)
	}
}

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type submissionRepositoryStub struct {
	result      Result
	requestHash []byte
	payload     []byte
}

func (r *submissionRepositoryStub) Submit(_ context.Context, request Request, requestHash []byte, payload []byte, _ time.Time) (Result, error) {
	r.requestHash = append([]byte(nil), requestHash...)
	r.payload = append([]byte(nil), payload...)
	return r.result, nil
}
