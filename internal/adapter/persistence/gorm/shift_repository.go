package gormstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ShiftRepository persists shift opening and its initial draft.
type ShiftRepository struct {
	db *gorm.DB
}

// NewShiftRepository creates a shift repository.
func NewShiftRepository(store *Store) *ShiftRepository {
	if store == nil {
		return &ShiftRepository{}
	}
	return &ShiftRepository{db: store.db}
}

// OpenShift locks the station, snapshots the catalog, and inserts one shift and draft.
func (r *ShiftRepository) OpenShift(ctx context.Context, request appshift.OpenRequest) (appshift.Shift, error) {
	if r == nil || r.db == nil {
		return appshift.Shift{}, appshift.ErrDependencyUnavailable
	}
	var result appshift.Shift
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var station StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
			return fmt.Errorf("lock station: %w", err)
		}

		var sequence int64
		if err := tx.Model(&ShiftModel{}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).Select("coalesce(max(station_seq), 0) + 1").Scan(&sequence).Error; err != nil {
			return fmt.Errorf("allocate station sequence: %w", err)
		}
		var originalEventDate *time.Time
		var shiftKE *int
		var backfillApprover *uuid.UUID
		var backfillReason *string
		var backfillApprovedAt *time.Time
		if request.Backfilled {
			parsedDate, err := time.Parse("2006-01-02", request.OriginalEventDate)
			if err != nil || request.BackfillApprover == uuid.Nil || request.ShiftKE < 1 || strings.TrimSpace(request.BackfillReason) == "" {
				return appshift.ErrBackfillApprovalRequired
			}
			var approverCount int64
			if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role in ?", request.OrgID, request.StationID, request.BackfillApprover, []string{"Owner", "Superadmin"}).Count(&approverCount).Error; err != nil {
				return fmt.Errorf("check backfill approver: %w", err)
			}
			if approverCount != 1 {
				return appshift.ErrBackfillApprovalRequired
			}
			originalEventDate = &parsedDate
			value := request.ShiftKE
			shiftKE = &value
			approver := request.BackfillApprover
			backfillApprover = &approver
			reason := strings.TrimSpace(request.BackfillReason)
			backfillReason = &reason
			approvedAt := request.OpenedAt
			backfillApprovedAt = &approvedAt
			var laterChainedReadings int64
			if err := tx.Table("shifts s").
				Joins("join shift_reports r on r.org_id = s.org_id and r.station_id = s.station_id and r.shift_id = s.shift_id and r.report_id = s.current_report_id").
				Joins("join dispenser_readings dr on dr.org_id = r.org_id and dr.station_id = r.station_id and dr.shift_id = r.shift_id and dr.report_id = r.report_id").
				Where("s.org_id = ? and s.station_id = ? and s.status = ? and s.business_date > ? and dr.is_carried_forward = ?", request.OrgID, request.StationID, "locked", parsedDate.Format("2006-01-02"), true).
				Count(&laterChainedReadings).Error; err != nil {
				return fmt.Errorf("check backfill order: %w", err)
			}
			if laterChainedReadings > 0 {
				return appshift.ErrBackfillOutOfOrder
			}
		} else if request.OriginalEventDate != "" || request.ShiftKE != 0 || request.BackfillApprover != uuid.Nil || strings.TrimSpace(request.BackfillReason) != "" {
			return appshift.ErrBackfillApprovalRequired
		}
		snapshot, hash, err := r.catalogSnapshot(tx, request, request.OpenedAt)
		if err != nil {
			return err
		}
		location, err := time.LoadLocation(station.Timezone)
		if err != nil {
			return fmt.Errorf("load station timezone: %w", err)
		}
		shiftID := uuid.New()
		openedAt := request.OpenedAt.UTC()
		shift := ShiftModel{
			ShiftID: shiftID, OrgID: request.OrgID, StationID: request.StationID,
			StationSeq: sequence, SupervisorID: request.ActorID, OpenedAt: openedAt,
			TimezoneSnapshot: station.Timezone, BusinessDate: openedAt.In(location).Format("2006-01-02"),
			Status: string(appshift.StatusOpen), OriginalEventDate: originalEventDate, ShiftKE: shiftKE, Backfilled: request.Backfilled, BackfillApprover: backfillApprover, BackfillApproved: backfillApprovedAt, BackfillReason: backfillReason, PriceMapSnapshot: snapshot, PriceMapHash: hash,
		}
		if err := tx.Create(&shift).Error; err != nil {
			return fmt.Errorf("create shift: %w", err)
		}
		draft := ShiftDraftModel{
			DraftID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: shiftID,
			OwnedBy: &request.ActorID, UpdatedBy: &request.ActorID, Status: "editing", Revision: 1,
			RecoveryCount: 0, UpdatedAt: openedAt,
		}
		if err := tx.Create(&draft).Error; err != nil {
			return fmt.Errorf("create shift draft: %w", err)
		}
		result = appshift.Shift{
			ShiftID: shiftID, OrgID: request.OrgID, StationID: request.StationID, StationSeq: sequence,
			SupervisorID: request.ActorID, OpenedAt: openedAt, TimezoneSnapshot: station.Timezone,
			BusinessDate: shift.BusinessDate, Status: appshift.StatusOpen, PriceMapSnapshot: snapshot, PriceMapHash: hash,
		}
		return nil
	})
	if err != nil {
		return appshift.Shift{}, err
	}
	return result, nil
}

// List reads shifts in stable sequence order within an organization scope.
func (r *ShiftRepository) List(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]appshift.Summary, error) {
	if r == nil || r.db == nil {
		return nil, appshift.ErrDependencyUnavailable
	}
	query := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if stationID != nil {
		query = query.Where("station_id = ?", *stationID)
	}
	var rows []ShiftModel
	if err := query.Order("station_id, station_seq").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list shifts: %w", err)
	}
	result := make([]appshift.Summary, 0, len(rows))
	for _, row := range rows {
		result = append(result, shiftSummary(row))
	}
	return result, nil
}

// Detail reads one shift and its draft revision within a tenant scope.
func (r *ShiftRepository) Detail(ctx context.Context, orgID, stationID, shiftID uuid.UUID) (appshift.Detail, error) {
	if r == nil || r.db == nil {
		return appshift.Detail{}, appshift.ErrDependencyUnavailable
	}
	var row ShiftModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and shift_id = ?", orgID, stationID, shiftID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appshift.Detail{}, appshift.ErrInvalidRequest
		}
		return appshift.Detail{}, fmt.Errorf("load shift detail: %w", err)
	}
	result := appshift.Detail{Summary: shiftSummary(row)}
	var draft ShiftDraftModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and shift_id = ?", orgID, stationID, shiftID).First(&draft).Error; err == nil {
		result.DraftID = &draft.DraftID
		result.Revision = &draft.Revision
	}
	return result, nil
}

func shiftSummary(row ShiftModel) appshift.Summary {
	return appshift.Summary{ShiftID: row.ShiftID, StationID: row.StationID, StationSeq: row.StationSeq, SupervisorID: row.SupervisorID, OpenedAt: row.OpenedAt.UTC().Format(time.RFC3339Nano), BusinessDate: row.BusinessDate, Status: row.Status, CurrentReportID: row.CurrentReportID}
}

type catalogSnapshotItem struct {
	DispenserID          string  `json:"dispenser_id"`
	NozzleID             string  `json:"nozzle_id"`
	MeterMax             string  `json:"meter_max"`
	Modulus              string  `json:"modulus"`
	Price                string  `json:"price"`
	PriceID              string  `json:"price_id"`
	DispenserNozzleMapID string  `json:"dispenser_nozzle_map_id"`
	NozzleTankMapID      *string `json:"nozzle_tank_map_id"`
}

type catalogSnapshot struct {
	HashVersion int                   `json:"hash_version"`
	Items       []catalogSnapshotItem `json:"items"`
}

func (r *ShiftRepository) catalogSnapshot(tx *gorm.DB, request appshift.OpenRequest, openedAt time.Time) ([]byte, []byte, error) {
	type catalogRow struct {
		DispenserID          uuid.UUID
		NozzleID             uuid.UUID
		MeterMax             Decimal
		Price                Decimal
		PriceID              uuid.UUID
		DispenserNozzleMapID uuid.UUID
		NozzleTankMapID      *uuid.UUID
	}
	var rows []catalogRow
	err := tx.Table("nozzles n").Select(`n.dispenser_id, n.nozzle_id, n.meter_max,
		p.price, p.price_id, dnm.map_id as dispenser_nozzle_map_id, ntm.map_id as nozzle_tank_map_id`).
		Joins("join dispenser_prices p on p.org_id = n.org_id and p.station_id = n.station_id and p.nozzle_id = n.nozzle_id and p.valid_period @> ?::timestamptz", openedAt).
		Joins("join dispenser_nozzle_map dnm on dnm.org_id = n.org_id and dnm.station_id = n.station_id and dnm.nozzle_id = n.nozzle_id and dnm.valid_period @> ?::timestamptz", openedAt).
		Joins("left join nozzle_tank_map ntm on ntm.org_id = n.org_id and ntm.station_id = n.station_id and ntm.nozzle_id = n.nozzle_id and ntm.valid_period @> ?::timestamptz", openedAt).
		Where("n.org_id = ? and n.station_id = ?", request.OrgID, request.StationID).
		Order("n.nozzle_id").Find(&rows).Error
	if err != nil {
		return nil, nil, fmt.Errorf("read catalog snapshot: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, errors.New("catalog snapshot is incomplete")
	}
	items := make([]catalogSnapshotItem, 0, len(rows))
	for _, row := range rows {
		var tankID *string
		if row.NozzleTankMapID != nil {
			value := row.NozzleTankMapID.String()
			tankID = &value
		}
		items = append(items, catalogSnapshotItem{
			DispenserID: row.DispenserID.String(), NozzleID: row.NozzleID.String(),
			MeterMax: row.MeterMax.String(), Modulus: decimalAddTenth(row.MeterMax.String()),
			Price: row.Price.String(), PriceID: row.PriceID.String(),
			DispenserNozzleMapID: row.DispenserNozzleMapID.String(), NozzleTankMapID: tankID,
		})
	}
	payload, err := json.Marshal(catalogSnapshot{HashVersion: 1, Items: items})
	if err != nil {
		return nil, nil, fmt.Errorf("encode catalog snapshot: %w", err)
	}
	hash := sha256.Sum256(payload)
	return payload, hash[:], nil
}

func decimalAddTenth(value string) string {
	parts := strings.SplitN(value, ".", 2)
	whole := parts[0]
	fraction := "0"
	if len(parts) == 2 && parts[1] != "" {
		fraction = parts[1]
	}
	if len(fraction) > 1 {
		fraction = fraction[:1]
	}
	scaled := new(big.Int)
	if _, ok := scaled.SetString(strings.TrimPrefix(whole, "-"), 10); !ok {
		return value
	}
	scaled.Mul(scaled, big.NewInt(10))
	frac, _ := new(big.Int).SetString(fraction, 10)
	scaled.Add(scaled, frac)
	scaled.Add(scaled, big.NewInt(1))
	text := scaled.String()
	if len(text) == 1 {
		return "0." + text
	}
	return text[:len(text)-1] + "." + text[len(text)-1:]
}
