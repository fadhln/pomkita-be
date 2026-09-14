package governance

import "testing"

func TestEvidenceService_Validate_WajibRejectsMissingEvidence(t *testing.T) {
	request := EvidenceValidationRequest{Mode: "wajib", MinimumCount: 1, FinalizedCount: 0}
	if err := ValidateEvidence(request); err != ErrEvidenceRequired {
		t.Fatalf("evidence error: got %v, want %v", err, ErrEvidenceRequired)
	}
}

func TestEvidenceService_Validate_OpsionalRequiresAnExceptionWithoutEvidence(t *testing.T) {
	if err := ValidateEvidence(EvidenceValidationRequest{Mode: "opsional", MinimumCount: 1}); err != ErrEvidenceExceptionRequired {
		t.Fatalf("evidence error: got %v, want %v", err, ErrEvidenceExceptionRequired)
	}
	if err := ValidateEvidence(EvidenceValidationRequest{Mode: "opsional", MinimumCount: 1, FinalizedCount: 1, HasException: true}); err != ErrEvidenceExceptionForbidden {
		t.Fatalf("evidence error with evidence: got %v, want %v", err, ErrEvidenceExceptionForbidden)
	}
}
