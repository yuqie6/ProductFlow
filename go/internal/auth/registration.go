package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	registrationChallengeTTL       = 10 * time.Minute
	registrationResendInterval     = 60 * time.Second
	registrationMaxFailedAttempts  = 5
	registrationCodeDigits         = 6
	registrationCodeUpperExclusive = 1_000_000
)

// registrationRetryError keeps the resend gate precise so HTTP can return a
// useful Retry-After value without exposing any challenge data.
type registrationRetryError struct {
	RetryAfter time.Duration
}

func (e registrationRetryError) Error() string { return "registration code resend is rate limited" }

type registrationChallengeIssue struct {
	ID   string
	Code string
}

// CreateRegistrationChallenge creates one bcrypt-backed verification challenge.
// It does not send mail; callers must send the returned code and invalidate the
// row if delivery fails.
func (s Service) CreateRegistrationChallenge(ctx context.Context, email string) (registrationChallengeIssue, error) {
	if err := s.requireDB(); err != nil {
		return registrationChallengeIssue{}, err
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return registrationChallengeIssue{}, apperr.Validation(err.Error())
	}
	code, err := newVerificationCode()
	if err != nil {
		return registrationChallengeIssue{}, apperr.Internal("生成验证码失败")
	}
	codeHash, err := bcrypt.GenerateFromPassword([]byte(code), passwordHashCost)
	if err != nil {
		return registrationChallengeIssue{}, apperr.Internal("验证码哈希失败")
	}
	now := s.now()
	issue := registrationChallengeIssue{Code: code}
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var previous schema.RegistrationChallenges
		findErr := gdb.Clauses(pfdb.ForUpdate()).
			Where("email = ? AND consumed_at IS NULL", emailNorm).
			Order("created_at DESC, id DESC").Take(&previous).Error
		if findErr == nil {
			if previous.ExpiresAt.After(now) && previous.ResendAvailableAt.After(now) {
				return registrationRetryError{RetryAfter: previous.ResendAvailableAt.Sub(now)}
			}
			if err := gdb.Model(&schema.RegistrationChallenges{}).
				Where("id = ? AND consumed_at IS NULL", previous.ID).
				Updates(map[string]any{"consumed_at": now}).Error; err != nil {
				return err
			}
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		row := schema.RegistrationChallenges{
			ID:                clockid.New(),
			Email:             emailNorm,
			CodeHash:          string(codeHash),
			FailedAttempts:    0,
			ResendAvailableAt: now.Add(registrationResendInterval),
			ExpiresAt:         now.Add(registrationChallengeTTL),
			CreatedAt:         now,
		}
		if err := gdb.Create(&row).Error; err != nil {
			return err
		}
		issue.ID = row.ID
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			var active schema.RegistrationChallenges
			lookupErr := s.DB.WithContext(ctx).
				Where("email = ? AND consumed_at IS NULL", emailNorm).
				Order("created_at DESC, id DESC").Take(&active).Error
			if lookupErr == nil {
				return registrationChallengeIssue{}, registrationRetryError{RetryAfter: active.ResendAvailableAt.Sub(s.now())}
			}
		}
		return registrationChallengeIssue{}, err
	}
	return issue, nil
}

// InvalidateRegistrationChallenge makes a challenge unusable after mail
// delivery fails. It is intentionally idempotent for retry/error cleanup.
func (s Service) InvalidateRegistrationChallenge(ctx context.Context, challengeID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	challengeID = strings.TrimSpace(challengeID)
	if challengeID == "" {
		return nil
	}
	now := s.now()
	return s.DB.WithContext(ctx).Model(&schema.RegistrationChallenges{}).
		Where("id = ? AND consumed_at IS NULL", challengeID).
		Updates(map[string]any{"consumed_at": now}).Error
}

// Register consumes a valid challenge and atomically creates the ordinary
// user, its merchant, session, and trial quota account.
func (s Service) Register(ctx context.Context, email, challengeID, code, password, displayName, merchantName, currentUserID string) (*Principal, string, string, error) {
	if err := s.requireDB(); err != nil {
		return nil, "", "", err
	}
	if s.EnsureRegistrationQuota == nil {
		return nil, "", "", apperr.Internal("注册额度服务未配置")
	}
	if strings.TrimSpace(currentUserID) != "" {
		return nil, "", "", apperr.Conflict("当前已登录，请先退出当前账号")
	}
	emailNorm, err := normalizeEmail(email)
	if err != nil {
		return nil, "", "", apperr.Validation(err.Error())
	}
	challengeID = strings.TrimSpace(challengeID)
	if challengeID == "" {
		return nil, "", "", apperr.Validation("验证码挑战无效")
	}
	if err := validateVerificationCode(code); err != nil {
		return nil, "", "", apperr.Validation(err.Error())
	}
	if err := validatePassword(password); err != nil {
		return nil, "", "", apperr.Validation(err.Error())
	}
	merchantName = strings.TrimSpace(merchantName)
	if merchantName == "" {
		return nil, "", "", apperr.Validation("商家名称不能为空")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.Split(emailNorm, "@")[0]
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, "", "", apperr.Internal("密码哈希失败")
	}

	now := s.now()
	var principal *Principal
	var sessionID, merchantID string
	var verificationErr error
	err = tx.WithGorm(ctx, s.DB, func(gdb *gorm.DB) error {
		var userCount int64
		if err := gdb.Model(&schema.Users{}).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount == 0 {
			return apperr.Conflict("实例尚未初始化，请先完成引导初始化")
		}

		var challenge schema.RegistrationChallenges
		if err := gdb.Clauses(pfdb.ForUpdate()).Where("id = ?", challengeID).Take(&challenge).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.Gone("验证码已失效")
			}
			return err
		}
		if challenge.Email != emailNorm || challenge.ConsumedAt != nil || !challenge.ExpiresAt.After(now) || challenge.FailedAttempts >= registrationMaxFailedAttempts {
			return apperr.Gone("验证码已失效")
		}
		if !checkPassword(challenge.CodeHash, code) {
			verificationErr = apperr.Error{Status: 401, Detail: "验证码不正确"}
			if err := gdb.Model(&schema.RegistrationChallenges{}).
				Where("id = ? AND consumed_at IS NULL", challenge.ID).
				Updates(map[string]any{"failed_attempts": gorm.Expr("failed_attempts + 1")}).Error; err != nil {
				return err
			}
			return nil
		}

		userID := clockid.New()
		merchantID = clockid.New()
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
		if err := createUser(gdb, userID, emailNorm, passwordHash, displayName, false, UserStatusActive, now, &merchantID); err != nil {
			if isUniqueViolation(err) {
				return apperr.Conflict("该邮箱已注册")
			}
			return err
		}
		if err := gdb.Create(&session).Error; err != nil {
			return err
		}
		if err := s.EnsureRegistrationQuota(ctx, gdb, merchantID); err != nil {
			return err
		}
		if err := gdb.Model(&schema.RegistrationChallenges{}).
			Where("id = ? AND consumed_at IS NULL", challenge.ID).
			Updates(map[string]any{"consumed_at": now}).Error; err != nil {
			return err
		}
		principal = &Principal{
			UserID: userID, SessionID: sessionID, Email: emailNorm,
			DisplayName: displayName, IsOperator: false, MerchantID: &merchantID,
		}
		return nil
	})
	if err != nil {
		return nil, "", "", err
	}
	if verificationErr != nil {
		return nil, "", "", verificationErr
	}
	return principal, sessionID, merchantID, nil
}

func newVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(registrationCodeUpperExclusive))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", registrationCodeDigits, n.Int64()), nil
}

func validateVerificationCode(code string) error {
	if len(code) != registrationCodeDigits {
		return fmt.Errorf("验证码格式无效")
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return fmt.Errorf("验证码格式无效")
		}
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
