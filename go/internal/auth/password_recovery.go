package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	passwordRecoveryChallengeTTL      = 10 * time.Minute
	passwordRecoveryResendInterval    = 60 * time.Second
	passwordRecoveryMaxFailedAttempts = 5
)

// passwordRecoveryIssue carries the one-time code only from the transaction to
// the SMTP boundary. The raw code is never written to PostgreSQL or logs.
type passwordRecoveryIssue struct {
	ID         string
	Code       string
	CodeHash   string
	ExpiresAt  time.Time
	ShouldSend bool
}

// recoveryChallengeID is stable for one normalized email. The value is an
// opaque HMAC-derived UUID so known, unknown, disabled, and failed-delivery
// requests have the same repeat behavior without storing unknown addresses.
func (s Service) recoveryChallengeID(email string) (string, error) {
	secret := strings.TrimSpace(s.RecoveryChallengeIDSecret)
	if secret == "" {
		return "", apperr.Unavailable("密码恢复服务配置不可用")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(email))
	sum := mac.Sum(nil)[:16]
	// UUID-shaped output keeps the existing varchar(36) contract while the
	// HMAC prevents the identifier from revealing the normalized email.
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(sum)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func invalidPasswordRecoveryCode() apperr.Error {
	return apperr.Error{
		Status: http.StatusBadRequest,
		Detail: "验证码无效或已过期，请重新申请",
		Code:   apperr.CodeInvalidRecoveryCode,
	}
}

// CreatePasswordRecoveryChallenge creates a challenge only for an active user.
// Unknown addresses return an unpersisted opaque id, while disabled users have
// any prior active challenge consumed; neither response reveals account state.
func (s Service) CreatePasswordRecoveryChallenge(ctx context.Context, email string) (passwordRecoveryIssue, error) {
	if err := s.requireDB(); err != nil {
		return passwordRecoveryIssue{}, err
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return passwordRecoveryIssue{}, apperr.Validation(err.Error())
	}
	challengeID, err := s.recoveryChallengeID(emailNorm)
	if err != nil {
		return passwordRecoveryIssue{}, err
	}
	now := s.now()
	issue := passwordRecoveryIssue{ID: challengeID, ExpiresAt: now.Add(passwordRecoveryChallengeTTL)}
	code, err := newVerificationCode()
	if err != nil {
		return passwordRecoveryIssue{}, apperr.Internal("生成验证码失败")
	}
	codeHash, err := hashPassword(code)
	if err != nil {
		return passwordRecoveryIssue{}, apperr.Internal("验证码哈希失败")
	}
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user userRow
		err := gdb.Clauses(pfdb.ForUpdate()).Table("users").Where("email = ?", emailNorm).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var previous schema.PasswordRecoveryChallenges
		findErr := gdb.Clauses(pfdb.ForUpdate()).
			Where("user_id = ?", user.ID).
			Order("created_at DESC, id DESC").Take(&previous).Error
		if user.Status != UserStatusActive {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil
			}
			if findErr != nil {
				return findErr
			}
			if previous.ConsumedAt == nil {
				return gdb.Model(&schema.PasswordRecoveryChallenges{}).
					Where("id = ? AND consumed_at IS NULL", previous.ID).
					Updates(map[string]any{"consumed_at": now}).Error
			}
			return nil
		}

		if findErr == nil {
			if previous.ConsumedAt == nil && previous.ExpiresAt.After(now) && previous.ResendAvailableAt.After(now) {
				issue.ID = previous.ID
				// The public contract always advertises a fresh ten-minute window.
				// Refresh the persisted expiry while retaining the current code so
				// the response cannot promise more time than the database grants.
				issue.ExpiresAt = now.Add(passwordRecoveryChallengeTTL)
				if err := gdb.Model(&schema.PasswordRecoveryChallenges{}).
					Where("id = ? AND consumed_at IS NULL", previous.ID).
					Updates(map[string]any{"expires_at": issue.ExpiresAt}).Error; err != nil {
					return err
				}
				issue.CodeHash = previous.CodeHash
				issue.ShouldSend = false
				return nil
			}
			issue.ID = previous.ID
			if err := gdb.Model(&schema.PasswordRecoveryChallenges{}).
				Where("id = ?", previous.ID).
				Updates(map[string]any{
					"code_hash":           codeHash,
					"failed_attempts":     0,
					"resend_available_at": now.Add(passwordRecoveryResendInterval),
					"expires_at":          now.Add(passwordRecoveryChallengeTTL),
					"consumed_at":         nil,
					"created_at":          now,
				}).Error; err != nil {
				return err
			}
			issue.CodeHash = codeHash
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		} else {
			row := schema.PasswordRecoveryChallenges{
				ID:                issue.ID,
				UserID:            user.ID,
				CodeHash:          codeHash,
				FailedAttempts:    0,
				ResendAvailableAt: now.Add(passwordRecoveryResendInterval),
				ExpiresAt:         now.Add(passwordRecoveryChallengeTTL),
				CreatedAt:         now,
			}
			if err := gdb.Create(&row).Error; err != nil {
				return err
			}
		}
		issue.Code = code
		issue.CodeHash = codeHash
		issue.ExpiresAt = now.Add(passwordRecoveryChallengeTTL)
		issue.ShouldSend = true
		return nil
	})
	if err != nil {
		return passwordRecoveryIssue{}, err
	}
	return issue, nil
}

// InvalidatePasswordRecoveryChallenge is idempotent and follows the same
// User -> RecoveryChallenge lock order as confirmation.
func (s Service) InvalidatePasswordRecoveryChallenge(ctx context.Context, challengeID, codeHash string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	challengeID = strings.TrimSpace(challengeID)
	codeHash = strings.TrimSpace(codeHash)
	if challengeID == "" || codeHash == "" {
		return nil
	}
	var lookup struct{ UserID string }
	err := s.DB.WithContext(ctx).Model(&schema.PasswordRecoveryChallenges{}).
		Select("user_id").Where("id = ? AND code_hash = ? AND consumed_at IS NULL", challengeID, codeHash).Take(&lookup).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := s.now()
	return tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user schema.Users
		err := gdb.Clauses(pfdb.ForUpdate()).Select("id").Where("id = ?", lookup.UserID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var challenge schema.PasswordRecoveryChallenges
		err = gdb.Clauses(pfdb.ForUpdate()).Where("id = ? AND code_hash = ? AND consumed_at IS NULL", challengeID, codeHash).Take(&challenge).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return gdb.Model(&schema.PasswordRecoveryChallenges{}).
			Where("id = ? AND code_hash = ? AND consumed_at IS NULL", challengeID, codeHash).
			Updates(map[string]any{"consumed_at": now}).Error
	})
}

// ConfirmPasswordRecovery consumes one valid code and atomically changes the
// password, invalidates all other recovery codes, and revokes all sessions.
func (s Service) ConfirmPasswordRecovery(ctx context.Context, email, challengeID, code, newPassword string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return apperr.Validation(err.Error())
	}
	challengeID = strings.TrimSpace(challengeID)
	if challengeID == "" {
		return apperr.Validation("恢复验证码挑战无效")
	}
	if err := validateVerificationCode(code); err != nil {
		return apperr.Validation(err.Error())
	}
	if err := validatePassword(newPassword); err != nil {
		return apperr.Validation(err.Error())
	}

	// Resolve the owner without locking; the transaction below establishes the
	// required User -> RecoveryChallenge -> AuthSessions order before mutation.
	var lookup struct{ UserID string }
	err = s.DB.WithContext(ctx).Model(&schema.PasswordRecoveryChallenges{}).
		Select("user_id").Where("id = ?", challengeID).Take(&lookup).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return invalidPasswordRecoveryCode()
	}
	if err != nil {
		return err
	}
	newHash, err := hashPassword(newPassword)
	if err != nil {
		return apperr.Internal("密码哈希失败")
	}
	now := s.now()
	var verificationErr error
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var user userRow
		err := gdb.Clauses(pfdb.ForUpdate()).Table("users").Where("id = ?", lookup.UserID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidPasswordRecoveryCode()
		}
		if err != nil {
			return err
		}
		if user.Status != UserStatusActive || user.Email != emailNorm {
			return invalidPasswordRecoveryCode()
		}

		var challenge schema.PasswordRecoveryChallenges
		err = gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", challengeID).Take(&challenge).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidPasswordRecoveryCode()
		}
		if err != nil {
			return err
		}
		if challenge.UserID != user.ID || challenge.ConsumedAt != nil ||
			!challenge.ExpiresAt.After(now) || challenge.FailedAttempts >= passwordRecoveryMaxFailedAttempts {
			return invalidPasswordRecoveryCode()
		}
		if !checkPassword(challenge.CodeHash, code) {
			verificationErr = invalidPasswordRecoveryCode()
			// Return nil so this business error commits the failed-attempt counter.
			return gdb.Model(&schema.PasswordRecoveryChallenges{}).
				Where("id = ? AND consumed_at IS NULL", challenge.ID).
				Updates(map[string]any{"failed_attempts": gorm.Expr("failed_attempts + 1")}).Error
		}

		if err := gdb.Model(&schema.Users{}).Where("id = ?", user.ID).Updates(map[string]any{
			"password_hash": newHash,
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}
		if err := gdb.Model(&schema.PasswordRecoveryChallenges{}).
			Where("user_id = ? AND consumed_at IS NULL", user.ID).
			Updates(map[string]any{"consumed_at": now}).Error; err != nil {
			return err
		}
		return gdb.Model(&schema.AuthSessions{}).
			Where("user_id = ? AND revoked_at IS NULL", user.ID).
			Updates(map[string]any{"revoked_at": now}).Error
	})
	if err != nil {
		return err
	}
	if verificationErr != nil {
		return verificationErr
	}
	return nil
}
