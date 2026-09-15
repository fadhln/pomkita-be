package users

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
)

// AdminService provides authenticated user administration use cases.
type AdminService interface {
	ListUsers(context.Context, appidentity.Actor, uuid.UUID) ([]appidentity.UserView, error)
	ReadUser(context.Context, appidentity.Actor, uuid.UUID) (appidentity.UserView, error)
	UpdateUser(context.Context, appidentity.Actor, uuid.UUID, appidentity.UserUpdateRequest) (appidentity.UserView, error)
	AssignRole(context.Context, appidentity.Actor, uuid.UUID, appidentity.RoleRequest) ([]appidentity.RoleView, error)
	RemoveRole(context.Context, appidentity.Actor, uuid.UUID, appidentity.RoleRequest) error
	RoleHistory(context.Context, appidentity.Actor, uuid.UUID) ([]appidentity.RoleHistoryEvent, error)
	IssuePasswordReset(context.Context, appidentity.Actor, uuid.UUID) (appidentity.PasswordResetResult, error)
}

// UpdateUserRequest is the public request for an administration user update.
type UpdateUserRequest struct {
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
}

// RoleRequest is the public request for one station role change.
type RoleRequest struct {
	StationID uuid.UUID `json:"station_id"`
	Role      string    `json:"role"`
}

// RegisterAdminRoutes registers authenticated user administration routes.
func RegisterAdminRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service AdminService) {
	router.GET("/users", transport.AuthMiddleware(verifier), listUsersHandler(sessions, service))
	router.GET("/users/:id", transport.AuthMiddleware(verifier), readUserHandler(sessions, service))
	router.PATCH("/users/:id", transport.AuthMiddleware(verifier), transport.RequireCSRF, updateUserHandler(sessions, service))
	router.POST("/users/:id/roles", transport.AuthMiddleware(verifier), transport.RequireCSRF, assignRoleHandler(sessions, service))
	router.DELETE("/users/:id/roles", transport.AuthMiddleware(verifier), transport.RequireCSRF, removeRoleHandler(sessions, service))
	router.GET("/users/:id/role-history", transport.AuthMiddleware(verifier), roleHistoryHandler(sessions, service))
	router.POST("/users/:id/password-reset", transport.AuthMiddleware(verifier), transport.RequireCSRF, passwordResetHandler(sessions, service))
}

func listUsersHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readAdminActor(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		orgID, ok := queryUUID(c, "org_id")
		if !ok {
			return
		}
		result, err := service.ListUsers(c.Request.Context(), actor, orgID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func readUserHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, userID, ok := adminTarget(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.ReadUser(c.Request.Context(), actor, userID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func updateUserHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, userID, ok := adminTarget(c, sessions)
		if !ok {
			return
		}
		var input UpdateUserRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.UpdateUser(c.Request.Context(), actor, userID, appidentity.UserUpdateRequest{DisplayName: input.DisplayName, Enabled: input.Enabled})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func assignRoleHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, userID, ok := adminTarget(c, sessions)
		if !ok {
			return
		}
		var input RoleRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.AssignRole(c.Request.Context(), actor, userID, appidentity.RoleRequest{StationID: input.StationID, Role: strings.TrimSpace(input.Role)})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func removeRoleHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, userID, ok := adminTarget(c, sessions)
		if !ok {
			return
		}
		var input RoleRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		err := service.RemoveRole(c.Request.Context(), actor, userID, appidentity.RoleRequest{StationID: input.StationID, Role: strings.TrimSpace(input.Role)})
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func roleHistoryHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, userID, ok := adminTarget(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.RoleHistory(c.Request.Context(), actor, userID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func passwordResetHandler(sessions transport.SessionService, service AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, userID, ok := adminTarget(c, sessions)
		if !ok {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.IssuePasswordReset(c.Request.Context(), actor, userID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func adminTarget(c *gin.Context, sessions transport.SessionService) (appidentity.Actor, uuid.UUID, bool) {
	userID, ok := transport.PathUUID(c, "id")
	if !ok {
		return appidentity.Actor{}, uuid.Nil, false
	}
	actor, ok := readAdminActor(c, sessions)
	return actor, userID, ok
}

func readAdminActor(c *gin.Context, sessions transport.SessionService) (appidentity.Actor, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return appidentity.Actor{}, false
	}
	view, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return appidentity.Actor{}, false
	}
	return appidentity.Actor{UserID: view.UserID, OrgID: view.OrgID, Roles: view.Roles}, true
}

func queryUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(value)
	if err != nil {
		transport.ValidationError(c)
		return uuid.Nil, false
	}
	return id, true
}
