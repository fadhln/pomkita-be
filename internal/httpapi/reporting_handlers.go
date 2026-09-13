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

func registerReportingRoutes(router *gin.Engine, verifier TokenVerifier, service ReportingService) {
	protectedRead := []gin.HandlerFunc{AuthMiddleware(verifier)}
	router.GET("/report/:id/printout", append(protectedRead, readReportPrintoutHandler(service))...)
	router.GET("/anomalies/export", append(protectedRead, readAnomalyExportHandler(service))...)
	router.GET("/audit", append(protectedRead, readAuditTrailHandler(service))...)
	router.GET("/audit/export", append(protectedRead, readAuditExportHandler(service))...)
	router.GET("/audit/verify", append(protectedRead, verifyAuditChainHandler(service))...)
	router.GET("/policy/history", append(protectedRead, readPolicyHistoryHandler(service))...)
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
