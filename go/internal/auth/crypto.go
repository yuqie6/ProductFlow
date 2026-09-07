package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"

	MerchantStatusActive    = "active"
	MerchantStatusSuspended = "suspended"

	MembershipStatusActive  = "active"
	MembershipStatusRevoked = "revoked"

	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"

	sessionCookieUserKey    = "user_id"
	sessionCookieSessionKey = "auth_session_id"
	sessionTTL              = 14 * 24 * time.Hour
	inviteTTL               = 7 * 24 * time.Hour
	minPasswordRunes        = 8
	bcryptCostDefault       = bcrypt.DefaultCost
)

// passwordHashCost 可在测试里降到 MinCost；生产保持 DefaultCost。
var passwordHashCost = bcryptCostDefault

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", fmt.Errorf("邮箱不能为空")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", fmt.Errorf("邮箱格式无效")
	}
	if len(email) > 320 {
		return "", fmt.Errorf("邮箱过长")
	}
	return email, nil
}

func validatePassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordRunes {
		return fmt.Errorf("密码至少 %d 个字符", minPasswordRunes)
	}
	if len(password) > 200 {
		return fmt.Errorf("密码过长")
	}
	return nil
}

func hashPassword(password string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return "", err
	}
	return string(sum), nil
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newOpaqueToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func secretEqual(provided, expected string) bool {
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func validRole(role string) bool {
	switch role {
	case RoleOwner, RoleEditor, RoleViewer:
		return true
	default:
		return false
	}
}
