package shift

import (
	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type UserModel = store.UserModel
type DispenserModel = store.DispenserModel
type NozzleModel = store.NozzleModel
type AuditLogModel = store.AuditLogModel
type DispenserReadingModel = store.DispenserReadingModel
type PolicySnapshotSetModel = store.PolicySnapshotSetModel
type ShiftReportModel = store.ShiftReportModel
type Store = store.Store
type Decimal = store.Decimal
type ShiftDraftModel = store.ShiftDraftModel
type ShiftModel = store.ShiftModel
type StationModel = store.StationModel

var newAuthTestStore = testsupport.NewStore
