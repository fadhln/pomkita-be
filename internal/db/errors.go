package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// HTTPStatusForError maps database errors to the backend status groups.
func HTTPStatusForError(err error) int {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return 500
	}
	switch pgErr.Code {
	case "23514", "22003":
		return 422
	case "23505", "40P01":
		return 409
	case "28000":
		return 401
	default:
		return 500
	}
}

// StableCodeForError returns a safe machine code for a database error.
func StableCodeForError(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return ""
	}
	if pgErr.Code == "28000" && pgErr.Message == "session_idle" {
		return "session_idle"
	}
	return ""
}
