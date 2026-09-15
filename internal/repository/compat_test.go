package repository

import (
	audit "github.com/pomkita/pomkita-be/internal/repository/audit"
	auth "github.com/pomkita/pomkita-be/internal/repository/auth"
	draft "github.com/pomkita/pomkita-be/internal/repository/draft"
	governance "github.com/pomkita/pomkita-be/internal/repository/governance"
	policy "github.com/pomkita/pomkita-be/internal/repository/policy"
	recovery "github.com/pomkita/pomkita-be/internal/repository/recovery"
	relay "github.com/pomkita/pomkita-be/internal/repository/relay"
	reporting "github.com/pomkita/pomkita-be/internal/repository/reporting"
	shift "github.com/pomkita/pomkita-be/internal/repository/shift"
	store "github.com/pomkita/pomkita-be/internal/repository/store"
	submission "github.com/pomkita/pomkita-be/internal/repository/submission"
)

type Store = store.Store
type Decimal = store.Decimal

var NewDecimal = store.NewDecimal
var Open = store.Open

type AuthRepository = auth.AuthRepository
type AuditRepository = audit.AuditRepository
type DraftRepository = draft.DraftRepository
type GovernanceRepository = governance.GovernanceRepository
type PolicyRepository = policy.PolicyRepository
type RecoveryRepository = recovery.RecoveryRepository
type RelayRepository = relay.RelayRepository
type ReportingRepository = reporting.ReportingRepository
type ShiftRepository = shift.ShiftRepository
type SubmissionRepository = submission.SubmissionRepository

var NewAuthRepository = auth.NewAuthRepository
var NewAuditRepository = audit.NewAuditRepository
var NewDraftRepository = draft.NewDraftRepository
var NewGovernanceRepository = governance.NewGovernanceRepository
var NewPolicyRepository = policy.NewPolicyRepository
var NewRecoveryRepository = recovery.NewRecoveryRepository
var NewRelayRepository = relay.NewRelayRepository
var NewReportingRepository = reporting.NewReportingRepository
var NewShiftRepository = shift.NewShiftRepository
var NewSubmissionRepository = submission.NewSubmissionRepository
var auditRowHash = audit.AuditRowHash
var decimalAddTenth = shift.DecimalAddTenth

type OrganizationModel = store.OrganizationModel
type UserModel = store.UserModel
type JWTKeyModel = store.JWTKeyModel
type SessionModel = store.SessionModel
type StationModel = store.StationModel
type ShiftModel = store.ShiftModel
type ShiftDraftModel = store.ShiftDraftModel
type ShiftReportModel = store.ShiftReportModel
type AckDecisionModel = store.AckDecisionModel
type AckHeadModel = store.AckHeadModel
type AckSupersessionModel = store.AckSupersessionModel
type AmendmentModel = store.AmendmentModel
type AmendmentItemModel = store.AmendmentItemModel
type AlertRuleModel = store.AlertRuleModel
type AlertEventModel = store.AlertEventModel
type AuditChainLockModel = store.AuditChainLockModel
type AuditLogModel = store.AuditLogModel
type AuditOutboxModel = store.AuditOutboxModel
type AuditDeniedModel = store.AuditDeniedModel
type OutboxRelayStateModel = store.OutboxRelayStateModel
type PolicySnapshotSetModel = store.PolicySnapshotSetModel
type DispenserModel = store.DispenserModel
type TankModel = store.TankModel
type NozzleModel = store.NozzleModel
type DraftReadingModel = store.DraftReadingModel
type DraftSalesModel = store.DraftSalesModel
type DraftLossModel = store.DraftLossModel
type DraftEvidenceStagingModel = store.DraftEvidenceStagingModel
type SubmitIdempotencyModel = store.SubmitIdempotencyModel
type DispenserReadingModel = store.DispenserReadingModel
type SalesDeclaredModel = store.SalesDeclaredModel
type LossIdentityModel = store.LossIdentityModel
type LossEntryModel = store.LossEntryModel
type DeliveryModel = store.DeliveryModel
type DipReadingModel = store.DipReadingModel
type ShiftTransitionModel = store.ShiftTransitionModel
type MeterResetEventModel = store.MeterResetEventModel
type ThresholdPolicyRevisionModel = store.ThresholdPolicyRevisionModel
type EvidencePolicyRevisionModel = store.EvidencePolicyRevisionModel
type EvidencePolicyTypeModel = store.EvidencePolicyTypeModel
type PolicySnapshotItemModel = store.PolicySnapshotItemModel
type DeliverySnapshotModel = store.DeliverySnapshotModel
type DipSnapshotModel = store.DipSnapshotModel
type LossExceptionModel = store.LossExceptionModel
type EvidenceEventModel = store.EvidenceEventModel
type NozzleBaselineRevisionModel = store.NozzleBaselineRevisionModel
type NozzleBaselineCurrentModel = store.NozzleBaselineCurrentModel
