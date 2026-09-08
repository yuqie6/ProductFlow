package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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
	// RecoveryChallengeIDSecret is an env-only secret used to make recovery
	// response IDs stable across non-delivery responses without exposing email
	// addresses. Production supplies SESSION_SECRET; an omitted secret makes
	// password recovery unavailable.
	RecoveryChallengeIDSecret string
	EnsureRegistrationQuota   func(ctx context.Context, db *gorm.DB, merchantID string) error
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

// Principal 是已认证会话投影。普通账号的商家归属直接来自 users.merchant_id；
// Operator 的 MerchantID 可为空，且不影响其独立的跨商管理权限。
type Principal struct {
	UserID      string
	SessionID   string
	Email       string
	DisplayName string
	IsOperator  bool
	MerchantID  *string
	Locale      string
	Theme       string
}

// MerchantView 是会话与直接商家归属的稳定投影。
type MerchantView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

const (
	merchantListDefaultPageSize = 20
	merchantListMaxPageSize     = 100
	merchantListMaxPage         = 100000
)

// MerchantPage 是 Operator 商家发现接口的有界列表投影。
type MerchantPage struct {
	Items    []MerchantView `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type principalRow struct {
	SessionID   string
	UserID      string
	Email       string
	DisplayName string
	IsOperator  bool
	MerchantID  sql.NullString
	Locale      string
	Theme       string
	UserStatus  string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

type userRow struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	IsOperator   bool
	MerchantID   sql.NullString
	Status       string
	Locale       string
	Theme        string
}

func nullableID(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	id := strings.TrimSpace(value.String)
	if id == "" {
		return nil
	}
	return &id
}

func createUser(gdb *gorm.DB, id, email, passwordHash, displayName string, operator bool, status string, now time.Time, merchantID *string) error {
	return gdb.Table("users").Create(map[string]any{
		"id":            id,
		"email":         email,
		"password_hash": passwordHash,
		"display_name":  displayName,
		"is_operator":   operator,
		"status":        status,
		"merchant_id":   merchantID,
		"locale":        defaultAccountLocale,
		"theme":         defaultAccountTheme,
		"created_at":    now,
		"updated_at":    now,
	}).Error
}

func (s Service) UserCount(ctx context.Context) (int64, error) {
	if err := s.requireDB(); err != nil {
		return 0, err
	}
	var n int64
	err := s.DB.WithContext(ctx).Model(&schema.Users{}).Count(&n).Error
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
	var row principalRow
	err := s.DB.WithContext(ctx).Raw(`
		SELECT s.id AS session_id, u.id AS user_id, u.email, u.display_name, u.is_operator,
		       u.merchant_id, u.locale, u.theme, u.status AS user_status, s.expires_at, s.revoked_at
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
		MerchantID:  nullableID(row.MerchantID),
		Locale:      row.Locale,
		Theme:       row.Theme,
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
		sessionID = clockid.New()
		merchant := schema.Merchants{
			ID: merchantID, Name: merchantName, Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		session := schema.AuthSessions{
			ID: sessionID, UserID: userID, ExpiresAt: now.Add(sessionTTL), CreatedAt: now,
		}
		if err := gdb.Create(&merchant).Error; err != nil {
			return err
		}
		if err := createUser(gdb, userID, emailNorm, passwordHash, displayName, true, UserStatusActive, now, &merchantID); err != nil {
			return err
		}
		if err := gdb.Create(&session).Error; err != nil {
			return err
		}
		principal = &Principal{
			UserID: userID, SessionID: sessionID, Email: emailNorm,
			DisplayName: displayName, IsOperator: true, MerchantID: &merchantID,
			Locale: defaultAccountLocale, Theme: defaultAccountTheme,
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
	now := s.now()
	var user userRow
	var sessionID string
	var principal *Principal
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		// Login and password changes acquire the User row before touching sessions.
		// This prevents an old-password login from surviving a committed password change.
		err := gdb.Clauses(pfdb.ForUpdate()).Table("users").Where("email = ?", emailNorm).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Error{Status: 401, Detail: "邮箱或密码不正确"}
		}
		if err != nil {
			return err
		}
		if user.Status != UserStatusActive || !checkPassword(user.PasswordHash, password) {
			return apperr.Error{Status: 401, Detail: "邮箱或密码不正确"}
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
			MerchantID: nullableID(user.MerchantID),
			Locale:     user.Locale, Theme: user.Theme,
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return principal, sessionID, nil
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

// OwnMerchant 返回账号的直接商家投影。Operator 没有 home merchant 时返回 nil。
func (s Service) OwnMerchant(ctx context.Context, userID string) (*MerchantView, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, apperr.Validation("缺少用户 ID")
	}
	var row struct {
		UserID         string
		UserStatus     string
		MerchantID     sql.NullString
		MerchantName   sql.NullString
		MerchantStatus sql.NullString
	}
	err := s.DB.WithContext(ctx).Raw(`
		SELECT u.id AS user_id, u.status AS user_status, u.merchant_id,
		       m.name AS merchant_name, m.status AS merchant_status
		FROM users u
		LEFT JOIN merchants m ON m.id = u.merchant_id
		WHERE u.id = ?
	`, userID).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.UserID == "" {
		return nil, apperr.NotFound("用户不存在")
	}
	merchantID := nullableID(row.MerchantID)
	if merchantID == nil {
		return nil, nil
	}
	if !row.MerchantName.Valid || !row.MerchantStatus.Valid {
		return nil, apperr.Internal("用户商家归属无效")
	}
	return &MerchantView{ID: *merchantID, Name: row.MerchantName.String, Status: row.MerchantStatus.String}, nil
}

// GetMerchant 返回 Operator 商家发现所需的单个商家摘要。
// 该读取不建立工作商家 context，也不授予目标商家业务权限。
func (s Service) GetMerchant(ctx context.Context, merchantID string) (MerchantView, error) {
	if err := s.requireDB(); err != nil {
		return MerchantView{}, err
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return MerchantView{}, apperr.Validation("缺少商家 ID")
	}
	var row schema.Merchants
	err := s.DB.WithContext(ctx).
		Select("id", "name", "status").
		Where("id = ?", merchantID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MerchantView{}, NotFoundCrossMerchant()
	}
	if err != nil {
		return MerchantView{}, apperr.Internal("读取商家失败")
	}
	return MerchantView{ID: row.ID, Name: row.Name, Status: row.Status}, nil
}

// ListMerchants 返回 Operator 商家发现所需的有界商家摘要。
// 该列表不建立工作商家 context，也不授予目标商家业务权限。
func (s Service) ListMerchants(ctx context.Context, page, pageSize int, nameQuery, status string) (MerchantPage, error) {
	if err := s.requireDB(); err != nil {
		return MerchantPage{}, err
	}
	if page < 1 || page > merchantListMaxPage {
		return MerchantPage{}, apperr.Validation("商家列表页码无效")
	}
	if pageSize < 1 || pageSize > merchantListMaxPageSize {
		return MerchantPage{}, apperr.Validation("商家列表每页数量无效")
	}
	nameQuery = strings.TrimSpace(nameQuery)
	if utf8.RuneCountInString(nameQuery) > 100 {
		return MerchantPage{}, apperr.Validation("商家列表搜索条件无效")
	}
	status = strings.TrimSpace(status)
	if status != "" && status != MerchantStatusActive && status != MerchantStatusSuspended {
		return MerchantPage{}, apperr.Validation("商家列表状态无效")
	}

	q := s.DB.WithContext(ctx).Model(&schema.Merchants{})
	if nameQuery != "" {
		q = q.Where(`name ILIKE ? ESCAPE '\'`, "%"+escapeLikeLiteral(nameQuery)+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return MerchantPage{}, err
	}
	var rows []schema.Merchants
	offset := (page - 1) * pageSize
	if err := q.Select("id", "name", "status").
		Order("created_at ASC, id ASC").Offset(offset).Limit(pageSize).Find(&rows).Error; err != nil {
		return MerchantPage{}, err
	}
	items := make([]MerchantView, 0, len(rows))
	for _, row := range rows {
		items = append(items, MerchantView{ID: row.ID, Name: row.Name, Status: row.Status})
	}
	return MerchantPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func escapeLikeLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	return strings.ReplaceAll(value, "_", `\_`)
}

// RequireOwnMerchant 验证用户直接归属目标商家。停用商家仍可读，写请求由
// RejectSuspendedMerchantWrites 统一拒绝；这样会话和运营查询仍能展示商家状态。
func (s Service) RequireOwnMerchant(ctx context.Context, userID, merchantID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	merchantID = strings.TrimSpace(merchantID)
	if userID == "" {
		return apperr.Error{Status: 401, Detail: "请先登录"}
	}
	if merchantID == "" {
		return apperr.Validation("缺少商家 ID")
	}
	var row struct {
		UserID         string
		UserStatus     string
		MerchantID     sql.NullString
		MerchantStatus sql.NullString
	}
	err := s.DB.WithContext(ctx).Raw(`
		SELECT u.id AS user_id, u.status AS user_status, u.merchant_id,
		       m.status AS merchant_status
		FROM users u
		LEFT JOIN merchants m ON m.id = u.merchant_id
		WHERE u.id = ?
	`, userID).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.UserID == "" {
		return apperr.NotFound("用户不存在")
	}
	if row.UserStatus != UserStatusActive {
		return apperr.Forbidden("账号已停用")
	}
	owned := nullableID(row.MerchantID)
	if owned == nil {
		return apperr.Forbidden("当前用户不属于任何商家")
	}
	if *owned != merchantID || !row.MerchantStatus.Valid {
		return NotFoundCrossMerchant()
	}
	return nil
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

type UserRef struct {
	UserID     string
	IsOperator bool
}
