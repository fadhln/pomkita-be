package scope

import (
	"github.com/fadhln/pomkita-be/internal/domain"
	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	// ErrOrganizationDisabled rejects an operational write in a disabled organization.
	ErrOrganizationDisabled = domain.NewError(domain.CategoryConflict, "org_disabled")
	// ErrStationDisabled rejects an operational write in a disabled station.
	ErrStationDisabled = domain.NewError(domain.CategoryConflict, "station_disabled")
)

// RequireEnabled rejects writes in a disabled organization or station.
func RequireEnabled(tx *gorm.DB, orgID, stationID uuid.UUID) error {
	var organization store.OrganizationModel
	if err := tx.Select("enabled").Where("org_id = ?", orgID).First(&organization).Error; err != nil {
		return err
	}
	if !organization.Enabled {
		return ErrOrganizationDisabled
	}
	var station store.StationModel
	if err := tx.Select("enabled").Where("org_id = ? and station_id = ?", orgID, stationID).First(&station).Error; err != nil {
		return err
	}
	if !station.Enabled {
		return ErrStationDisabled
	}
	return nil
}
