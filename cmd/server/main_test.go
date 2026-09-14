package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

func TestRouterDependenciesIncludeTypedReportingService(t *testing.T) {
	service := typedReportingMarker{}
	dependencies := composeRouterDependencies(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &service)
	if dependencies.Reports == nil {
		t.Fatal("typed reporting service is not wired")
	}
}

type typedReportingMarker struct{}

func (typedReportingMarker) ReadReport(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appreporting.ReportView, error) {
	return appreporting.ReportView{}, nil
}

var _ httpapi.ModernReportingService = (*typedReportingMarker)(nil)
