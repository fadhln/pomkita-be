package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB2HTTP_ShiftDraftAndSubmitEndpointsUseRegisteredProcedures(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyB1HTTPMigrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	const org = "11111111-1111-4111-8111-111111111111"
	const station = "22222222-2222-4222-8222-222222222222"
	const user = "33333333-3333-4333-8333-333333333333"
	const dispenser = "44444444-4444-4444-8444-444444444444"
	const nozzle = "55555555-5555-4555-8555-555555555555"
	const tank = "66666666-6666-4666-8666-666666666666"
	const policy = "77777777-7777-4777-8777-777777777777"
	const threshold = "88888888-8888-4888-8888-888888888888"
	const evidence = "99999999-9999-4999-8999-999999999999"
	seedB01User(t, conn, org, station, user)
	for _, q := range []string{
		`update user_station_roles set role='Supervisor' where org_id=$1 and station_id=$2 and user_id=$3`,
		`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`,
		`insert into tanks(org_id,station_id,tank_id) values($1,$2,$3)`,
		`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`,
		`insert into nozzle_tank_map(org_id,station_id,nozzle_id,tank_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2030-01-01','[)'))`,
		`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2030-01-01','[)'))`,
		`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values($1,$2,$3,10000,tstzrange('2020-01-01','2030-01-01','[)'),$4)`,
		`insert into threshold_policy_revisions(rev_id,policy_id,org_id,valid_from,created_by) values($1,$2,$3,'2020-01-01',$4)`,
		`insert into evidence_policy_revisions(rev_id,policy_id,org_id,valid_from,mode,created_by) values($1,$2,$3,'2020-01-01','opsional',$4)`,
	} {
		var args []any
		switch {
		case strings.HasPrefix(q, "update"):
			args = []any{org, station, user}
		case strings.Contains(q, "dispensers("):
			args = []any{org, station, dispenser}
		case strings.Contains(q, "tanks("):
			args = []any{org, station, tank}
		case strings.Contains(q, "nozzles("):
			args = []any{org, station, nozzle, dispenser}
		case strings.Contains(q, "nozzle_tank"):
			args = []any{org, station, nozzle, tank}
		case strings.Contains(q, "dispenser_nozzle"):
			args = []any{org, station, dispenser, nozzle}
		case strings.Contains(q, "dispenser_prices"):
			args = []any{org, station, nozzle, user}
		case strings.Contains(q, "threshold_policy"):
			args = []any{threshold, policy, org, user}
		case strings.Contains(q, "evidence_policy"):
			args = []any{evidence, policy, org, user}
		}
		if _, err := conn.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed %s args=%v: %v", q, args, err)
		}
	}
	var missing int
	if err := conn.QueryRow(ctx, `select count(*) from unnest(array['fn_open_shift','fn_claim_draft','fn_heartbeat_draft','fn_write_draft_reading','fn_write_draft_sales','fn_write_draft_loss','fn_stage_draft_evidence','read_draft','fn_submit_shift','read_shift_list','read_shift_detail','read_report']) n(name) where not exists(select 1 from procedure_registry p where p.name=n.name)`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("procedure registry: missing=%d err=%v", missing, err)
	}
	var catalog int
	if err := conn.QueryRow(ctx, `select count(*) from nozzles n join dispensers d using(org_id,station_id,dispenser_id) join dispenser_nozzle_map m using(org_id,station_id,dispenser_id,nozzle_id) join dispenser_prices p using(org_id,station_id,nozzle_id) where n.station_id=$1 and m.valid_period @> '2026-01-01 12:00+00'::timestamptz and p.valid_period @> '2026-01-01 12:00+00'::timestamptz`, station).Scan(&catalog); err != nil || catalog != 1 {
		t.Fatalf("catalog: count=%d err=%v", catalog, err)
	}
	database, err := appdb.New(ctx, testDatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetJWTSecrets(map[string]string{"app.jwt_secret.key_1": "test-secret"})
	database.SetJWTAudience("spbu-recon")
	tokens := appjwt.NewService(database.JWTStore(map[string]string{"app.jwt_secret.key_1": "test-secret"}), appjwt.Config{Issuer: "pomkita", Audience: "spbu-recon", Now: time.Now})
	router := httpapi.NewRouterWithAllDependencies("test", nil, database, tokens, appdb.NewSessionManager(database, tokens), appdb.NewShiftManager(database))
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"user@example.com","password":"correct-password"}`))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(login, req)
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	cookie := login.Result().Cookies()[0]
	call := func(method, path, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		q := httptest.NewRequest(method, path, strings.NewReader(body))
		q.AddCookie(cookie)
		if csrf {
			q.Header.Set("X-Requested-With", "XMLHttpRequest")
		}
		router.ServeHTTP(r, q)
		return r
	}
	open := call("POST", "/shift/open", `{"station_id":"`+station+`","opened_at":"2026-01-01T12:00:00Z","backfilled":false}`, true)
	if open.Code != 200 {
		t.Fatalf("open: %d %s", open.Code, open.Body)
	}
	var opened struct {
		ShiftID string `json:"shift_id"`
	}
	json.NewDecoder(open.Body).Decode(&opened)
	claim := call("POST", "/draft/claim", `{"shift_id":"`+opened.ShiftID+`"}`, true)
	if claim.Code != 200 {
		t.Fatalf("claim: %d %s", claim.Code, claim.Body)
	}
	var leased struct {
		DraftID    string `json:"draft_id"`
		ClaimToken string `json:"claim_token"`
		Revision   int    `json:"revision"`
	}
	json.NewDecoder(claim.Body).Decode(&leased)
	if _, err := conn.Exec(ctx, `update shift_drafts set status='submitting' where draft_id=$1`, leased.DraftID); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/draft/heartbeat", `{"draft_id":"`+leased.DraftID+`","claim_token":"`+leased.ClaimToken+`"}`, true); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"renewed":true`) {
		t.Fatalf("heartbeat: %d %s", r.Code, r.Body)
	}
	if _, err := conn.Exec(ctx, `update shift_drafts set status='editing' where draft_id=$1`, leased.DraftID); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/draft/reading", `{"draft_id":"`+leased.DraftID+`","claim_token":"`+leased.ClaimToken+`","revision":0,"nozzle_id":"`+nozzle+`","meter_start":"1.0","meter_end":"2.0"}`, true); r.Code != http.StatusConflict {
		t.Fatalf("stale fence: %d %s", r.Code, r.Body)
	}
	reading := `{"draft_id":"` + leased.DraftID + `","claim_token":"` + leased.ClaimToken + `","revision":` + itoa(leased.Revision) + `,"nozzle_id":"` + nozzle + `","meter_start":"1.0","meter_end":"2.0"}`
	r := call("POST", "/draft/reading", reading, true)
	if r.Code != 200 {
		t.Fatalf("reading: %d %s", r.Code, r.Body)
	}
	leased.Revision++
	sales := `{"draft_id":"` + leased.DraftID + `","claim_token":"` + leased.ClaimToken + `","revision":` + itoa(leased.Revision) + `,"dispenser_id":"` + dispenser + `","cash_amount":"10000","cashless_amount":"0"}`
	r = call("POST", "/draft/sales", sales, true)
	if r.Code != 200 {
		t.Fatalf("sales: %d %s", r.Code, r.Body)
	}
	leased.Revision++
	loss := `{"draft_id":"` + leased.DraftID + `","claim_token":"` + leased.ClaimToken + `","revision":` + itoa(leased.Revision) + `,"loss_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","direction":"loss","reason_code":"test","liters":"1.00","cash_amount":"","note":"note"}`
	r = call("POST", "/draft/loss", loss, true)
	if r.Code != 200 {
		t.Fatalf("loss: %d %s", r.Code, r.Body)
	}
	leased.Revision++
	var lossRow string
	if err := conn.QueryRow(ctx, `select row_id::text from draft_losses where draft_id=$1`, leased.DraftID).Scan(&lossRow); err != nil {
		t.Fatal(err)
	}
	evidenceRequest := `{"draft_id":"` + leased.DraftID + `","claim_token":"` + leased.ClaimToken + `","revision":` + itoa(leased.Revision) + `,"loss_row_id":"` + lossRow + `","evidence_type":"photo","object_key":"object-key","content_hash":"0000000000000000000000000000000000000000000000000000000000000000","size_bytes":10,"mime":"image/jpeg"}`
	if r = call("POST", "/draft/evidence", evidenceRequest, true); r.Code != http.StatusOK {
		t.Fatalf("evidence: %d %s", r.Code, r.Body)
	}
	leased.Revision++
	if r = call("GET", "/draft?shift_id="+opened.ShiftID, "", false); r.Code != 200 {
		t.Fatalf("draft: %d %s", r.Code, r.Body)
	}
	if r = call("GET", "/shifts", "", false); r.Code != 200 {
		t.Fatalf("list: %d %s", r.Code, r.Body)
	}
	if r = call("GET", "/shifts/"+opened.ShiftID, "", false); r.Code != 200 {
		t.Fatalf("detail: %d %s", r.Code, r.Body)
	}
	submit := `{"shift_id":"` + opened.ShiftID + `","draft_id":"` + leased.DraftID + `","claim_token":"` + leased.ClaimToken + `","revision":` + itoa(leased.Revision) + `,"hash_version":1,"readings":[{"nozzle_id":"` + nozzle + `","meter_start":"1.0","meter_end":"2.0"}],"sales":[{"dispenser_id":"` + dispenser + `","cash_amount":"10000","cashless_amount":"0"}],"losses":[]}`
	submitCall := func(key, body string) *httptest.ResponseRecorder {
		q := httptest.NewRequest("POST", "/shift/submit", strings.NewReader(body))
		q.AddCookie(cookie)
		q.Header.Set("X-Requested-With", "XMLHttpRequest")
		q.Header.Set("Idempotency-Key", key)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, q)
		return rr
	}
	rr := submitCall("b2-http-1", submit)
	if rr.Code != 200 {
		t.Fatalf("submit: %d %s", rr.Code, rr.Body)
	}
	var report struct {
		ReportID string `json:"report_id"`
	}
	json.NewDecoder(rr.Body).Decode(&report)
	if r = call("GET", "/report/"+report.ReportID, "", false); r.Code != 200 {
		t.Fatalf("report: %d %s", r.Code, r.Body)
	}
	if rr = submitCall("b2-http-1", submit); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"replay":true`) {
		t.Fatalf("idempotent replay: %d %s", rr.Code, rr.Body)
	}
	if rr = submitCall("b2-http-1", strings.Replace(submit, `"10000"`, `"10001"`, 1)); rr.Code != http.StatusConflict {
		t.Fatalf("idempotency mismatch: %d %s", rr.Code, rr.Body)
	}
}

func applyB1HTTPMigrations(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	resetB0Foundation(t, conn, true)
	for _, name := range []string{"000005_b1_catalog.up.sql", "000006_b1_shifts.up.sql", "000007_b1_drafts.up.sql", "000008_b1_submit.up.sql", "000009_b1_recovery.up.sql"} {
		if _, err := conn.Exec(context.Background(), readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func itoa(value int) string { return strconv.Itoa(value) }
