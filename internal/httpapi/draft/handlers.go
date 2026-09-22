package draft

import (
	"context"
	"net/http"
	"slices"

	transport "github.com/fadhln/pomkita-be/internal/httpapi/transport"
	appdraft "github.com/fadhln/pomkita-be/internal/service/draft"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// DraftService is the typed draft lease boundary.
type DraftService interface {
	Claim(context.Context, appdraft.ClaimRequest) (appdraft.ClaimResult, error)
}

type ClaimDraftRequest struct {
	StationID uuid.UUID `json:"station_id"`
	ShiftID   uuid.UUID `json:"shift_id"`
	DraftID   uuid.UUID `json:"draft_id"`
}

func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service DraftService) {
	router.POST("/drafts/claim", transport.AuthMiddleware(verifier), transport.RequireCSRF, ClaimDraftHandler(sessions, service))
}

func ClaimDraftHandler(sessions transport.SessionService, service DraftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input ClaimDraftRequest
		if !transport.DecodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.DraftID == uuid.Nil {
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if !slices.Contains(session.StationIDs, input.StationID) {
			transport.WriteError(c, http.StatusForbidden, "station_scope_forbidden")
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
