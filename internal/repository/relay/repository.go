package relayrepository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	apprelay "github.com/pomkita/pomkita-be/internal/service/relay"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RelayRepository persists outbox leases and delivery results.
type RelayRepository struct {
	db *gorm.DB
}

// NewRelayRepository creates a relay repository.
func NewRelayRepository(store *Store) *RelayRepository {
	if store == nil {
		return &RelayRepository{}
	}
	return &RelayRepository{db: store.DB}
}

// Claim claims one ready outbox event with a five-minute lease.
func (r *RelayRepository) Claim(ctx context.Context, orgID, eventID uuid.UUID) (apprelay.Event, uuid.UUID, bool, error) {
	return r.ClaimAt(ctx, orgID, eventID, time.Now().UTC())
}

// ClaimAt claims one event at a deterministic time.
func (r *RelayRepository) ClaimAt(ctx context.Context, orgID, eventID uuid.UUID, now time.Time) (apprelay.Event, uuid.UUID, bool, error) {
	if r == nil || r.db == nil {
		return apprelay.Event{}, uuid.Nil, false, fmt.Errorf("relay repository is unavailable")
	}
	var event apprelay.Event
	var lease uuid.UUID
	var claimed bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state OutboxRelayStateModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and event_id = ?", orgID, eventID).First(&state).Error; err != nil {
			return fmt.Errorf("load relay state: %w", err)
		}
		if state.RelayStatus == "delivered" || state.RelayStatus == "in_flight" && state.LeaseExpiresAt != nil && state.LeaseExpiresAt.After(now) || state.RelayStatus == "failed" && state.NextAttemptAt != nil && state.NextAttemptAt.After(now) {
			return nil
		}
		var outbox AuditOutboxModel
		if err := tx.Where("org_id = ? and event_id = ?", orgID, eventID).First(&outbox).Error; err != nil {
			return fmt.Errorf("load relay outbox: %w", err)
		}
		lease = uuid.New()
		expires := now.Add(5 * time.Minute)
		if err := tx.Model(&OutboxRelayStateModel{}).Where("org_id = ? and event_id = ?", orgID, eventID).Updates(map[string]any{"relay_status": "in_flight", "attempt_count": state.AttemptCount + 1, "lease_token": lease, "lease_expires_at": expires, "last_attempt_at": now, "last_error": nil}).Error; err != nil {
			return fmt.Errorf("claim relay event: %w", err)
		}
		event = apprelay.Event{EventID: outbox.EventID, EventType: outbox.EventType, Payload: append([]byte(nil), outbox.Payload...)}
		claimed = true
		return nil
	})
	if err != nil {
		return apprelay.Event{}, uuid.Nil, false, err
	}
	return event, lease, claimed, nil
}

// Finish records a successful or failed delivery for a valid lease.
func (r *RelayRepository) Finish(ctx context.Context, orgID, eventID, lease uuid.UUID, success bool, failure string) error {
	return r.FinishAt(ctx, orgID, eventID, lease, success, failure, time.Now().UTC())
}

// FinishAt records a delivery result at a deterministic time.
func (r *RelayRepository) FinishAt(ctx context.Context, orgID, eventID, lease uuid.UUID, success bool, failure string, now time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("relay repository is unavailable")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state OutboxRelayStateModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and event_id = ? and lease_token = ? and relay_status = ?", orgID, eventID, lease, "in_flight").First(&state).Error; err != nil {
			return fmt.Errorf("load relay lease: %w", err)
		}
		if state.LeaseExpiresAt == nil || !state.LeaseExpiresAt.After(now) {
			return fmt.Errorf("relay lease expired")
		}
		updates := map[string]any{"lease_token": nil, "lease_expires_at": nil, "last_error": nil}
		if success {
			updates["relay_status"] = "delivered"
			updates["delivered_at"] = now
			updates["next_attempt_at"] = nil
		} else {
			updates["relay_status"] = "failed"
			updates["last_error"] = truncateRelayError(failure)
			attempt := state.AttemptCount
			if attempt > 12 {
				attempt = 12
			}
			updates["next_attempt_at"] = now.Add(time.Duration(1<<attempt) * time.Second)
		}
		if err := tx.Model(&OutboxRelayStateModel{}).Where("org_id = ? and event_id = ?", orgID, eventID).Updates(updates).Error; err != nil {
			return fmt.Errorf("finish relay event: %w", err)
		}
		return nil
	})
}

func truncateRelayError(value string) string {
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
