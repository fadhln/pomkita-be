package submission

import (
	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
)

type OrganizationModel = store.OrganizationModel
type UserModel = store.UserModel
type DispenserModel = store.DispenserModel
type NozzleModel = store.NozzleModel
type AuditLogModel = store.AuditLogModel
type Store = store.Store
type Decimal = store.Decimal
type AckHeadModel = store.AckHeadModel
type AlertEventModel = store.AlertEventModel
type AlertRuleModel = store.AlertRuleModel
type DispenserReadingModel = store.DispenserReadingModel
type DraftEvidenceStagingModel = store.DraftEvidenceStagingModel
type DraftLossModel = store.DraftLossModel
type DraftReadingModel = store.DraftReadingModel
type DraftSalesModel = store.DraftSalesModel
type EvidenceEventModel = store.EvidenceEventModel
type EvidencePolicyRevisionModel = store.EvidencePolicyRevisionModel
type EvidencePolicyTypeModel = store.EvidencePolicyTypeModel
type LossEntryModel = store.LossEntryModel
type LossIdentityModel = store.LossIdentityModel
type MeterResetEventModel = store.MeterResetEventModel
type PolicySnapshotItemModel = store.PolicySnapshotItemModel
type PolicySnapshotSetModel = store.PolicySnapshotSetModel
type SalesDeclaredModel = store.SalesDeclaredModel
type ShiftDraftModel = store.ShiftDraftModel
type ShiftModel = store.ShiftModel
type ShiftReportModel = store.ShiftReportModel
type StationModel = store.StationModel
type SubmitIdempotencyModel = store.SubmitIdempotencyModel
type ThresholdPolicyRevisionModel = store.ThresholdPolicyRevisionModel

var newAuthTestStore = testsupport.NewStore
