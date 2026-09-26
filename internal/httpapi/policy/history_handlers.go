package policy

import (
	"context"
	"net/http"

	transport "github.com/fadhln/pomkita-be/internal/httpapi/transport"
	apppolicy "github.com/fadhln/pomkita-be/internal/service/policy"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PolicyReadService is the typed policy history boundary.
type PolicyReadService interface {
	History(context.Context, uuid.UUID, *uuid.UUID) ([]apppolicy.RevisionView, error)
}

func RegisterHistoryRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service PolicyReadService) {
	if service == nil {
		return
	}
	router.GET("/policies/history", transport.AuthMiddleware(verifier), PolicyHistoryHandler(sessions, service))
}

func PolicyHistoryHandler(sessions transport.SessionService, service PolicyReadService) gin.HandlerFunc {
	return func(c *gin.Context) {
		stationID, ok := requestedStation(c, sessions)
		if !ok {
			return
		}
		session, ok := transport.ReadSession(c, sessions, stationID)
		if !ok {
			return
		}
		rows, err := service.History(c.Request.Context(), transport.ActiveOrgID(session), &stationID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		if rows == nil {
			rows = []apppolicy.RevisionView{}
		}
		c.JSON(http.StatusOK, rows)
	}
}

func requestedStation(c *gin.Context, sessions transport.SessionService) (uuid.UUID, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return uuid.Nil, false
	}
	if value := c.Query("station_id"); value != "" {
		stationID, err := uuid.Parse(value)
		if err != nil || stationID == uuid.Nil {
			transport.ValidationError(c)
			return uuid.Nil, false
		}
		return stationID, true
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return uuid.Nil, false
	}
	if session.ActiveContext != nil {
		return session.ActiveContext.StationID, true
	}
	if len(session.StationIDs) != 1 {
		transport.ValidationError(c)
		return uuid.Nil, false
	}
	return session.StationIDs[0], true
}
