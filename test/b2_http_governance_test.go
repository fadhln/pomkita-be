package test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB2HTTP_GovernanceEndpointsCallRegisteredProcedures(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyB2Migrations(t, conn)
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000014_b2_scheduler.up.sql")); err != nil {
		t.Fatalf("apply 000014_b2_scheduler.up.sql: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000012_b2_audit.up.sql")); err != nil {
		t.Fatalf("apply 000012_b2_audit.up.sql: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000019_b2_amendment_request.up.sql")); err != nil {
		t.Fatalf("apply 000019_b2_amendment_request.up.sql: %v", err)
	}
	defer resetB0Foundation(t, conn, true)

	const (
		org        = "11111111-1111-4111-8111-111111111111"
		station    = "22222222-2222-4222-8222-222222222222"
		supervisor = "33333333-3333-4333-8333-333333333333"
		admin      = "44444444-4444-4444-8444-444444444444"
		requester  = "55555555-5555-4555-8555-555555555555"
		dispenser  = "66666666-6666-4666-8666-666666666666"
		nozzle     = "77777777-7777-4777-8777-777777777777"
		tank       = "88888888-8888-4888-8888-888888888888"
		policy     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		threshold  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		evidence   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	)
	seedGovernanceData(t, conn, org, station, supervisor, admin, requester, dispenser, nozzle, tank, policy, threshold, evidence)
	if _, err := conn.Exec(ctx, `insert into jwt_keys(kid,secret_ref,status,activated_at,max_token_expiry) values('key_1','app.jwt_secret.key_1','active',clock_timestamp(),clock_timestamp()+interval '15 minutes')`); err != nil {
		t.Fatalf("seed JWT key: %v", err)
	}
	database, err := appdb.New(ctx, testDatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	secrets := map[string]string{"app.jwt_secret.key_1": "test-secret"}
	database.SetJWTSecrets(secrets)
	database.SetJWTAudience("spbu-recon")
	tokens := appjwt.NewService(database.JWTStore(secrets), appjwt.Config{Issuer: "pomkita", Audience: "spbu-recon"})
	adminToken := issueGovernanceToken(t, tokens, uuid.MustParse(admin))
	supervisorToken := issueGovernanceToken(t, tokens, uuid.MustParse(supervisor))
	ownerToken := issueGovernanceToken(t, tokens, uuid.MustParse(requester))
	router := httpapi.NewRouterWithAllDependencies("test", nil, database, tokens, nil, nil, appdb.NewGovernanceManager(database))

	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	shift, report := submitGovernanceReport(t, conn, station, supervisor, "99999999-9999-4999-8999-999999999999")
	ack := governanceCall(router, adminToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+shift+`","report_id":"`+report+`","version_no":1,"decision":"acked"}`, true)
	if ack.Code != http.StatusOK || !strings.Contains(ack.Body.String(), `"decision":"acked"`) {
		t.Fatalf("ack: status=%d body=%s", ack.Code, ack.Body)
	}
	if second := governanceCall(router, adminToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+shift+`","report_id":"`+report+`","version_no":1,"decision":"acked"}`, true); second.Code != http.StatusConflict {
		t.Fatalf("second ack: status=%d body=%s", second.Code, second.Body)
	}
	requestBody := `{"shift_id":"` + shift + `","base_report_id":"` + report + `","reason":"cash correction","items":[{"target_kind":"sales_declared","target_logical_id":"` + governanceSaleID(t, conn, report) + `","field":"cash_amount","old_value":"2000","new_value":"2500"}]}`
	requested := governanceCall(router, supervisorToken, http.MethodPost, "/amendment/request", requestBody, true)
	if requested.Code != http.StatusOK || !strings.Contains(requested.Body.String(), `"status":"pending"`) {
		t.Fatalf("request amendment: status=%d body=%s", requested.Code, requested.Body)
	}
	queued := governanceCall(router, adminToken, http.MethodGet, "/amendments", "", false)
	if queued.Code != http.StatusOK || !strings.Contains(queued.Body.String(), `"display_name":"Supervisor"`) || !strings.Contains(queued.Body.String(), `"reason":"cash correction"`) {
		t.Fatalf("amendment queue: status=%d body=%s", queued.Code, queued.Body)
	}
	var requestedPayload struct {
		AmendmentID string `json:"amendment_id"`
	}
	if err := json.Unmarshal(requested.Body.Bytes(), &requestedPayload); err != nil {
		t.Fatalf("decode requested amendment: %v", err)
	}
	rejectedAmendment := governanceCall(router, adminToken, http.MethodPost, "/amendment/reject", `{"amendment_id":"`+requestedPayload.AmendmentID+`","rejection_reason":"not enough evidence"}`, true)
	if rejectedAmendment.Code != http.StatusOK || !strings.Contains(rejectedAmendment.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject amendment: status=%d body=%s", rejectedAmendment.Code, rejectedAmendment.Body)
	}

	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	breakGlassShift, breakGlassReport := submitGovernanceReport(t, conn, station, supervisor, "99999999-9999-4999-8999-999999999998")
	missingReason := governanceCall(router, ownerToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+breakGlassShift+`","report_id":"`+breakGlassReport+`","version_no":1,"decision":"acked","is_break_glass":true}`, true)
	if missingReason.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing break-glass reason: status=%d body=%s", missingReason.Code, missingReason.Body)
	}
	breakGlass := governanceCall(router, ownerToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+breakGlassShift+`","report_id":"`+breakGlassReport+`","version_no":1,"decision":"acked","is_break_glass":true,"break_glass_reason":"incident review"}`, true)
	if breakGlass.Code != http.StatusOK {
		t.Fatalf("break-glass ack: status=%d body=%s", breakGlass.Code, breakGlass.Body)
	}
	anomalies := governanceCall(router, ownerToken, http.MethodGet, "/anomalies", "", false)
	if anomalies.Code != http.StatusOK || !strings.Contains(anomalies.Body.String(), `"is_break_glass":true`) {
		t.Fatalf("anomalies: status=%d body=%s", anomalies.Code, anomalies.Body)
	}

	staleHash := governanceBaseHash(t, conn, org, station, shift, report, 1)
	saleID := governanceSaleID(t, conn, report)
	amendmentID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	insertGovernanceAmendment(t, conn, org, station, shift, report, amendmentID, requester, staleHash, saleID, "cash_amount")
	approved := governanceCall(router, adminToken, http.MethodPost, "/amendment/approve", `{"amendment_id":"`+amendmentID+`","stale_check_hash":"`+hex.EncodeToString(staleHash)+`"}`, true)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"version_no":2`) {
		t.Fatalf("approve: status=%d body=%s", approved.Code, approved.Body)
	}

	currentReport := uuid.MustParse(report)
	if err := conn.QueryRow(ctx, `select current_report_id from shifts where shift_id=$1`, shift).Scan(&currentReport); err != nil {
		t.Fatalf("current report: %v", err)
	}
	if reack := governanceCall(router, ownerToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+shift+`","report_id":"`+currentReport.String()+`","version_no":2,"decision":"acked"}`, true); reack.Code != http.StatusOK {
		t.Fatalf("re-ack amended report: status=%d body=%s", reack.Code, reack.Body)
	}
	staleAmendmentID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	insertGovernanceAmendment(t, conn, org, station, shift, currentReport.String(), staleAmendmentID, requester, make([]byte, 32), saleID, "cash_amount")
	stale := governanceCall(router, adminToken, http.MethodPost, "/amendment/approve", `{"amendment_id":"`+staleAmendmentID+`","stale_check_hash":"`+strings.Repeat("00", 32)+`"}`, true)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale approval: status=%d body=%s", stale.Code, stale.Body)
	}

	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	correctionShift, correctionReport := submitGovernanceReport(t, conn, station, supervisor, "99999999-9999-4999-8999-999999999997")
	rejected := governanceCall(router, adminToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+correctionShift+`","report_id":"`+correctionReport+`","version_no":1,"decision":"rejected","rejection_reason":"meter image is unclear"}`, true)
	if rejected.Code != http.StatusOK || !strings.Contains(rejected.Body.String(), `"decision":"rejected"`) {
		t.Fatalf("rejection: status=%d body=%s", rejected.Code, rejected.Body)
	}
	var correctionStatus string
	if err := conn.QueryRow(ctx, `select status::text from shifts where shift_id=$1`, correctionShift).Scan(&correctionStatus); err != nil || correctionStatus != "needs_correction" {
		t.Fatalf("rejection state: status=%q error=%v", correctionStatus, err)
	}

	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	meterShift, meterReport := submitGovernanceReport(t, conn, station, supervisor, "99999999-9999-4999-8999-999999999996")
	if ackMeter := governanceCall(router, adminToken, http.MethodPost, "/shift/ack", `{"shift_id":"`+meterShift+`","report_id":"`+meterReport+`","version_no":1,"decision":"acked"}`, true); ackMeter.Code != http.StatusOK {
		t.Fatalf("meter fixture ack: status=%d body=%s", ackMeter.Code, ackMeter.Body)
	}
	meterHash := governanceBaseHash(t, conn, org, station, meterShift, meterReport, 1)
	meterAmendmentID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	insertGovernanceAmendment(t, conn, org, station, meterShift, meterReport, meterAmendmentID, requester, meterHash, governanceSaleID(t, conn, meterReport), "meter_end")
	meter := governanceCall(router, adminToken, http.MethodPost, "/amendment/approve", `{"amendment_id":"`+meterAmendmentID+`","stale_check_hash":"`+hex.EncodeToString(meterHash)+`"}`, true)
	if meter.Code != http.StatusUnprocessableEntity || !strings.Contains(meter.Body.String(), `"code":"amendment_field_forbidden"`) {
		t.Fatalf("meter amendment: status=%d body=%s", meter.Code, meter.Body)
	}
}

func issueGovernanceToken(t *testing.T, tokens *appjwt.Service, user uuid.UUID) string {
	t.Helper()
	token, _, err := tokens.Issue(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

func governanceCall(router http.Handler, token string, method, path, body string, csrf bool) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	if csrf {
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func governanceBaseHash(t *testing.T, conn *pgx.Conn, org, station, shift, report string, version int) []byte {
	t.Helper()
	var hash []byte
	if err := conn.QueryRow(context.Background(), `select app.digest(convert_to(jsonb_build_array(fn_amendment_base_payload($1,$2,$3,$4),$5::integer)::text,'UTF8'),'sha256')`, org, station, shift, report, version).Scan(&hash); err != nil {
		t.Fatalf("base hash: %v", err)
	}
	return hash
}

func governanceSaleID(t *testing.T, conn *pgx.Conn, report string) string {
	t.Helper()
	var saleID string
	if err := conn.QueryRow(context.Background(), `select sales_id::text from sales_declared where report_id=$1`, report).Scan(&saleID); err != nil {
		t.Fatalf("sale ID: %v", err)
	}
	return saleID
}

func insertGovernanceAmendment(t *testing.T, conn *pgx.Conn, org, station, shift, report, amendment, requester string, staleHash []byte, saleID, field string) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.Exec(ctx, `insert into amendments(amendment_id,org_id,station_id,shift_id,base_report_id,reason,requester_user_id,stale_check_hash) values($1,$2,$3,$4,$5,'correct declared cash',$6,$7)`, amendment, org, station, shift, report, requester, staleHash); err != nil {
		t.Fatalf("insert amendment: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into amendment_items(amendment_id,org_id,station_id,shift_id,target_kind,target_logical_id,field,old_value,new_value) values($1,$2,$3,$4,'sales_declared',$5,$6,to_jsonb('2000'::text),to_jsonb('2500'::text))`, amendment, org, station, shift, saleID, field); err != nil {
		t.Fatalf("insert amendment item: %v", err)
	}
}
