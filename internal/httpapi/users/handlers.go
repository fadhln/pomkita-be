// Package users contains user administration HTTP handlers.
package users

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
)

// Service provides invitation and activation use cases.
type Service interface {
	Invite(context.Context, appidentity.InvitationRequest) error
	Accept(context.Context, appidentity.AcceptanceRequest) error
}

// InviteRequest is the public request for a user invitation.
type InviteRequest struct {
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	StationID   uuid.UUID `json:"station_id"`
	OrgID       uuid.UUID `json:"org_id"`
}

// AcceptRequest is the public request for invitation activation.
type AcceptRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// RegisterRoutes registers user invitation and public activation routes.
func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service Service) {
	router.POST("/users", transport.AuthMiddleware(verifier), transport.RequireCSRF, inviteHandler(sessions, service))
	router.POST("/auth/invitations/accept", transport.RequireCSRF, acceptHandler(service))
}

func inviteHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessions == nil || service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input InviteRequest
		if !transport.DecodeRequest(c, &input) || input.StationID == uuid.Nil {
			return
		}
		session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		role := highestAdministrationRole(session.Roles)
		targetOrgID := session.OrgID
		if role == "Owner" && input.OrgID != uuid.Nil {
			targetOrgID = input.OrgID
		}
		if role == "Superadmin" {
			targetOrgID = input.OrgID
		}
		err = service.Invite(c.Request.Context(), appidentity.InvitationRequest{
			ActorID: session.UserID, ActorOrgID: session.OrgID, ActorRole: role,
			TargetOrgID: targetOrgID, Email: input.Email, DisplayName: input.DisplayName,
			Role: input.Role, StationID: input.StationID,
		})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func acceptHandler(service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input AcceptRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		err := service.Accept(c.Request.Context(), appidentity.AcceptanceRequest{
			Token: input.Token, Username: input.Username, Password: input.Password, DisplayName: input.DisplayName,
		})
		if err != nil {
			if errors.Is(err, appidentity.ErrInvalidToken) {
				transport.WriteError(c, http.StatusBadRequest, "INVALID_TOKEN")
				return
			}
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func highestAdministrationRole(roles []string) string {
	for _, role := range roles {
		if role == "Superadmin" {
			return role
		}
	}
	for _, role := range roles {
		if role == "Owner" {
			return role
		}
	}
	return strings.TrimSpace("")
}
