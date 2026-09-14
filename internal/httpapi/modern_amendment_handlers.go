package httpapi

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

// ModernAmendmentService is the typed amendment governance boundary.
type ModernAmendmentService interface {
	Request(context.Context, appgovernance.AmendmentRequest) (appgovernance.Amendment, error)
	Approve(context.Context, appgovernance.ApproveAmendmentRequest) (appgovernance.Amendment, error)
	Reject(context.Context, appgovernance.RejectAmendmentRequest) error
}

type modernAmendmentItem struct {
	TargetKind      string    `json:"target_kind"`
	TargetLogicalID uuid.UUID `json:"target_logical_id"`
	Field           string    `json:"field"`
	OldValue        string    `json:"old_value"`
	NewValue        string    `json:"new_value"`
}

type modernAmendmentRequest struct {
	StationID        uuid.UUID             `json:"station_id"`
	ShiftID          uuid.UUID             `json:"shift_id"`
	BaseReportID     uuid.UUID             `json:"base_report_id"`
	Reason           string                `json:"reason"`
	StaleCheckHash   string                `json:"stale_check_hash"`
	IsBreakGlass     bool                  `json:"is_break_glass"`
	BreakGlassReason string                `json:"break_glass_reason"`
	Items            []modernAmendmentItem `json:"items"`
}

type modernAmendmentDecisionRequest struct {
	StationID       uuid.UUID `json:"station_id"`
	StaleCheckHash  string    `json:"stale_check_hash"`
	RejectionReason string    `json:"rejection_reason"`
}

func registerModernAmendmentRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernAmendmentService) {
	if service == nil {
		return
	}
	router.POST("/amendments", AuthMiddleware(verifier), requireCSRF, modernAmendmentRequestHandler(sessions, service))
	router.POST("/amendments/:id/approve", AuthMiddleware(verifier), requireCSRF, modernAmendmentApproveHandler(sessions, service))
	router.POST("/amendments/:id/reject", AuthMiddleware(verifier), requireCSRF, modernAmendmentRejectHandler(sessions, service))
}

func modernAmendmentRequestHandler(sessions SessionService, service ModernAmendmentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input modernAmendmentRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.ShiftID == uuid.Nil || input.BaseReportID == uuid.Nil || strings.TrimSpace(input.Reason) == "" || len(input.Items) == 0 {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		staleHash, err := decodeHash(input.StaleCheckHash)
		if err != nil {
			validationError(c)
			return
		}
		items := make([]appgovernance.AmendmentItem, 0, len(input.Items))
		for _, item := range input.Items {
			if item.TargetLogicalID == uuid.Nil || item.TargetKind == "" || item.Field == "" {
				validationError(c)
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

func modernAmendmentApproveHandler(sessions SessionService, service ModernAmendmentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		amendmentID, ok := pathUUID(c, "id")
		if !ok {
			return
		}
		var input modernAmendmentDecisionRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		staleHash, err := decodeHash(input.StaleCheckHash)
		if err != nil {
			validationError(c)
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

func modernAmendmentRejectHandler(sessions SessionService, service ModernAmendmentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		amendmentID, ok := pathUUID(c, "id")
		if !ok {
			return
		}
		var input modernAmendmentDecisionRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || strings.TrimSpace(input.RejectionReason) == "" {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
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

func readModernSession(c *gin.Context, sessions SessionService, stationID uuid.UUID) (SessionView, bool) {
	if sessions == nil {
		writeError(c, http.StatusInternalServerError, "internal_error")
		return SessionView{}, false
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return SessionView{}, false
	}
	if !containsUUID(session.StationIDs, stationID) && !containsString(session.Roles, "Owner") && !containsString(session.Roles, "Superadmin") {
		writeError(c, http.StatusForbidden, "station_scope_forbidden")
		return SessionView{}, false
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
