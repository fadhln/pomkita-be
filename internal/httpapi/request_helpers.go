package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func containsUUID(values []uuid.UUID, target uuid.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		validationError(c)
		return uuid.Nil, false
	}
	return id, true
}

func validDecimal(value string) bool { return decimalPattern.MatchString(value) }

func validationError(c *gin.Context) { writeError(c, http.StatusBadRequest, "validation_error") }

func decodeRequest(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || ensureEndOfJSON(decoder) != nil {
		validationError(c)
		return false
	}
	return true
}
