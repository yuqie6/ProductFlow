package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	accountDisplayNameMaxRunes  = 160
	accountMerchantNameMaxRunes = 160
	accountSessionDefaultLimit  = 20
	accountSessionMaxLimit      = 100
)

const (
	defaultAccountLocale = "zh-CN"
	defaultAccountTheme  = "system"
)

// AccountPreferences 是当前账号跨会话保存的界面偏好。
type AccountPreferences struct {
	Locale string `json:"locale"`
	Theme  string `json:"theme"`
}

// AccountUserView 是本人账户页允许读取的用户字段。
type AccountUserView struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsOperator  bool   `json:"is_operator"`
}

// AccountView 是 GET/PATCH /api/account 的稳定投影。
type AccountView struct {
	User        AccountUserView    `json:"user"`
	Merchant    *MerchantView      `json:"merchant"`
	Preferences AccountPreferences `json:"preferences"`
}

func accountUserView(user schema.Users) AccountUserView {
	return AccountUserView{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, IsOperator: user.IsOperator,
	}
}

func isAccountLocale(value string) bool {
	switch value {
	case "zh-CN", "en-US", "ja-JP", "vi-VN":
		return true
	default:
		return false
	}
}

func isAccountTheme(value string) bool {
	switch value {
	case "light", "dark", "system":
		return true
	default:
		return false
	}
}

// Account 返回当前用户的资料和直接商家投影。密码及其他账号关系永远不进入该投影。
func (s Service) Account(ctx context.Context, userID string) (AccountView, error) {
	if err := s.requireDB(); err != nil {
		return AccountView{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AccountView{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	var user schema.Users
	err := s.DB.WithContext(ctx).Select("id", "email", "display_name", "is_operator", "merchant_id", "status", "locale", "theme").
		Where("id = ?", userID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AccountView{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	if err != nil {
		return AccountView{}, err
	}
	if user.Status != UserStatusActive {
		return AccountView{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	merchant, err := s.OwnMerchant(ctx, userID)
	if err != nil {
		return AccountView{}, err
	}
	return AccountView{
		User:        accountUserView(user),
		Merchant:    merchant,
		Preferences: AccountPreferences{Locale: user.Locale, Theme: user.Theme},
	}, nil
}

func validateDisplayName(displayName string) (string, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return "", apperr.Validation("显示名不能为空")
	}
	if utf8.RuneCountInString(displayName) > accountDisplayNameMaxRunes {
		return "", apperr.Validation("显示名不能超过 160 个字符")
	}
	return displayName, nil
}

// UpdateDisplayName updates only the current user's display name under the User lock.
func (s Service) UpdateDisplayName(ctx context.Context, userID, displayName string) (AccountView, error) {
	if err := s.requireDB(); err != nil {
		return AccountView{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AccountView{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	displayName, err := validateDisplayName(displayName)
	if err != nil {
		return AccountView{}, err
	}
	now := s.now()
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user schema.Users
		err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", userID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		if err != nil {
			return err
		}
		if user.Status != UserStatusActive {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		if err := gdb.Model(&schema.Users{}).Where("id = ?", userID).Updates(map[string]any{
			"display_name": displayName,
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return AccountView{}, err
	}
	return s.Account(ctx, userID)
}

func validateAccountLocale(locale string) (string, error) {
	if !isAccountLocale(locale) {
		return "", apperr.Validation("语言偏好无效")
	}
	return locale, nil
}

func validateAccountTheme(theme string) (string, error) {
	if !isAccountTheme(theme) {
		return "", apperr.Validation("主题偏好无效")
	}
	return theme, nil
}

// UpdatePreferences updates only supplied fields while holding the User row
// lock, so concurrent preference writes and authentication observe one order.
func (s Service) UpdatePreferences(ctx context.Context, userID string, locale, theme *string) (AccountPreferences, error) {
	if err := s.requireDB(); err != nil {
		return AccountPreferences{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AccountPreferences{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	if locale == nil && theme == nil {
		return AccountPreferences{}, apperr.Validation("至少提供一个账户偏好")
	}
	if locale != nil {
		if _, err := validateAccountLocale(*locale); err != nil {
			return AccountPreferences{}, err
		}
	}
	if theme != nil {
		if _, err := validateAccountTheme(*theme); err != nil {
			return AccountPreferences{}, err
		}
	}

	now := s.now()
	var preferences AccountPreferences
	err := tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user schema.Users
		if err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", userID).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.Error{Status: 401, Detail: "请先登录"}
			}
			return err
		}
		if user.Status != UserStatusActive {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		updates := map[string]any{"updated_at": now}
		if locale != nil {
			updates["locale"] = *locale
			user.Locale = *locale
		}
		if theme != nil {
			updates["theme"] = *theme
			user.Theme = *theme
		}
		if err := gdb.Model(&schema.Users{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
			return err
		}
		preferences = AccountPreferences{Locale: user.Locale, Theme: user.Theme}
		return nil
	})
	if err != nil {
		return AccountPreferences{}, err
	}
	return preferences, nil
}

func validateAccountMerchantName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperr.Validation("商家名称不能为空")
	}
	if utf8.RuneCountInString(name) > accountMerchantNameMaxRunes {
		return "", apperr.Validation("商家名称不能超过 160 个字符")
	}
	return name, nil
}

// UpdateOwnMerchantName updates the merchant directly owned by the account.
// The User -> Merchant lock order keeps ownership and status checks atomic.
func (s Service) UpdateOwnMerchantName(ctx context.Context, userID, name string) (MerchantView, error) {
	if err := s.requireDB(); err != nil {
		return MerchantView{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return MerchantView{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	name, err := validateAccountMerchantName(name)
	if err != nil {
		return MerchantView{}, err
	}
	now := s.now()
	var view MerchantView
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user schema.Users
		if err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", userID).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.Error{Status: 401, Detail: "请先登录"}
			}
			return err
		}
		if user.Status != UserStatusActive {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		if user.MerchantID == nil || strings.TrimSpace(*user.MerchantID) == "" {
			return apperr.Forbidden("当前用户不属于任何商家")
		}
		merchantID := strings.TrimSpace(*user.MerchantID)
		var merchant schema.Merchants
		if err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", merchantID).Take(&merchant).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NotFoundCrossMerchant()
			}
			return err
		}
		if merchant.Status == MerchantStatusSuspended {
			return apperr.Forbidden("商家已停用，无法写入")
		}
		if err := gdb.Model(&schema.Merchants{}).Where("id = ?", merchantID).Updates(map[string]any{
			"name": name, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		view = MerchantView{ID: merchant.ID, Name: name, Status: merchant.Status}
		return nil
	})
	if err != nil {
		return MerchantView{}, err
	}
	return view, nil
}

// ChangePassword verifies the current password and revokes every existing
// session in the same User -> AuthSessions transaction.
func (s Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return apperr.Error{Status: 401, Detail: "请先登录"}
	}
	if err := validatePassword(newPassword); err != nil {
		return apperr.Validation(err.Error())
	}
	newHash, err := hashPassword(newPassword)
	if err != nil {
		return apperr.Internal("密码哈希失败")
	}
	now := s.now()
	return tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user userRow
		err := gdb.Clauses(pfdb.ForUpdate()).Table("users").Where("id = ?", userID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		if err != nil {
			return err
		}
		if user.Status != UserStatusActive || !checkPassword(user.PasswordHash, currentPassword) {
			return apperr.Error{Status: 401, Detail: "当前密码不正确"}
		}
		if err := gdb.Model(&schema.Users{}).Where("id = ?", userID).Updates(map[string]any{
			"password_hash": newHash,
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}
		// A password change also invalidates every outstanding recovery code.
		// The User lock is already held, so this follows User -> RecoveryChallenge
		// before acquiring the AuthSessions row locks below.
		if err := gdb.Model(&schema.PasswordRecoveryChallenges{}).
			Where("user_id = ? AND consumed_at IS NULL", userID).
			Updates(map[string]any{"consumed_at": now}).Error; err != nil {
			return err
		}
		return gdb.Model(&schema.AuthSessions{}).
			Where("user_id = ? AND revoked_at IS NULL", userID).
			Updates(map[string]any{"revoked_at": now}).Error
	})
}

// AccountSessionView intentionally contains only opaque id and timestamps.
type AccountSessionView struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
}

// AccountSessionPage is the bounded, keyset-paginated account session list.
type AccountSessionPage struct {
	Items      []AccountSessionView `json:"items"`
	NextCursor *string              `json:"next_cursor"`
}

type accountSessionCursor struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

func encodeAccountSessionCursor(createdAt time.Time, id string) (string, error) {
	raw, err := json.Marshal(accountSessionCursor{
		Version: 1, CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: id,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeAccountSessionCursor(raw string) (time.Time, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", false
	}
	var cursor accountSessionCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 || cursor.ID == "" || cursor.CreatedAt == "" {
		return time.Time{}, "", false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil {
		return time.Time{}, "", false
	}
	return createdAt.UTC(), cursor.ID, true
}

// ListAccountSessions lists only non-revoked, non-expired sessions owned by userID.
func (s Service) ListAccountSessions(ctx context.Context, userID, currentSessionID, after string, limit int) (AccountSessionPage, error) {
	if err := s.requireDB(); err != nil {
		return AccountSessionPage{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AccountSessionPage{}, apperr.Error{Status: 401, Detail: "请先登录"}
	}
	if limit < 1 || limit > accountSessionMaxLimit {
		return AccountSessionPage{}, apperr.Validation("会话列表 limit 必须在 1 到 100 之间")
	}
	var cursorAt time.Time
	var cursorID string
	if strings.TrimSpace(after) != "" {
		var ok bool
		cursorAt, cursorID, ok = decodeAccountSessionCursor(after)
		if !ok {
			return AccountSessionPage{}, apperr.Validation("会话列表游标无效")
		}
	}
	now := s.now()
	query := s.DB.WithContext(ctx).Model(&schema.AuthSessions{}).
		Select("id", "user_id", "created_at", "expires_at").
		Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, now)
	if cursorID != "" {
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", cursorAt, cursorAt, cursorID)
	}
	var rows []schema.AuthSessions
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return AccountSessionPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]AccountSessionView, 0, len(rows))
	for _, row := range rows {
		items = append(items, AccountSessionView{
			ID: row.ID, CreatedAt: row.CreatedAt.UTC(), ExpiresAt: row.ExpiresAt.UTC(), Current: row.ID == strings.TrimSpace(currentSessionID),
		})
	}
	page := AccountSessionPage{Items: items}
	if hasMore && len(rows) > 0 {
		next, err := encodeAccountSessionCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
		if err != nil {
			return AccountSessionPage{}, err
		}
		page.NextCursor = &next
	}
	return page, nil
}

// RevokeOwnSession enforces ownership under the same User -> AuthSessions lock order.
func (s Service) RevokeOwnSession(ctx context.Context, userID, sessionID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" {
		return apperr.Error{Status: 401, Detail: "请先登录"}
	}
	if sessionID == "" {
		return NotFoundCrossMerchant()
	}
	now := s.now()
	return tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user schema.Users
		err := gdb.Clauses(pfdb.ForUpdate()).Select("id", "status").Where("id = ?", userID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		if err != nil {
			return err
		}
		if user.Status != UserStatusActive {
			return apperr.Error{Status: 401, Detail: "请先登录"}
		}
		var session schema.AuthSessions
		err = gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", sessionID).Take(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NotFoundCrossMerchant()
		}
		if err != nil {
			return err
		}
		if session.UserID != userID {
			return NotFoundCrossMerchant()
		}
		if session.RevokedAt != nil {
			return nil
		}
		return gdb.Model(&schema.AuthSessions{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", sessionID, userID).
			Updates(map[string]any{"revoked_at": now}).Error
	})
}
