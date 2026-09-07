package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Service 拥有身份写入与会话校验；HTTP 只做绑定与投影。
type Service struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s Service) requireDB() error {
	if s.DB == nil {
		return apperr.Internal("身份存储未配置")
	}
	return nil
}

// Principal 是已认证会话投影；商家权限另查 Membership。
type Principal struct {
	UserID      string
	SessionID   string
	Email       string
	DisplayName string
	IsOperator  bool
}

type MembershipView struct {
	MerchantID     string `json:"merchant_id"`
	MerchantName   string `json:"merchant_name"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	MerchantStatus string `json:"merchant_status"`
}

type InviteResult struct {
	InviteID  string    `json:"invite_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s Service) UserCount(ctx context.Context) (int64, error) {
	if err := s.requireDB(); err != nil {
		return 0, err
	}
	var n int64
	err := s.DB.WithContext(ctx).Model(&schema.Users{}).Count(&n).Error
	return n, err
}

func (s Service) MerchantCount(ctx context.Context) (int64, error) {
	if err := s.requireDB(); err != nil {
		return 0, err
	}
	var n int64
	err := s.DB.WithContext(ctx).Model(&schema.Merchants{}).Count(&n).Error
	return n, err
}

func (s Service) LoadPrincipal(ctx context.Context, sessionID string) (*Principal, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	now := s.now()
	var row struct {
		SessionID   string
		UserID      string
		Email       string
		DisplayName string
		IsOperator  bool
		UserStatus  string
		ExpiresAt   time.Time
		RevokedAt   *time.Time
	}
	err := s.DB.WithContext(ctx).Raw(`
		SELECT s.id AS session_id, u.id AS user_id, u.email, u.display_name, u.is_operator,
		       u.status AS user_status, s.expires_at, s.revoked_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ?
	`, sessionID).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.SessionID == "" {
		return nil, nil
	}
	if row.RevokedAt != nil || !row.ExpiresAt.After(now) || row.UserStatus != UserStatusActive {
		return nil, nil
	}
	return &Principal{
		UserID:      row.UserID,
		SessionID:   row.SessionID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		IsOperator:  row.IsOperator,
	}, nil
}

func (s Service) Bootstrap(ctx context.Context, adminKey, expectedAdminKey, email, password, displayName, merchantName string) (*Principal, string, error) {
	if err := s.requireDB(); err != nil {
		return nil, "", err
	}
	if !secretEqual(adminKey, expectedAdminKey) || expectedAdminKey == "" {
		return nil, "", apperr.Error{Status: 401, Detail: "管理员密钥不正确"}
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return nil, "", apperr.Validation(err.Error())
	}
	if err := validatePassword(password); err != nil {
		return nil, "", apperr.Validation(err.Error())
	}
	merchantName = strings.TrimSpace(merchantName)
	if merchantName == "" {
		return nil, "", apperr.Validation("商家名称不能为空")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.Split(emailNorm, "@")[0]
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, "", apperr.Internal("密码哈希失败")
	}
	now := s.now()
	var principal *Principal
	var sessionID string
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var userCount int64
		if err := gdb.Model(&schema.Users{}).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount > 0 {
			return apperr.Conflict("实例已初始化，请使用密码登录")
		}
		var merchantCount int64
		if err := gdb.Model(&schema.Merchants{}).Count(&merchantCount).Error; err != nil {
			return err
		}
		if merchantCount > 0 {
			return apperr.Conflict("实例已存在商家，无法引导初始化")
		}
		userID := clockid.New()
		merchantID := clockid.New()
		membershipID := clockid.New()
		sessionID = clockid.New()
		user := schema.Users{
			ID: userID, Email: emailNorm, PasswordHash: passwordHash, DisplayName: displayName,
			IsOperator: true, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		merchant := schema.Merchants{
			ID: merchantID, Name: merchantName, Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		membership := schema.Memberships{
			ID: membershipID, MerchantID: merchantID, UserID: userID, Role: RoleOwner,
			Status: MembershipStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		session := schema.AuthSessions{
			ID: sessionID, UserID: userID, ExpiresAt: now.Add(sessionTTL), CreatedAt: now,
		}
		if err := gdb.Create(&user).Error; err != nil {
			return err
		}
		if err := gdb.Create(&merchant).Error; err != nil {
			return err
		}
		if err := gdb.Create(&membership).Error; err != nil {
			return err
		}
		if err := gdb.Create(&session).Error; err != nil {
			return err
		}
		principal = &Principal{
			UserID: userID, SessionID: sessionID, Email: emailNorm,
			DisplayName: displayName, IsOperator: true,
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return principal, sessionID, nil
}

func (s Service) Login(ctx context.Context, email, password string) (*Principal, string, error) {
	if err := s.requireDB(); err != nil {
		return nil, "", err
	}
	if len([]byte(password)) > maxPasswordBytes {
		return nil, "", apperr.Validation("密码过长")
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return nil, "", apperr.Error{Status: 401, Detail: "邮箱或密码不正确"}
	}
	var user schema.Users
	err = s.DB.WithContext(ctx).Where("email = ?", emailNorm).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", apperr.Error{Status: 401, Detail: "邮箱或密码不正确"}
	}
	if err != nil {
		return nil, "", err
	}
	if user.Status != UserStatusActive || !checkPassword(user.PasswordHash, password) {
		return nil, "", apperr.Error{Status: 401, Detail: "邮箱或密码不正确"}
	}
	now := s.now()
	sessionID := clockid.New()
	session := schema.AuthSessions{
		ID: sessionID, UserID: user.ID, ExpiresAt: now.Add(sessionTTL), CreatedAt: now,
	}
	if err := s.DB.WithContext(ctx).Create(&session).Error; err != nil {
		return nil, "", err
	}
	return &Principal{
		UserID: user.ID, SessionID: sessionID, Email: user.Email,
		DisplayName: user.DisplayName, IsOperator: user.IsOperator,
	}, sessionID, nil
}

func (s Service) RevokeSession(ctx context.Context, sessionID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	now := s.now()
	return s.DB.WithContext(ctx).Model(&schema.AuthSessions{}).
		Where("id = ? AND revoked_at IS NULL", sessionID).
		Updates(map[string]any{"revoked_at": now}).Error
}

func (s Service) ListMemberships(ctx context.Context, userID string) ([]MembershipView, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	var rows []MembershipView
	err := s.DB.WithContext(ctx).Raw(`
		SELECT m.merchant_id, mer.name AS merchant_name, m.role, m.status,
		       mer.status AS merchant_status
		FROM memberships m
		JOIN merchants mer ON mer.id = m.merchant_id
		WHERE m.user_id = ? AND m.status = ?
		ORDER BY mer.name, m.merchant_id
	`, userID, MembershipStatusActive).Scan(&rows).Error
	return rows, err
}

// MerchantStatus 返回商家启停状态；不存在则 NotFound。
func (s Service) MerchantStatus(ctx context.Context, merchantID string) (string, error) {
	if err := s.requireDB(); err != nil {
		return "", err
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return "", apperr.Validation("缺少商家 ID")
	}
	var row schema.Merchants
	err := s.DB.WithContext(ctx).Select("id", "status").Where("id = ?", merchantID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", apperr.NotFound("商家不存在")
	}
	if err != nil {
		return "", err
	}
	return row.Status, nil
}

func (s Service) requireMerchantActive(ctx context.Context, merchantID string) error {
	status, err := s.MerchantStatus(ctx, merchantID)
	if err != nil {
		return err
	}
	if status == MerchantStatusSuspended {
		return apperr.Forbidden("商家已停用，无法写入")
	}
	return nil
}

// SetMerchantStatus 由站点 Operator 启停商家；status 仅 active|suspended。
func (s Service) SetMerchantStatus(ctx context.Context, operator UserRef, merchantID, status string) (*schema.Merchants, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	if !operator.IsOperator {
		return nil, apperr.Forbidden("仅站点 Operator 可启停商家")
	}
	merchantID = strings.TrimSpace(merchantID)
	status = strings.TrimSpace(status)
	if merchantID == "" {
		return nil, apperr.Validation("缺少商家 ID")
	}
	if status != MerchantStatusActive && status != MerchantStatusSuspended {
		return nil, apperr.Validation("商家状态无效")
	}
	now := s.now()
	var merchant schema.Merchants
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", merchantID).Take(&merchant).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("商家不存在")
		}
		if err != nil {
			return err
		}
		if merchant.Status == status {
			return nil
		}
		if err := gdb.Model(&merchant).Updates(map[string]any{
			"status": status, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		merchant.Status = status
		merchant.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &merchant, nil
}

func (s Service) ActiveMembership(ctx context.Context, userID, merchantID string) (*schema.Memberships, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	var row schema.Memberships
	err := s.DB.WithContext(ctx).
		Where("user_id = ? AND merchant_id = ? AND status = ?", userID, merchantID, MembershipStatusActive).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.Forbidden("不是该商家成员")
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s Service) CreateMerchant(ctx context.Context, operator UserRef, name string) (*schema.Merchants, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	if !operator.IsOperator {
		return nil, apperr.Forbidden("仅站点 Operator 可创建商家")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperr.Validation("商家名称不能为空")
	}
	now := s.now()
	var merchant schema.Merchants
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var ids []string
		if err := gdb.Clauses(pfdb.ForUpdate()).
			Model(&schema.Merchants{}).
			Order("id").
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > 0 {
			return apperr.Conflict("当前版本仅允许唯一开发商家")
		}
		merchant = schema.Merchants{
			ID: clockid.New(), Name: name, Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		membership := schema.Memberships{
			ID: clockid.New(), MerchantID: merchant.ID, UserID: operator.UserID, Role: RoleOwner,
			Status: MembershipStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		if err := gdb.Create(&merchant).Error; err != nil {
			return err
		}
		return gdb.Create(&membership).Error
	})
	if err != nil {
		return nil, err
	}
	return &merchant, nil
}

type UserRef struct {
	UserID     string
	IsOperator bool
}

func (s Service) CreateInvite(ctx context.Context, actor UserRef, merchantID, email, role string) (*InviteResult, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	if err := s.requireMerchantActive(ctx, merchantID); err != nil {
		return nil, err
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return nil, apperr.Validation(err.Error())
	}
	if !validRole(role) {
		return nil, apperr.Validation("成员角色无效")
	}
	membership, err := s.ActiveMembership(ctx, actor.UserID, merchantID)
	if err != nil {
		return nil, err
	}
	if membership.Role != RoleOwner {
		return nil, apperr.Forbidden("仅商家 Owner 可邀请成员")
	}
	token, err := newOpaqueToken()
	if err != nil {
		return nil, apperr.Internal("生成邀请令牌失败")
	}
	now := s.now()
	invite := schema.MerchantInvites{
		ID: clockid.New(), MerchantID: merchantID, Email: emailNorm, Role: role,
		TokenHash: hashToken(token), InvitedBy: actor.UserID, ExpiresAt: now.Add(inviteTTL), CreatedAt: now,
	}
	if err := s.DB.WithContext(ctx).Create(&invite).Error; err != nil {
		return nil, err
	}
	return &InviteResult{
		InviteID: invite.ID, Email: emailNorm, Role: role, Token: token, ExpiresAt: invite.ExpiresAt,
	}, nil
}

// AcceptInvite consumes an invitation after proving the invited account.
// currentUserID is the already validated browser session identity, or empty
// when the request has no active session.
func (s Service) AcceptInvite(ctx context.Context, token, password, displayName, currentUserID string) (*Principal, string, error) {
	if err := s.requireDB(); err != nil {
		return nil, "", err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, "", apperr.Validation("邀请令牌无效")
	}
	if len([]byte(password)) > maxPasswordBytes {
		return nil, "", apperr.Validation("密码过长")
	}
	tokenHash := hashToken(token)
	now := s.now()
	var principal *Principal
	var sessionID string
	currentID := strings.TrimSpace(currentUserID)
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var invite schema.MerchantInvites
		err := gdb.Clauses(pfdb.ForUpdate()).
			Where("token_hash = ?", tokenHash).
			Take(&invite).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("邀请不存在")
		}
		if err != nil {
			return err
		}
		if invite.RevokedAt != nil || invite.AcceptedAt != nil || !invite.ExpiresAt.After(now) {
			return apperr.Gone("邀请已失效")
		}
		var merchant schema.Merchants
		err = gdb.Select("id", "status").Where("id = ?", invite.MerchantID).Take(&merchant).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("商家不存在")
		}
		if err != nil {
			return err
		}
		if merchant.Status == MerchantStatusSuspended {
			return apperr.Forbidden("商家已停用，无法接受邀请")
		}
		var user schema.Users
		err = gdb.Where("email = ?", invite.Email).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if currentID != "" {
				return apperr.Conflict("当前登录账号与邀请账号不一致，请先退出当前账号")
			}
			if err := validatePassword(password); err != nil {
				return apperr.Validation(err.Error())
			}
			displayName = strings.TrimSpace(displayName)
			if displayName == "" {
				displayName = strings.Split(invite.Email, "@")[0]
			}
			hash, err := hashPassword(password)
			if err != nil {
				return apperr.Internal("密码哈希失败")
			}
			user = schema.Users{
				ID: clockid.New(), Email: invite.Email, PasswordHash: hash, DisplayName: displayName,
				IsOperator: false, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now,
			}
			if err := gdb.Create(&user).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if currentID != "" && currentID != user.ID {
			return apperr.Conflict("当前登录账号与邀请账号不一致，请先退出当前账号")
		} else if user.Status != UserStatusActive {
			return apperr.Forbidden("账号已停用")
		} else if currentID == "" && !checkPassword(user.PasswordHash, password) {
			// An existing account must prove its identity with its password or a
			// matching active session. Empty password is never an invitation login.
			return apperr.Error{Status: 401, Detail: "邮箱或密码不正确"}
		}
		var existing schema.Memberships
		err = gdb.Clauses(pfdb.ForUpdate()).
			Where("merchant_id = ? AND user_id = ?", invite.MerchantID, user.ID).
			Take(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			membership := schema.Memberships{
				ID: clockid.New(), MerchantID: invite.MerchantID, UserID: user.ID, Role: invite.Role,
				Status: MembershipStatusActive, CreatedAt: now, UpdatedAt: now,
			}
			if err := gdb.Create(&membership).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existing.Status == MembershipStatusRevoked {
			if err := gdb.Model(&existing).Updates(map[string]any{
				"status": MembershipStatusActive, "role": invite.Role, "revoked_at": nil, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		} else {
			return apperr.Conflict("已是该商家成员")
		}
		if err := gdb.Model(&invite).Updates(map[string]any{"accepted_at": now}).Error; err != nil {
			return err
		}
		sessionID = clockid.New()
		session := schema.AuthSessions{
			ID: sessionID, UserID: user.ID, ExpiresAt: now.Add(sessionTTL), CreatedAt: now,
		}
		if err := gdb.Create(&session).Error; err != nil {
			return err
		}
		principal = &Principal{
			UserID: user.ID, SessionID: sessionID, Email: user.Email,
			DisplayName: user.DisplayName, IsOperator: user.IsOperator,
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return principal, sessionID, nil
}

func (s Service) RevokeMembership(ctx context.Context, actor UserRef, merchantID, targetUserID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	if err := s.requireMerchantActive(ctx, merchantID); err != nil {
		return err
	}
	now := s.now()
	return tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var actorMembership schema.Memberships
		err := gdb.Clauses(pfdb.ForUpdate()).
			Where("merchant_id = ? AND user_id = ? AND status = ?", merchantID, actor.UserID, MembershipStatusActive).
			Take(&actorMembership).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Forbidden("不是该商家成员")
		}
		if err != nil {
			return err
		}
		if actorMembership.Role != RoleOwner {
			return apperr.Forbidden("仅商家 Owner 可移除成员")
		}
		var target schema.Memberships
		err = gdb.Clauses(pfdb.ForUpdate()).
			Where("merchant_id = ? AND user_id = ?", merchantID, targetUserID).
			Take(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("成员不存在")
		}
		if err != nil {
			return err
		}
		if target.Status == MembershipStatusRevoked {
			return nil
		}
		if target.Role == RoleOwner {
			var ownerCount int64
			if err := gdb.Model(&schema.Memberships{}).
				Where("merchant_id = ? AND role = ? AND status = ?", merchantID, RoleOwner, MembershipStatusActive).
				Count(&ownerCount).Error; err != nil {
				return err
			}
			if ownerCount <= 1 {
				return apperr.Conflict("不能移除最后一位 Owner")
			}
		}
		return gdb.Model(&target).Updates(map[string]any{
			"status": MembershipStatusRevoked, "revoked_at": now, "updated_at": now,
		}).Error
	})
}

func (s Service) RestoreMembership(ctx context.Context, actor UserRef, merchantID, targetUserID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	if err := s.requireMerchantActive(ctx, merchantID); err != nil {
		return err
	}
	now := s.now()
	return tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var actorMembership schema.Memberships
		err := gdb.Clauses(pfdb.ForUpdate()).
			Where("merchant_id = ? AND user_id = ? AND status = ?", merchantID, actor.UserID, MembershipStatusActive).
			Take(&actorMembership).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Forbidden("不是该商家成员")
		}
		if err != nil {
			return err
		}
		if actorMembership.Role != RoleOwner {
			return apperr.Forbidden("仅商家 Owner 可恢复成员")
		}
		var target schema.Memberships
		err = gdb.Clauses(pfdb.ForUpdate()).
			Where("merchant_id = ? AND user_id = ?", merchantID, targetUserID).
			Take(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("成员不存在")
		}
		if err != nil {
			return err
		}
		if target.Status == MembershipStatusActive {
			return nil
		}
		return gdb.Model(&target).Updates(map[string]any{
			"status": MembershipStatusActive, "revoked_at": nil, "updated_at": now,
		}).Error
	})
}

func (s Service) RevokeInvite(ctx context.Context, actor UserRef, merchantID, inviteID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	if err := s.requireMerchantActive(ctx, merchantID); err != nil {
		return err
	}
	membership, err := s.ActiveMembership(ctx, actor.UserID, merchantID)
	if err != nil {
		return err
	}
	if membership.Role != RoleOwner {
		return apperr.Forbidden("仅商家 Owner 可撤销邀请")
	}
	now := s.now()
	res := s.DB.WithContext(ctx).Model(&schema.MerchantInvites{}).
		Where("id = ? AND merchant_id = ? AND accepted_at IS NULL AND revoked_at IS NULL", inviteID, merchantID).
		Updates(map[string]any{"revoked_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("邀请不存在或已失效")
	}
	return nil
}
