package db

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestHTTPStatusForError_MapsRequiredSQLStates(t *testing.T) {
	cases := []struct {
		state  string
		status int
	}{
		{state: "23514", status: 422},
		{state: "23505", status: 409},
		{state: "22003", status: 422},
		{state: "40P01", status: 409},
		{state: "28000", status: 401},
		{state: "XX000", status: 500},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			err := &pgconn.PgError{Code: tc.state}
			if got := HTTPStatusForError(err); got != tc.status {
				t.Fatalf("got %d, want %d", got, tc.status)
			}
		})
	}
}

func TestStableCodeForErrorDistinguishesIdleSession(t *testing.T) {
	err := &pgconn.PgError{Code: "28000", Message: "session_idle"}
	if got := StableCodeForError(err); got != "session_idle" {
		t.Fatalf("got %q, want session_idle", got)
	}
}

func TestGovernanceErrorsMapToStableHTTPResults(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		message string
		status  int
		code    string
	}{
		{name: "stale amendment", state: "23505", message: "stale_amendment_base", status: http.StatusConflict, code: "stale_amendment_base"},
		{name: "missing shift", state: "42501", message: "shift_not_found", status: http.StatusNotFound, code: "shift_not_found"},
		{name: "ack role", state: "42501", message: "ack_role_required", status: http.StatusForbidden, code: "ack_role_required"},
		{name: "invalid ack", state: "22023", message: "invalid_ack_request", status: http.StatusBadRequest, code: "invalid_ack_request"},
		{name: "invalid amendment request", state: "22023", message: "invalid_amendment_request", status: http.StatusBadRequest, code: "invalid_amendment_request"},
		{name: "amendment request role", state: "42501", message: "amendment_request_role_required", status: http.StatusForbidden, code: "amendment_request_role_required"},
		{name: "amendment reject role", state: "42501", message: "amendment_reject_role_required", status: http.StatusForbidden, code: "amendment_reject_role_required"},
		{name: "amendment queue role", state: "42501", message: "amendment_queue_role_required", status: http.StatusForbidden, code: "amendment_queue_role_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &pgconn.PgError{Code: tc.state, Message: tc.message}
			if got := HTTPStatusForError(err); got != tc.status {
				t.Fatalf("status: got %d, want %d", got, tc.status)
			}
			if got := StableCodeForError(err); got != tc.code {
				t.Fatalf("code: got %q, want %q", got, tc.code)
			}
		})
	}
}
