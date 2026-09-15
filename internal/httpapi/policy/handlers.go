package policyapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	transport "github.com/pomkita/pomkita-be/internal/httpapi/transport"
	apppolicy "github.com/pomkita/pomkita-be/internal/service/policy"
)

// PolicyService is the typed policy revision boundary.
type PolicyService interface {
	CreateRevision(context.Context, apppolicy.PolicyRevisionRequest) (apppolicy.PolicyRevision, error)
}

type PolicyRevisionRequest struct {
	PolicyKind              string     `json:"policy_kind"`
	PolicyID                uuid.UUID  `json:"policy_id"`
	StationID               *uuid.UUID `json:"station_id"`
	ValidFrom               string     `json:"valid_from"`
	SupersedesRevisionID    *uuid.UUID `json:"supersedes_revision_id"`
	Disabled                bool       `json:"disabled"`
	TombstoneReason         string     `json:"tombstone_reason"`
	LossLiterThreshold      string     `json:"loss_liter_threshold"`
	GainLiterThreshold      string     `json:"gain_liter_threshold"`
	LossRupiahThreshold     string     `json:"loss_rupiah_threshold"`
	GainRupiahThreshold     string     `json:"gain_rupiah_threshold"`
	VarianceRupiahThreshold string     `json:"variance_rupiah_threshold"`
	RolloverThreshold       string     `json:"rollover_threshold"`
	EvidenceMode            string     `json:"evidence_mode"`
}

func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service PolicyService) {
	router.POST("/policies/revisions", transport.AuthMiddleware(verifier), transport.RequireCSRF, PolicyRevisionHandler(sessions, service))
}

func PolicyRevisionHandler(sessions transport.SessionService, service PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input PolicyRevisionRequest
		if !transport.DecodeRequest(c, &input) || input.PolicyID == uuid.Nil || input.PolicyKind == "" || input.ValidFrom == "" {
			return
		}
		validFrom, err := time.Parse(time.RFC3339Nano, input.ValidFrom)
		if err != nil {
			transport.ValidationError(c)
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		role := ""
		for _, candidate := range session.Roles {
			if candidate == "Owner" || candidate == "Superadmin" {
				role = candidate
				break
			}
		}
		if input.StationID != nil && !transport.ContainsUUID(session.StationIDs, *input.StationID) && role != "Superadmin" {
			transport.WriteError(c, http.StatusForbidden, "station_scope_forbidden")
			return
		}
		result, err := service.CreateRevision(c.Request.Context(), apppolicy.PolicyRevisionRequest{
			OrgID: session.OrgID, StationID: optionalRequestUUID(input.StationID), ActorID: session.UserID, Role: role,
			PolicyKind: input.PolicyKind, PolicyID: input.PolicyID, SupersedesRevisionID: input.SupersedesRevisionID,
			ValidFrom: validFrom, Disabled: input.Disabled, TombstoneReason: input.TombstoneReason,
			LossLiterThreshold: input.LossLiterThreshold, GainLiterThreshold: input.GainLiterThreshold,
			LossRupiahThreshold: input.LossRupiahThreshold, GainRupiahThreshold: input.GainRupiahThreshold,
			VarianceRupiahThreshold: input.VarianceRupiahThreshold, RolloverThreshold: input.RolloverThreshold,
			EvidenceMode: input.EvidenceMode,
		})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func optionalRequestUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}
