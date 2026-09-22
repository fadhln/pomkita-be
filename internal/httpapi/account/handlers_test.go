package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	appaccount "github.com/fadhln/pomkita-be/internal/service/account"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type accountHTTPServiceStub struct{}

func (accountHTTPServiceStub) Read(context.Context, uuid.UUID) (appaccount.Profile, error) {
	return appaccount.Profile{}, nil
}
func (accountHTTPServiceStub) Update(context.Context, uuid.UUID, uuid.UUID, appaccount.UpdateRequest) (appaccount.Profile, error) {
	return appaccount.Profile{}, nil
}
func (accountHTTPServiceStub) ChangePassword(context.Context, uuid.UUID, uuid.UUID, appaccount.PasswordRequest) error {
	return nil
}
func (accountHTTPServiceStub) Forgot(context.Context, string) error                 { return nil }
func (accountHTTPServiceStub) Reset(context.Context, appaccount.ResetRequest) error { return nil }

type accountHTTPVerifier struct{}

func (accountHTTPVerifier) Verify(context.Context, string) (appjwt.Claims, error) {
	return appjwt.Claims{Subject: uuid.New(), JTI: uuid.New()}, nil
}

func TestForgotUnknownIdentifierAlwaysReturnsAcceptedWithEmptyBody(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, accountHTTPVerifier{}, nil, accountHTTPServiceStub{}, NewLimiter(5, time.Minute, time.Now))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/password/forgot", strings.NewReader(`{"identifier":"unknown@example.test"}`))
	request.Header.Set("X-Requested-With", "PomKita")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || recorder.Body.Len() != 0 {
		t.Fatalf("response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestAccountPasswordWithoutSessionIsUnauthorized(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, nil, nil, accountHTTPServiceStub{}, NewLimiter(5, time.Minute, time.Now))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(`{"current_password":"x","new_password":"strong-password"}`))
	request.Header.Set("X-Requested-With", "PomKita")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestLimiterRejectsSixthRequestAndAllowsAfterWindow(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	current := now
	limiter := NewLimiter(5, time.Minute, func() time.Time { return current })
	for i := 0; i < 5; i++ {
		if !limiter.Allow("198.51.100.4") {
			t.Fatalf("request %d was limited", i+1)
		}
	}
	if limiter.Allow("198.51.100.4") {
		t.Fatal("sixth request was not limited")
	}
	current = current.Add(time.Minute)
	if !limiter.Allow("198.51.100.4") {
		t.Fatal("request after the window was limited")
	}
}

func TestForgotReturnsTooManyRequestsAfterFiveAttempts(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, accountHTTPVerifier{}, nil, accountHTTPServiceStub{}, NewLimiter(5, time.Minute, time.Now))
	for i := 0; i < 6; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/auth/password/forgot", strings.NewReader(`{"identifier":"unknown@example.test"}`))
		request.Header.Set("X-Requested-With", "PomKita")
		router.ServeHTTP(recorder, request)
		want := http.StatusAccepted
		if i == 5 {
			want = http.StatusTooManyRequests
		}
		if recorder.Code != want {
			t.Fatalf("attempt %d: status=%d want=%d", i+1, recorder.Code, want)
		}
	}
}
