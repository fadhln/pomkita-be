package governance

import (
	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type UserModel = store.UserModel
type PolicySnapshotSetModel = store.PolicySnapshotSetModel
type AuditLogModel = store.AuditLogModel
type DispenserModel = store.DispenserModel
type Store = store.Store
type Decimal = store.Decimal
type AckDecisionModel = store.AckDecisionModel
type AckHeadModel = store.AckHeadModel
type AckSupersessionModel = store.AckSupersessionModel
type AlertEventModel = store.AlertEventModel
type AlertRuleModel = store.AlertRuleModel
type AmendmentItemModel = store.AmendmentItemModel
type AmendmentModel = store.AmendmentModel
type DispenserReadingModel = store.DispenserReadingModel
type LossEntryModel = store.LossEntryModel
type SalesDeclaredModel = store.SalesDeclaredModel
type ShiftModel = store.ShiftModel
type ShiftReportModel = store.ShiftReportModel
type StationModel = store.StationModel

var newAuthTestStore = testsupport.NewStore
