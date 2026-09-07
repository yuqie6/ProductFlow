package auth

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// DualMerchantFixture 是 B10 测试夹具：两个互不共享商家的普通账号，另保留
// 一个只用于专门 Operator 路径的引导会话。
type DualMerchantFixture struct {
	MerchantAID     string
	MerchantBID     string
	UserAID         string
	UserBID         string
	EmailA          string
	PasswordA       string
	EmailB          string
	PasswordB       string
	CookiesA        []*http.Cookie
	CookiesB        []*http.Cookie
	OperatorCookies []*http.Cookie
}

const (
	TestUserAPassword = "test-password-a-ok"
	TestMerchantBName = "夹具商家B"
	TestUserBPassword = "test-password-b-ok"
)

// SeedDualMerchants 在已挂 auth 的测试服上引导 Operator，然后创建两个普通账号
// 与各自直接归属商家。普通账号通过真实登录拿到会话；Operator 会话单独返回给
// 需要验证专门 ops 路径的测试。
func SeedDualMerchants(t *testing.T, db *gorm.DB, client *http.Client, baseURL string) DualMerchantFixture {
	t.Helper()
	if db == nil || client == nil {
		t.Fatal("SeedDualMerchants: nil db/client")
	}
	passwordHashCost = bcrypt.MinCost
	operatorCookies := MustAuthenticate(t, client, baseURL)

	now := time.Now().UTC()
	emailA := fmt.Sprintf("merchant-a-%d@test.local", now.UnixNano())
	emailB := fmt.Sprintf("merchant-b-%d@test.local", now.UnixNano())
	hashA, err := hashPassword(TestUserAPassword)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := hashPassword(TestUserBPassword)
	if err != nil {
		t.Fatal(err)
	}
	merchantA := schema.Merchants{
		ID: clockid.New(), Name: "夹具商家A", Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	merchantB := schema.Merchants{
		ID: clockid.New(), Name: TestMerchantBName, Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	userA := schema.Users{ID: clockid.New(), Email: emailA, PasswordHash: hashA, DisplayName: "夹具商家A账号", IsOperator: false, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	userB := schema.Users{ID: clockid.New(), Email: emailB, PasswordHash: hashB, DisplayName: "夹具商家B账号", IsOperator: false, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&merchantA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&merchantB).Error; err != nil {
		t.Fatal(err)
	}
	if err := createUser(db, userA.ID, userA.Email, userA.PasswordHash, userA.DisplayName, userA.IsOperator, userA.Status, now, &merchantA.ID); err != nil {
		t.Fatal(err)
	}
	if err := createUser(db, userB.ID, userB.Email, userB.PasswordHash, userB.DisplayName, userB.IsOperator, userB.Status, now, &merchantB.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("user_id = ?", userA.ID).Delete(&schema.AuthSessions{}).Error
		_ = db.Where("user_id = ?", userB.ID).Delete(&schema.AuthSessions{}).Error
		_ = db.Where("id = ?", userA.ID).Delete(&schema.Users{}).Error
		_ = db.Where("id = ?", userB.ID).Delete(&schema.Users{}).Error
		_ = db.Where("id = ?", merchantA.ID).Delete(&schema.Merchants{}).Error
		_ = db.Where("id = ?", merchantB.ID).Delete(&schema.Merchants{}).Error
	})

	login := func(email, password string) []*http.Cookie {
		loginBody := `{"email":"` + email + `","password":"` + password + `"}`
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/auth/session", strings.NewReader(loginBody))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("direct user login %s %d %s", email, resp.StatusCode, raw)
		}
		return resp.Cookies()
	}

	return DualMerchantFixture{
		MerchantAID:     merchantA.ID,
		MerchantBID:     merchantB.ID,
		UserAID:         userA.ID,
		UserBID:         userB.ID,
		EmailA:          emailA,
		PasswordA:       TestUserAPassword,
		EmailB:          emailB,
		PasswordB:       TestUserBPassword,
		CookiesA:        login(emailA, TestUserAPassword),
		CookiesB:        login(emailB, TestUserBPassword),
		OperatorCookies: operatorCookies,
	}
}
