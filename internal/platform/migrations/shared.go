// Package migrations creates the shared database objects before the migration set.
package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// sharedObjectLockKey serializes the creation of the shared application schema.
const sharedObjectLockKey = "pomkita:shared-objects"

// sharedObjectStatements create the objects that are shared by all tenants.
//
// The statements are idempotent. A duplicate error is tolerated, because two
// processes can create the same object at the same time.
var sharedObjectStatements = []string{
	`create schema if not exists app`,
	`create extension if not exists pgcrypto with schema app`,
}

// EnsureSharedObjects creates the application schema and the pgcrypto extension.
//
// The migration set also creates these objects. This function runs first, so the
// migration statements always find the objects. Several processes can migrate one
// database at the same time. CREATE SCHEMA IF NOT EXISTS is not atomic: the check
// and the insert are separate steps, so two processes can both insert and one
// process receives a duplicate error. The function prevents this race with a
// session advisory lock, and it tolerates a duplicate error as a second guard.
//
// The function is safe to call more than one time. It has no effect when the
// objects exist.
func EnsureSharedObjects(ctx context.Context, databaseURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if databaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect for shared objects: %w", err)
	}
	defer func() {
		_ = connection.Close(context.WithoutCancel(ctx))
	}()

	// A session lock is used, because a session lock does not depend on the
	// transaction state of the connection.
	if _, err := connection.Exec(ctx, `select pg_advisory_lock(hashtext($1))`, sharedObjectLockKey); err != nil {
		return fmt.Errorf("lock shared objects: %w", err)
	}
	defer func() {
		_, _ = connection.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock(hashtext($1))`, sharedObjectLockKey)
	}()

	for _, statement := range sharedObjectStatements {
		if _, err := connection.Exec(ctx, statement); err != nil && !isDuplicateObject(err) {
			return fmt.Errorf("ensure shared objects: %w", err)
		}
	}
	return nil
}

// isDuplicateObject reports whether the error means that the object exists.
func isDuplicateObject(err error) bool {
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) {
		return false
	}
	switch databaseError.Code {
	case "42P06", // duplicate_schema
		"42710", // duplicate_object
		"23505": // unique_violation, for example on the pg_namespace name index
		return true
	default:
		return false
	}
}
