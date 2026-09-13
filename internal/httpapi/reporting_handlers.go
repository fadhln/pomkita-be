package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

// AnomalyExportRow is one flat anomaly row for CSV output.
type AnomalyExportRow = appdb.AnomalyExportRow

// AuditExportRow is one flat audit row for CSV output.
type AuditExportRow = appdb.AuditExportRow

// AuditTrailRow is one audit row for the audit trail screen.
type AuditTrailRow = appdb.AuditTrailRow

// AuditVerifyResult is the result of an audit-chain verification.
type AuditVerifyResult = appdb.AuditVerifyResult

// ReportingService is the procedure boundary for reporting APIs.
type ReportingService interface {
	ReadReportPrintout(context.Context, string, uuid.UUID) (json.RawMessage, error)
	ReadAnomalyExport(context.Context, string) ([]AnomalyExportRow, error)
	ReadAuditTrail(context.Context, string) ([]AuditTrailRow, error)
	ReadAuditExport(context.Context, string) ([]AuditExportRow, error)
	VerifyAuditChain(context.Context, string) (AuditVerifyResult, error)
	ReadPolicyHistory(context.Context, string) ([]json.RawMessage, error)
}

// PolicyRevisionService is the procedure boundary for policy revision APIs.
type PolicyRevisionService interface {
	CreatePolicyRevision(context.Context, string, appdb.CreatePolicyRevisionInput) (appdb.PolicyRevisionResult, error)
	TombstonePolicyRevision(context.Context, string, appdb.TombstonePolicyRevisionInput) (appdb.PolicyRevisionResult, error)
}

func registerReportingRoutes(router *gin.Engine, verifier TokenVerifier, service ReportingService, policy PolicyRevisionService) {
	protectedRead := []gin.HandlerFunc{AuthMiddleware(verifier)}
	protectedWrite := []gin.HandlerFunc{AuthMiddleware(verifier), requireCSRF}
	router.GET("/report/:id/printout", append(protectedRead, readReportPrintoutHandler(service))...)
	router.GET("/anomalies/export", append(protectedRead, readAnomalyExportHandler(service))...)
	router.GET("/audit", append(protectedRead, readAuditTrailHandler(service))...)
	router.GET("/audit/export", append(protectedRead, readAuditExportHandler(service))...)
	router.GET("/audit/verify", append(protectedRead, verifyAuditChainHandler(service))...)
	router.GET("/policy/history", append(protectedRead, readPolicyHistoryHandler(service))...)
	router.POST("/policy/revision", append(protectedWrite, createPolicyRevisionHandler(policy))...)
	router.POST("/policy/revision/tombstone", append(protectedWrite, tombstonePolicyRevisionHandler(policy))...)
}

type createPolicyRevisionRequest struct {
	PolicyKind              string                     `json:"policy_kind"`
	PolicyID                uuid.UUID                  `json:"policy_id"`
	StationID               *uuid.UUID                 `json:"station_id"`
	ValidFrom               string                     `json:"valid_from"`
	SupersedesOrgID         *uuid.UUID                 `json:"supersedes_org_id"`
	SupersedesRevisionID    *uuid.UUID                 `json:"supersedes_revision_id"`
	LossLiterThreshold      string                     `json:"loss_liter_threshold"`
	GainLiterThreshold      string                     `json:"gain_liter_threshold"`
	LossRupiahThreshold     string                     `json:"loss_rupiah_threshold"`
	GainRupiahThreshold     string                     `json:"gain_rupiah_threshold"`
	VarianceRupiahThreshold string                     `json:"variance_rupiah_threshold"`
	RolloverThreshold       string                     `json:"rollover_threshold"`
	Mode                    string                     `json:"mode"`
	Types                   []appdb.EvidencePolicyType `json:"types"`
}

type tombstonePolicyRevisionRequest struct {
	PolicyKind string    `json:"policy_kind"`
	RevisionID uuid.UUID `json:"revision_id"`
	Reason     string    `json:"reason"`
}

func createPolicyRevisionHandler(service PolicyRevisionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input createPolicyRevisionRequest
		if !decodeRequest(c, &input) || !validCreatePolicyRevisionRequest(input) {
			return
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.CreatePolicyRevision(c, c.GetString("raw_token"), appdb.CreatePolicyRevisionInput{
			PolicyKind: input.PolicyKind, PolicyID: input.PolicyID, StationID: input.StationID,
			ValidFrom: input.ValidFrom, SupersedesOrgID: input.SupersedesOrgID,
			SupersedesRevisionID: input.SupersedesRevisionID,
			LossLiterThreshold:   input.LossLiterThreshold, GainLiterThreshold: input.GainLiterThreshold,
			LossRupiahThreshold: input.LossRupiahThreshold, GainRupiahThreshold: input.GainRupiahThreshold,
			VarianceRupiahThreshold: input.VarianceRupiahThreshold, RolloverThreshold: input.RolloverThreshold,
			Mode: input.Mode, Types: input.Types,
		})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func tombstonePolicyRevisionHandler(service PolicyRevisionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input tombstonePolicyRevisionRequest
		if !decodeRequest(c, &input) || input.PolicyKind == "" || input.RevisionID == uuid.Nil || strings.TrimSpace(input.Reason) == "" {
			return
		}
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.TombstonePolicyRevision(c, c.GetString("raw_token"), appdb.TombstonePolicyRevisionInput{
			PolicyKind: input.PolicyKind, RevisionID: input.RevisionID, Reason: input.Reason,
		})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func validCreatePolicyRevisionRequest(input createPolicyRevisionRequest) bool {
	if input.PolicyKind != "threshold" && input.PolicyKind != "evidence" || input.PolicyID == uuid.Nil || input.ValidFrom == "" {
		return false
	}
	if input.PolicyKind == "threshold" {
		return validDecimal(input.LossLiterThreshold) && validDecimal(input.GainLiterThreshold) &&
			validDecimal(input.LossRupiahThreshold) && validDecimal(input.GainRupiahThreshold) &&
			validDecimal(input.VarianceRupiahThreshold) && validDecimal(input.RolloverThreshold) &&
			input.Mode == "" && input.Types == nil
	}
	if input.Mode != "opsional" && input.Mode != "wajib" || len(input.Types) == 0 {
		return false
	}
	return input.LossLiterThreshold == "" && input.GainLiterThreshold == "" &&
		input.LossRupiahThreshold == "" && input.GainRupiahThreshold == "" &&
		input.VarianceRupiahThreshold == "" && input.RolloverThreshold == ""
}

func readReportPrintoutHandler(service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		reportID, err := uuid.Parse(c.Param("id"))
		if err != nil || reportID == uuid.Nil {
			validationError(c)
			return
		}
		payload, err := service.ReadReportPrintout(c, c.GetString("raw_token"), reportID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		writeJSONResult(c, payload, nil)
	}
}

func readAnomalyExportHandler(service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		rows, err := service.ReadAnomalyExport(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		writeCSV(c, "anomalies.csv", []string{
			"kind", "source", "source_id", "org_id", "station_id", "shift_id",
			"report_id", "version_no", "reason", "variance_rupiah", "threshold", "happened_at",
		}, func(writer *csv.Writer) error {
			for _, row := range rows {
				if err := writer.Write([]string{row.Kind, row.Source, row.SourceID, row.OrgID, row.StationID, row.ShiftID, row.ReportID, row.VersionNo, row.Reason, row.VarianceRupiah, row.Threshold, row.HappenedAt}); err != nil {
					return err
				}
			}
			return nil
		})
	}
}

func readAuditTrailHandler(service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		rows, err := service.ReadAuditTrail(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if rows == nil {
			rows = []AuditTrailRow{}
		}
		c.JSON(http.StatusOK, rows)
	}
}

func readAuditExportHandler(service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		rows, err := service.ReadAuditExport(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		writeCSV(c, "audit.csv", []string{
			"event_id", "org_sequence", "event_type", "payload", "outcome",
			"outcome_error", "created_at", "prev_hash", "row_hash",
		}, func(writer *csv.Writer) error {
			for _, row := range rows {
				if err := writer.Write([]string{row.EventID, row.OrgSequence, row.EventType, string(row.Payload), row.Outcome, row.OutcomeError, row.CreatedAt, row.PreviousHash, row.RowHash}); err != nil {
					return err
				}
			}
			return nil
		})
	}
}

func verifyAuditChainHandler(service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.VerifyAuditChain(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func readPolicyHistoryHandler(service ReportingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.ReadPolicyHistory(c, c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		if result == nil {
			result = []json.RawMessage{}
		}
		payload, err := json.Marshal(result)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
	}
}

func writeCSV(c *gin.Context, filename string, header []string, rows func(*csv.Writer) error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	if err := writer.Write(header); err != nil {
		_ = c.Error(err)
		return
	}
	if err := rows(writer); err != nil {
		_ = c.Error(err)
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = c.Error(err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(builder.String()))
}
