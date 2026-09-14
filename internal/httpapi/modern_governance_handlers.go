package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

// ModernGovernanceService is the typed acknowledgement boundary.
type ModernGovernanceService interface {
	Acknowledge(context.Context, appgovernance.AcknowledgeRequest) (appgovernance.Acknowledgement, error)
}

type modernAcknowledgeRequest struct {
	StationID        uuid.UUID `json:"station_id"`
	ShiftID          uuid.UUID `json:"shift_id"`
	VersionNo        int       `json:"version_no"`
	Decision         string    `json:"decision"`
	RejectionReason  string    `json:"rejection_reason"`
	IsBreakGlass     bool      `json:"is_break_glass"`
	BreakGlassReason string    `json:"break_glass_reason"`
}

func registerModernGovernanceRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernGovernanceService) {
	router.POST("/reports/:id/acknowledgement", AuthMiddleware(verifier), requireCSRF, modernAcknowledgeHandler(sessions, service))
}

func modernAcknowledgeHandler(sessions SessionService, service ModernGovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		reportID, err := uuid.Parse(c.Param("id"))
		if err != nil || reportID == uuid.Nil {
			validationError(c)
			return
		}
		var input modernAcknowledgeRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.VersionNo < 1 {
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
			if candidate == "Station Admin" || candidate == "Owner" || candidate == "Superadmin" {
				role = candidate
				break
			}
		}
		result, err := service.Acknowledge(c.Request.Context(), appgovernance.AcknowledgeRequest{
			OrgID: session.OrgID, StationID: input.StationID, ShiftID: input.ShiftID, ReportID: reportID,
			ActorID: session.UserID, VersionNo: input.VersionNo, Role: role, Decision: input.Decision,
			RejectionReason: input.RejectionReason, IsBreakGlass: input.IsBreakGlass, BreakGlassReason: input.BreakGlassReason,
		})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
