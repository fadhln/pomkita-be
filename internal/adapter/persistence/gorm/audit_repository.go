package gormstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/canonical"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AuditRepository persists the organization audit chain and outbox.
type AuditRepository struct {
	db *gorm.DB
}

// NewAuditRepository creates an audit repository.
func NewAuditRepository(store *Store) *AuditRepository {
	if store == nil {
		return &AuditRepository{}
	}
	return &AuditRepository{db: store.db}
}

// Append adds one chained audit event and relay state in one transaction.
func (r *AuditRepository) Append(ctx context.Context, request appaudit.AppendRequest, now time.Time) (appaudit.Event, error) {
	if r == nil || r.db == nil {
		return appaudit.Event{}, appaudit.ErrDependencyUnavailable
	}
	var result appaudit.Event
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = appendAuditInTransaction(tx, request, now)
		return err
	})
	if err != nil {
		return appaudit.Event{}, err
	}
	return result, nil
}

func appendAuditInTransaction(tx *gorm.DB, request appaudit.AppendRequest, now time.Time) (appaudit.Event, error) {
	if request.OrgID == uuid.Nil || request.EventID == uuid.Nil || strings.TrimSpace(request.EventType) == "" || len(request.Payload) == 0 || !json.Valid(request.Payload) || strings.TrimSpace(request.Outcome) == "" {
		return appaudit.Event{}, appaudit.ErrInvalidRequest
	}
	var lock AuditChainLockModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ?", request.OrgID).First(&lock).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return appaudit.Event{}, fmt.Errorf("lock audit chain: %w", err)
		}
		lock = AuditChainLockModel{OrgID: request.OrgID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lock).Error; err != nil {
			return appaudit.Event{}, fmt.Errorf("create audit chain lock: %w", err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ?", request.OrgID).First(&lock).Error; err != nil {
			return appaudit.Event{}, fmt.Errorf("lock new audit chain: %w", err)
		}
	}
	var last AuditLogModel
	if err := tx.Where("org_id = ?", request.OrgID).Order("org_sequence desc").First(&last).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return appaudit.Event{}, fmt.Errorf("load audit tail: %w", err)
	}
	sequence := last.OrgSequence + 1
	prevHash := last.RowHash
	if len(prevHash) == 0 {
		prevHash = make([]byte, 32)
	}
	createdAt := now.UTC()
	rowHash := auditRowHash(request.OrgID, sequence, request.EventID, request.EventType, request.Payload, createdAt, prevHash)
	if len(rowHash) != 32 {
		return appaudit.Event{}, appaudit.ErrInvalidRequest
	}
	row := AuditLogModel{EventID: request.EventID, OrgID: request.OrgID, OrgSequence: sequence, EventType: request.EventType, Payload: append([]byte(nil), request.Payload...), Outcome: request.Outcome, CreatedAt: createdAt, PrevHash: append([]byte(nil), prevHash...), RowHash: rowHash}
	if strings.TrimSpace(request.OutcomeError) != "" {
		row.OutcomeError = stringPointer(request.OutcomeError)
	}
	if err := tx.Create(&row).Error; err != nil {
		return appaudit.Event{}, fmt.Errorf("append audit log: %w", err)
	}
	outbox := AuditOutboxModel{OrgID: request.OrgID, EventID: request.EventID, EventType: request.EventType, Payload: append([]byte(nil), request.Payload...), CreatedAt: createdAt}
	if err := tx.Create(&outbox).Error; err != nil {
		return appaudit.Event{}, fmt.Errorf("append audit outbox: %w", err)
	}
	state := OutboxRelayStateModel{OrgID: request.OrgID, EventID: request.EventID, RelayStatus: "pending", NextAttemptAt: &createdAt}
	if err := tx.Create(&state).Error; err != nil {
		return appaudit.Event{}, fmt.Errorf("create relay state: %w", err)
	}
	return appaudit.Event{EventID: request.EventID, OrgSequence: sequence, RowHash: append([]byte(nil), rowHash...)}, nil
}

// Verify checks sequence links and row hashes for one organization.
func (r *AuditRepository) Verify(ctx context.Context, orgID uuid.UUID) error {
	if r == nil || r.db == nil {
		return appaudit.ErrDependencyUnavailable
	}
	var rows []AuditLogModel
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("org_sequence").Find(&rows).Error; err != nil {
		return fmt.Errorf("load audit chain: %w", err)
	}
	prev := make([]byte, 32)
	for index, row := range rows {
		sequence := int64(index + 1)
		if row.OrgSequence != sequence || !bytes.Equal(row.PrevHash, prev) {
			return appaudit.ErrChainTampered
		}
		expected := auditRowHash(row.OrgID, row.OrgSequence, row.EventID, row.EventType, row.Payload, row.CreatedAt, row.PrevHash)
		if !bytes.Equal(row.RowHash, expected) {
			return appaudit.ErrChainTampered
		}
		prev = row.RowHash
	}
	return nil
}

func auditRowHash(orgID uuid.UUID, sequence int64, eventID uuid.UUID, eventType string, payload []byte, createdAt time.Time, prevHash []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var decodedPayload any
	if err := decoder.Decode(&decodedPayload); err != nil {
		return nil
	}
	canonicalBytes, err := canonical.Marshal([]any{1, orgID.String(), sequence, strings.ToLower(eventID.String()), eventType, decodedPayload, createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"), hex.EncodeToString(prevHash)})
	if err != nil {
		return nil
	}
	digest := sha256.Sum256(canonicalBytes)
	return digest[:]
}

func stringPointer(value string) *string { return &value }
