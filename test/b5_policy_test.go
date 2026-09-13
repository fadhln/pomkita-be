package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestB5PolicyRevisionWrite_UsesOwnerContextAndWritesAudit(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	seedDemoPolicyContext(t, conn, "Owner")

	threshold := createThresholdRevision(t, conn, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab", "2026-02-01T00:00:00Z", "organization", "")
	if threshold == "" {
		t.Fatal("threshold revision id is empty")
	}
	evidence := createEvidenceRevision(t, conn, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaac", "2026-02-01T00:00:00Z", "organization", "")
	if evidence == "" {
		t.Fatal("evidence revision id is empty")
	}

	var history []json.RawMessage
	rows, err := conn.Query(ctx, `select item from public.read_policy_history() item`)
	if err != nil {
		t.Fatalf("read policy history: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item []byte
		if err := rows.Scan(&item); err != nil {
			t.Fatalf("scan policy history: %v", err)
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read policy history rows: %v", err)
	}
	historyText, _ := json.Marshal(history)
	if !strings.Contains(string(historyText), threshold) || !strings.Contains(string(historyText), evidence) {
		t.Fatalf("created revisions are not visible in policy history: %s", historyText)
	}

	var auditCount int
	if err := conn.QueryRow(ctx, `select count(*) from public.audit_log where event_type in ('policy_revision_created') and payload->>'revision_id' in ($1,$2)`, threshold, evidence).Scan(&auditCount); err != nil {
		t.Fatalf("count policy audit events: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("policy audit events: got %d, want 2", auditCount)
	}
}

func TestB5PolicyRevisionWrite_EnforcesOrderingSupersessionScopeAndTombstone(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	seedDemoPolicyContext(t, conn, "Owner")

	first := createThresholdRevision(t, conn, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaad", "2026-03-01T00:00:00Z", "organization", "")
	second := createThresholdRevision(t, conn, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaad", "2026-04-01T00:00:00Z", "organization", first)

	expectPolicyError(t, conn, `select public.fn_create_policy_revision('threshold',$1::uuid,null,$2::timestamptz,'11111111-1111-4111-8111-111111111111'::uuid,$3::uuid,1,2,3,4,5,6,null,null)`, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaad", "2026-02-01T00:00:00Z", second, "policy_valid_from_order")
	expectPolicyError(t, conn, `select public.fn_create_policy_revision('threshold',$1::uuid,null,$2::timestamptz,'11111111-1111-4111-8111-111111111111'::uuid,$3::uuid,1,2,3,4,5,6,null,null)`, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaad", "2026-05-01T00:00:00Z", first, "policy_already_superseded")

	station := createThresholdRevision(t, conn, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaf", "2026-03-01T00:00:00Z", "station", "")
	expectPolicyError(t, conn, `select public.fn_create_policy_revision('threshold',$1::uuid,$2::uuid,$3::timestamptz,null,null,1,2,3,4,5,6,null,null)`, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaba", "22222222-2222-4222-8222-222222222222", "2026-03-01T00:00:00Z", "policy_revision_overlap")

	if _, err := conn.Exec(ctx, `select public.fn_tombstone_policy_revision('threshold',$1::uuid,'obsolete policy')`, station); err != nil {
		t.Fatalf("tombstone station revision: %v", err)
	}
	var disabled bool
	if err := conn.QueryRow(ctx, `select disabled from public.threshold_policy_revisions where rev_id=$1`, station).Scan(&disabled); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if !disabled {
		t.Fatal("tombstone did not disable revision")
	}
	var reason string
	if err := conn.QueryRow(ctx, `select payload->>'reason' from public.audit_log where event_type='policy_revision_tombstoned' and payload->>'revision_id'=$1`, station).Scan(&reason); err != nil {
		t.Fatalf("read tombstone audit: %v", err)
	}
	if reason != "obsolete policy" {
		t.Fatalf("tombstone reason: got %q", reason)
	}
}

func TestB5PolicyRevisionWrite_TombstoneIsExcludedFromSubmitResolution(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	seedDemoPolicyContext(t, conn, "Owner")

	newest := createThresholdRevision(t, conn, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "2026-01-02T00:00:00Z", "organization", "99999999-9999-4999-8999-999999999999")
	if _, err := conn.Exec(ctx, `select public.fn_tombstone_policy_revision('threshold',$1::uuid,'replaced')`, newest); err != nil {
		t.Fatalf("tombstone newest policy: %v", err)
	}
	setTestContext(t, conn, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "66666666-6666-4666-8666-666666666666", "Supervisor")
	var shift, draft, claim string
	var revision int
	if err := conn.QueryRow(ctx, `select shift_id::text from public.fn_open_shift($1,$2,'2026-01-03 12:00+00',false,null,null,null,null)`, "22222222-2222-4222-8222-222222222222", "66666666-6666-4666-8666-666666666666").Scan(&shift); err != nil {
		t.Fatalf("open shift for resolution: %v", err)
	}
	if err := conn.QueryRow(ctx, `select draft_id::text, claim_token::text, revision from public.fn_claim_draft($1)`, shift).Scan(&draft, &claim, &revision); err != nil {
		t.Fatalf("claim draft for resolution: %v", err)
	}
	request := `{"hash_version":1,"readings":[],"sales":[],"losses":[]}`
	var report string
	if err := conn.QueryRow(ctx, `select report_id::text from public.fn_submit_shift($1,$2,$3,$4,'tombstone-resolution',$5::jsonb)`, shift, draft, claim, revision, request).Scan(&report); err != nil {
		t.Fatalf("submit after tombstone: %v", err)
	}
	var threshold string
	if err := conn.QueryRow(ctx, `select i.payload->>'loss_liter_threshold' from public.policy_snapshot_items i join public.shift_reports r on r.org_id=i.org_id and r.station_id=i.station_id and r.shift_id=i.shift_id and r.policy_snapshot_set_id=i.set_id where r.report_id=$1 and i.policy_kind='threshold'`, report).Scan(&threshold); err != nil {
		t.Fatalf("read resolved policy: %v", err)
	}
	if threshold != "10.00" {
		t.Fatalf("resolved tombstoned threshold: got %q, want old active value 10.00", threshold)
	}
}

func seedDemoPolicyContext(t *testing.T, conn *pgx.Conn, role string) {
	t.Helper()
	ctx := context.Background()
	seed := readMigration(t, repositoryRoot(t), "../seed/demo.sql")
	if _, err := conn.Exec(ctx, seed); err != nil {
		t.Fatalf("seed demo policy data: %v", err)
	}
	if _, err := conn.Exec(ctx, `select set_config('app.context_valid','true',false), set_config('app.org_id','11111111-1111-4111-8111-111111111111',false), set_config('app.station_id','',false), set_config('app.user_id','88888888-8888-4888-8888-888888888888',false), set_config('app.role',$1,false)`, role); err != nil {
		t.Fatalf("set policy context: %v", err)
	}
}

func createThresholdRevision(t *testing.T, conn *pgx.Conn, policyID, validFrom, scope, supersedes string) string {
	t.Helper()
	var revision string
	var station any
	var supersedesOrg any
	if scope == "station" {
		station = "22222222-2222-4222-8222-222222222222"
	}
	if supersedes != "" {
		supersedesOrg = "11111111-1111-4111-8111-111111111111"
	}
	err := conn.QueryRow(context.Background(), `select revision_id::text from public.fn_create_policy_revision('threshold',$1::uuid,$2::uuid,$3::timestamptz,$4::uuid,$5::uuid,1,2,3,4,5,6,null,null)`, policyID, station, validFrom, supersedesOrg, nullableRevision(supersedes)).Scan(&revision)
	if err != nil {
		t.Fatalf("create threshold revision: %v", err)
	}
	return revision
}

func createEvidenceRevision(t *testing.T, conn *pgx.Conn, policyID, validFrom, scope, supersedes string) string {
	t.Helper()
	var revision string
	var station any
	var supersedesOrg any
	if scope == "station" {
		station = "22222222-2222-4222-8222-222222222222"
	}
	if supersedes != "" {
		supersedesOrg = "11111111-1111-4111-8111-111111111111"
	}
	types := `[ {"evidence_type":"foto","minimum_count_per_loss":1,"accepted_mime_types":["image/jpeg"]} ]`
	err := conn.QueryRow(context.Background(), `select revision_id::text from public.fn_create_policy_revision('evidence',$1::uuid,$2::uuid,$3::timestamptz,$4::uuid,$5::uuid,null,null,null,null,null,null,'wajib',$6::jsonb)`, policyID, station, validFrom, supersedesOrg, nullableRevision(supersedes), types).Scan(&revision)
	if err != nil {
		t.Fatalf("create evidence revision: %v", err)
	}
	return revision
}

func nullableRevision(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func expectPolicyError(t *testing.T, conn *pgx.Conn, query, policyID, validFrom, supersedes, want string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), query, policyID, validFrom, supersedes); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("policy error: got %v, want %q", err, want)
	}
}
