package gormstore

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OrganizationModel maps the organizations table.
type OrganizationModel struct {
	OrgID     uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	Name      string    `gorm:"column:name;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the organizations table name.
func (OrganizationModel) TableName() string { return "organizations" }

// UserModel maps credentials and account state for one user.
type UserModel struct {
	UserID       uuid.UUID `gorm:"column:user_id;type:uuid;primaryKey"`
	OrgID        uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	DisplayName  string    `gorm:"column:display_name;not null"`
	Email        string    `gorm:"column:email;not null"`
	PasswordHash string    `gorm:"column:password_hash;not null"`
	Enabled      bool      `gorm:"column:enabled;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the users table name.
func (UserModel) TableName() string { return "users" }

// JWTKeyModel maps signing-key metadata. Secret values stay outside PostgreSQL.
type JWTKeyModel struct {
	KID            string    `gorm:"column:kid;primaryKey"`
	SecretRef      string    `gorm:"column:secret_ref;not null"`
	Status         string    `gorm:"column:status;not null"`
	ActivatedAt    time.Time `gorm:"column:activated_at;not null"`
	MaxTokenExpiry time.Time `gorm:"column:max_token_expiry;not null"`
}

// TableName returns the jwt_keys table name.
func (JWTKeyModel) TableName() string { return "jwt_keys" }

// SessionModel maps one issued session token.
type SessionModel struct {
	JTI          uuid.UUID  `gorm:"column:jti;type:uuid;primaryKey"`
	UserID       uuid.UUID  `gorm:"column:user_id;type:uuid;not null"`
	KID          string     `gorm:"column:kid;not null"`
	IssuedAt     time.Time  `gorm:"column:issued_at;not null"`
	ExpiresAt    time.Time  `gorm:"column:expires_at;not null"`
	LastActiveAt time.Time  `gorm:"column:last_active_at;not null"`
	RevokedAt    *time.Time `gorm:"column:revoked_at"`
}

// TableName returns the sessions table name.
func (SessionModel) TableName() string { return "sessions" }

type legacySessionModel struct {
	JTI          uuid.UUID  `gorm:"column:jti;type:uuid;primaryKey"`
	KID          string     `gorm:"column:kid;not null"`
	IssuedAt     time.Time  `gorm:"column:issued_at;not null"`
	ExpiresAt    time.Time  `gorm:"column:expires_at;not null"`
	LastActiveAt time.Time  `gorm:"column:last_active_at;not null"`
	RevokedAt    *time.Time `gorm:"column:revoked_at"`
}

func (legacySessionModel) TableName() string { return "sessions" }

// StationModel maps the tenant-scoped stations table.
type StationModel struct {
	OrgID     uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	StationID uuid.UUID `gorm:"column:station_id;type:uuid;primaryKey"`
	Timezone  string    `gorm:"column:timezone;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the stations table name.
func (StationModel) TableName() string { return "stations" }

// ShiftModel maps the shift state and chronology columns.
type ShiftModel struct {
	ShiftID           uuid.UUID       `gorm:"column:shift_id;type:uuid;primaryKey"`
	OrgID             uuid.UUID       `gorm:"column:org_id;type:uuid;not null;index:idx_shifts_tenant_seq,priority:1"`
	StationID         uuid.UUID       `gorm:"column:station_id;type:uuid;not null;index:idx_shifts_tenant_seq,priority:2"`
	StationSeq        int64           `gorm:"column:station_seq;not null;index:idx_shifts_tenant_seq,priority:3,unique"`
	SupervisorID      uuid.UUID       `gorm:"column:supervisor_id;type:uuid;not null"`
	OpenedAt          time.Time       `gorm:"column:opened_at;not null"`
	ClosedAt          *time.Time      `gorm:"column:closed_at"`
	TimezoneSnapshot  string          `gorm:"column:timezone_snapshot;not null"`
	BusinessDate      string          `gorm:"column:business_date;type:date;not null"`
	Status            string          `gorm:"column:status;not null"`
	CurrentReportID   *uuid.UUID      `gorm:"column:current_report_id;type:uuid"`
	OriginalEventDate *time.Time      `gorm:"column:original_event_date;type:date"`
	ShiftKE           *int            `gorm:"column:shift_ke"`
	Backfilled        bool            `gorm:"column:backfilled;not null"`
	BackfillApprover  *uuid.UUID      `gorm:"column:backfill_approver;type:uuid"`
	BackfillApproved  *time.Time      `gorm:"column:backfill_approved_at"`
	BackfillReason    *string         `gorm:"column:backfill_reason"`
	PriceMapSnapshot  json.RawMessage `gorm:"column:shift_price_map_snapshot;type:jsonb;not null"`
	PriceMapHash      []byte          `gorm:"column:shift_price_map_hash;not null"`
	CreatedAt         time.Time       `gorm:"column:created_at;not null"`
}

// TableName returns the shifts table name.
func (ShiftModel) TableName() string { return "shifts" }

// ShiftDraftModel maps the draft lease and revision columns.
type ShiftDraftModel struct {
	DraftID        uuid.UUID  `gorm:"column:draft_id;type:uuid;primaryKey"`
	OrgID          uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID      uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	ShiftID        uuid.UUID  `gorm:"column:shift_id;type:uuid;not null;uniqueIndex:idx_shift_drafts_shift"`
	OwnedBy        *uuid.UUID `gorm:"column:owned_by;type:uuid"`
	ClaimToken     *uuid.UUID `gorm:"column:claim_token;type:uuid"`
	ClaimExpiresAt *time.Time `gorm:"column:claim_expires_at"`
	Status         string     `gorm:"column:status;not null"`
	Revision       int        `gorm:"column:revision;not null"`
	UpdatedBy      *uuid.UUID `gorm:"column:updated_by;type:uuid"`
	RecoveryCount  int        `gorm:"column:recovery_count;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null"`
}

// TableName returns the shift_drafts table name.
func (ShiftDraftModel) TableName() string { return "shift_drafts" }

// ShiftReportModel maps the immutable report identity columns.
type ShiftReportModel struct {
	ReportID       uuid.UUID  `gorm:"column:report_id;type:uuid;primaryKey"`
	OrgID          uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID      uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	ShiftID        uuid.UUID  `gorm:"column:shift_id;type:uuid;not null"`
	VersionNo      int        `gorm:"column:version_no;not null"`
	SupersedesID   *uuid.UUID `gorm:"column:supersedes_report_id;type:uuid"`
	Status         string     `gorm:"column:status;not null"`
	SubmittedBy    uuid.UUID  `gorm:"column:submitted_by;type:uuid;not null"`
	SubmittedAt    time.Time  `gorm:"column:submitted_at;not null"`
	PolicySnapshot uuid.UUID  `gorm:"column:policy_snapshot_set_id;type:uuid;not null"`
}

// TableName returns the shift_reports table name.
func (ShiftReportModel) TableName() string { return "shift_reports" }

// AckDecisionModel maps one immutable acknowledgement decision.
type AckDecisionModel struct {
	AckID            uuid.UUID `gorm:"column:ack_id;type:uuid;primaryKey"`
	OrgID            uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID        uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID          uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID         uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	VersionNo        int       `gorm:"column:version_no;not null"`
	AckSeq           int64     `gorm:"column:ack_seq;not null"`
	Decision         string    `gorm:"column:decision;not null"`
	ActorUserID      uuid.UUID `gorm:"column:actor_user_id;type:uuid;not null"`
	DecidedAt        time.Time `gorm:"column:decided_at;not null"`
	RejectionReason  *string   `gorm:"column:rejection_reason"`
	IsSuperadmin     bool      `gorm:"column:is_superadmin;not null"`
	IsBreakGlass     bool      `gorm:"column:is_break_glass;not null"`
	BreakGlassReason *string   `gorm:"column:break_glass_reason"`
}

// TableName returns the ack_decisions table name.
func (AckDecisionModel) TableName() string { return "ack_decisions" }

// AckHeadModel maps the active acknowledgement pointer for one report version.
type AckHeadModel struct {
	OrgID       uuid.UUID  `gorm:"column:org_id;type:uuid;primaryKey"`
	StationID   uuid.UUID  `gorm:"column:station_id;type:uuid;primaryKey"`
	ShiftID     uuid.UUID  `gorm:"column:shift_id;type:uuid;primaryKey"`
	ReportID    uuid.UUID  `gorm:"column:report_id;type:uuid;primaryKey"`
	VersionNo   int        `gorm:"column:version_no;primaryKey"`
	ActiveAckID *uuid.UUID `gorm:"column:active_ack_id;type:uuid"`
}

// TableName returns the ack_head table name.
func (AckHeadModel) TableName() string { return "ack_head" }

// AckSupersessionModel maps an acknowledgement replacement relation.
type AckSupersessionModel struct {
	SupersessionID       uuid.UUID `gorm:"column:supersession_id;type:uuid;primaryKey"`
	OldOrgID             uuid.UUID `gorm:"column:old_org_id;type:uuid;not null"`
	OldStationID         uuid.UUID `gorm:"column:old_station_id;type:uuid;not null"`
	OldShiftID           uuid.UUID `gorm:"column:old_shift_id;type:uuid;not null"`
	OldReportID          uuid.UUID `gorm:"column:old_report_id;type:uuid;not null"`
	OldVersionNo         int       `gorm:"column:old_version_no;not null"`
	SupersededAckID      uuid.UUID `gorm:"column:superseded_ack_id;type:uuid;not null"`
	ReplacementOrgID     uuid.UUID `gorm:"column:replacement_org_id;type:uuid;not null"`
	ReplacementStationID uuid.UUID `gorm:"column:replacement_station_id;type:uuid;not null"`
	ReplacementShiftID   uuid.UUID `gorm:"column:replacement_shift_id;type:uuid;not null"`
	ReplacementReportID  uuid.UUID `gorm:"column:replacement_report_id;type:uuid;not null"`
	ReplacementVersionNo int       `gorm:"column:replacement_version_no;not null"`
	Reason               string    `gorm:"column:reason;not null"`
	CreatedAt            time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the ack_supersessions table name.
func (AckSupersessionModel) TableName() string { return "ack_supersessions" }

// AmendmentModel maps one amendment request and its decision state.
type AmendmentModel struct {
	AmendmentID      uuid.UUID  `gorm:"column:amendment_id;type:uuid;primaryKey"`
	OrgID            uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID        uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	ShiftID          uuid.UUID  `gorm:"column:shift_id;type:uuid;not null"`
	BaseReportID     uuid.UUID  `gorm:"column:base_report_id;type:uuid;not null"`
	Reason           string     `gorm:"column:reason;not null"`
	Status           string     `gorm:"column:status;not null"`
	RequesterUserID  uuid.UUID  `gorm:"column:requester_user_id;type:uuid;not null"`
	ApproverUserID   *uuid.UUID `gorm:"column:approver_user_id;type:uuid"`
	RequestedAt      time.Time  `gorm:"column:requested_at;not null"`
	DecidedAt        *time.Time `gorm:"column:decided_at"`
	RejectionReason  *string    `gorm:"column:rejection_reason"`
	AppliedReportID  *uuid.UUID `gorm:"column:applied_report_id;type:uuid"`
	StaleCheckHash   []byte     `gorm:"column:stale_check_hash;not null"`
	IsBreakGlass     bool       `gorm:"column:is_break_glass;not null"`
	BreakGlassReason *string    `gorm:"column:break_glass_reason"`
}

// TableName returns the amendments table name.
func (AmendmentModel) TableName() string { return "amendments" }

// AmendmentItemModel maps one allowlisted amendment field change.
type AmendmentItemModel struct {
	ItemID          uuid.UUID       `gorm:"column:item_id;type:uuid;primaryKey"`
	AmendmentID     uuid.UUID       `gorm:"column:amendment_id;type:uuid;not null"`
	OrgID           uuid.UUID       `gorm:"column:org_id;type:uuid;not null"`
	StationID       uuid.UUID       `gorm:"column:station_id;type:uuid;not null"`
	ShiftID         uuid.UUID       `gorm:"column:shift_id;type:uuid;not null"`
	TargetKind      string          `gorm:"column:target_kind;not null"`
	TargetLogicalID uuid.UUID       `gorm:"column:target_logical_id;type:uuid;not null"`
	Field           string          `gorm:"column:field;not null"`
	OldValue        json.RawMessage `gorm:"column:old_value;type:jsonb;not null"`
	NewValue        json.RawMessage `gorm:"column:new_value;type:jsonb"`
}

// TableName returns the amendment_items table name.
func (AmendmentItemModel) TableName() string { return "amendment_items" }

// PolicySnapshotSetModel maps the policy snapshot set for one shift.
type PolicySnapshotSetModel struct {
	SetID     uuid.UUID  `gorm:"column:set_id;type:uuid;primaryKey"`
	OrgID     uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	ShiftID   *uuid.UUID `gorm:"column:shift_id;type:uuid"`
	CreatedAt time.Time  `gorm:"column:created_at;not null"`
}

// TableName returns the policy_snapshot_sets table name.
func (PolicySnapshotSetModel) TableName() string { return "policy_snapshot_sets" }

// DispenserModel maps a dispenser in one station.
type DispenserModel struct {
	OrgID       uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	StationID   uuid.UUID `gorm:"column:station_id;type:uuid;primaryKey"`
	DispenserID uuid.UUID `gorm:"column:dispenser_id;type:uuid;primaryKey"`
}

// TableName returns the dispensers table name.
func (DispenserModel) TableName() string { return "dispensers" }

// TankModel maps a tank in one station.
type TankModel struct {
	OrgID     uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	StationID uuid.UUID `gorm:"column:station_id;type:uuid;primaryKey"`
	TankID    uuid.UUID `gorm:"column:tank_id;type:uuid;primaryKey"`
}

// TableName returns the tanks table name.
func (TankModel) TableName() string { return "tanks" }

// NozzleModel maps a nozzle and its meter maximum.
type NozzleModel struct {
	OrgID       uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	StationID   uuid.UUID `gorm:"column:station_id;type:uuid;primaryKey"`
	NozzleID    uuid.UUID `gorm:"column:nozzle_id;type:uuid;primaryKey"`
	DispenserID uuid.UUID `gorm:"column:dispenser_id;type:uuid;not null"`
	MeterMax    Decimal   `gorm:"column:meter_max;type:numeric(10,1);not null"`
}

// TableName returns the nozzles table name.
func (NozzleModel) TableName() string { return "nozzles" }

// DraftReadingModel maps one draft nozzle reading.
type DraftReadingModel struct {
	RowID      uuid.UUID `gorm:"column:row_id;type:uuid;primaryKey"`
	OrgID      uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID  uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	DraftID    uuid.UUID `gorm:"column:draft_id;type:uuid;not null"`
	NozzleID   uuid.UUID `gorm:"column:nozzle_id;type:uuid;not null"`
	MeterStart Decimal   `gorm:"column:meter_start;type:numeric(10,1);not null"`
	MeterEnd   Decimal   `gorm:"column:meter_end;type:numeric(10,1);not null"`
	CreatedBy  uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the draft_readings table name.
func (DraftReadingModel) TableName() string { return "draft_readings" }

// DraftSalesModel maps one draft dispenser sales row.
type DraftSalesModel struct {
	RowID          uuid.UUID `gorm:"column:row_id;type:uuid;primaryKey"`
	OrgID          uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID      uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	DraftID        uuid.UUID `gorm:"column:draft_id;type:uuid;not null"`
	DispenserID    uuid.UUID `gorm:"column:dispenser_id;type:uuid;not null"`
	CashAmount     Decimal   `gorm:"column:cash_amount;type:numeric(14,0);not null"`
	CashlessAmount Decimal   `gorm:"column:cashless_amount;type:numeric(14,0);not null"`
	CreatedBy      uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the draft_sales table name.
func (DraftSalesModel) TableName() string { return "draft_sales" }

// DraftLossModel maps one logical draft loss or gain.
type DraftLossModel struct {
	RowID      uuid.UUID `gorm:"column:row_id;type:uuid;primaryKey"`
	OrgID      uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID  uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	DraftID    uuid.UUID `gorm:"column:draft_id;type:uuid;not null"`
	LossID     uuid.UUID `gorm:"column:loss_id;type:uuid;not null"`
	Direction  string    `gorm:"column:direction;not null"`
	ReasonCode string    `gorm:"column:reason_code;not null"`
	Liters     Decimal   `gorm:"column:liters;type:numeric(8,2);not null"`
	CashAmount *Decimal  `gorm:"column:cash_amount;type:numeric(14,0)"`
	Note       *string   `gorm:"column:note"`
	CreatedBy  uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the draft_losses table name.
func (DraftLossModel) TableName() string { return "draft_losses" }

// DraftEvidenceStagingModel maps staged evidence for a draft loss.
type DraftEvidenceStagingModel struct {
	RowID        uuid.UUID `gorm:"column:row_id;type:uuid;primaryKey"`
	OrgID        uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID    uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	DraftID      uuid.UUID `gorm:"column:draft_id;type:uuid;not null"`
	LossRowID    uuid.UUID `gorm:"column:loss_row_id;type:uuid;not null"`
	EvidenceType string    `gorm:"column:evidence_type;not null"`
	ObjectKey    string    `gorm:"column:object_key;not null"`
	ContentHash  []byte    `gorm:"column:content_hash;not null"`
	SizeBytes    int64     `gorm:"column:size_bytes;not null"`
	MIME         string    `gorm:"column:mime;not null"`
	Status       string    `gorm:"column:status;not null"`
	UploadedBy   uuid.UUID `gorm:"column:uploaded_by;type:uuid;not null"`
}

// TableName returns the draft_evidence_staging table name.
func (DraftEvidenceStagingModel) TableName() string { return "draft_evidence_staging" }

// SubmitIdempotencyModel maps one submit request claim.
type SubmitIdempotencyModel struct {
	IdemID          uuid.UUID  `gorm:"column:idem_id;type:uuid;primaryKey"`
	OrgID           uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID       uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	ShiftID         uuid.UUID  `gorm:"column:shift_id;type:uuid;not null"`
	IdempotencyKey  string     `gorm:"column:idempotency_key;not null"`
	RequestHash     []byte     `gorm:"column:request_hash;not null"`
	Status          string     `gorm:"column:status;not null"`
	ClaimToken      *uuid.UUID `gorm:"column:claim_token;type:uuid"`
	AttemptCount    int        `gorm:"column:attempt_count;not null"`
	LeaseStartedAt  *time.Time `gorm:"column:lease_started_at"`
	LeaseExpiresAt  *time.Time `gorm:"column:lease_expires_at"`
	ResultingReport *uuid.UUID `gorm:"column:resulting_report_id;type:uuid"`
	ErrorDetail     *string    `gorm:"column:error_detail"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;not null"`
}

// TableName returns the submit_idempotency table name.
func (SubmitIdempotencyModel) TableName() string { return "submit_idempotency" }

// DispenserReadingModel maps one immutable nozzle reading in a report.
type DispenserReadingModel struct {
	ReadingID        uuid.UUID  `gorm:"column:reading_id;type:uuid;primaryKey"`
	OrgID            uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID        uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	ShiftID          uuid.UUID  `gorm:"column:shift_id;type:uuid;not null"`
	ReportID         uuid.UUID  `gorm:"column:report_id;type:uuid;not null"`
	NozzleID         uuid.UUID  `gorm:"column:nozzle_id;type:uuid;not null"`
	MeterStart       Decimal    `gorm:"column:meter_start;type:numeric(10,1);not null"`
	MeterEnd         Decimal    `gorm:"column:meter_end;type:numeric(10,1);not null"`
	PriceUsed        Decimal    `gorm:"column:price_used;type:numeric(14,0);not null"`
	ExpectedSale     Decimal    `gorm:"column:expected_sale_rupiah;type:numeric(14,0);not null"`
	Observed         bool       `gorm:"column:observed;not null"`
	IsCarriedForward bool       `gorm:"column:is_carried_forward;not null"`
	SourceShiftID    *uuid.UUID `gorm:"column:source_shift_id;type:uuid"`
	SourceReportID   *uuid.UUID `gorm:"column:source_report_id;type:uuid"`
	SourceReadingID  *uuid.UUID `gorm:"column:source_reading_id;type:uuid"`
}

// TableName returns the dispenser_readings table name.
func (DispenserReadingModel) TableName() string { return "dispenser_readings" }

// SalesDeclaredModel maps declared sales for one dispenser in a report.
type SalesDeclaredModel struct {
	SalesID        uuid.UUID `gorm:"column:sales_id;type:uuid;primaryKey"`
	OrgID          uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID      uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID        uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID       uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	DispenserID    uuid.UUID `gorm:"column:dispenser_id;type:uuid;not null"`
	CashAmount     Decimal   `gorm:"column:cash_amount;type:numeric(14,0);not null"`
	CashlessAmount Decimal   `gorm:"column:cashless_amount;type:numeric(14,0);not null"`
	CreatedBy      uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the sales_declared table name.
func (SalesDeclaredModel) TableName() string { return "sales_declared" }

// LossIdentityModel maps the immutable logical identity of a loss entry.
type LossIdentityModel struct {
	LossID    uuid.UUID `gorm:"column:loss_id;type:uuid;primaryKey"`
	OrgID     uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	CreatedBy uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the loss_identity table name.
func (LossIdentityModel) TableName() string { return "loss_identity" }

// LossEntryModel maps one report loss or gain entry.
type LossEntryModel struct {
	RowID      uuid.UUID `gorm:"column:row_id;type:uuid;primaryKey"`
	OrgID      uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID  uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID    uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID   uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	VersionNo  int       `gorm:"column:version_no;not null"`
	LossID     uuid.UUID `gorm:"column:loss_id;type:uuid;not null"`
	NozzleID   uuid.UUID `gorm:"column:nozzle_id;type:uuid;not null"`
	Direction  string    `gorm:"column:direction;not null"`
	ReasonCode string    `gorm:"column:reason_code;not null"`
	Liters     Decimal   `gorm:"column:liters;type:numeric(8,2);not null"`
	CashAmount *Decimal  `gorm:"column:cash_amount;type:numeric(14,0)"`
	Note       *string   `gorm:"column:note"`
	CreatedBy  uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the loss_entries table name.
func (LossEntryModel) TableName() string { return "loss_entries" }

// DeliveryModel maps one delivery recorded in a shift.
type DeliveryModel struct {
	DeliveryID uuid.UUID `gorm:"column:delivery_id;type:uuid;primaryKey"`
	OrgID      uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID  uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID    uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	DONumber   string    `gorm:"column:do_number;not null"`
	TankID     uuid.UUID `gorm:"column:tank_id;type:uuid;not null"`
	Liters     Decimal   `gorm:"column:liters;type:numeric(8,2);not null"`
	CreatedBy  uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the deliveries table name.
func (DeliveryModel) TableName() string { return "deliveries" }

// DipReadingModel maps one tank dip reading recorded in a shift.
type DipReadingModel struct {
	DipID     uuid.UUID `gorm:"column:dip_id;type:uuid;primaryKey"`
	OrgID     uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID   uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	TankID    uuid.UUID `gorm:"column:tank_id;type:uuid;not null"`
	DipLiters Decimal   `gorm:"column:dip_liters;type:numeric(8,2);not null"`
	CreatedBy uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
}

// TableName returns the dip_readings table name.
func (DipReadingModel) TableName() string { return "dip_readings" }

// ShiftTransitionModel maps one shift state transition.
type ShiftTransitionModel struct {
	TransitionID uuid.UUID `gorm:"column:transition_id;type:uuid;primaryKey"`
	OrgID        uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID    uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID      uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	FromStatus   string    `gorm:"column:from_status;not null"`
	ToStatus     string    `gorm:"column:to_status;not null"`
	ActorUserID  uuid.UUID `gorm:"column:actor_user_id;type:uuid;not null"`
	At           time.Time `gorm:"column:at;not null"`
	Reason       *string   `gorm:"column:reason"`
}

// TableName returns the shift_transitions table name.
func (ShiftTransitionModel) TableName() string { return "shift_transitions" }

// MeterResetEventModel maps one approved or pending meter reset.
type MeterResetEventModel struct {
	ResetID          uuid.UUID  `gorm:"column:reset_id;type:uuid;primaryKey"`
	OrgID            uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID        uuid.UUID  `gorm:"column:station_id;type:uuid;not null"`
	NozzleID         uuid.UUID  `gorm:"column:nozzle_id;type:uuid;not null"`
	OldValue         Decimal    `gorm:"column:old_value;type:numeric(10,1);not null"`
	NewValue         Decimal    `gorm:"column:new_value;type:numeric(10,1);not null"`
	EffectiveShiftID uuid.UUID  `gorm:"column:effective_shift_id;type:uuid;not null"`
	Reason           string     `gorm:"column:reason;not null"`
	ActorUserID      uuid.UUID  `gorm:"column:actor_user_id;type:uuid;not null"`
	ApproverUserID   uuid.UUID  `gorm:"column:approver_user_id;type:uuid;not null"`
	ApprovedAt       *time.Time `gorm:"column:approved_at"`
	Status           string     `gorm:"column:status;not null"`
}

// TableName returns the meter_reset_events table name.
func (MeterResetEventModel) TableName() string { return "meter_reset_events" }

// ThresholdPolicyRevisionModel maps one threshold policy revision.
type ThresholdPolicyRevisionModel struct {
	RevID               uuid.UUID  `gorm:"column:rev_id;type:uuid;primaryKey"`
	PolicyID            uuid.UUID  `gorm:"column:policy_id;type:uuid;not null"`
	OrgID               uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID           *uuid.UUID `gorm:"column:station_id;type:uuid"`
	ValidFrom           time.Time  `gorm:"column:valid_from;not null"`
	SupersedesOrgID     *uuid.UUID `gorm:"column:supersedes_org_id;type:uuid"`
	SupersedesRevID     *uuid.UUID `gorm:"column:supersedes_rev_id;type:uuid"`
	Disabled            bool       `gorm:"column:disabled;not null"`
	LossLiterThreshold  Decimal    `gorm:"column:loss_liter_threshold;type:numeric(8,2);not null"`
	GainLiterThreshold  Decimal    `gorm:"column:gain_liter_threshold;type:numeric(8,2);not null"`
	LossRupiahThreshold Decimal    `gorm:"column:loss_rupiah_threshold;type:numeric(14,0);not null"`
	GainRupiahThreshold Decimal    `gorm:"column:gain_rupiah_threshold;type:numeric(14,0);not null"`
	VarianceThreshold   Decimal    `gorm:"column:variance_rupiah_threshold;type:numeric(14,0);not null"`
	RolloverThreshold   Decimal    `gorm:"column:rollover_threshold;type:numeric(10,1);not null"`
	CreatedBy           uuid.UUID  `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null"`
}

// TableName returns the threshold_policy_revisions table name.
func (ThresholdPolicyRevisionModel) TableName() string { return "threshold_policy_revisions" }

// EvidencePolicyRevisionModel maps one evidence policy revision.
type EvidencePolicyRevisionModel struct {
	RevID           uuid.UUID  `gorm:"column:rev_id;type:uuid;primaryKey"`
	PolicyID        uuid.UUID  `gorm:"column:policy_id;type:uuid;not null"`
	OrgID           uuid.UUID  `gorm:"column:org_id;type:uuid;not null"`
	StationID       *uuid.UUID `gorm:"column:station_id;type:uuid"`
	ValidFrom       time.Time  `gorm:"column:valid_from;not null"`
	SupersedesOrgID *uuid.UUID `gorm:"column:supersedes_org_id;type:uuid"`
	SupersedesRevID *uuid.UUID `gorm:"column:supersedes_rev_id;type:uuid"`
	Mode            string     `gorm:"column:mode;not null"`
	Disabled        bool       `gorm:"column:disabled;not null"`
	CreatedBy       uuid.UUID  `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null"`
}

// TableName returns the evidence_policy_revisions table name.
func (EvidencePolicyRevisionModel) TableName() string { return "evidence_policy_revisions" }

// EvidencePolicyTypeModel maps one accepted evidence type.
type EvidencePolicyTypeModel struct {
	OrgID               uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	RevID               uuid.UUID `gorm:"column:rev_id;type:uuid;primaryKey"`
	EvidenceType        string    `gorm:"column:evidence_type;primaryKey"`
	MinimumCountPerLoss int       `gorm:"column:minimum_count_per_loss;not null"`
	AcceptedMIMETypes   []string  `gorm:"column:accepted_mime_types;type:text[];not null"`
}

// TableName returns the evidence_policy_types table name.
func (EvidencePolicyTypeModel) TableName() string { return "evidence_policy_types" }

// PolicySnapshotItemModel maps one immutable policy snapshot item.
type PolicySnapshotItemModel struct {
	ItemID      uuid.UUID       `gorm:"column:item_id;type:uuid;primaryKey"`
	OrgID       uuid.UUID       `gorm:"column:org_id;type:uuid;not null"`
	StationID   uuid.UUID       `gorm:"column:station_id;type:uuid;not null"`
	ShiftID     uuid.UUID       `gorm:"column:shift_id;type:uuid;not null"`
	SetID       uuid.UUID       `gorm:"column:set_id;type:uuid;not null"`
	PolicyKind  string          `gorm:"column:policy_kind;not null"`
	PolicyID    uuid.UUID       `gorm:"column:policy_id;type:uuid;not null"`
	RevID       uuid.UUID       `gorm:"column:rev_id;type:uuid;not null"`
	Scope       string          `gorm:"column:scope;not null"`
	Payload     json.RawMessage `gorm:"column:payload;type:jsonb;not null"`
	PayloadHash []byte          `gorm:"column:payload_hash;not null"`
}

// TableName returns the policy_snapshot_items table name.
func (PolicySnapshotItemModel) TableName() string { return "policy_snapshot_items" }

// DeliverySnapshotModel maps one delivery reference in a report snapshot.
type DeliverySnapshotModel struct {
	SnapshotID uuid.UUID `gorm:"column:snapshot_id;type:uuid;primaryKey"`
	OrgID      uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID  uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID    uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID   uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	DeliveryID uuid.UUID `gorm:"column:delivery_id;type:uuid;not null"`
}

// TableName returns the delivery_snapshots table name.
func (DeliverySnapshotModel) TableName() string { return "delivery_snapshots" }

// DipSnapshotModel maps one dip reference in a report snapshot.
type DipSnapshotModel struct {
	SnapshotID uuid.UUID `gorm:"column:snapshot_id;type:uuid;primaryKey"`
	OrgID      uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID  uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID    uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID   uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	DipID      uuid.UUID `gorm:"column:dip_id;type:uuid;not null"`
}

// TableName returns the dip_snapshots table name.
func (DipSnapshotModel) TableName() string { return "dip_snapshots" }

// LossExceptionModel maps an evidence exception for one loss.
type LossExceptionModel struct {
	ExceptionID uuid.UUID `gorm:"column:exception_id;type:uuid;primaryKey"`
	OrgID       uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID   uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID     uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID    uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	LossID      uuid.UUID `gorm:"column:loss_id;type:uuid;not null"`
	Reason      string    `gorm:"column:reason;not null"`
	ActorUserID uuid.UUID `gorm:"column:actor_user_id;type:uuid;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the loss_exception table name.
func (LossExceptionModel) TableName() string { return "loss_exception" }

// EvidenceEventModel maps one append-only evidence event.
type EvidenceEventModel struct {
	EvidenceEventID uuid.UUID `gorm:"column:evidence_event_id;type:uuid;primaryKey"`
	EvidenceID      uuid.UUID `gorm:"column:evidence_id;type:uuid;not null"`
	OrgID           uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID       uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	ShiftID         uuid.UUID `gorm:"column:shift_id;type:uuid;not null"`
	ReportID        uuid.UUID `gorm:"column:report_id;type:uuid;not null"`
	LossRowID       uuid.UUID `gorm:"column:loss_row_id;type:uuid;not null"`
	EventSeq        int64     `gorm:"column:event_seq;not null"`
	EventType       string    `gorm:"column:event_type;not null"`
	EvidenceType    string    `gorm:"column:evidence_type;not null"`
	ObjectKey       string    `gorm:"column:object_key;not null"`
	ContentHash     []byte    `gorm:"column:content_hash;not null"`
	SizeBytes       int64     `gorm:"column:size_bytes;not null"`
	MIME            string    `gorm:"column:mime;not null"`
	ActorUserID     uuid.UUID `gorm:"column:actor_user_id;type:uuid;not null"`
	At              time.Time `gorm:"column:at;not null"`
}

// TableName returns the evidence_event table name.
func (EvidenceEventModel) TableName() string { return "evidence_event" }

// NozzleBaselineRevisionModel maps one immutable meter baseline revision.
type NozzleBaselineRevisionModel struct {
	BaselineRevID       uuid.UUID `gorm:"column:baseline_rev_id;type:uuid;primaryKey"`
	OrgID               uuid.UUID `gorm:"column:org_id;type:uuid;not null"`
	StationID           uuid.UUID `gorm:"column:station_id;type:uuid;not null"`
	NozzleID            uuid.UUID `gorm:"column:nozzle_id;type:uuid;not null"`
	EffectiveStationSeq int64     `gorm:"column:effective_station_seq;not null"`
	InitialMeterValue   Decimal   `gorm:"column:initial_meter_value;type:numeric(10,1);not null"`
	MeterMax            Decimal   `gorm:"column:meter_max;type:numeric(10,1);not null"`
	CreatedBy           uuid.UUID `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the nozzle_baseline_revisions table name.
func (NozzleBaselineRevisionModel) TableName() string { return "nozzle_baseline_revisions" }

// NozzleBaselineCurrentModel maps the current baseline pointer.
type NozzleBaselineCurrentModel struct {
	OrgID                uuid.UUID `gorm:"column:org_id;type:uuid;primaryKey"`
	StationID            uuid.UUID `gorm:"column:station_id;type:uuid;primaryKey"`
	NozzleID             uuid.UUID `gorm:"column:nozzle_id;type:uuid;primaryKey"`
	CurrentBaselineRevID uuid.UUID `gorm:"column:current_baseline_rev_id;type:uuid;not null"`
}

// TableName returns the nozzle_baseline_current table name.
func (NozzleBaselineCurrentModel) TableName() string { return "nozzle_baseline_current" }
