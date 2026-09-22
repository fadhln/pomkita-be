package reporting

import (
	"context"
	"net/http"
	"slices"

	transport "github.com/fadhln/pomkita-be/internal/httpapi/transport"
	appreporting "github.com/fadhln/pomkita-be/internal/service/reporting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ReportingService is the typed report read boundary.
type ReportingService interface {
	ReadReport(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appreporting.ReportView, error)
}

func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service ReportingService) {
	router.GET("/reports/:id", transport.AuthMiddleware(verifier), ReportHandler(sessions, service))
	router.GET("/reports/:id/printout", transport.AuthMiddleware(verifier), ReportHandler(sessions, service))
}

func ReportHandler(sessions transport.SessionService, service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		reportID, err := uuid.Parse(c.Param("id"))
		if err != nil || reportID == uuid.Nil {
			transport.ValidationError(c)
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
				transport.ValidationError(c)
				return
			}
		} else if len(session.StationIDs) == 1 {
			stationID = session.StationIDs[0]
		}
		if stationID == uuid.Nil || !slices.Contains(session.StationIDs, stationID) {
			transport.ValidationError(c)
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
