package auth_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func seedDirectUser(t *testing.T, db *gorm.DB, merchantID, email, password, displayName string, operator bool) schema.Users {
	t.Helper()
	now := time.Now().UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := schema.Users{
		ID: clockid.New(), Email: strings.ToLower(email), PasswordHash: string(hash), DisplayName: displayName,
		IsOperator: operator, MerchantID: &merchantID, Status: auth.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("user_id = ?", user.ID).Delete(&schema.AuthSessions{}).Error
		_ = db.Where("id = ?", user.ID).Delete(&schema.Users{}).Error
	})
	return user
}

func loginDirectUser(t *testing.T, as *authServer, email, password string) []*http.Cookie {
	t.Helper()
	resp := as.do(t, http.MethodPost, "/api/auth/session", `{"email":"`+email+`","password":"`+password+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		raw := make([]byte, 0)
		buf := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				raw = append(raw, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		resp.Body.Close()
		t.Fatalf("login direct user %d %s", resp.StatusCode, raw)
	}
	cookies := resp.Cookies()
	resp.Body.Close()
	return cookies
}
