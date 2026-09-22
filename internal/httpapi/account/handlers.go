// Package account contains account and password HTTP handlers.
package account

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fadhln/pomkita-be/internal/httpapi/transport"
	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	appaccount "github.com/fadhln/pomkita-be/internal/service/account"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Service provides account use cases required by the HTTP adapter.
type Service interface {
	Read(context.Context, uuid.UUID) (appaccount.Profile, error)
	Update(context.Context, uuid.UUID, uuid.UUID, appaccount.UpdateRequest) (appaccount.Profile, error)
	ChangePassword(context.Context, uuid.UUID, uuid.UUID, appaccount.PasswordRequest) error
	Forgot(context.Context, string) error
	Reset(context.Context, appaccount.ResetRequest) error
}

// RateLimiter limits anonymous password requests in one process.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	visits map[string][]time.Time
}

// NewLimiter creates a client-address rate limiter.
func NewLimiter(limit int, window time.Duration, now func() time.Time) *RateLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{limit: limit, window: window, now: now, visits: make(map[string][]time.Time)}
}

// Allow records one request for address and reports if it is within the limit.
func (l *RateLimiter) Allow(address string) bool {
	if l == nil {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	visits := l.visits[address][:0]
	for _, visit := range l.visits[address] {
		if visit.After(cutoff) {
			visits = append(visits, visit)
		}
	}
	if len(visits) >= l.limit {
		l.visits[address] = visits
		return false
	}
	l.visits[address] = append(visits, now)
	return true
}

// RegisterRoutes registers account and anonymous password routes.
func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service Service, limiter *RateLimiter) {
	router.GET("/account", transport.AuthMiddleware(verifier), readHandler(sessions, service))
	router.PATCH("/account", transport.AuthMiddleware(verifier), transport.RequireCSRF, updateHandler(sessions, service))
	router.POST("/account/password", transport.AuthMiddleware(verifier), transport.RequireCSRF, passwordHandler(sessions, service))
	router.POST("/auth/password/forgot", transport.RequireCSRF, limitedHandler(limiter, forgotHandler(service)))
	router.POST("/auth/password/reset", transport.RequireCSRF, limitedHandler(limiter, resetHandler(service)))
}

type updateInput struct {
	DisplayName *string `json:"display_name"`
	Username    *string `json:"username"`
}
type passwordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ForgotRequest is the public password reset request.
type ForgotRequest struct {
	Identifier string `json:"identifier"`
}

// ResetRequest is the public password reset confirmation request.
type ResetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func readHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, ok := actorSession(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		profile, err := service.Read(c.Request.Context(), view.UserID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, profile)
	}
}

func updateHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, ok := actorSession(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input updateInput
		if !transport.DecodeRequest(c, &input) {
			return
		}
		claims := c.MustGet("jwt_claims").(appjwt.Claims)
		profile, err := service.Update(c.Request.Context(), view.UserID, claims.JTI, appaccount.UpdateRequest{DisplayName: input.DisplayName, Username: input.Username})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, profile)
	}
}

func passwordHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, ok := actorSession(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input passwordInput
		if !transport.DecodeRequest(c, &input) {
			return
		}
		claims := c.MustGet("jwt_claims").(appjwt.Claims)
		if err := service.ChangePassword(c.Request.Context(), view.UserID, claims.JTI, appaccount.PasswordRequest{CurrentPassword: input.CurrentPassword, NewPassword: input.NewPassword}); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func forgotHandler(service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input ForgotRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if err := service.Forgot(c.Request.Context(), input.Identifier); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusAccepted)
	}
}

func resetHandler(service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input ResetRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if err := service.Reset(c.Request.Context(), appaccount.ResetRequest{Token: input.Token, Password: input.Password}); err != nil {
			if errors.Is(err, appaccount.ErrInvalidToken) {
				transport.WriteError(c, http.StatusBadRequest, "INVALID_TOKEN")
				return
			}
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func actorSession(c *gin.Context, sessions transport.SessionService) (transport.SessionView, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return transport.SessionView{}, false
	}
	view, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return transport.SessionView{}, false
	}
	return view, true
}

func limitedHandler(limiter *RateLimiter, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter != nil && !limiter.Allow(clientAddress(c.Request)) {
			transport.WriteError(c, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next(c)
	}
}

func clientAddress(request *http.Request) string {
	address := request.RemoteAddr
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return strings.TrimSpace(address)
}
