// Package identity persists user invitations and account activation.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	store "github.com/pomkita/pomkita-be/internal/repository/store"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdentityRepository persists identity changes.
type IdentityRepository struct {
	db *gorm.DB
}

// ListUsers reads all users in one organization with their role and station views.
func (r *IdentityRepository) ListUsers(ctx context.Context, orgID uuid.UUID) ([]appidentity.UserView, error) {
	if r == nil || r.db == nil {
		return nil, appidentity.ErrDependencyUnavailable
	}
	var users []store.UserModel
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("user_id").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list administration users: %w", err)
	}
	result := make([]appidentity.UserView, 0, len(users))
	for _, user := range users {
		view, err := readUserView(r.db.WithContext(ctx), orgID, user.UserID)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

// ReadUser reads one user in an organization with the role and station views.
func (r *IdentityRepository) ReadUser(ctx context.Context, orgID, userID uuid.UUID) (appidentity.UserView, error) {
	if r == nil || r.db == nil {
		return appidentity.UserView{}, appidentity.ErrDependencyUnavailable
	}
	return readUserView(r.db.WithContext(ctx), orgID, userID)
}

// UpdateUser changes an administration profile in the actor's permitted scope.
func (r *IdentityRepository) UpdateUser(ctx context.Context, actor appidentity.Actor, userID uuid.UUID, request appidentity.UserUpdateRequest, eventID uuid.UUID, now time.Time) (appidentity.UserView, error) {
	if r == nil || r.db == nil {
		return appidentity.UserView{}, appidentity.ErrDependencyUnavailable
	}
	var result appidentity.UserView
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := findAdminTarget(tx, actor, userID)
		if err != nil {
			return err
		}
		if err := checkAdminActor(tx, actor, user.OrgID); err != nil {
			return err
		}
		privileged, err := hasPrivilegedRole(tx, user.OrgID, user.UserID)
		if err != nil {
			return fmt.Errorf("check protected administration user: %w", err)
		}
		if isOwner(actor) && privileged {
			return appidentity.ErrProtectedUser
		}
		if err := tx.Model(&store.UserModel{}).Where("org_id = ? and user_id = ?", user.OrgID, user.UserID).Updates(map[string]any{"display_name": request.DisplayName, "enabled": request.Enabled, "updated_at": now.UTC()}).Error; err != nil {
			return fmt.Errorf("update administration user: %w", err)
		}
		payload, err := json.Marshal(map[string]any{"actor": actor.UserID.String(), "target": user.UserID.String(), "enabled": request.Enabled})
		if err != nil {
			return fmt.Errorf("encode user audit payload: %w", err)
		}
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(user.OrgID, eventID, "user.updated", payload), now); err != nil {
			return fmt.Errorf("append user audit event: %w", err)
		}
		result, err = readUserView(tx, user.OrgID, user.UserID)
		return err
	})
	return result, err
}

// AssignRole grants a station role and appends its audit event atomically.
func (r *IdentityRepository) AssignRole(ctx context.Context, actor appidentity.Actor, userID uuid.UUID, request appidentity.RoleRequest, eventID uuid.UUID, now time.Time) ([]appidentity.RoleView, error) {
	if r == nil || r.db == nil {
		return nil, appidentity.ErrDependencyUnavailable
	}
	var result []appidentity.RoleView
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := findAdminTarget(tx, actor, userID)
		if err != nil {
			return err
		}
		if err := checkAdminActor(tx, actor, user.OrgID); err != nil {
			return err
		}
		if isOwner(actor) && (user.UserID == actor.UserID || request.Role == "Owner" || request.Role == "Superadmin") {
			return appidentity.ErrForbidden
		}
		if err := checkRoleStation(tx, user.OrgID, request.StationID); err != nil {
			return err
		}
		var organization store.OrganizationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ?", user.OrgID).First(&organization).Error; err != nil {
			return appidentity.ErrUserNotFound
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and user_id = ?", user.OrgID, user.UserID).First(&user).Error; err != nil {
			return mapAdminUserError(err)
		}
		var count int64
		if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role = ?", user.OrgID, request.StationID, user.UserID, request.Role).Count(&count).Error; err != nil {
			return fmt.Errorf("check existing user role: %w", err)
		}
		if count != 0 {
			return appidentity.ErrRoleExists
		}
		if roleCombinationBreaksSeparation(tx, user.OrgID, user.UserID, request.StationID, request.Role) {
			return appidentity.ErrRoleSeparation
		}
		if err := tx.Table("user_station_roles").Create(map[string]any{"org_id": user.OrgID, "station_id": request.StationID, "user_id": user.UserID, "role": request.Role}).Error; err != nil {
			return fmt.Errorf("assign user role: %w", err)
		}
		stationName, err := stationName(tx, user.OrgID, request.StationID)
		if err != nil {
			return err
		}
		payload, err := roleAuditPayload(actor.UserID, user.UserID, request, stationName, false, true)
		if err != nil {
			return err
		}
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(user.OrgID, eventID, "role.assigned", payload), now); err != nil {
			return fmt.Errorf("append role assignment audit event: %w", err)
		}
		result, err = readUserRoles(tx, user.OrgID, user.UserID)
		return err
	})
	return result, err
}

// RemoveRole removes a station role and appends its audit event atomically.
func (r *IdentityRepository) RemoveRole(ctx context.Context, actor appidentity.Actor, userID uuid.UUID, request appidentity.RoleRequest, eventID uuid.UUID, now time.Time) error {
	if r == nil || r.db == nil {
		return appidentity.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := findAdminTarget(tx, actor, userID)
		if err != nil {
			return err
		}
		if err := checkAdminActor(tx, actor, user.OrgID); err != nil {
			return err
		}
		if isOwner(actor) && (user.UserID == actor.UserID || request.Role == "Owner" || request.Role == "Superadmin") {
			return appidentity.ErrForbidden
		}
		if err := checkRoleStation(tx, user.OrgID, request.StationID); err != nil {
			return err
		}
		var organization store.OrganizationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ?", user.OrgID).First(&organization).Error; err != nil {
			return appidentity.ErrUserNotFound
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and user_id = ?", user.OrgID, user.UserID).First(&user).Error; err != nil {
			return mapAdminUserError(err)
		}
		var count int64
		if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role = ?", user.OrgID, request.StationID, user.UserID, request.Role).Count(&count).Error; err != nil {
			return fmt.Errorf("check user role: %w", err)
		}
		if count == 0 {
			return appidentity.ErrRoleNotFound
		}
		if request.Role == "Owner" {
			var owners int64
			if err := tx.Table("user_station_roles").Where("org_id = ? and role = ?", user.OrgID, "Owner").Count(&owners).Error; err != nil {
				return fmt.Errorf("count organization owners: %w", err)
			}
			if owners <= 1 {
				return appidentity.ErrLastOwner
			}
		}
		if err := tx.Exec("DELETE FROM user_station_roles WHERE org_id = ? AND station_id = ? AND user_id = ? AND role = ?", user.OrgID, request.StationID, user.UserID, request.Role).Error; err != nil {
			return fmt.Errorf("remove user role: %w", err)
		}
		stationName, err := stationName(tx, user.OrgID, request.StationID)
		if err != nil {
			return err
		}
		payload, err := roleAuditPayload(actor.UserID, user.UserID, request, stationName, true, false)
		if err != nil {
			return err
		}
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(user.OrgID, eventID, "role.revoked", payload), now); err != nil {
			return fmt.Errorf("append role removal audit event: %w", err)
		}
		return nil
	})
}

// RoleHistory reads role events from the audit chain for one target user.
func (r *IdentityRepository) RoleHistory(ctx context.Context, orgID, userID uuid.UUID) ([]appidentity.RoleHistoryEvent, error) {
	if r == nil || r.db == nil {
		return nil, appidentity.ErrDependencyUnavailable
	}
	var target store.UserModel
	query := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if orgID != uuid.Nil {
		query = query.Where("org_id = ?", orgID)
	}
	if err := query.First(&target).Error; err != nil {
		return nil, mapAdminUserError(err)
	}
	orgID = target.OrgID
	var rows []store.AuditLogModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and event_type in ? and payload ->> 'target' = ?", orgID, []string{"role.assigned", "role.revoked"}, userID.String()).Order("org_sequence").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read role history: %w", err)
	}
	result := make([]appidentity.RoleHistoryEvent, 0, len(rows))
	for _, row := range rows {
		var payload struct {
			Actor     uuid.UUID `json:"actor"`
			Target    uuid.UUID `json:"target"`
			StationID uuid.UUID `json:"station_id"`
			Station   string    `json:"station"`
			Role      string    `json:"role"`
			Before    bool      `json:"before"`
			After     bool      `json:"after"`
		}
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode role history payload: %w", err)
		}
		result = append(result, appidentity.RoleHistoryEvent{Sequence: row.OrgSequence, Actor: payload.Actor, Target: payload.Target, StationID: payload.StationID, Station: payload.Station, Role: payload.Role, Before: payload.Before, After: payload.After, CreatedAt: row.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z")})
	}
	return result, nil
}

// IssuePasswordReset creates one hashed administrator reset token atomically.
func (r *IdentityRepository) IssuePasswordReset(ctx context.Context, actor appidentity.Actor, userID, tokenID uuid.UUID, tokenHash []byte, now, expiry time.Time) error {
	if r == nil || r.db == nil {
		return appidentity.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := findAdminTarget(tx, actor, userID)
		if err != nil {
			return err
		}
		if err := checkAdminActor(tx, actor, user.OrgID); err != nil {
			return err
		}
		privileged, err := hasPrivilegedRole(tx, user.OrgID, user.UserID)
		if err != nil {
			return fmt.Errorf("check protected reset user: %w", err)
		}
		if isOwner(actor) && privileged {
			return appidentity.ErrProtectedUser
		}
		if !user.Enabled {
			return appidentity.ErrDisabledTarget
		}
		if err := tx.Model(&store.AccountTokenModel{}).Where("user_id = ? and purpose = ? and consumed_at is null", user.UserID, "password_reset").Update("consumed_at", now.UTC()).Error; err != nil {
			return fmt.Errorf("consume previous administrator reset token: %w", err)
		}
		if err := tx.Create(&store.AccountTokenModel{TokenID: tokenID, OrgID: user.OrgID, UserID: user.UserID, Purpose: "password_reset", TokenHash: append([]byte(nil), tokenHash...), ExpiresAt: expiry.UTC(), CreatedBy: &actor.UserID, CreatedAt: now.UTC()}).Error; err != nil {
			return fmt.Errorf("create administrator reset token: %w", err)
		}
		payload, err := json.Marshal(map[string]any{"actor": actor.UserID.String(), "target": user.UserID.String()})
		if err != nil {
			return fmt.Errorf("encode administrator reset audit payload: %w", err)
		}
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(user.OrgID, tokenID, "user.password_reset_issued", payload), now); err != nil {
			return fmt.Errorf("append administrator reset audit event: %w", err)
		}
		return nil
	})
}

func readUserView(db *gorm.DB, orgID, userID uuid.UUID) (appidentity.UserView, error) {
	var row struct {
		UserID      uuid.UUID
		OrgID       uuid.UUID
		Email       string
		Username    *string
		DisplayName string
		Enabled     bool
	}
	query := db.Table("users").Select("user_id, org_id, email, username, display_name, enabled").Where("user_id = ?", userID)
	if orgID != uuid.Nil {
		query = query.Where("org_id = ?", orgID)
	}
	if err := query.Take(&row).Error; err != nil {
		return appidentity.UserView{}, mapAdminUserError(err)
	}
	orgID = row.OrgID
	roles, err := readUserRoles(db, orgID, userID)
	if err != nil {
		return appidentity.UserView{}, err
	}
	stations := make([]appidentity.StationView, 0, len(roles))
	seen := make(map[uuid.UUID]struct{})
	for _, role := range roles {
		if _, ok := seen[role.StationID]; ok {
			continue
		}
		seen[role.StationID] = struct{}{}
		stations = append(stations, appidentity.StationView{StationID: role.StationID, Name: role.StationName})
	}
	sort.Slice(roles, func(i, j int) bool { return roleLess(roles[i], roles[j]) })
	sort.Slice(stations, func(i, j int) bool { return stations[i].StationID.String() < stations[j].StationID.String() })
	return appidentity.UserView{UserID: row.UserID, Email: row.Email, Username: row.Username, DisplayName: row.DisplayName, Enabled: row.Enabled, Roles: roles, Stations: stations}, nil
}

func readUserRoles(db *gorm.DB, orgID, userID uuid.UUID) ([]appidentity.RoleView, error) {
	var rows []struct {
		Role        string
		StationID   uuid.UUID
		StationName string
	}
	if err := db.Table("user_station_roles").Select("user_station_roles.role, user_station_roles.station_id, stations.name as station_name").Joins("join stations on stations.org_id = user_station_roles.org_id and stations.station_id = user_station_roles.station_id").Where("user_station_roles.org_id = ? and user_station_roles.user_id = ?", orgID, userID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read administration user roles: %w", err)
	}
	result := make([]appidentity.RoleView, 0, len(rows))
	for _, row := range rows {
		result = append(result, appidentity.RoleView{Role: row.Role, StationID: row.StationID, StationName: row.StationName})
	}
	sort.Slice(result, func(i, j int) bool { return roleLess(result[i], result[j]) })
	return result, nil
}

func roleLess(left, right appidentity.RoleView) bool {
	if left.StationID == right.StationID {
		return left.Role < right.Role
	}
	return left.StationID.String() < right.StationID.String()
}

func findAdminTarget(tx *gorm.DB, actor appidentity.Actor, userID uuid.UUID) (store.UserModel, error) {
	var user store.UserModel
	query := tx.Where("user_id = ?", userID)
	if !isSuperadmin(actor) {
		query = query.Where("org_id = ?", actor.OrgID)
	}
	if err := query.First(&user).Error; err != nil {
		return user, mapAdminUserError(err)
	}
	return user, nil
}

func checkAdminActor(tx *gorm.DB, actor appidentity.Actor, orgID uuid.UUID) error {
	if actor.UserID == uuid.Nil || actor.OrgID == uuid.Nil {
		return appidentity.ErrForbidden
	}
	if isOwner(actor) && actor.OrgID != orgID {
		return appidentity.ErrWrongOrganization
	}
	var count int64
	role := "Owner"
	if isSuperadmin(actor) {
		role = "Superadmin"
	}
	if err := tx.Table("user_station_roles").Where("org_id = ? and user_id = ? and role = ?", orgID, actor.UserID, role).Count(&count).Error; err != nil {
		return fmt.Errorf("check administration actor: %w", err)
	}
	if count == 0 {
		return appidentity.ErrForbidden
	}
	return nil
}

func checkRoleStation(tx *gorm.DB, orgID, stationID uuid.UUID) error {
	var count int64
	if err := tx.Table("stations").Where("org_id = ? and station_id = ?", orgID, stationID).Count(&count).Error; err != nil {
		return fmt.Errorf("check role station: %w", err)
	}
	if count == 0 {
		return appidentity.ErrWrongOrganization
	}
	return nil
}

func hasPrivilegedRole(tx *gorm.DB, orgID, userID uuid.UUID) (bool, error) {
	var count int64
	err := tx.Table("user_station_roles").Where("org_id = ? and user_id = ? and role in ?", orgID, userID, []string{"Owner", "Superadmin"}).Count(&count).Error
	return count != 0, err
}

func roleCombinationBreaksSeparation(tx *gorm.DB, orgID, userID, stationID uuid.UUID, added string) bool {
	var roles []string
	if tx.Table("user_station_roles").Where("org_id = ? and user_id = ? and station_id = ?", orgID, userID, stationID).Pluck("role", &roles).Error != nil {
		return true
	}
	hasRequester, hasApprover := added == "Supervisor", added == "Station Admin" || added == "Owner" || added == "Superadmin"
	for _, role := range roles {
		if role == "Supervisor" {
			hasRequester = true
		}
		if role == "Station Admin" || role == "Owner" || role == "Superadmin" {
			hasApprover = true
		}
	}
	return hasRequester && hasApprover
}

func stationName(tx *gorm.DB, orgID, stationID uuid.UUID) (string, error) {
	var station store.StationModel
	if err := tx.Where("org_id = ? and station_id = ?", orgID, stationID).First(&station).Error; err != nil {
		return "", appidentity.ErrWrongOrganization
	}
	return station.Name, nil
}

func roleAuditPayload(actorID, targetID uuid.UUID, request appidentity.RoleRequest, station string, before, after bool) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{"actor": actorID.String(), "target": targetID.String(), "station_id": request.StationID.String(), "station": station, "role": request.Role, "before": before, "after": after})
	if err != nil {
		return nil, fmt.Errorf("encode role audit payload: %w", err)
	}
	return payload, nil
}

func mapAdminUserError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appidentity.ErrUserNotFound
	}
	return fmt.Errorf("read administration user: %w", err)
}

func isSuperadmin(actor appidentity.Actor) bool {
	for _, role := range actor.Roles {
		if role == "Superadmin" {
			return true
		}
	}
	return false
}
func isOwner(actor appidentity.Actor) bool {
	return !isSuperadmin(actor) && containsRole(actor.Roles, "Owner")
}
func containsRole(roles []string, target string) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

// NewIdentityRepository creates an identity repository.
func NewIdentityRepository(database *store.Store) *IdentityRepository {
	if database == nil {
		return &IdentityRepository{}
	}
	return &IdentityRepository{db: database.DB}
}

// CreateInvitation creates the user, role, token, and audit event atomically.
func (r *IdentityRepository) CreateInvitation(ctx context.Context, request appidentity.InvitationRequest, eventID uuid.UUID, tokenHash []byte, now time.Time) (uuid.UUID, error) {
	if r == nil || r.db == nil {
		return uuid.Nil, appidentity.ErrDependencyUnavailable
	}
	userID := uuid.New()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var organization store.OrganizationModel
		if err := tx.Where("org_id = ?", request.TargetOrgID).First(&organization).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrWrongOrganization
			}
			return fmt.Errorf("find invitation organization: %w", err)
		}
		var stationCount int64
		if err := tx.Model(&store.StationModel{}).Where("org_id = ?", request.TargetOrgID).Count(&stationCount).Error; err != nil {
			return fmt.Errorf("count invitation stations: %w", err)
		}
		if stationCount == 0 {
			return appidentity.ErrNoStation
		}
		var station store.StationModel
		if err := tx.Where("org_id = ? and station_id = ?", request.TargetOrgID, request.StationID).First(&station).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrWrongOrganization
			}
			return fmt.Errorf("find invitation station: %w", err)
		}
		var emailCount int64
		if err := tx.Model(&store.UserModel{}).Where("lower(email) = lower(?)", strings.TrimSpace(request.Email)).Count(&emailCount).Error; err != nil {
			return fmt.Errorf("check invitation email: %w", err)
		}
		if emailCount != 0 {
			return appidentity.ErrEmailConflict
		}
		createdAt := now.UTC()
		user := map[string]any{
			"user_id": userID, "org_id": request.TargetOrgID, "email": strings.TrimSpace(request.Email),
			"display_name": strings.TrimSpace(request.DisplayName), "enabled": false,
			"invited_at": createdAt, "updated_at": createdAt, "password_hash": nil, "username": nil,
			"created_at": createdAt,
		}
		if err := tx.Table("users").Create(user).Error; err != nil {
			return fmt.Errorf("create invited user: %w", err)
		}
		if err := tx.Table("user_station_roles").Create(map[string]any{
			"org_id": request.TargetOrgID, "station_id": request.StationID, "user_id": userID, "role": request.Role,
		}).Error; err != nil {
			return fmt.Errorf("create invited user role: %w", err)
		}
		if err := tx.Model(&store.AccountTokenModel{}).Where("user_id = ? and purpose = ? and consumed_at is null", userID, "invitation").Update("consumed_at", createdAt).Error; err != nil {
			return fmt.Errorf("consume previous invitation token: %w", err)
		}
		createdBy := request.ActorID
		token := store.AccountTokenModel{TokenID: eventID, OrgID: request.TargetOrgID, UserID: userID, Purpose: "invitation", TokenHash: append([]byte(nil), tokenHash...), ExpiresAt: createdAt.Add(7 * 24 * time.Hour), CreatedBy: &createdBy, CreatedAt: createdAt}
		if err := tx.Create(&token).Error; err != nil {
			return fmt.Errorf("create invitation token: %w", err)
		}
		payload := []byte(fmt.Sprintf(`{"actor_user_id":%q,"user_id":%q,"station_id":%q,"role":%q}`, request.ActorID.String(), userID.String(), station.StationID.String(), request.Role))
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(request.TargetOrgID, uuid.New(), "user.created", payload), createdAt); err != nil {
			return fmt.Errorf("append user creation audit event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// AcceptInvitation activates a user and consumes the matching token atomically.
func (r *IdentityRepository) AcceptInvitation(ctx context.Context, request appidentity.AcceptanceRequest, passwordHash string, tokenHash []byte, now time.Time) error {
	if r == nil || r.db == nil {
		return appidentity.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token store.AccountTokenModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("purpose = ? and token_hash = ?", "invitation", tokenHash).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrInvalidToken
			}
			return fmt.Errorf("find invitation token: %w", err)
		}
		if token.ConsumedAt != nil || !now.Before(token.ExpiresAt) {
			return appidentity.ErrInvalidToken
		}
		var usernameCount int64
		if err := tx.Model(&store.UserModel{}).Where("lower(username) = lower(?) and user_id <> ?", request.Username, token.UserID).Count(&usernameCount).Error; err != nil {
			return fmt.Errorf("check invitation username: %w", err)
		}
		if usernameCount != 0 {
			return appidentity.ErrUsernameConflict
		}
		var user store.UserModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and user_id = ?", token.OrgID, token.UserID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrInvalidToken
			}
			return fmt.Errorf("find invited user: %w", err)
		}
		if user.Enabled {
			return appidentity.ErrInvalidToken
		}
		activatedAt := now.UTC()
		if err := tx.Model(&store.UserModel{}).Where("org_id = ? and user_id = ?", token.OrgID, token.UserID).Updates(map[string]any{
			"username": request.Username, "password_hash": passwordHash, "display_name": request.DisplayName,
			"enabled": true, "activated_at": activatedAt, "updated_at": activatedAt,
		}).Error; err != nil {
			return fmt.Errorf("activate invited user: %w", err)
		}
		if err := tx.Model(&store.AccountTokenModel{}).Where("token_id = ?", token.TokenID).Update("consumed_at", activatedAt).Error; err != nil {
			return fmt.Errorf("consume invitation token: %w", err)
		}
		payload := []byte(fmt.Sprintf(`{"user_id":%q,"username":%q}`, token.UserID.String(), request.Username))
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(token.OrgID, uuid.New(), "invitation.accepted", payload), activatedAt); err != nil {
			return fmt.Errorf("append invitation acceptance audit event: %w", err)
		}
		return nil
	})
}

func appauditRequest(orgID, eventID uuid.UUID, eventType string, payload []byte) appaudit.AppendRequest {
	return appaudit.AppendRequest{OrgID: orgID, EventID: eventID, EventType: eventType, Payload: payload, Outcome: "success"}
}
