package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

func TestRouterDependenciesIncludeTypedReportingService(t *testing.T) {
	service := typedReportingMarker{}
	dependencies := composeRouterDependencies(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &service)
	if dependencies.Reports == nil {
		t.Fatal("typed reporting service is not wired")
	}
}

func TestCleanMigrationsDirectory_AcceptsRootAndCleanPaths(t *testing.T) {
	if got := cleanMigrationsDirectory("migrations"); got != filepath.Join("migrations", "clean") {
		t.Fatalf("root path: got %q", got)
	}
	if got := cleanMigrationsDirectory(filepath.Join("migrations", "clean")); got != filepath.Join("migrations", "clean") {
		t.Fatalf("clean path: got %q", got)
	}
}

type typedReportingMarker struct{}

func (typedReportingMarker) ReadReport(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appreporting.ReportView, error) {
	return appreporting.ReportView{}, nil
}

var _ httpapi.ModernReportingService = (*typedReportingMarker)(nil)
