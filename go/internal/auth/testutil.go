package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	TestAdminKey      = "k"
	TestOperatorEmail = "operator@test.local"
	TestPassword      = "test-password-ok"
	TestMerchantName  = "开发商家"
)

// MountTest 挂上 Principal、工作商家、停用写拒绝与 auth 路由（调用方须先挂 Session 中间件）。
func MountTest(engine *gin.Engine, gdb *gorm.DB, store settings.RuntimeReader, adminKey string) HTTP {
	passwordHashCost = bcrypt.MinCost
	httpx.AuthenticatedFunc = Authenticated
	h := HTTP{
		AdminAccessKey: adminKey,
		Store:          store,
		DB:             gdb,
		Service:        Service{DB: gdb},
		AttemptLimiter: testAllowAttemptLimiter{},
	}
	engine.Use(h.LoadPrincipal())
	engine.Use(h.AttachWorkingMerchant())
	engine.Use(h.RejectSuspendedMerchantWrites())
	h.Register(engine)
	return h
}

type testAllowAttemptLimiter struct{}

func (testAllowAttemptLimiter) Allow(context.Context, string, string) (AttemptDecision, error) {
	return AttemptDecision{Allowed: true}, nil
}

// MustDevMerchantID ensures the shared test account and returns its explicit
// merchant ownership, including for service fixtures that run before HTTP tests.
func MustDevMerchantID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var user schema.Users
	err := db.Where("email = ?", TestOperatorEmail).Take(&user).Error
	if err == nil {
		if user.MerchantID == nil {
			t.Fatal("test operator has no merchant")
		}
		return *user.MerchantID
	}
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("load test operator: %v", err)
	}
	now := time.Now().UTC()
	merchant := schema.Merchants{ID: clockid.New(), Name: TestMerchantName, Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(TestPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&merchant).Error; err != nil {
			return err
		}
		return createUser(tx, clockid.New(), TestOperatorEmail, string(passwordHash), "Test operator", true, UserStatusActive, now, &merchant.ID)
	})
	if err != nil {
		t.Fatalf("create test account and merchant: %v", err)
	}
	return merchant.ID
}

// MustAuthenticate 引导（若需要）并用测试账号登录，返回会话 cookie。
func MustAuthenticate(t *testing.T, client *http.Client, baseURL string) []*http.Cookie {
	t.Helper()
	bootstrapBody := `{"admin_key":"` + TestAdminKey + `","email":"` + TestOperatorEmail + `","password":"` + TestPassword + `","merchant_name":"` + TestMerchantName + `"}`
	resp, err := client.Post(baseURL+"/api/auth/bootstrap", "application/json", strings.NewReader(bootstrapBody))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("bootstrap %d", resp.StatusCode)
	}
	loginBody := `{"email":"` + TestOperatorEmail + `","password":"` + TestPassword + `"}`
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/auth/session", strings.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %d %s", resp.StatusCode, body)
	}
	return resp.Cookies()
}
