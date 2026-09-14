package governance

import "github.com/pomkita/pomkita-be/internal/domain"

var (
	// ErrEvidenceInvalid identifies an invalid evidence policy evaluation.
	ErrEvidenceInvalid = domain.NewError(domain.CategoryValidation, "invalid_evidence_request")
	// ErrEvidenceRequired identifies a wajib loss without enough evidence.
	ErrEvidenceRequired = domain.NewError(domain.CategoryValidation, "evidence_required")
	// ErrEvidenceExceptionRequired identifies an opsional loss without evidence or exception.
	ErrEvidenceExceptionRequired = domain.NewError(domain.CategoryValidation, "evidence_exception_required")
	// ErrEvidenceExceptionForbidden identifies an exception that conflicts with the policy.
	ErrEvidenceExceptionForbidden = domain.NewError(domain.CategoryValidation, "evidence_exception_forbidden")
)

// EvidenceValidationRequest contains one loss evidence result.
type EvidenceValidationRequest struct {
	Mode           string
	MinimumCount   int
	FinalizedCount int
	HasException   bool
}

// ValidateEvidence applies one immutable evidence policy to one loss.
func ValidateEvidence(request EvidenceValidationRequest) error {
	if request.MinimumCount < 0 || request.FinalizedCount < 0 {
		return ErrEvidenceInvalid
	}
	switch request.Mode {
	case "wajib":
		if request.HasException {
			return ErrEvidenceExceptionForbidden
		}
		if request.FinalizedCount < request.MinimumCount || request.MinimumCount == 0 {
			return ErrEvidenceRequired
		}
		return nil
	case "opsional":
		if request.FinalizedCount == 0 && !request.HasException {
			return ErrEvidenceExceptionRequired
		}
		if request.FinalizedCount > 0 && request.HasException {
			return ErrEvidenceExceptionForbidden
		}
		return nil
	default:
		return ErrEvidenceInvalid
	}
}
