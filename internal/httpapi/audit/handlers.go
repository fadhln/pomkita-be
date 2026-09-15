package audit

import (
	"context"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	transport "github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

// AuditService is the typed audit export boundary.
type AuditService interface {
	ExportAudit(context.Context, uuid.UUID) ([]appreporting.AuditRow, error)
}

// AuditVerificationService is the typed audit-chain verification boundary.
type AuditVerificationService interface {
	Verify(context.Context, uuid.UUID) error
}

func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service AuditService) {
	router.GET("/audit", transport.AuthMiddleware(verifier), AuditTrailHandler(sessions, service))
	router.GET("/audit/export", transport.AuthMiddleware(verifier), AuditExportHandler(sessions, service))
}

func AuditTrailHandler(sessions transport.SessionService, service AuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		orgID, ok := readAuditOrganization(c, sessions)
		if !ok {
			return
		}
		rows, err := service.ExportAudit(c.Request.Context(), orgID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		if rows == nil {
			rows = []appreporting.AuditRow{}
		}
		c.JSON(http.StatusOK, rows)
	}
}

func RegisterVerifyRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service AuditVerificationService) {
	router.GET("/audit/verify", transport.AuthMiddleware(verifier), AuditVerifyHandler(sessions, service))
}

func AuditVerifyHandler(sessions transport.SessionService, service AuditVerificationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		orgID, ok := readAuditOrganization(c, sessions)
		if !ok {
			return
		}
		if err := service.Verify(c.Request.Context(), orgID); err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"verified": true})
	}
}

func AuditExportHandler(sessions transport.SessionService, service AuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		orgID, ok := readAuditOrganization(c, sessions)
		if !ok {
			return
		}
		rows, err := service.ExportAudit(c.Request.Context(), orgID)
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

func readAuditOrganization(c *gin.Context, sessions transport.SessionService) (uuid.UUID, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return uuid.Nil, false
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return uuid.Nil, false
	}
	if !transport.ContainsString(session.Roles, "Owner") && !transport.ContainsString(session.Roles, "Superadmin") {
		transport.WriteError(c, http.StatusForbidden, "audit_role_required")
		return uuid.Nil, false
	}
	return session.OrgID, true
}
