package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

// ShiftService is the database procedure boundary for shift API handlers.
type ShiftService interface {
	OpenShift(context.Context, string, uuid.UUID, uuid.UUID, time.Time, bool, *string, *string, *int) (appdb.OpenShiftResult, error)
	ClaimDraft(context.Context, string, uuid.UUID) (appdb.ClaimDraftResult, error)
	HeartbeatDraft(context.Context, string, uuid.UUID, uuid.UUID) (bool, error)
	WriteDraftReading(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string) (int, error)
	WriteDraftSales(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string) (int, error)
	WriteDraftLoss(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string, string, string, string) (int, error)
	StageDraftEvidence(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string, []byte, int64, string) (int, error)
	ReadDraft(context.Context, string, uuid.UUID) (json.RawMessage, error)
	ReadShiftList(context.Context, string, *uuid.UUID) ([]json.RawMessage, error)
	ReadShiftDetail(context.Context, string, uuid.UUID) (json.RawMessage, error)
	ReadReport(context.Context, string, uuid.UUID) (json.RawMessage, error)
	SubmitShift(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, json.RawMessage) (appdb.SubmitShiftResult, error)
}

type openShiftRequest struct {
	StationID         uuid.UUID `json:"station_id"`
	OpenedAt          string    `json:"opened_at"`
	Backfilled        bool      `json:"backfilled"`
	OriginalEventDate *string   `json:"original_event_date"`
	ShiftKE           *int      `json:"shift_ke"`
	BackfillReason    *string   `json:"backfill_reason"`
}

type claimDraftRequest struct {
	ShiftID uuid.UUID `json:"shift_id"`
}
type heartbeatDraftRequest struct {
	DraftID    uuid.UUID `json:"draft_id"`
	ClaimToken uuid.UUID `json:"claim_token"`
}

type draftReadingRequest struct {
	DraftID    uuid.UUID `json:"draft_id"`
	ClaimToken uuid.UUID `json:"claim_token"`
	Revision   int       `json:"revision"`
	NozzleID   uuid.UUID `json:"nozzle_id"`
	MeterStart string    `json:"meter_start"`
	MeterEnd   string    `json:"meter_end"`
}
type draftSalesRequest struct {
	DraftID        uuid.UUID `json:"draft_id"`
	ClaimToken     uuid.UUID `json:"claim_token"`
	Revision       int       `json:"revision"`
	DispenserID    uuid.UUID `json:"dispenser_id"`
	CashAmount     string    `json:"cash_amount"`
	CashlessAmount string    `json:"cashless_amount"`
}
type draftLossRequest struct {
	DraftID    uuid.UUID `json:"draft_id"`
	ClaimToken uuid.UUID `json:"claim_token"`
	Revision   int       `json:"revision"`
	LossID     uuid.UUID `json:"loss_id"`
	Direction  string    `json:"direction"`
	ReasonCode string    `json:"reason_code"`
	Liters     string    `json:"liters"`
	CashAmount string    `json:"cash_amount"`
	Note       string    `json:"note"`
}
type draftEvidenceRequest struct {
	DraftID      uuid.UUID `json:"draft_id"`
	ClaimToken   uuid.UUID `json:"claim_token"`
	Revision     int       `json:"revision"`
	LossRowID    uuid.UUID `json:"loss_row_id"`
	EvidenceType string    `json:"evidence_type"`
	ObjectKey    string    `json:"object_key"`
	ContentHash  string    `json:"content_hash"`
	SizeBytes    int64     `json:"size_bytes"`
	MIME         string    `json:"mime"`
}
type submitReading struct {
	NozzleID   uuid.UUID `json:"nozzle_id"`
	MeterStart string    `json:"meter_start"`
	MeterEnd   string    `json:"meter_end"`
	Observed   *bool     `json:"observed"`
}
type submitSale struct {
	DispenserID    uuid.UUID `json:"dispenser_id"`
	CashAmount     string    `json:"cash_amount"`
	CashlessAmount string    `json:"cashless_amount"`
}
type submitLoss struct {
	LossID     uuid.UUID  `json:"loss_id"`
	NozzleID   *uuid.UUID `json:"nozzle_id"`
	Direction  string     `json:"direction"`
	ReasonCode string     `json:"reason_code"`
	Liters     string     `json:"liters"`
	CashAmount string     `json:"cash_amount"`
	Note       string     `json:"note"`
}
type submitRequest struct {
	ShiftID     uuid.UUID       `json:"shift_id"`
	DraftID     uuid.UUID       `json:"draft_id"`
	ClaimToken  uuid.UUID       `json:"claim_token"`
	Revision    int             `json:"revision"`
	HashVersion int             `json:"hash_version"`
	Readings    []submitReading `json:"readings"`
	Sales       []submitSale    `json:"sales"`
	Losses      []submitLoss    `json:"losses"`
}
type submitPayload struct {
	HashVersion int             `json:"hash_version"`
	Readings    []submitReading `json:"readings"`
	Sales       []submitSale    `json:"sales"`
	Losses      []submitLoss    `json:"losses"`
}

func openShiftHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input openShiftRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.OpenedAt == "" {
			return
		}
		openedAt, err := time.Parse(time.RFC3339Nano, input.OpenedAt)
		if err != nil {
			validationError(c)
			return
		}
		if input.Backfilled != (input.OriginalEventDate != nil && input.ShiftKE != nil && input.BackfillReason != nil) {
			validationError(c)
			return
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		claims := c.MustGet("jwt_claims").(appjwt.Claims)
		result, err := service.OpenShift(c, c.GetString("raw_token"), claims.Subject, input.StationID, openedAt, input.Backfilled, input.OriginalEventDate, input.BackfillReason, input.ShiftKE)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func claimDraftHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input claimDraftRequest
		if !decodeRequest(c, &input) || input.ShiftID == uuid.Nil {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.ClaimDraft(c, c.GetString("raw_token"), input.ShiftID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(200, result)
	}
}
func heartbeatDraftHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input heartbeatDraftRequest
		if !decodeRequest(c, &input) || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.HeartbeatDraft(c, c.GetString("raw_token"), input.DraftID, input.ClaimToken)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(200, gin.H{"renewed": result})
	}
}
func writeDraftReadingHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input draftReadingRequest
		if !decodeRequest(c, &input) || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.NozzleID == uuid.Nil || !validDecimal(input.MeterStart) || !validDecimal(input.MeterEnd) {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.WriteDraftReading(c, c.GetString("raw_token"), input.DraftID, input.ClaimToken, input.NozzleID, input.Revision, input.MeterStart, input.MeterEnd)
		writeRevision(c, result, err, "write draft reading")
	}
}
func writeDraftSalesHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input draftSalesRequest
		if !decodeRequest(c, &input) || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.DispenserID == uuid.Nil || !validDecimal(input.CashAmount) || !validDecimal(input.CashlessAmount) {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.WriteDraftSales(c, c.GetString("raw_token"), input.DraftID, input.ClaimToken, input.DispenserID, input.Revision, input.CashAmount, input.CashlessAmount)
		writeRevision(c, result, err, "write draft sales")
	}
}
func writeDraftLossHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input draftLossRequest
		decoded := decodeRequest(c, &input)
		fmt.Printf("LOSS decoded=%v value=%#v\\n", decoded, input)
		if !decoded || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.LossID == uuid.Nil || !validDecimal(input.Liters) || (input.CashAmount != "" && !validDecimal(input.CashAmount)) {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.WriteDraftLoss(c, c.GetString("raw_token"), input.DraftID, input.ClaimToken, input.LossID, input.Revision, input.Direction, input.ReasonCode, input.Liters, input.CashAmount, input.Note)
		writeRevision(c, result, err, "write draft loss")
	}
}
func stageDraftEvidenceHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input draftEvidenceRequest
		decoded := decodeRequest(c, &input)
		fmt.Printf("EVIDENCE decoded=%v value=%#v\\n", decoded, input)
		if !decoded || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.LossRowID == uuid.Nil || input.SizeBytes <= 0 || input.ContentHash == "" {
			return
		}
		hash, err := hex.DecodeString(input.ContentHash)
		if err != nil || len(hash) != 32 {
			validationError(c)
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.StageDraftEvidence(c, c.GetString("raw_token"), input.DraftID, input.ClaimToken, input.LossRowID, input.Revision, input.EvidenceType, input.ObjectKey, hash, input.SizeBytes, input.MIME)
		writeRevision(c, result, err, "stage draft evidence")
	}
}

func readDraftHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Query("shift_id"))
		if err != nil {
			validationError(c)
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.ReadDraft(c, c.GetString("raw_token"), id)
		writeJSONResult(c, result, err)
	}
}
func readShiftDetailHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathUUID(c, "id")
		if !ok {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.ReadShiftDetail(c, c.GetString("raw_token"), id)
		writeJSONResult(c, result, err)
	}
}
func readReportHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathUUID(c, "id")
		if !ok {
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.ReadReport(c, c.GetString("raw_token"), id)
		writeJSONResult(c, result, err)
	}
}
func readShiftListHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var station *uuid.UUID
		if value := c.Query("station_id"); value != "" {
			id, err := uuid.Parse(value)
			if err != nil {
				validationError(c)
				return
			}
			station = &id
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.ReadShiftList(c, c.GetString("raw_token"), station)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(200, result)
	}
}

func submitShiftHandler(service ShiftService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input submitRequest
		if !decodeRequest(c, &input) || input.ShiftID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 0 || input.HashVersion < 1 {
			return
		}
		key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if key == "" {
			validationError(c)
			return
		}
		if input.Readings == nil {
			input.Readings = []submitReading{}
		}
		if input.Sales == nil {
			input.Sales = []submitSale{}
		}
		if input.Losses == nil {
			input.Losses = []submitLoss{}
		}
		payload, err := json.Marshal(submitPayload{input.HashVersion, input.Readings, input.Sales, input.Losses})
		if err != nil {
			validationError(c)
			return
		}
		if service == nil {
			writeError(c, 500, "internal_error")
			return
		}
		result, err := service.SubmitShift(c, c.GetString("raw_token"), input.ShiftID, input.DraftID, input.ClaimToken, input.Revision, key, payload)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(200, result)
	}
}

func writeRevision(c *gin.Context, revision int, err error, _ string) {
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(200, gin.H{"revision": revision})
}
func writeJSONResult(c *gin.Context, result json.RawMessage, err error) {
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.Data(200, "application/json; charset=utf-8", result)
}
func pathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		validationError(c)
		return uuid.Nil, false
	}
	return id, true
}

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func validDecimal(value string) bool { return decimalPattern.MatchString(value) }
func validationError(c *gin.Context) { writeError(c, http.StatusBadRequest, "validation_error") }
func decodeRequest(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || ensureEndOfJSON(decoder) != nil {
		validationError(c)
		return false
	}
	return true
}
