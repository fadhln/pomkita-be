package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appdraft "github.com/pomkita/pomkita-be/internal/service/draft"
)

// ModernDraftService is the typed draft lease boundary.
type ModernDraftService interface {
	Claim(context.Context, appdraft.ClaimRequest) (appdraft.ClaimResult, error)
}

type modernClaimDraftRequest struct {
	StationID uuid.UUID `json:"station_id"`
	ShiftID   uuid.UUID `json:"shift_id"`
	DraftID   uuid.UUID `json:"draft_id"`
}

func registerModernDraftRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernDraftService) {
	router.POST("/drafts/claim", AuthMiddleware(verifier), requireCSRF, modernClaimDraftHandler(sessions, service))
}

func modernClaimDraftHandler(sessions SessionService, service ModernDraftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input modernClaimDraftRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.DraftID == uuid.Nil {
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if !containsUUID(session.StationIDs, input.StationID) {
			writeError(c, http.StatusForbidden, "station_scope_forbidden")
			return
		}
		result, err := service.Claim(c.Request.Context(), appdraft.ClaimRequest{DraftID: input.DraftID, ShiftID: input.ShiftID, ActorID: session.UserID})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
