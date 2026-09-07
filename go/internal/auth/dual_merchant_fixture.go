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

// DualMerchantFixture 是 B10 测试夹具：商家 A（引导开发商）与商家 B（仅 DB 种子，不走产品注册）。
// 不开放第二外部商 UI；CreateMerchant API 仍保持唯一开发商家冲突。
type DualMerchantFixture struct {
	MerchantAID string
	MerchantBID string
	UserAID     string
	UserBID     string
	EmailB      string
	PasswordB   string
	CookiesA    []*http.Cookie
	CookiesB    []*http.Cookie
}

const (
	TestMerchantBName = "夹具商家B"
	TestUserBPassword = "test-password-b-ok"
)

// SeedDualMerchants 在已挂 auth 的测试服上引导商家 A，并仅用 DB 种子商家 B + Owner 用户后登录。
// 清理时删除 B 侧用户/成员/商家行；不触碰共享开发库以外的 testdb。
func SeedDualMerchants(t *testing.T, db *gorm.DB, client *http.Client, baseURL string) DualMerchantFixture {
	t.Helper()
	if db == nil || client == nil {
		t.Fatal("SeedDualMerchants: nil db/client")
	}
	passwordHashCost = bcrypt.MinCost
	cookiesA := MustAuthenticate(t, client, baseURL)
	merchantA := MustDevMerchantID(t, db)

	var userA schema.Users
	if err := db.Where("email = ?", strings.ToLower(TestOperatorEmail)).Take(&userA).Error; err != nil {
		t.Fatalf("load merchant A owner: %v", err)
	}

	now := time.Now().UTC()
	emailB := fmt.Sprintf("merchant-b-%d@test.local", now.UnixNano())
	hash, err := hashPassword(TestUserBPassword)
	if err != nil {
		t.Fatal(err)
	}
	userB := schema.Users{
		ID: clockid.New(), Email: emailB, PasswordHash: hash, DisplayName: "夹具商家B Owner",
		IsOperator: false, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	merchantB := schema.Merchants{
		ID: clockid.New(), Name: TestMerchantBName, Status: MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	membershipB := schema.Memberships{
		ID: clockid.New(), MerchantID: merchantB.ID, UserID: userB.ID, Role: RoleOwner,
		Status: MembershipStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&userB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&merchantB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&membershipB).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("user_id = ?", userB.ID).Delete(&schema.AuthSessions{}).Error
		_ = db.Where("id = ?", membershipB.ID).Delete(&schema.Memberships{}).Error
		_ = db.Where("id = ?", merchantB.ID).Delete(&schema.Merchants{}).Error
		_ = db.Where("id = ?", userB.ID).Delete(&schema.Users{}).Error
	})

	loginBody := `{"email":"` + emailB + `","password":"` + TestUserBPassword + `"}`
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
		t.Fatalf("merchant B login %d %s", resp.StatusCode, raw)
	}

	return DualMerchantFixture{
		MerchantAID: merchantA,
		MerchantBID: merchantB.ID,
		UserAID:     userA.ID,
		UserBID:     userB.ID,
		EmailB:      emailB,
		PasswordB:   TestUserBPassword,
		CookiesA:    cookiesA,
		CookiesB:    resp.Cookies(),
	}
}

// MerchantHeaderName 是工作商家声明头（夹具/测试用）。
func MerchantHeaderName() string {
	return merchantHeader
}
