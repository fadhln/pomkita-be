package governanceapi

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	transport "github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

// AmendmentService is the typed amendment governance boundary.
type AmendmentService interface {
	Request(context.Context, appgovernance.AmendmentRequest) (appgovernance.Amendment, error)
	Approve(context.Context, appgovernance.ApproveAmendmentRequest) (appgovernance.Amendment, error)
	Reject(context.Context, appgovernance.RejectAmendmentRequest) error
}

type AmendmentItem struct {
	TargetKind      string    `json:"target_kind"`
	TargetLogicalID uuid.UUID `json:"target_logical_id"`
	Field           string    `json:"field"`
	OldValue        string    `json:"old_value"`
	NewValue        string    `json:"new_value"`
}

type AmendmentRequest struct {
	StationID        uuid.UUID       `json:"station_id"`
	ShiftID          uuid.UUID       `json:"shift_id"`
	BaseReportID     uuid.UUID       `json:"base_report_id"`
	Reason           string          `json:"reason"`
	StaleCheckHash   string          `json:"stale_check_hash"`
	IsBreakGlass     bool            `json:"is_break_glass"`
	BreakGlassReason string          `json:"break_glass_reason"`
	Items            []AmendmentItem `json:"items"`
}

type AmendmentDecisionRequest struct {
	StationID       uuid.UUID `json:"station_id"`
	StaleCheckHash  string    `json:"stale_check_hash"`
	RejectionReason string    `json:"rejection_reason"`
}

func RegisterAmendmentRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service AmendmentService) {
	if service == nil {
		return
	}
	router.POST("/amendments", transport.AuthMiddleware(verifier), transport.RequireCSRF, AmendmentRequestHandler(sessions, service))
	router.POST("/amendments/:id/approve", transport.AuthMiddleware(verifier), transport.RequireCSRF, AmendmentApproveHandler(sessions, service))
	router.POST("/amendments/:id/reject", transport.AuthMiddleware(verifier), transport.RequireCSRF, AmendmentRejectHandler(sessions, service))
}

func AmendmentRequestHandler(sessions transport.SessionService, service AmendmentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input AmendmentRequest
		if !transport.DecodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.BaseReportID == uuid.Nil || strings.TrimSpace(input.Reason) == "" || len(input.Items) == 0 {
			return
		}
		session, ok := readSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		staleHash, err := decodeHash(input.StaleCheckHash)
		if err != nil {
			transport.ValidationError(c)
			return
		}
		items := make([]appgovernance.AmendmentItem, 0, len(input.Items))
		for _, item := range input.Items {
			if item.TargetLogicalID == uuid.Nil || item.TargetKind == "" || item.Field == "" {
				transport.ValidationError(c)
				return
			}
			items = append(items, appgovernance.AmendmentItem{TargetKind: item.TargetKind, TargetLogicalID: item.TargetLogicalID, Field: item.Field, OldValue: []byte(item.OldValue), NewValue: []byte(item.NewValue)})
		}
		role := firstRole(session.Roles, "Supervisor")
		result, err := service.Request(c.Request.Context(), appgovernance.AmendmentRequest{OrgID: session.OrgID, StationID: input.StationID, ShiftID: input.ShiftID, BaseReportID: input.BaseReportID, RequesterID: session.UserID, Role: role, Reason: input.Reason, StaleCheckHash: staleHash, IsBreakGlass: input.IsBreakGlass, BreakGlassReason: input.BreakGlassReason, Items: items})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func AmendmentApproveHandler(sessions transport.SessionService, service AmendmentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		amendmentID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		var input AmendmentDecisionRequest
		if !transport.DecodeRequest(c, &input) || input.StationID == uuid.Nil {
			return
		}
		session, ok := readSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		staleHash, err := decodeHash(input.StaleCheckHash)
		if err != nil {
			transport.ValidationError(c)
			return
		}
		role := firstRole(session.Roles, "Station Admin", "Owner", "Superadmin")
		result, err := service.Approve(c.Request.Context(), appgovernance.ApproveAmendmentRequest{OrgID: session.OrgID, StationID: input.StationID, AmendmentID: amendmentID, ApproverID: session.UserID, Role: role, StaleCheckHash: staleHash})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func AmendmentRejectHandler(sessions transport.SessionService, service AmendmentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		amendmentID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		var input AmendmentDecisionRequest
		if !transport.DecodeRequest(c, &input) || input.StationID == uuid.Nil || strings.TrimSpace(input.RejectionReason) == "" {
			return
		}
		session, ok := readSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		role := firstRole(session.Roles, "Station Admin", "Owner", "Superadmin")
		if err := service.Reject(c.Request.Context(), appgovernance.RejectAmendmentRequest{OrgID: session.OrgID, StationID: input.StationID, AmendmentID: amendmentID, ApproverID: session.UserID, Role: role, RejectionReason: input.RejectionReason}); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func readSession(c *gin.Context, sessions transport.SessionService, stationID uuid.UUID) (transport.SessionView, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return transport.SessionView{}, false
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return transport.SessionView{}, false
	}
	if !transport.ContainsUUID(session.StationIDs, stationID) && !containsString(session.Roles, "Owner") && !containsString(session.Roles, "Superadmin") {
		transport.WriteError(c, http.StatusForbidden, "station_scope_forbidden")
		return transport.SessionView{}, false
	}
	return session, true
}

func decodeHash(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != 32 {
		return nil, err
	}
	return decoded, nil
}

func firstRole(roles []string, allowed ...string) string {
	for _, candidate := range allowed {
		if containsString(roles, candidate) {
			return candidate
		}
	}
	return ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
