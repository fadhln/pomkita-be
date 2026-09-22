package recovery

import (
	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type StationModel = store.StationModel
type UserModel = store.UserModel
type AuditLogModel = store.AuditLogModel
type Store = store.Store
type ShiftDraftModel = store.ShiftDraftModel
type ShiftModel = store.ShiftModel

var newAuthTestStore = testsupport.NewStore
