package gormstore

import "testing"

func TestPersistenceModels_UseExplicitTableNames(t *testing.T) {
	cases := []struct {
		name  string
		model interface{ TableName() string }
		want  string
	}{
		{name: "organization", model: OrganizationModel{}, want: "organizations"},
		{name: "user", model: UserModel{}, want: "users"},
		{name: "JWT key", model: JWTKeyModel{}, want: "jwt_keys"},
		{name: "session", model: SessionModel{}, want: "sessions"},
		{name: "station", model: StationModel{}, want: "stations"},
		{name: "shift", model: ShiftModel{}, want: "shifts"},
		{name: "draft", model: ShiftDraftModel{}, want: "shift_drafts"},
		{name: "report", model: ShiftReportModel{}, want: "shift_reports"},
		{name: "ack decision", model: AckDecisionModel{}, want: "ack_decisions"},
		{name: "ack head", model: AckHeadModel{}, want: "ack_head"},
		{name: "ack supersession", model: AckSupersessionModel{}, want: "ack_supersessions"},
		{name: "dispenser", model: DispenserModel{}, want: "dispensers"},
		{name: "tank", model: TankModel{}, want: "tanks"},
		{name: "nozzle", model: NozzleModel{}, want: "nozzles"},
		{name: "draft reading", model: DraftReadingModel{}, want: "draft_readings"},
		{name: "draft sales", model: DraftSalesModel{}, want: "draft_sales"},
		{name: "draft loss", model: DraftLossModel{}, want: "draft_losses"},
		{name: "draft evidence", model: DraftEvidenceStagingModel{}, want: "draft_evidence_staging"},
		{name: "submit idempotency", model: SubmitIdempotencyModel{}, want: "submit_idempotency"},
		{name: "dispenser reading", model: DispenserReadingModel{}, want: "dispenser_readings"},
		{name: "sales declared", model: SalesDeclaredModel{}, want: "sales_declared"},
		{name: "loss identity", model: LossIdentityModel{}, want: "loss_identity"},
		{name: "loss entry", model: LossEntryModel{}, want: "loss_entries"},
		{name: "delivery", model: DeliveryModel{}, want: "deliveries"},
		{name: "dip reading", model: DipReadingModel{}, want: "dip_readings"},
		{name: "shift transition", model: ShiftTransitionModel{}, want: "shift_transitions"},
		{name: "meter reset", model: MeterResetEventModel{}, want: "meter_reset_events"},
		{name: "threshold policy", model: ThresholdPolicyRevisionModel{}, want: "threshold_policy_revisions"},
		{name: "evidence policy", model: EvidencePolicyRevisionModel{}, want: "evidence_policy_revisions"},
		{name: "evidence policy type", model: EvidencePolicyTypeModel{}, want: "evidence_policy_types"},
		{name: "policy snapshot item", model: PolicySnapshotItemModel{}, want: "policy_snapshot_items"},
		{name: "delivery snapshot", model: DeliverySnapshotModel{}, want: "delivery_snapshots"},
		{name: "dip snapshot", model: DipSnapshotModel{}, want: "dip_snapshots"},
		{name: "loss exception", model: LossExceptionModel{}, want: "loss_exception"},
		{name: "evidence event", model: EvidenceEventModel{}, want: "evidence_event"},
		{name: "baseline revision", model: NozzleBaselineRevisionModel{}, want: "nozzle_baseline_revisions"},
		{name: "baseline current", model: NozzleBaselineCurrentModel{}, want: "nozzle_baseline_current"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.model.TableName(); got != testCase.want {
				t.Fatalf("table name: got %q, want %q", got, testCase.want)
			}
		})
	}
}
