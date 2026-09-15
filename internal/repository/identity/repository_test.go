package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
)

func TestIdentityRepository_CreateAndAcceptInvitationIsAtomicAndAudited(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := database.DB.Create(&store.StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := database.DB.Create(&store.UserModel{UserID: actorID, OrgID: orgID, DisplayName: "Owner", Email: "owner@example.test", Username: "owner", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create actor: %v", err)
	}
	repository := NewIdentityRepository(database)
	rawToken := "known-invitation-token"
	digest := sha256.Sum256([]byte(rawToken))
	request := appidentity.InvitationRequest{ActorID: actorID, ActorOrgID: orgID, ActorRole: "Owner", TargetOrgID: orgID, StationID: stationID, Email: "new@example.test", DisplayName: "New User", Role: "Operator"}
	userID, err := repository.CreateInvitation(ctx, request, uuid.New(), digest[:], now)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	var token store.AccountTokenModel
	if err := database.DB.Where("user_id = ?", userID).First(&token).Error; err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(token.TokenHash) == rawToken || len(token.TokenHash) != sha256.Size {
		t.Fatalf("token storage is not a SHA-256 digest: %x", token.TokenHash)
	}
	if err := repository.AcceptInvitation(ctx, appidentity.AcceptanceRequest{Token: rawToken, Username: "new.user", Password: "hash", DisplayName: "Activated User"}, "bcrypt-hash", digest[:], now.Add(time.Hour)); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	var user store.UserModel
	if err := database.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		t.Fatalf("read activated user: %v", err)
	}
	if !user.Enabled || user.Username != "new.user" || user.PasswordHash != "bcrypt-hash" || user.ActivatedAt == nil {
		t.Fatalf("activated user: %+v", user)
	}
	var auditCount int64
	if err := database.DB.Table("audit_log").Where("org_id = ? and event_type in ?", orgID, []string{"user.created", "invitation.accepted"}).Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("audit event count: got %d, want 2", auditCount)
	}
	if err := repository.AcceptInvitation(ctx, appidentity.AcceptanceRequest{Token: rawToken, Username: "other-user", Password: "hash", DisplayName: "Other"}, "bcrypt-hash", digest[:], now.Add(2*time.Hour)); !errors.Is(err, appidentity.ErrInvalidToken) {
		t.Fatalf("consumed token error: got %v, want invalid token", err)
	}
	_, err = repository.CreateInvitation(ctx, request, uuid.New(), digest[:], now.Add(2*time.Hour))
	if !errors.Is(err, appidentity.ErrEmailConflict) {
		t.Fatalf("duplicate email error: got %v, want email conflict", err)
	}
	secondRequest := request
	secondRequest.Email = "second@example.test"
	secondToken := "second-invitation-token"
	secondDigest := sha256.Sum256([]byte(secondToken))
	if _, err := repository.CreateInvitation(ctx, secondRequest, uuid.New(), secondDigest[:], now.Add(3*time.Hour)); err != nil {
		t.Fatalf("create second invitation: %v", err)
	}
	if err := repository.AcceptInvitation(ctx, appidentity.AcceptanceRequest{Token: secondToken, Username: "new.user", Password: "hash", DisplayName: "Second"}, "bcrypt-hash", secondDigest[:], now.Add(4*time.Hour)); !errors.Is(err, appidentity.ErrUsernameConflict) {
		t.Fatalf("duplicate username error: got %v, want username conflict", err)
	}
	expiredToken := "expired-invitation-token"
	expiredDigest := sha256.Sum256([]byte(expiredToken))
	thirdRequest := request
	thirdRequest.Email = "third@example.test"
	if _, err := repository.CreateInvitation(ctx, thirdRequest, uuid.New(), expiredDigest[:], now.Add(5*time.Hour)); err != nil {
		t.Fatalf("create expired invitation: %v", err)
	}
	if err := repository.AcceptInvitation(ctx, appidentity.AcceptanceRequest{Token: expiredToken, Username: "third-user", Password: "hash", DisplayName: "Third"}, "bcrypt-hash", expiredDigest[:], now.Add(12*24*time.Hour)); !errors.Is(err, appidentity.ErrInvalidToken) {
		t.Fatalf("expired token error: got %v, want invalid token", err)
	}
}

func TestIdentityRepository_CreateInvitationRejectsOrganizationWithoutStation(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	orgID, actorID := uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Empty Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	repository := NewIdentityRepository(database)
	digest := sha256.Sum256([]byte("token"))
	_, err := repository.CreateInvitation(ctx, appidentity.InvitationRequest{ActorID: actorID, ActorOrgID: orgID, ActorRole: "Owner", TargetOrgID: orgID, StationID: uuid.New(), Email: "new@example.test", DisplayName: "New User", Role: "Operator"}, uuid.New(), digest[:], now)
	if !errors.Is(err, appidentity.ErrNoStation) {
		t.Fatalf("error: got %v, want no station", err)
	}
}
