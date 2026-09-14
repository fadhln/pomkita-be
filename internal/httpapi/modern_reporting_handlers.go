package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

// ModernReportingService is the typed report read boundary.
type ModernReportingService interface {
	ReadReport(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appreporting.ReportView, error)
}

func registerModernReportingRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernReportingService) {
	router.GET("/reports/:id", AuthMiddleware(verifier), modernReportHandler(sessions, service))
	router.GET("/reports/:id/printout", AuthMiddleware(verifier), modernReportHandler(sessions, service))
}

func modernReportHandler(sessions SessionService, service ModernReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		reportID, err := uuid.Parse(c.Param("id"))
		if err != nil || reportID == uuid.Nil {
			validationError(c)
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		stationID := uuid.Nil
		if value := c.Query("station_id"); value != "" {
			stationID, err = uuid.Parse(value)
			if err != nil {
				validationError(c)
				return
			}
		} else if len(session.StationIDs) == 1 {
			stationID = session.StationIDs[0]
		}
		if stationID == uuid.Nil || !containsUUID(session.StationIDs, stationID) {
			validationError(c)
			return
		}
		view, err := service.ReadReport(c.Request.Context(), session.OrgID, stationID, reportID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, view)
	}
}
