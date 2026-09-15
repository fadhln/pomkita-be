package governance

import (
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type UserModel = store.UserModel
type PolicySnapshotSetModel = store.PolicySnapshotSetModel
type AuditLogModel = store.AuditLogModel
type DispenserModel = store.DispenserModel

var newAuthTestStore = testsupport.NewStore
