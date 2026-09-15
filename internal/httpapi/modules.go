package httpapi

import (
	audit "github.com/pomkita/pomkita-be/internal/httpapi/audit"
	draft "github.com/pomkita/pomkita-be/internal/httpapi/draft"
	governance "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	policy "github.com/pomkita/pomkita-be/internal/httpapi/policy"
	reporting "github.com/pomkita/pomkita-be/internal/httpapi/reporting"
	shift "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	submission "github.com/pomkita/pomkita-be/internal/httpapi/submission"
)

type ShiftService = shift.ShiftService
type ShiftReadService = shift.ShiftReadService
type DraftService = draft.DraftService
type DraftWriteService = draft.DraftWriteService
type SubmissionService = submission.SubmissionService
type GovernanceService = governance.GovernanceService
type AmendmentService = governance.AmendmentService
type PolicyService = policy.PolicyService
type PolicyReadService = policy.PolicyReadService
type AuditService = audit.AuditService
type AuditVerificationService = audit.AuditVerificationService
type AnomalyService = reporting.AnomalyService
type ReportingService = reporting.ReportingService

type OpenShiftRequest = shift.OpenShiftRequest
type ClaimDraftRequest = draft.ClaimDraftRequest
type DraftLeaseRequest = draft.DraftLeaseRequest
type DraftReadingRequest = draft.DraftReadingRequest
type DraftSalesRequest = draft.DraftSalesRequest
type DraftLossRequest = draft.DraftLossRequest
type DraftEvidenceRequest = draft.DraftEvidenceRequest
type SubmitRequest = submission.SubmitRequest
type AcknowledgeRequest = governance.AcknowledgeRequest
type AmendmentRequest = governance.AmendmentRequest
type AmendmentDecisionRequest = governance.AmendmentDecisionRequest
type PolicyRevisionRequest = policy.PolicyRevisionRequest
