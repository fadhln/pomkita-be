package account

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/repository/store"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
	appaccount "github.com/fadhln/pomkita-be/internal/service/account"
	"github.com/google/uuid"
)

func TestRepository_ReadProfileAndRevokeOtherSessions(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.StationModel{OrgID: orgID, StationID: stationID, Name: "Main", Timezone: "Asia/Jakarta", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.UserModel{UserID: userID, OrgID: orgID, DisplayName: "Test User", Email: "user@example.test", Username: "test-user", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": userID, "role": "Operator"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.JWTKeyModel{KID: "key", SecretRef: "key-ref", Status: "active", ActivatedAt: now, MaxTokenExpiry: now.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	current, other := uuid.New(), uuid.New()
	for _, jti := range []uuid.UUID{current, other} {
		if err := database.DB.Create(&store.SessionModel{JTI: jti, UserID: userID, KID: "key", IssuedAt: now, ExpiresAt: now.Add(10 * time.Minute), LastActiveAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repository := NewRepository(database)
	profile, err := repository.ReadProfile(ctx, userID)
	if err != nil || profile.Email != "user@example.test" || profile.Org.Name != "Test Org" || len(profile.Stations) != 1 || profile.Stations[0].Name != "Main" {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
	if err := repository.ChangePassword(ctx, userID, "new-hash", current, uuid.New(), now); err != nil {
		t.Fatal(err)
	}
	var session store.SessionModel
	if err := database.DB.Where("jti = ?", other).First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.RevokedAt == nil {
		t.Fatal("other session was not revoked")
	}
	var currentSession store.SessionModel
	if err := database.DB.Where("jti = ?", current).First(&currentSession).Error; err != nil {
		t.Fatal(err)
	}
	if currentSession.RevokedAt != nil {
		t.Fatal("current session was revoked")
	}
	var changedEvent store.AuditLogModel
	if err := database.DB.Where("event_type = ?", "account.password_changed").First(&changedEvent).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRepository_ResetTokenLifecycleAndDuplicateUsername(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	orgID, stationID, userID, otherID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Create(&store.StationModel{OrgID: orgID, StationID: stationID, Name: "Main", Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for id, username := range map[uuid.UUID]string{userID: "first-user", otherID: "second-user"} {
		if err := database.DB.Create(&store.UserModel{UserID: id, OrgID: orgID, DisplayName: username, Email: username + "@example.test", Username: username, PasswordHash: "old-hash", Enabled: true, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": userID, "role": "Operator"}).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(database)
	duplicate := "SECOND-USER"
	if _, err := repository.UpdateProfile(ctx, userID, appaccount.UpdateRequest{Username: &duplicate}, uuid.New(), now); err != appaccount.ErrUsernameConflict {
		t.Fatalf("duplicate error: got %v, want %v", err, appaccount.ErrUsernameConflict)
	}
	oldRaw, newRaw := "old-reset-token", "new-reset-token"
	oldHash, newHash := sha256.Sum256([]byte(oldRaw)), sha256.Sum256([]byte(newRaw))
	if err := repository.IssuePasswordReset(ctx, userID, uuid.New(), oldHash[:], now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	secondID := uuid.New()
	if err := repository.IssuePasswordReset(ctx, userID, secondID, newHash[:], now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var oldToken store.AccountTokenModel
	if err := database.DB.Where("token_hash = ?", oldHash[:]).First(&oldToken).Error; err != nil {
		t.Fatal(err)
	}
	if oldToken.ConsumedAt == nil {
		t.Fatal("old reset token was not consumed")
	}
	if err := repository.ResetPassword(ctx, []byte("unknown"), "new-hash", uuid.New(), now); err != appaccount.ErrInvalidToken {
		t.Fatalf("unknown token error: got %v", err)
	}
	expiredHash := sha256.Sum256([]byte("expired-token"))
	if err := database.DB.Create(&store.AccountTokenModel{TokenID: uuid.New(), OrgID: orgID, UserID: otherID, Purpose: "password_reset", TokenHash: expiredHash[:], ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.ResetPassword(ctx, expiredHash[:], "new-hash", uuid.New(), now); err != appaccount.ErrInvalidToken {
		t.Fatalf("expired token error: got %v", err)
	}
	consumedHash := sha256.Sum256([]byte("consumed-token"))
	consumedAt := now.Add(-time.Minute)
	if err := database.DB.Create(&store.AccountTokenModel{TokenID: uuid.New(), OrgID: orgID, UserID: otherID, Purpose: "password_reset", TokenHash: consumedHash[:], ExpiresAt: now.Add(time.Hour), ConsumedAt: &consumedAt, CreatedAt: now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.ResetPassword(ctx, consumedHash[:], "new-hash", uuid.New(), now); err != appaccount.ErrInvalidToken {
		t.Fatalf("consumed token error: got %v", err)
	}
	current, other := uuid.New(), uuid.New()
	if err := database.DB.Create(&store.JWTKeyModel{KID: "reset-key", SecretRef: "reset-ref", Status: "active", ActivatedAt: now, MaxTokenExpiry: now.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	for _, jti := range []uuid.UUID{current, other} {
		if err := database.DB.Create(&store.SessionModel{JTI: jti, UserID: userID, KID: "reset-key", IssuedAt: now, ExpiresAt: now.Add(10 * time.Minute), LastActiveAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.ResetPassword(ctx, newHash[:], "reset-hash", uuid.New(), now); err != nil {
		t.Fatal(err)
	}
	var user store.UserModel
	if err := database.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash != "reset-hash" {
		t.Fatalf("password hash: got %q", user.PasswordHash)
	}
	var sessions []store.SessionModel
	if err := database.DB.Where("user_id = ?", userID).Find(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if session.RevokedAt == nil {
			t.Fatalf("session %s was not revoked", session.JTI)
		}
	}
	var token store.AccountTokenModel
	if err := database.DB.Where("token_hash = ?", newHash[:]).First(&token).Error; err != nil {
		t.Fatal(err)
	}
	if token.ConsumedAt == nil {
		t.Fatal("reset token was not consumed")
	}
	var resetEvent store.AuditLogModel
	if err := database.DB.Where("event_type = ?", "account.password_reset").First(&resetEvent).Error; err != nil {
		t.Fatal(err)
	}
}
