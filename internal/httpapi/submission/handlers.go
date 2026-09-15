package submission

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	transport "github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

// SubmissionService is the typed report submission boundary.
type SubmissionService interface {
	Submit(context.Context, appsubmission.Request) (appsubmission.Result, error)
}

type SubmitRequest struct {
	StationID  uuid.UUID       `json:"station_id"`
	ShiftID    uuid.UUID       `json:"shift_id"`
	DraftID    uuid.UUID       `json:"draft_id"`
	ClaimToken uuid.UUID       `json:"claim_token"`
	Revision   int             `json:"revision"`
	Payload    json.RawMessage `json:"payload"`
}

func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service SubmissionService) {
	router.POST("/submissions", transport.AuthMiddleware(verifier), transport.RequireCSRF, SubmitHandler(sessions, service))
}

func SubmitHandler(sessions transport.SessionService, service SubmissionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input SubmitRequest
		if !transport.DecodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 1 || len(input.Payload) == 0 {
			return
		}
		idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if idempotencyKey == "" {
			transport.ValidationError(c)
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if !transport.ContainsUUID(session.StationIDs, input.StationID) {
			transport.WriteError(c, http.StatusForbidden, "station_scope_forbidden")
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
