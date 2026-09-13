package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB4DemoSeed_SupportsLoginAndCompleteShiftHappyPath(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	defer resetB0Foundation(t, conn, true)

	seed, err := os.ReadFile(filepath.Join(repositoryRoot(t), "seed", "demo.sql"))
	if err != nil {
		t.Fatalf("read demo seed: %v", err)
	}
	if _, err := conn.Exec(ctx, string(seed)); err != nil {
		t.Fatalf("apply demo seed: %v", err)
	}

	database, err := appdb.New(ctx, testDatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	secrets := map[string]string{"app.jwt_secret.key_1": "pomkita-demo-only-secret"}
	database.SetJWTSecrets(secrets)
	database.SetJWTAudience("spbu-recon")
	tokens := appjwt.NewService(database.JWTStore(secrets), appjwt.Config{Issuer: "pomkita", Audience: "spbu-recon"})
	router := httpapi.NewRouterWithAllDependencies("test", nil, database, tokens,
		appdb.NewSessionManager(database, tokens), appdb.NewShiftManager(database),
		appdb.NewGovernanceManager(database), appdb.NewReportingManager(database))

	supervisorCookie := demoLogin(t, router, "supervisor@demo.pomkita.test", "demo-password")
	stationAdminCookie := demoLogin(t, router, "station-admin@demo.pomkita.test", "demo-password")
	const stationID = "22222222-2222-4222-8222-222222222222"
	const nozzleID = "44444444-4444-4444-8444-444444444444"
	const dispenserID = "33333333-3333-4333-8333-333333333333"

	open := demoCall(router, supervisorCookie, http.MethodPost, "/shift/open", `{"station_id":"`+stationID+`","opened_at":"2026-09-13T08:00:00Z","backfilled":false}`, true)
	if open.Code != http.StatusOK {
		t.Fatalf("open: status=%d body=%s", open.Code, open.Body)
	}
	var opened struct {
		ShiftID string `json:"shift_id"`
	}
	if err := json.Unmarshal(open.Body.Bytes(), &opened); err != nil || opened.ShiftID == "" {
		t.Fatalf("decode open response: %v body=%s", err, open.Body)
	}
	claim := demoCall(router, supervisorCookie, http.MethodPost, "/draft/claim", `{"shift_id":"`+opened.ShiftID+`"}`, true)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim: status=%d body=%s", claim.Code, claim.Body)
	}
	var leased struct {
		DraftID    string `json:"draft_id"`
		ClaimToken string `json:"claim_token"`
		Revision   int    `json:"revision"`
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &leased); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	submit := `{"shift_id":"` + opened.ShiftID + `","draft_id":"` + leased.DraftID + `","claim_token":"` + leased.ClaimToken + `","revision":` + itoa(leased.Revision) + `,"hash_version":1,"readings":[{"nozzle_id":"` + nozzleID + `","meter_start":"1.0","meter_end":"11.0"}],"sales":[{"dispenser_id":"` + dispenserID + `","cash_amount":"10000","cashless_amount":"0"}],"losses":[]}`
	request := httptest.NewRequest(http.MethodPost, "/shift/submit", strings.NewReader(submit))
	request.AddCookie(supervisorCookie)
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	request.Header.Set("Idempotency-Key", "demo-seed-happy-path")
	submitted := httptest.NewRecorder()
	router.ServeHTTP(submitted, request)
	if submitted.Code != http.StatusOK {
		t.Fatalf("submit: status=%d body=%s", submitted.Code, submitted.Body)
	}
	var report struct {
		ReportID string `json:"report_id"`
	}
	if err := json.Unmarshal(submitted.Body.Bytes(), &report); err != nil || report.ReportID == "" {
		t.Fatalf("decode submit response: %v body=%s", err, submitted.Body)
	}
	ack := demoCall(router, stationAdminCookie, http.MethodPost, "/shift/ack", `{"shift_id":"`+opened.ShiftID+`","report_id":"`+report.ReportID+`","version_no":1,"decision":"acked"}`, true)
	if ack.Code != http.StatusOK || !strings.Contains(ack.Body.String(), `"decision":"acked"`) {
		t.Fatalf("ack: status=%d body=%s", ack.Code, ack.Body)
	}

	reportView := demoCall(router, stationAdminCookie, http.MethodGet, "/report/"+report.ReportID, "", false)
	printout := demoCall(router, stationAdminCookie, http.MethodGet, "/report/"+report.ReportID+"/printout", "", false)
	if reportView.Code != http.StatusOK || printout.Code != http.StatusOK {
		t.Fatalf("report endpoints: report=%d printout=%d", reportView.Code, printout.Code)
	}
	if reportView.Body.String() != printout.Body.String() {
		t.Fatalf("report and printout bytes differ: report=%s printout=%s", reportView.Body, printout.Body)
	}
	if _, err := appdb.NewReportingManager(database).ReadAnomalyExport(ctx, stationAdminCookie.Value); err != nil {
		t.Fatalf("read anomaly export procedure: %v", err)
	}
	anomalies := demoCall(router, stationAdminCookie, http.MethodGet, "/anomalies/export", "", false)
	if anomalies.Code != http.StatusOK || !strings.HasPrefix(anomalies.Body.String(), "kind,source,source_id") {
		t.Fatalf("anomaly export: status=%d body=%s", anomalies.Code, anomalies.Body)
	}
	audit := demoCall(router, stationAdminCookie, http.MethodGet, "/audit/export", "", false)
	if audit.Code != http.StatusOK || !strings.HasPrefix(audit.Body.String(), "event_id,org_sequence,event_type") {
		t.Fatalf("audit export: status=%d body=%s", audit.Code, audit.Body)
	}
	verified := demoCall(router, stationAdminCookie, http.MethodGet, "/audit/verify", "", false)
	if verified.Code != http.StatusOK || !strings.Contains(verified.Body.String(), `"verified":true`) {
		t.Fatalf("audit verify: status=%d body=%s", verified.Code, verified.Body)
	}
	ownerCookie := demoLogin(t, router, "owner@demo.pomkita.test", "demo-password")
	if _, err := appdb.NewReportingManager(database).ReadPolicyHistory(ctx, ownerCookie.Value); err != nil {
		t.Fatalf("read policy history procedure: %v", err)
	}
	policy := demoCall(router, ownerCookie, http.MethodGet, "/policy/history", "", false)
	if policy.Code != http.StatusOK || !strings.Contains(policy.Body.String(), `"policy_kind":"threshold"`) {
		t.Fatalf("policy history: status=%d body=%s", policy.Code, policy.Body)
	}
}

func demoLogin(t *testing.T, router http.Handler, email, password string) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"`+email+`","password":"`+password+`"}`))
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login %s: status=%d body=%s", email, recorder.Code, recorder.Body)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "pomkita_session" {
			return cookie
		}
	}
	t.Fatalf("login %s did not return a session cookie", email)
	return nil
}

func demoCall(router http.Handler, cookie *http.Cookie, method, path, body string, csrf bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(cookie)
	if csrf {
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
