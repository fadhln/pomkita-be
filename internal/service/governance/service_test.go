package governance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_Acknowledge_RejectsAnUnpermittedRole(t *testing.T) {
	repository := &acknowledgementRepositorySpy{}
	service := NewService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := AcknowledgeRequest{
		OrgID:     uuid.New(),
		StationID: uuid.New(),
		ShiftID:   uuid.New(),
		ReportID:  uuid.New(),
		ActorID:   uuid.New(),
		VersionNo: 1,
		Role:      "Operator",
		Decision:  "acked",
	}

	if _, err := service.Acknowledge(context.Background(), request); err != ErrAckRoleRequired {
		t.Fatalf("acknowledge error: got %v, want %v", err, ErrAckRoleRequired)
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls: got %d, want 0", repository.calls)
	}
}

func TestService_Acknowledge_ValidatesDecisionRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*AcknowledgeRequest)
		want error
	}{
		{name: "rejection reason required", edit: func(request *AcknowledgeRequest) { request.Decision = "rejected" }, want: ErrRejectionReasonRequired},
		{name: "acked rejects a reason", edit: func(request *AcknowledgeRequest) { request.RejectionReason = "not needed" }, want: ErrUnexpectedRejectionReason},
		{name: "break glass reason required", edit: func(request *AcknowledgeRequest) { request.Role = "Owner"; request.IsBreakGlass = true }, want: ErrBreakGlassReasonRequired},
		{name: "station admin cannot break glass", edit: func(request *AcknowledgeRequest) { request.IsBreakGlass = true; request.BreakGlassReason = "urgent" }, want: ErrAckRoleRequired},
		{name: "normal acknowledgement rejects break glass reason", edit: func(request *AcknowledgeRequest) { request.BreakGlassReason = "unexpected" }, want: ErrUnexpectedBreakGlassReason},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &acknowledgementRepositorySpy{}
			service := NewService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
			request := validAcknowledgementRequest()
			testCase.edit(&request)
			if _, err := service.Acknowledge(context.Background(), request); err != testCase.want {
				t.Fatalf("acknowledge error: got %v, want %v", err, testCase.want)
			}
			if repository.calls != 0 {
				t.Fatalf("repository calls: got %d, want 0", repository.calls)
			}
		})
	}
}

func TestService_Acknowledge_PassesValidatedRequestAndUTCNow(t *testing.T) {
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.FixedZone("test", 7*60*60))
	repository := &acknowledgementRepositorySpy{result: Acknowledgement{AckID: uuid.New(), ReportID: uuid.New(), VersionNo: 1, Decision: "acked", ShiftStatus: "locked"}}
	service := NewService(repository, governanceClock{value: now})
	request := validAcknowledgementRequest()
	result, err := service.Acknowledge(context.Background(), request)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if result != repository.result {
		t.Fatalf("result: got %+v, want %+v", result, repository.result)
	}
	if repository.request != request {
		t.Fatalf("request passed to repository: got %+v, want %+v", repository.request, request)
	}
	if !repository.now.Equal(now.UTC()) {
		t.Fatalf("time passed to repository: got %v, want %v", repository.now, now.UTC())
	}
}

func validAcknowledgementRequest() AcknowledgeRequest {
	return AcknowledgeRequest{OrgID: uuid.New(), StationID: uuid.New(), ShiftID: uuid.New(), ReportID: uuid.New(), ActorID: uuid.New(), VersionNo: 1, Role: "Station Admin", Decision: "acked"}
}

type acknowledgementRepositorySpy struct {
	calls   int
	request AcknowledgeRequest
	now     time.Time
	result  Acknowledgement
}

func (s *acknowledgementRepositorySpy) Acknowledge(_ context.Context, request AcknowledgeRequest, now time.Time) (Acknowledgement, error) {
	s.calls++
	s.request = request
	s.now = now
	return s.result, nil
}

type governanceClock struct {
	value time.Time
}

func (c governanceClock) Now() time.Time { return c.value }
