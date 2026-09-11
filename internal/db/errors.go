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
	default:
		return 500
	}
}
