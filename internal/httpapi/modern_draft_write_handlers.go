package httpapi

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appdraft "github.com/pomkita/pomkita-be/internal/service/draft"
)

// ModernDraftWriteService is the typed revision-fenced draft write boundary.
type ModernDraftWriteService interface {
	Heartbeat(context.Context, appdraft.HeartbeatRequest) error
	WriteReading(context.Context, appdraft.WriteReadingRequest) (int, error)
	WriteSales(context.Context, appdraft.WriteSalesRequest) (int, error)
	WriteLoss(context.Context, appdraft.WriteLossRequest) (int, error)
	StageEvidence(context.Context, appdraft.StageEvidenceRequest) (int, error)
}

type modernDraftLeaseRequest struct {
	StationID  uuid.UUID `json:"station_id"`
	DraftID    uuid.UUID `json:"draft_id"`
	ClaimToken uuid.UUID `json:"claim_token"`
}

type modernDraftReadingRequest struct {
	modernDraftLeaseRequest
	Revision   int       `json:"revision"`
	NozzleID   uuid.UUID `json:"nozzle_id"`
	MeterStart string    `json:"meter_start"`
	MeterEnd   string    `json:"meter_end"`
}

type modernDraftSalesRequest struct {
	modernDraftLeaseRequest
	Revision       int       `json:"revision"`
	DispenserID    uuid.UUID `json:"dispenser_id"`
	CashAmount     string    `json:"cash_amount"`
	CashlessAmount string    `json:"cashless_amount"`
}

type modernDraftLossRequest struct {
	modernDraftLeaseRequest
	Revision   int       `json:"revision"`
	LossID     uuid.UUID `json:"loss_id"`
	Direction  string    `json:"direction"`
	ReasonCode string    `json:"reason_code"`
	Liters     string    `json:"liters"`
	CashAmount string    `json:"cash_amount"`
	Note       string    `json:"note"`
}

type modernDraftEvidenceRequest struct {
	modernDraftLeaseRequest
	Revision     int       `json:"revision"`
	LossRowID    uuid.UUID `json:"loss_row_id"`
	EvidenceType string    `json:"evidence_type"`
	ObjectKey    string    `json:"object_key"`
	ContentHash  string    `json:"content_hash"`
	SizeBytes    int64     `json:"size_bytes"`
	MIME         string    `json:"mime"`
}

func registerModernDraftWriteRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernDraftWriteService) {
	if service == nil {
		return
	}
	protectedWrite := []gin.HandlerFunc{AuthMiddleware(verifier), requireCSRF}
	router.POST("/drafts/heartbeat", append(protectedWrite, modernDraftHeartbeatHandler(sessions, service))...)
	router.POST("/drafts/readings", append(protectedWrite, modernDraftReadingHandler(sessions, service))...)
	router.POST("/drafts/sales", append(protectedWrite, modernDraftSalesHandler(sessions, service))...)
	router.POST("/drafts/losses", append(protectedWrite, modernDraftLossHandler(sessions, service))...)
	router.POST("/drafts/evidence", append(protectedWrite, modernDraftEvidenceHandler(sessions, service))...)
}

func modernDraftHeartbeatHandler(sessions SessionService, service ModernDraftWriteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input modernDraftLeaseRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		if err := service.Heartbeat(c.Request.Context(), appdraft.HeartbeatRequest{DraftID: input.DraftID, ClaimToken: input.ClaimToken, ActorID: session.UserID}); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func modernDraftReadingHandler(sessions SessionService, service ModernDraftWriteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input modernDraftReadingRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 1 || input.NozzleID == uuid.Nil || input.MeterStart == "" || input.MeterEnd == "" {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		next, err := service.WriteReading(c.Request.Context(), appdraft.WriteReadingRequest{DraftID: input.DraftID, ClaimToken: input.ClaimToken, Revision: input.Revision, NozzleID: input.NozzleID, MeterStart: input.MeterStart, MeterEnd: input.MeterEnd, ActorID: session.UserID})
		writeDraftRevisionResult(c, next, err)
	}
}

func modernDraftSalesHandler(sessions SessionService, service ModernDraftWriteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input modernDraftSalesRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 1 || input.DispenserID == uuid.Nil || input.CashAmount == "" || input.CashlessAmount == "" {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		next, err := service.WriteSales(c.Request.Context(), appdraft.WriteSalesRequest{DraftID: input.DraftID, ClaimToken: input.ClaimToken, Revision: input.Revision, DispenserID: input.DispenserID, CashAmount: input.CashAmount, CashlessAmount: input.CashlessAmount, ActorID: session.UserID})
		writeDraftRevisionResult(c, next, err)
	}
}

func modernDraftLossHandler(sessions SessionService, service ModernDraftWriteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input modernDraftLossRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 1 || input.LossID == uuid.Nil || input.Direction == "" || input.ReasonCode == "" || input.Liters == "" {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		next, err := service.WriteLoss(c.Request.Context(), appdraft.WriteLossRequest{DraftID: input.DraftID, ClaimToken: input.ClaimToken, Revision: input.Revision, LossID: input.LossID, Direction: input.Direction, ReasonCode: input.ReasonCode, Liters: input.Liters, CashAmount: input.CashAmount, Note: input.Note, ActorID: session.UserID})
		writeDraftRevisionResult(c, next, err)
	}
}

func modernDraftEvidenceHandler(sessions SessionService, service ModernDraftWriteService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input modernDraftEvidenceRequest
		if !decodeRequest(c, &input) || input.StationID == uuid.Nil || input.DraftID == uuid.Nil || input.ClaimToken == uuid.Nil || input.Revision < 1 || input.LossRowID == uuid.Nil || input.EvidenceType == "" || input.ObjectKey == "" || input.SizeBytes <= 0 || input.MIME == "" {
			return
		}
		session, ok := readModernSession(c, sessions, input.StationID)
		if !ok {
			return
		}
		hash, err := hex.DecodeString(strings.TrimSpace(input.ContentHash))
		if err != nil || len(hash) != 32 {
			validationError(c)
			return
		}
		next, err := service.StageEvidence(c.Request.Context(), appdraft.StageEvidenceRequest{DraftID: input.DraftID, ClaimToken: input.ClaimToken, Revision: input.Revision, LossRowID: input.LossRowID, EvidenceType: input.EvidenceType, ObjectKey: input.ObjectKey, ContentHash: hash, SizeBytes: input.SizeBytes, MIME: input.MIME, ActorID: session.UserID})
		writeDraftRevisionResult(c, next, err)
	}
}

func writeDraftRevisionResult(c *gin.Context, revision int, err error) {
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revision": revision})
}
