package draft

import (
	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type StationModel = store.StationModel
type UserModel = store.UserModel
type ShiftModel = store.ShiftModel
type Store = store.Store
type Decimal = store.Decimal
type DraftEvidenceStagingModel = store.DraftEvidenceStagingModel
type DraftLossModel = store.DraftLossModel
type DraftReadingModel = store.DraftReadingModel
type DraftSalesModel = store.DraftSalesModel
type ShiftDraftModel = store.ShiftDraftModel

var newAuthTestStore = testsupport.NewStore
