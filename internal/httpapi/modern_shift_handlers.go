package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
)

// ModernShiftService is the typed shift write boundary.
type ModernShiftService interface {
	OpenShift(context.Context, appshift.OpenRequest) (appshift.Shift, error)
}

type modernOpenShiftRequest struct {
	StationID         uuid.UUID  `json:"station_id"`
	OpenedAt          string     `json:"opened_at"`
	Backfilled        bool       `json:"backfilled"`
	OriginalEventDate string     `json:"original_event_date"`
	ShiftKE           int        `json:"shift_ke"`
	BackfillApprover  *uuid.UUID `json:"backfill_approver"`
	BackfillReason    string     `json:"backfill_reason"`
}

func registerModernShiftRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernShiftService) {
	router.POST("/shifts", AuthMiddleware(verifier), requireCSRF, modernOpenShiftHandler(sessions, service))
}

func modernOpenShiftHandler(sessions SessionService, service ModernShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input modernOpenShiftRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.OpenedAt == "" {
			return
		}
		openedAt, err := time.Parse(time.RFC3339Nano, input.OpenedAt)
		if err != nil {
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
		role := ""
		for _, candidate := range session.Roles {
			if candidate == "Supervisor" {
				role = candidate
				break
			}
		}
		request := appshift.OpenRequest{
			OrgID: session.OrgID, StationID: input.StationID, ActorID: session.UserID, Role: role,
			OpenedAt: openedAt, Backfilled: input.Backfilled, OriginalEventDate: input.OriginalEventDate,
			ShiftKE: input.ShiftKE, BackfillReason: input.BackfillReason,
		}
		if input.BackfillApprover != nil {
			request.BackfillApprover = *input.BackfillApprover
		}
		result, err := service.OpenShift(c.Request.Context(), request)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
