package shift

import (
	"context"
	"net/http"

	transport "github.com/fadhln/pomkita-be/internal/httpapi/transport"
	appshift "github.com/fadhln/pomkita-be/internal/service/shift"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ShiftReadService is the typed shift read boundary.
type ShiftReadService interface {
	List(context.Context, uuid.UUID, *uuid.UUID) ([]appshift.Summary, error)
	Detail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appshift.Detail, error)
}

func RegisterReadRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service ShiftReadService) {
	if service == nil {
		return
	}
	router.GET("/shifts", transport.AuthMiddleware(verifier), ShiftListHandler(sessions, service))
	router.GET("/shifts/:id", transport.AuthMiddleware(verifier), ShiftDetailHandler(sessions, service))
}

func ShiftListHandler(sessions transport.SessionService, service ShiftReadService) gin.HandlerFunc {
	return func(c *gin.Context) {
		stationID, ok := requestedStation(c, sessions)
		if !ok {
			return
		}
		session, ok := transport.ReadSession(c, sessions, stationID)
		if !ok {
			return
		}
		rows, err := service.List(c.Request.Context(), transport.ActiveOrgID(session), &stationID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		if rows == nil {
			rows = []appshift.Summary{}
		}
		c.JSON(http.StatusOK, rows)
	}
}

func ShiftDetailHandler(sessions transport.SessionService, service ShiftReadService) gin.HandlerFunc {
	return func(c *gin.Context) {
		shiftID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		stationID, ok := requestedStation(c, sessions)
		if !ok {
			return
		}
		session, ok := transport.ReadSession(c, sessions, stationID)
		if !ok {
			return
		}
		result, err := service.Detail(c.Request.Context(), transport.ActiveOrgID(session), stationID, shiftID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
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
