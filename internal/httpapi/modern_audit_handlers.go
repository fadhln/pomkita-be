package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

// ModernAuditService is the typed audit export boundary.
type ModernAuditService interface {
	ExportAudit(context.Context, uuid.UUID) ([]appreporting.AuditRow, error)
}

// ModernAuditVerificationService is the typed audit-chain verification boundary.
type ModernAuditVerificationService interface {
	Verify(context.Context, uuid.UUID) error
}

func registerModernAuditRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernAuditService) {
	router.GET("/audit/export", AuthMiddleware(verifier), modernAuditExportHandler(sessions, service))
}

func registerModernAuditVerifyRoutes(router gin.IRoutes, verifier TokenVerifier, sessions SessionService, service ModernAuditVerificationService) {
	router.GET("/audit/verify", AuthMiddleware(verifier), modernAuditVerifyHandler(sessions, service))
}

func modernAuditVerifyHandler(sessions SessionService, service ModernAuditVerificationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if err := service.Verify(c.Request.Context(), session.OrgID); err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"verified": true})
	}
}

func modernAuditExportHandler(sessions SessionService, service ModernAuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		rows, err := service.ExportAudit(c.Request.Context(), session.OrgID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.Header("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(c.Writer)
		if err := writer.Write([]string{"event_id", "org_sequence", "event_type", "payload", "outcome", "created_at", "prev_hash", "row_hash"}); err != nil {
			_ = c.Error(err)
			return
		}
		for _, row := range rows {
			if err := writer.Write([]string{row.EventID.String(), strconv.FormatInt(row.OrgSequence, 10), row.EventType, string(row.Payload), row.Outcome, row.CreatedAt, hex.EncodeToString(row.PrevHash), hex.EncodeToString(row.RowHash)}); err != nil {
				_ = c.Error(err)
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			_ = c.Error(err)
		}
	}
}
