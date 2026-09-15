// Package organization contains organization administration HTTP handlers.
package organization

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	apporg "github.com/pomkita/pomkita-be/internal/service/organization"
)

// Service provides organization use cases required by the HTTP adapter.
type Service interface {
	List(context.Context, apporg.Actor) ([]apporg.Organization, error)
	Read(context.Context, apporg.Actor, uuid.UUID) (apporg.Organization, error)
	Create(context.Context, apporg.Actor, apporg.CreateRequest) (apporg.Organization, error)
	Update(context.Context, apporg.Actor, uuid.UUID, apporg.UpdateRequest) (apporg.Organization, error)
	Disable(context.Context, apporg.Actor, uuid.UUID) error
}

// RegisterRoutes registers organization administration routes.
func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service Service) {
	router.GET("/organizations", transport.AuthMiddleware(verifier), listHandler(sessions, service))
	router.POST("/organizations", transport.AuthMiddleware(verifier), transport.RequireCSRF, createHandler(sessions, service))
	router.GET("/organizations/:id", transport.AuthMiddleware(verifier), readHandler(sessions, service))
	router.PATCH("/organizations/:id", transport.AuthMiddleware(verifier), transport.RequireCSRF, updateHandler(sessions, service))
	router.POST("/organizations/:id/disable", transport.AuthMiddleware(verifier), transport.RequireCSRF, disableHandler(sessions, service))
}

func listHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readActor(c, sessions)
		if !ok || !allowed(c, actor) {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.List(c.Request.Context(), actor)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func createHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readActor(c, sessions)
		if !ok || !isRole(actor, "Superadmin") {
			if ok {
				transport.WriteError(c, http.StatusForbidden, "organization_administration_forbidden")
			}
			return
		}
		var input apporg.CreateRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if input.FirstStation == nil {
			transport.WriteError(c, http.StatusUnprocessableEntity, "first_station_required")
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.Create(c.Request.Context(), actor, input)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusCreated, result)
	}
}

func readHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readActor(c, sessions)
		if !ok || !allowed(c, actor) {
			return
		}
		orgID, ok := transport.PathUUID(c, "id")
		if !ok || service == nil {
			if service == nil {
				transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		result, err := service.Read(c.Request.Context(), actor, orgID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func updateHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readActor(c, sessions)
		if !ok || !allowed(c, actor) {
			return
		}
		orgID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		var input apporg.UpdateRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.Update(c.Request.Context(), actor, orgID, input)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func disableHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readActor(c, sessions)
		if !ok || !isRole(actor, "Superadmin") {
			if ok {
				transport.WriteError(c, http.StatusForbidden, "organization_administration_forbidden")
			}
			return
		}
		orgID, ok := transport.PathUUID(c, "id")
		if !ok || service == nil {
			if service == nil {
				transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		if err := service.Disable(c.Request.Context(), actor, orgID); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func readActor(c *gin.Context, sessions transport.SessionService) (apporg.Actor, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return apporg.Actor{}, false
	}
	view, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return apporg.Actor{}, false
	}
	return apporg.Actor{UserID: view.UserID, OrgID: view.OrgID, Roles: view.Roles}, true
}

func allowed(c *gin.Context, actor apporg.Actor) bool {
	if isRole(actor, "Superadmin") || isRole(actor, "Owner") {
		return true
	}
	transport.WriteError(c, http.StatusForbidden, "organization_administration_forbidden")
	return false
}

func isRole(actor apporg.Actor, role string) bool {
	for _, candidate := range actor.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}
