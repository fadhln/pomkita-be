// Package session provides session HTTP handlers.
package session

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appauth "github.com/pomkita/pomkita-be/internal/service/auth"
)

// Service provides the session use cases required by HTTP handlers.
type Service interface {
	Login(context.Context, string, string) (string, appjwt.Claims, error)
	Logout(context.Context, uuid.UUID) error
	ReadSession(context.Context, string) (appjwt.SessionView, error)
}

// LoginRequest contains session login credentials.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRoutes registers login, logout, and session routes.
func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, service Service, secure bool) {
	router.POST("/login", transport.RequireCSRF, loginHandler(service, secure))
	router.DELETE("/logout", transport.AuthMiddleware(verifier), transport.RequireCSRF, logoutHandler(service, secure))
	router.GET("/session", transport.AuthMiddleware(verifier), sessionHandler(service))
}

func loginHandler(service Service, secure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input LoginRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if strings.TrimSpace(input.Email) == "" || input.Password == "" {
			transport.ValidationError(c)
			return
		}
		token, _, err := service.Login(c.Request.Context(), input.Email, input.Password)
		if err != nil {
			if errors.Is(err, appauth.ErrInvalidCredentials) {
				transport.WriteError(c, http.StatusUnauthorized, "invalid_credentials")
				return
			}
			_ = c.Error(err)
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("pomkita_session", token, 900, "/", "", secure, true)
		c.Status(http.StatusOK)
	}
}

func logoutHandler(service Service, secure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		claims := c.MustGet("jwt_claims").(appjwt.Claims)
		if err := service.Logout(c.Request.Context(), claims.JTI); err != nil {
			_ = c.Error(err)
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("pomkita_session", "", -1, "/", "", secure, true)
		c.Status(http.StatusNoContent)
	}
}

func sessionHandler(service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		view, err := service.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, view)
	}
}
