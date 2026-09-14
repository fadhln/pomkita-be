package httpapi

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

// ModernAnomalyService is the typed anomaly read boundary.
type ModernAnomalyService interface {
	Anomalies(context.Context, uuid.UUID, *uuid.UUID) ([]appreporting.AnomalyView, error)
}

func registerModernAnomalyRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernAnomalyService) {
	if service == nil {
		return
	}
	router.GET("/anomalies", AuthMiddleware(verifier), modernAnomalyJSONHandler(sessions, service))
	router.GET("/anomalies/export", AuthMiddleware(verifier), modernAnomalyExportHandler(sessions, service))
}

func modernAnomalyJSONHandler(sessions SessionService, service ModernAnomalyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, ok := readModernAnomalies(c, sessions, service)
		if !ok {
			return
		}
		if rows == nil {
			rows = []appreporting.AnomalyView{}
		}
		c.JSON(http.StatusOK, rows)
	}
}

func modernAnomalyExportHandler(sessions SessionService, service ModernAnomalyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, ok := readModernAnomalies(c, sessions, service)
		if !ok {
			return
		}
		c.Header("Content-Disposition", `attachment; filename="anomalies.csv"`)
		c.Header("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(c.Writer)
		_ = writer.Write([]string{"event_id", "station_id", "rule_id", "subject_kind", "subject_id", "event_type", "source_kind", "source_id", "source_version_no", "happened_at"})
		for _, row := range rows {
			version := ""
			if row.SourceVersionNo != nil {
				version = strconv.Itoa(*row.SourceVersionNo)
			}
			_ = writer.Write([]string{row.EventID.String(), row.StationID.String(), row.RuleID.String(), row.SubjectKind, row.SubjectID.String(), row.EventType, row.SourceKind, row.SourceID.String(), version, row.HappenedAt})
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			_ = c.Error(err)
		}
	}
}

func readModernAnomalies(c *gin.Context, sessions SessionService, service ModernAnomalyService) ([]appreporting.AnomalyView, bool) {
	if sessions == nil || service == nil {
		writeError(c, http.StatusInternalServerError, "internal_error")
		return nil, false
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return nil, false
	}
	var stationID *uuid.UUID
	if value := c.Query("station_id"); value != "" {
		parsed, parseErr := uuid.Parse(value)
		if parseErr != nil || parsed == uuid.Nil || !containsUUID(session.StationIDs, parsed) {
			writeError(c, http.StatusForbidden, "station_scope_forbidden")
			return nil, false
		}
		stationID = &parsed
	}
	rows, err := service.Anomalies(c.Request.Context(), session.OrgID, stationID)
	if err != nil {
		_ = c.Error(err)
		return nil, false
	}
	return rows, true
}
