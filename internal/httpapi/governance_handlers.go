package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

// GovernanceService is the database procedure boundary for governance APIs.
type GovernanceService interface {
	AckShift(context.Context, string, uuid.UUID, uuid.UUID, int, string, *string, bool, *string) (appdb.AckShiftResult, error)
	ApproveAmendment(context.Context, string, uuid.UUID, []byte) (appdb.ApproveAmendmentResult, error)
	RequestAmendment(context.Context, string, uuid.UUID, uuid.UUID, string, []appdb.AmendmentItem) (appdb.RequestAmendmentResult, error)
	RejectAmendment(context.Context, string, uuid.UUID, string) (appdb.RejectAmendmentResult, error)
	ReadAmendmentQueue(context.Context, string) ([]appdb.AmendmentQueueEntry, error)
	ReadGovernanceAnomalies(context.Context, string) ([]json.RawMessage, error)
}

type ackShiftRequest struct {
	ShiftID          uuid.UUID `json:"shift_id"`
	ReportID         uuid.UUID `json:"report_id"`
	VersionNo        int       `json:"version_no"`
	Decision         string    `json:"decision"`
	RejectionReason  *string   `json:"rejection_reason"`
	IsBreakGlass     bool      `json:"is_break_glass"`
	BreakGlassReason *string   `json:"break_glass_reason"`
}

type approveAmendmentRequest struct {
	AmendmentID    uuid.UUID `json:"amendment_id"`
	StaleCheckHash string    `json:"stale_check_hash"`
}

type requestAmendmentRequest struct {
	ShiftID      uuid.UUID             `json:"shift_id"`
	BaseReportID uuid.UUID             `json:"base_report_id"`
	Reason       string                `json:"reason"`
	Items        []appdb.AmendmentItem `json:"items"`
}

type rejectAmendmentRequest struct {
	AmendmentID     uuid.UUID `json:"amendment_id"`
	RejectionReason string    `json:"rejection_reason"`
}

func ackShiftHandler(service GovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input ackShiftRequest
		if !decodeRequest(c, &input) || input.ShiftID == uuid.Nil || input.ReportID == uuid.Nil || input.VersionNo < 1 || (input.Decision != "acked" && input.Decision != "rejected") {
			return
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.AckShift(c, c.GetString("raw_token"), input.ShiftID, input.ReportID, input.VersionNo, input.Decision, input.RejectionReason, input.IsBreakGlass, input.BreakGlassReason)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func approveAmendmentHandler(service GovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input approveAmendmentRequest
		if !decodeRequest(c, &input) || input.AmendmentID == uuid.Nil || len(input.StaleCheckHash) != 64 {
			return
		}
		hash, err := hex.DecodeString(input.StaleCheckHash)
		if err != nil || len(hash) != 32 {
			validationError(c)
			return
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.ApproveAmendment(c, c.GetString("raw_token"), input.AmendmentID, hash)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func requestAmendmentHandler(service GovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input requestAmendmentRequest
		if !decodeRequest(c, &input) || input.ShiftID == uuid.Nil || input.BaseReportID == uuid.Nil || strings.TrimSpace(input.Reason) == "" || len(input.Items) == 0 {
			return
		}
		for _, item := range input.Items {
			if item.TargetLogicalID == uuid.Nil || strings.TrimSpace(item.TargetKind) == "" || strings.TrimSpace(item.Field) == "" || len(item.OldValue) == 0 || len(item.NewValue) == 0 {
				validationError(c)
				return
			}
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.RequestAmendment(c, c.GetString("raw_token"), input.ShiftID, input.BaseReportID, input.Reason, input.Items)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func rejectAmendmentHandler(service GovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input rejectAmendmentRequest
		if !decodeRequest(c, &input) || input.AmendmentID == uuid.Nil || strings.TrimSpace(input.RejectionReason) == "" {
			return
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.RejectAmendment(c, c.GetString("raw_token"), input.AmendmentID, input.RejectionReason)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func readAmendmentQueueHandler(service GovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.ReadAmendmentQueue(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if result == nil {
			result = []appdb.AmendmentQueueEntry{}
		}
		c.JSON(http.StatusOK, result)
	}
}

func readGovernanceAnomaliesHandler(service GovernanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.ReadGovernanceAnomalies(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if result == nil {
			result = []json.RawMessage{}
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			_ = c.Error(marshalErr)
			return
		}
		if normalized, normalizeErr := normalizeTimestamps(payload); normalizeErr == nil {
			c.Data(http.StatusOK, "application/json; charset=utf-8", normalized)
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	}
}
