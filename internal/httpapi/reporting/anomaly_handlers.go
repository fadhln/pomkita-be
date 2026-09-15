package reportingapi

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	transport "github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

// AnomalyService is the typed anomaly read boundary.
type AnomalyService interface {
	Anomalies(context.Context, uuid.UUID, *uuid.UUID) ([]appreporting.AnomalyView, error)
}

func RegisterAnomalyRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service AnomalyService) {
	if service == nil {
		return
	}
	router.GET("/anomalies", transport.AuthMiddleware(verifier), AnomalyJSONHandler(sessions, service))
	router.GET("/anomalies/export", transport.AuthMiddleware(verifier), AnomalyExportHandler(sessions, service))
}

func AnomalyJSONHandler(sessions transport.SessionService, service AnomalyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, ok := readAnomalies(c, sessions, service)
		if !ok {
			return
		}
		if rows == nil {
			rows = []appreporting.AnomalyView{}
		}
		c.JSON(http.StatusOK, rows)
	}
}

func AnomalyExportHandler(sessions transport.SessionService, service AnomalyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, ok := readAnomalies(c, sessions, service)
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

func readAnomalies(c *gin.Context, sessions transport.SessionService, service AnomalyService) ([]appreporting.AnomalyView, bool) {
	if sessions == nil || service == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
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
		if parseErr != nil || parsed == uuid.Nil || !transport.ContainsUUID(session.StationIDs, parsed) {
			transport.WriteError(c, http.StatusForbidden, "station_scope_forbidden")
			return nil, false
		}
		stationID = &parsed
	}
	if stationID == nil && !transport.ContainsString(session.Roles, "Owner") && !transport.ContainsString(session.Roles, "Superadmin") {
		transport.WriteError(c, http.StatusForbidden, "station_scope_forbidden")
		return nil, false
	}
	rows, err := service.Anomalies(c.Request.Context(), session.OrgID, stationID)
	if err != nil {
		_ = c.Error(err)
		return nil, false
	}
	return rows, true
}
