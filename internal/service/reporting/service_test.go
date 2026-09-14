package reporting

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestService_ReadReport_RejectsMissingScope(t *testing.T) {
	service := NewService(&reportRepositorySpy{})
	if _, err := service.ReadReport(context.Background(), uuid.Nil, uuid.New(), uuid.New()); err != ErrInvalidRequest {
		t.Fatalf("read report error: got %v, want %v", err, ErrInvalidRequest)
	}
}

type reportRepositorySpy struct{}

func (reportRepositorySpy) ReadReport(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (ReportView, error) {
	return ReportView{}, nil
}

func (reportRepositorySpy) ExportAudit(context.Context, uuid.UUID) ([]AuditRow, error) {
	return nil, nil
}
