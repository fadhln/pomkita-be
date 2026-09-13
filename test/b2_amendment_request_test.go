package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestB2AmendmentRequest_CreatesPendingAmendmentWithItems(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyAmendmentRequestMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	_, _, _, _, _, shift, report, saleID := amendmentRequestFixture(t, conn)
	items := `[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"cash_amount","old_value":"2000","new_value":"2500"}]`

	var amendmentID, status string
	if err := conn.QueryRow(ctx, `
		select amendment_id::text, status
		from fn_request_amendment($1::uuid,$2::uuid,$3::text,$4::jsonb)
	`, shift, report, "correct declared cash", items).Scan(&amendmentID, &status); err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	if amendmentID == "" || status != "pending" {
		t.Fatalf("amendment: id=%q status=%q", amendmentID, status)
	}
}

func TestB2AmendmentRequest_RejectsMeterAndStaleValues(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyAmendmentRequestMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	org, station, supervisor, _, _, shift, report, saleID := amendmentRequestFixture(t, conn)

	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	for _, item := range []string{
		`[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"meter_end","old_value":"2000","new_value":"2500"}]`,
		`[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"cash_amount","old_value":"999","new_value":"2500"}]`,
	} {
		_, err := conn.Exec(ctx, `select * from fn_request_amendment($1::uuid,$2::uuid,'invalid',$3::jsonb)`, shift, report, item)
		if err == nil {
			t.Fatal("invalid amendment item was accepted")
		}
		var pgErr *pgconn.PgError
		if !errorsAs(err, &pgErr) || pgErr.Code != "23514" && pgErr.Code != "23505" {
			t.Fatalf("invalid amendment error: %v", err)
		}
	}
}

func TestB2AmendmentRequest_SupersedesPendingAndAppendsOneAuditEventEach(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyAmendmentRequestMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	org, station, supervisor, _, _, shift, report, saleID := amendmentRequestFixture(t, conn)
	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	items := `[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"cash_amount","old_value":"2000","new_value":"2500"}]`
	var first string
	if err := conn.QueryRow(ctx, `select amendment_id::text from fn_request_amendment($1,$2,'first',$3::jsonb)`, shift, report, items).Scan(&first); err != nil {
		t.Fatalf("first amendment: %v", err)
	}
	var second string
	if err := conn.QueryRow(ctx, `select amendment_id::text from fn_request_amendment($1,$2,'second',$3::jsonb)`, shift, report, items).Scan(&second); err != nil {
		t.Fatalf("second amendment: %v", err)
	}
	var firstStatus string
	if err := conn.QueryRow(ctx, `select status from amendments where amendment_id=$1`, first).Scan(&firstStatus); err != nil {
		t.Fatalf("first status: %v", err)
	}
	if firstStatus != "superseded" || first == second {
		t.Fatalf("supersession: first=%s second=%s status=%s", first, second, firstStatus)
	}
	var auditCount int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where org_id=$1 and event_type='amendment_requested'`, org).Scan(&auditCount); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("audit count: got %d, want 2", auditCount)
	}
}

func TestB2AmendmentQueue_ContainsScopedRequesterReasonAndDiffs(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyAmendmentRequestMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	org, station, supervisor, admin, _, shift, report, saleID := amendmentRequestFixture(t, conn)
	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	items := `[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"cash_amount","old_value":"2000","new_value":"2500"}]`
	if _, err := conn.Exec(ctx, `select * from fn_request_amendment($1,$2,'cash correction',$3::jsonb)`, shift, report, items); err != nil {
		t.Fatalf("request queue item: %v", err)
	}
	setTestContext(t, conn, org, station, admin, "Station Admin")
	var payload []byte
	if err := conn.QueryRow(ctx, `select amendment from read_amendment_queue() amendment`).Scan(&payload); err != nil {
		t.Fatalf("read amendment queue: %v", err)
	}
	var queue map[string]any
	if err := json.Unmarshal(payload, &queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if queue["reason"] != "cash correction" || queue["status"] != "pending" {
		t.Fatalf("queue identity: %v", queue)
	}
	if !strings.Contains(string(payload), "Supervisor") || !strings.Contains(string(payload), "2000") || !strings.Contains(string(payload), "2500") {
		t.Fatalf("queue details: %s", payload)
	}
}

func TestB2AmendmentReject_ChangesPendingStateAndWritesOneAuditEvent(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyAmendmentRequestMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	org, station, supervisor, admin, _, shift, report, saleID := amendmentRequestFixture(t, conn)
	items := `[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"cash_amount","old_value":"2000","new_value":"2500"}]`
	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	var amendment string
	if err := conn.QueryRow(ctx, `select amendment_id::text from fn_request_amendment($1,$2,'cash correction',$3::jsonb)`, shift, report, items).Scan(&amendment); err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	setTestContext(t, conn, org, station, admin, "Station Admin")
	var status, reason string
	if err := conn.QueryRow(ctx, `select status, rejection_reason from fn_reject_amendment($1,'not enough evidence')`, amendment).Scan(&status, &reason); err != nil {
		t.Fatalf("reject amendment: %v", err)
	}
	if status != "rejected" || reason != "not enough evidence" {
		t.Fatalf("rejection: status=%q reason=%q", status, reason)
	}
	var auditCount int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where org_id=$1 and event_type='amendment_rejected'`, org).Scan(&auditCount); err != nil {
		t.Fatalf("rejection audit count: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("rejection audit count: got %d, want 1", auditCount)
	}
}

func TestB2AmendmentReject_DeniesRequesterAndNonApprover(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyAmendmentRequestMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	org, station, supervisor, admin, requester, shift, report, saleID := amendmentRequestFixture(t, conn)
	items := `[{"target_kind":"sales_declared","target_logical_id":"` + saleID + `","field":"cash_amount","old_value":"2000","new_value":"2500"}]`
	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	var amendment string
	if err := conn.QueryRow(ctx, `select amendment_id::text from fn_request_amendment($1,$2,'cash correction',$3::jsonb)`, shift, report, items).Scan(&amendment); err != nil {
		t.Fatalf("request amendment: %v", err)
	}
	if _, err := conn.Exec(ctx, `select * from fn_reject_amendment($1,'not allowed')`, amendment); err == nil {
		t.Fatal("supervisor rejected an amendment")
	}
	if _, err := conn.Exec(ctx, `select set_config('app.transition','approve_amendment',false)`); err != nil {
		t.Fatalf("set test transition: %v", err)
	}
	if _, err := conn.Exec(ctx, `update amendments set requester_user_id=$2 where amendment_id=$1`, amendment, requester); err != nil {
		t.Fatalf("set requester fixture: %v", err)
	}
	setTestContext(t, conn, org, station, requester, "Owner")
	if _, err := conn.Exec(ctx, `select * from fn_reject_amendment($1,'self reject')`, amendment); err == nil {
		t.Fatal("requester rejected their own amendment")
	}
	_ = admin
}

func applyAmendmentRequestMigrations(t *testing.T, conn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	applyB2Migrations(t, conn.(*pgx.Conn))
	ctx := context.Background()
	for _, name := range []string{"000012_b2_audit.up.sql", "000019_b2_amendment_request.up.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func amendmentRequestFixture(t *testing.T, conn *pgx.Conn) (string, string, string, string, string, string, string, string) {
	t.Helper()
	const (
		org        = "11111111-1111-4111-8111-111111111111"
		station    = "22222222-2222-4222-8222-222222222222"
		supervisor = "33333333-3333-4333-8333-333333333333"
		admin      = "44444444-4444-4444-8444-444444444444"
		requester  = "55555555-5555-4555-8555-555555555555"
		dispenser  = "66666666-6666-4666-8666-666666666666"
		nozzle     = "77777777-7777-4777-8777-777777777777"
		tank       = "88888888-8888-4888-8888-888888888888"
		lossID     = "99999999-9999-4999-8999-999999999999"
		policy     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		threshold  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		evidence   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	)
	seedGovernanceData(t, conn, org, station, supervisor, admin, requester, dispenser, nozzle, tank, policy, threshold, evidence)
	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	shift, report := submitGovernanceReport(t, conn, station, supervisor, lossID)
	return org, station, supervisor, admin, requester, shift, report, governanceSaleID(t, conn, report)
}

func errorsAs(err error, target **pgconn.PgError) bool {
	return errors.As(err, target)
}
