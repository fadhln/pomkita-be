package test

import (
	"context"
	"testing"
)

func TestB0ApplicationRoleHasNoTableDMLPrivileges(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0Foundation(t, conn, false)

	rows, err := conn.Query(ctx, `
		select n.nspname, c.relname, c.relkind::text,
		       has_table_privilege('pomkita_app', c.oid, 'SELECT'),
		       has_table_privilege('pomkita_app', c.oid, 'INSERT'),
		       has_table_privilege('pomkita_app', c.oid, 'UPDATE'),
		       has_table_privilege('pomkita_app', c.oid, 'DELETE')
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname not in ('pg_catalog', 'information_schema')
		  and n.nspname not like 'pg_toast%'
		  and c.relkind in ('r', 'p', 'v', 'm', 'S')
		order by n.nspname, c.relname
	`)
	if err != nil {
		t.Fatalf("query table privilege matrix: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var schema, name string
		var kind string
		var selectOK, insertOK, updateOK, deleteOK bool
		if err := rows.Scan(&schema, &name, &kind, &selectOK, &insertOK, &updateOK, &deleteOK); err != nil {
			t.Fatalf("scan table privilege matrix: %v", err)
		}
		if selectOK || insertOK || updateOK || deleteOK {
			t.Fatalf("application role has table DML on %s.%s (%s): select=%t insert=%t update=%t delete=%t", schema, name, kind, selectOK, insertOK, updateOK, deleteOK)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read table privilege matrix: %v", err)
	}

	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000001_b0_foundation.down.sql")); err != nil {
		t.Fatalf("reverse foundation: %v", err)
	}
}
