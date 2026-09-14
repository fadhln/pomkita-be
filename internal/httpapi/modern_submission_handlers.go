package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

// ModernSubmissionService is the typed report submission boundary.
type ModernSubmissionService interface {
	Submit(context.Context, appsubmission.Request) (appsubmission.Result, error)
}

type modernSubmitRequest struct {
	StationID  uuid.UUID       `json:"station_id"`
	ShiftID    uuid.UUID       `json:"shift_id"`
	DraftID    uuid.UUID       `json:"draft_id"`
	ClaimToken uuid.UUID       `json:"claim_token"`
	Revision   int             `json:"revision"`
	Payload    json.RawMessage `json:"payload"`
}

func registerModernSubmissionRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernSubmissionService) {
	router.POST("/submissions", AuthMiddleware(verifier), requireCSRF, modernSubmitHandler(sessions, service))
}

func modernSubmitHandler(sessions SessionService, service ModernSubmissionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input modernSubmitRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 1 || len(input.Payload) == 0 {
			return
		}
		idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if idempotencyKey == "" {
			validationError(c)
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
		result, err := service.Submit(c.Request.Context(), appsubmission.Request{
			OrgID: session.OrgID, StationID: input.StationID, ShiftID: input.ShiftID, DraftID: input.DraftID,
			ClaimToken: input.ClaimToken, ExpectedRevision: input.Revision, ActorID: session.UserID,
			IdempotencyKey: idempotencyKey, Payload: append([]byte(nil), input.Payload...),
		})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
