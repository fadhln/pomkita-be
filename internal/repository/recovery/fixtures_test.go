package recovery

import (
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type StationModel = store.StationModel
type UserModel = store.UserModel
type AuditLogModel = store.AuditLogModel

var newAuthTestStore = testsupport.NewStore
