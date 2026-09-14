package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
)

// ModernShiftReadService is the typed shift read boundary.
type ModernShiftReadService interface {
	List(context.Context, uuid.UUID, *uuid.UUID) ([]appshift.Summary, error)
	Detail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appshift.Detail, error)
}

func registerModernShiftReadRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernShiftReadService) {
	if service == nil {
		return
	}
	router.GET("/shifts", AuthMiddleware(verifier), modernShiftListHandler(sessions, service))
	router.GET("/shifts/:id", AuthMiddleware(verifier), modernShiftDetailHandler(sessions, service))
}

func modernShiftListHandler(sessions SessionService, service ModernShiftReadService) gin.HandlerFunc {
	return func(c *gin.Context) {
		stationID, ok := requestedStation(c, sessions)
		if !ok {
			return
		}
		session, ok := readModernSession(c, sessions, stationID)
		if !ok {
			return
		}
		rows, err := service.List(c.Request.Context(), session.OrgID, &stationID)
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

func modernShiftDetailHandler(sessions SessionService, service ModernShiftReadService) gin.HandlerFunc {
	return func(c *gin.Context) {
		shiftID, ok := pathUUID(c, "id")
		if !ok {
			return
		}
		stationID, ok := requestedStation(c, sessions)
		if !ok {
			return
		}
		session, ok := readModernSession(c, sessions, stationID)
		if !ok {
			return
		}
		result, err := service.Detail(c.Request.Context(), session.OrgID, stationID, shiftID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func requestedStation(c *gin.Context, sessions SessionService) (uuid.UUID, bool) {
	if sessions == nil {
		writeError(c, http.StatusInternalServerError, "internal_error")
		return uuid.Nil, false
	}
	if value := c.Query("station_id"); value != "" {
		stationID, err := uuid.Parse(value)
		if err != nil || stationID == uuid.Nil {
			validationError(c)
			return uuid.Nil, false
		}
		return stationID, true
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return uuid.Nil, false
	}
	if len(session.StationIDs) != 1 {
		validationError(c)
		return uuid.Nil, false
	}
	return session.StationIDs[0], true
}
