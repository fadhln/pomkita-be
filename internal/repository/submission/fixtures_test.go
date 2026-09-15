package submission

import (
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type UserModel = store.UserModel
type DispenserModel = store.DispenserModel
type NozzleModel = store.NozzleModel
type AuditLogModel = store.AuditLogModel

var newAuthTestStore = testsupport.NewStore
