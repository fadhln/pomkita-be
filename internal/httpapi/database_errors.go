package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
)

func httpStatusForDatabaseError(err error) int {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return http.StatusInternalServerError
	}
	switch pgErr.Code {
	case "22023":
		return http.StatusBadRequest
	case "23514", "22003":
		return http.StatusUnprocessableEntity
	case "23505", "40P01":
		return http.StatusConflict
	case "28000":
		return http.StatusUnauthorized
	case "42501":
		switch pgErr.Message {
		case "shift_not_found", "report_not_found", "amendment_not_found", "policy_revision_not_found", "policy_supersede_target_not_found":
			return http.StatusNotFound
		default:
			return http.StatusForbidden
		}
	default:
		return http.StatusInternalServerError
	}
}

func stableCodeForDatabaseError(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ""
	}
	if pgErr.Code == "28000" && pgErr.Message == "session_idle" {
		return "session_idle"
	}
	if pgErr.Code == "28000" && pgErr.Message == "invalid_credentials" {
		return "invalid_credentials"
	}
	if safeDatabaseCode(pgErr.Message) {
		return pgErr.Message
	}
	return ""
}

func safeDatabaseCode(message string) bool {
	switch message {
	case "ack_already_decided", "ack_head_missing", "ack_report_not_pending", "ack_role_required",
		"ack_separation_required", "amendment_base_not_current", "amendment_creator_forbidden",
		"amendment_field_forbidden", "amendment_not_found", "amendment_not_pending",
		"amendment_role_required", "amendment_request_role_required", "amendment_reject_role_required",
		"amendment_queue_role_required", "amendment_separation_required", "amendment_target_not_in_base",
		"break_glass_reason_required", "invalid_ack_request", "report_not_found", "shift_not_found",
		"invalid_amendment_request", "stale_amendment_base", "stale_amendment_value", "unexpected_break_glass_reason",
		"unexpected_rejection_reason", "rejection_reason_required", "policy_revision_role_required",
		"policy_revision_overlap", "policy_revision_already_disabled", "policy_already_superseded",
		"policy_valid_from_order", "policy_revision_not_found", "policy_supersede_target_not_found",
		"invalid_policy_revision_request", "invalid_threshold_policy", "invalid_evidence_policy",
		"invalid_evidence_policy_type", "duplicate_evidence_type", "required_evidence_minimum_missing",
		"invalid_evidence_mime_type", "tombstone_reason_required":
		return true
	default:
		return false
	}
}

func fieldErrorsForDatabaseError(err error) map[string]string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.Message == "rollover_over_threshold" {
		return map[string]string{"readings": "Meter rollover is above the allowed threshold"}
	}
	return nil
}
