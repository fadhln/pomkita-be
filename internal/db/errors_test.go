package db

import (
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
