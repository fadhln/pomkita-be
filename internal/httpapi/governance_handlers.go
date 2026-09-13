package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

// GovernanceService is the database procedure boundary for governance APIs.
type GovernanceService interface {
	AckShift(context.Context, string, uuid.UUID, uuid.UUID, int, string, *string, bool, *string) (appdb.AckShiftResult, error)
	ApproveAmendment(context.Context, string, uuid.UUID, []byte) (appdb.ApproveAmendmentResult, error)
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
		c.JSON(http.StatusOK, result)
	}
}
