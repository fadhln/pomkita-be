// Package station contains station administration HTTP handlers.
package station

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appstation "github.com/pomkita/pomkita-be/internal/service/station"
)

// Service provides station use cases required by the HTTP adapter.
type Service interface {
	List(context.Context, appstation.Actor, uuid.UUID) ([]appstation.Station, error)
	Read(context.Context, appstation.Actor, uuid.UUID, uuid.UUID) (appstation.Station, error)
	Create(context.Context, appstation.Actor, uuid.UUID, appstation.CreateRequest) (appstation.Station, error)
	Update(context.Context, appstation.Actor, uuid.UUID, uuid.UUID, appstation.UpdateRequest) (appstation.Station, error)
	Disable(context.Context, appstation.Actor, uuid.UUID, uuid.UUID) error
}

// RegisterRoutes registers station administration routes.
func RegisterRoutes(router gin.IRoutes, verifier transport.TokenVerifier, sessions transport.SessionService, service Service) {
	router.GET("/stations", transport.AuthMiddleware(verifier), listHandler(sessions, service))
	router.POST("/stations", transport.AuthMiddleware(verifier), transport.RequireCSRF, createHandler(sessions, service))
	router.GET("/stations/:id", transport.AuthMiddleware(verifier), readHandler(sessions, service))
	router.PATCH("/stations/:id", transport.AuthMiddleware(verifier), transport.RequireCSRF, updateHandler(sessions, service))
	router.POST("/stations/:id/disable", transport.AuthMiddleware(verifier), transport.RequireCSRF, disableHandler(sessions, service))
}

func listHandler(sessions transport.SessionService, service Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := readActor(c, sessions)
		if !ok || !allowed(c, actor) {
			return
		}
		orgID, ok := requestedOrganization(c, actor)
		if !ok || service == nil {
			if service == nil {
				transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		result, err := service.List(c.Request.Context(), actor, orgID)
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
		if !ok || !allowed(c, actor) {
			return
		}
		orgID, ok := requestedOrganization(c, actor)
		if !ok {
			return
		}
		var input appstation.CreateRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if strings.TrimSpace(input.Name) == "" {
			transport.WriteError(c, http.StatusUnprocessableEntity, "invalid_station_request")
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.Create(c.Request.Context(), actor, orgID, input)
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
		stationID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		orgID, ok := requestedOrganization(c, actor)
		if !ok || service == nil {
			if service == nil {
				transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		result, err := service.Read(c.Request.Context(), actor, orgID, stationID)
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
		stationID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		orgID, ok := requestedOrganization(c, actor)
		if !ok {
			return
		}
		var input appstation.UpdateRequest
		if !transport.DecodeRequest(c, &input) {
			return
		}
		if service == nil {
			transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		result, err := service.Update(c.Request.Context(), actor, orgID, stationID, input)
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
		if !ok || !allowed(c, actor) {
			return
		}
		stationID, ok := transport.PathUUID(c, "id")
		if !ok {
			return
		}
		orgID, ok := requestedOrganization(c, actor)
		if !ok || service == nil {
			if service == nil {
				transport.WriteError(c, http.StatusInternalServerError, "internal_error")
			}
			return
		}
		if err := service.Disable(c.Request.Context(), actor, orgID, stationID); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func readActor(c *gin.Context, sessions transport.SessionService) (appstation.Actor, bool) {
	if sessions == nil {
		transport.WriteError(c, http.StatusInternalServerError, "internal_error")
		return appstation.Actor{}, false
	}
	view, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return appstation.Actor{}, false
	}
	return appstation.Actor{UserID: view.UserID, OrgID: view.OrgID, Roles: view.Roles}, true
}

func requestedOrganization(c *gin.Context, actor appstation.Actor) (uuid.UUID, bool) {
	if !isRole(actor, "Superadmin") {
		if raw := c.Query("org_id"); raw != "" {
			orgID, err := uuid.Parse(raw)
			if err != nil {
				transport.ValidationError(c)
				return uuid.Nil, false
			}
			return orgID, true
		}
		return actor.OrgID, true
	}
	raw := c.Query("org_id")
	if raw == "" {
		transport.WriteError(c, http.StatusUnprocessableEntity, "organization_scope_required")
		return uuid.Nil, false
	}
	orgID, err := uuid.Parse(raw)
	if err != nil {
		transport.ValidationError(c)
		return uuid.Nil, false
	}
	return orgID, true
}

func allowed(c *gin.Context, actor appstation.Actor) bool {
	if isRole(actor, "Superadmin") || isRole(actor, "Owner") {
		return true
	}
	transport.WriteError(c, http.StatusForbidden, "station_administration_forbidden")
	return false
}

func isRole(actor appstation.Actor, role string) bool {
	for _, candidate := range actor.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}
