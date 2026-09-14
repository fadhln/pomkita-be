package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	apppolicy "github.com/pomkita/pomkita-be/internal/service/policy"
)

// ModernPolicyReadService is the typed policy history boundary.
type ModernPolicyReadService interface {
	History(context.Context, uuid.UUID, *uuid.UUID) ([]apppolicy.RevisionView, error)
}

func registerModernPolicyReadRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernPolicyReadService) {
	if service == nil {
		return
	}
	router.GET("/policies/history", AuthMiddleware(verifier), modernPolicyHistoryHandler(sessions, service))
}

func modernPolicyHistoryHandler(sessions SessionService, service ModernPolicyReadService) gin.HandlerFunc {
	return func(c *gin.Context) {
		stationID, ok := requestedStation(c, sessions)
		if !ok {
			return
		}
		session, ok := readModernSession(c, sessions, stationID)
		if !ok {
			return
		}
		rows, err := service.History(c.Request.Context(), session.OrgID, &stationID)
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
