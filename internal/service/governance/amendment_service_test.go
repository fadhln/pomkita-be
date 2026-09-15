package governance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAmendmentService_Request_RejectsMeterFields(t *testing.T) {
	repository := &amendmentRepositorySpy{}
	service := NewAmendmentService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := validAmendmentRequest()
	request.Items[0].Field = "meter_end"

	if _, err := service.Request(context.Background(), request); err != ErrAmendmentFieldForbidden {
		t.Fatalf("request error: got %v, want %v", err, ErrAmendmentFieldForbidden)
	}
	if repository.requestCalls != 0 {
		t.Fatalf("repository calls: got %d, want 0", repository.requestCalls)
	}
}

func TestAmendmentService_ListQueue_UsesStationAdminSessionScope(t *testing.T) {
	stationID := uuid.New()
	repository := &amendmentRepositorySpy{queue: []AmendmentQueueView{{AmendmentID: uuid.New(), StationID: stationID, Status: "pending"}}}
	service := NewAmendmentService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := AmendmentQueueRequest{OrgID: uuid.New(), ActorID: uuid.New(), Role: "Station Admin", StationIDs: []uuid.UUID{stationID}}

	result, err := service.ListQueue(context.Background(), request)
	if err != nil {
		t.Fatalf("list amendment queue: %v", err)
	}
	if len(result) != 1 || result[0].AmendmentID != repository.queue[0].AmendmentID {
		t.Fatalf("queue result: got %+v, want %+v", result, repository.queue)
	}
	if repository.queueRequest.OrgID != request.OrgID || repository.queueRequest.ActorID != request.ActorID || repository.queueRequest.Role != request.Role || len(repository.queueRequest.StationIDs) != 1 || repository.queueRequest.StationIDs[0] != stationID {
		t.Fatalf("queue request: got %+v, want %+v", repository.queueRequest, request)
	}
}

func TestAmendmentService_ListQueue_RejectsNonApproverRole(t *testing.T) {
	repository := &amendmentRepositorySpy{}
	service := NewAmendmentService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := AmendmentQueueRequest{OrgID: uuid.New(), ActorID: uuid.New(), Role: "Supervisor", StationIDs: []uuid.UUID{uuid.New()}}

	if _, err := service.ListQueue(context.Background(), request); err != ErrAmendmentRoleRequired {
		t.Fatalf("list queue error: got %v, want %v", err, ErrAmendmentRoleRequired)
	}
	if repository.queueCalls != 0 {
		t.Fatalf("repository queue calls: got %d, want 0", repository.queueCalls)
	}
}

type amendmentRepositorySpy struct {
	requestCalls int
	queue        []AmendmentQueueView
	queueRequest AmendmentQueueRequest
	queueCalls   int
}

func (s *amendmentRepositorySpy) RequestAmendment(context.Context, AmendmentRequest, time.Time) (Amendment, error) {
	s.requestCalls++
	return Amendment{}, nil
}

func (s *amendmentRepositorySpy) ApproveAmendment(context.Context, ApproveAmendmentRequest, time.Time) (Amendment, error) {
	return Amendment{}, nil
}

func (s *amendmentRepositorySpy) RejectAmendment(context.Context, RejectAmendmentRequest, time.Time) error {
	return nil
}

func (s *amendmentRepositorySpy) ListAmendmentQueue(_ context.Context, request AmendmentQueueRequest) ([]AmendmentQueueView, error) {
	s.queueCalls++
	s.queueRequest = request
	return s.queue, nil
}

func validAmendmentRequest() AmendmentRequest {
	return AmendmentRequest{
		OrgID: uuid.New(), StationID: uuid.New(), ShiftID: uuid.New(), BaseReportID: uuid.New(), RequesterID: uuid.New(), Role: "Supervisor", Reason: "correct cash amount", StaleCheckHash: make([]byte, 32),
		Items: []AmendmentItem{{TargetKind: "sales_declared", TargetLogicalID: uuid.New(), Field: "cash_amount", OldValue: []byte(`"100"`), NewValue: []byte(`"110"`)}},
	}
}
