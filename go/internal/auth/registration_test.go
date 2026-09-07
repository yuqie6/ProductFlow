package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type registrationMail struct {
	Email string
	Code  string
}

type fakeRegistrationMailer struct {
	mu              sync.Mutex
	available       bool
	availabilityErr error
	sendErr         error
	sent            []registrationMail
}

func (m *fakeRegistrationMailer) RegistrationAvailable(context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.available, m.availabilityErr
}

func (m *fakeRegistrationMailer) SendVerificationCode(_ context.Context, email, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, registrationMail{Email: email, Code: code})
	return m.sendErr
}

func (m *fakeRegistrationMailer) last() registrationMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return registrationMail{}
	}
	return m.sent[len(m.sent)-1]
}

func (m *fakeRegistrationMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

type registrationServer struct {
	*authServer
	mailer    *fakeRegistrationMailer
	failQuota *atomic.Bool
	clock     *registrationClock
}

type registrationClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *registrationClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *registrationClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *registrationClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func newRegistrationServer(t *testing.T) *registrationServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "registration-test-session-secret"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	mailer := &fakeRegistrationMailer{available: true}
	failQuota := &atomic.Bool{}
	clock := &registrationClock{now: time.Now().UTC()}
	h := auth.HTTP{
		AdminAccessKey: auth.TestAdminKey,
		Store:          store,
		Mailer:         mailer,
		DB:             gdb,
		Service:        auth.Service{DB: gdb, Now: clock.Now},
		AttemptLimiter: registrationAllowLimiter{},
	}
	trialUnits := int64(100)
	h.Service.EnsureRegistrationQuota = func(ctx context.Context, txdb *gorm.DB, merchantID string) error {
		if failQuota.Load() {
			return errors.New("quota seed failed")
		}
		_, err := (&quota.Service{DB: txdb, TrialUnits: &trialUnits}).EnsureAccount(ctx, merchantID)
		return err
	}
	httpx.AuthenticatedFunc = auth.Authenticated
	engine.Use(h.LoadPrincipal())
	h.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &registrationServer{authServer: &authServer{srv: srv, client: &http.Client{}, db: gdb}, mailer: mailer, failQuota: failQuota, clock: clock}
}

type registrationAllowLimiter struct{}

func (registrationAllowLimiter) Allow(context.Context, string, string) (auth.AttemptDecision, error) {
	return auth.AttemptDecision{Allowed: true}, nil
}

func wrongVerificationCode(actual string) string {
	for n := 0; n < 1_000_000; n++ {
		candidate := fmt.Sprintf("%06d", n)
		if candidate != actual {
			return candidate
		}
	}
	return "000000"
}

func (rs *registrationServer) requestCode(t *testing.T, email string, cookies []*http.Cookie) (int, struct {
	ChallengeID string `json:"challenge_id"`
	RetryAfter  int    `json:"retry_after_seconds"`
}, []byte) {
	t.Helper()
	resp := rs.do(t, http.MethodPost, "/api/auth/registration-code", `{"email":"`+email+`"}`, cookies)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body struct {
		ChallengeID string `json:"challenge_id"`
		RetryAfter  int    `json:"retry_after_seconds"`
	}
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body, raw
}

func (rs *registrationServer) register(t *testing.T, email, challengeID, code, merchant string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	return rs.do(t, http.MethodPost, "/api/auth/register", fmt.Sprintf(
		`{"email":%q,"challenge_id":%q,"code":%q,"password":"new-password-ok","display_name":"新用户","merchant_name":%q}`,
		email, challengeID, code, merchant), cookies)
}

func TestRegistrationCodeAndRegisterAtomicSuccess(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	email := "new-registration-" + clockid.New() + "@example.test"
	status, challenge, raw := rs.requestCode(t, strings.ToUpper(email), nil)
	if status != http.StatusOK || challenge.ChallengeID == "" || challenge.RetryAfter != 60 {
		t.Fatalf("registration code status=%d body=%s", status, raw)
	}
	sent := rs.mailer.last()
	if sent.Email != email || len(sent.Code) != 6 {
		t.Fatalf("sent mail %#v", sent)
	}
	resp := rs.register(t, email, challenge.ChallengeID, sent.Code, "New Merchant", nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("register %d %s", resp.StatusCode, raw)
	}
	cookies := resp.Cookies()
	resp.Body.Close()
	if len(cookies) == 0 {
		t.Fatal("registration did not set session cookie")
	}
	var user schema.Users
	if err := rs.db.Where("email = ?", email).Take(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.IsOperator || user.Status != auth.UserStatusActive {
		t.Fatalf("registered user %#v", user)
	}
	var membership schema.Memberships
	if err := rs.db.Where("user_id = ?", user.ID).Take(&membership).Error; err != nil {
		t.Fatal(err)
	}
	if membership.Role != auth.RoleOwner {
		t.Fatalf("role %s", membership.Role)
	}
	var quota schema.MerchantQuotaAccounts
	if err := rs.db.Where("merchant_id = ?", membership.MerchantID).Take(&quota).Error; err != nil {
		t.Fatal(err)
	}
	if quota.AvailableUnits != 100 || quota.ReservedUnits != 0 {
		t.Fatalf("quota account=%#v", quota)
	}
	var challengeRow schema.RegistrationChallenges
	if err := rs.db.Where("id = ?", challenge.ChallengeID).Take(&challengeRow).Error; err != nil {
		t.Fatal(err)
	}
	if challengeRow.ConsumedAt == nil {
		t.Fatal("challenge was not consumed")
	}
}

func TestRegistrationChallengeExpires(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	email := "expired-registration-" + clockid.New() + "@example.test"
	status, challenge, _ := rs.requestCode(t, email, nil)
	if status != http.StatusOK {
		t.Fatalf("registration code status=%d", status)
	}
	sent := rs.mailer.last()
	var row schema.RegistrationChallenges
	if err := rs.db.Where("id = ?", challenge.ChallengeID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	rs.clock.Set(row.ExpiresAt.Add(time.Nanosecond))
	resp := rs.register(t, email, challenge.ChallengeID, sent.Code, "Expired Merchant", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("expired challenge status=%d", resp.StatusCode)
	}
	var users int64
	if err := rs.db.Model(&schema.Users{}).Where("email = ?", email).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("expired challenge created users=%d", users)
	}
}

func TestRegistrationResendInvalidatesPreviousProof(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	email := "resend-registration-" + clockid.New() + "@example.test"
	status, first, _ := rs.requestCode(t, email, nil)
	if status != http.StatusOK {
		t.Fatalf("first registration code status=%d", status)
	}
	oldCode := rs.mailer.last().Code
	rs.clock.Advance(61 * time.Second)
	status, second, raw := rs.requestCode(t, email, nil)
	if status != http.StatusOK || second.ChallengeID == "" || second.ChallengeID == first.ChallengeID {
		t.Fatalf("resend status=%d body=%s first=%#v second=%#v", status, raw, first, second)
	}
	newCode := rs.mailer.last().Code
	stale := rs.register(t, email, first.ChallengeID, oldCode, "Resend Merchant", nil)
	stale.Body.Close()
	if stale.StatusCode != http.StatusGone {
		t.Fatalf("old challenge status=%d", stale.StatusCode)
	}
	current := rs.register(t, email, second.ChallengeID, newCode, "Resend Merchant", nil)
	current.Body.Close()
	if current.StatusCode != http.StatusOK {
		t.Fatalf("new challenge status=%d", current.StatusCode)
	}
	var oldRow schema.RegistrationChallenges
	if err := rs.db.Where("id = ?", first.ChallengeID).Take(&oldRow).Error; err != nil {
		t.Fatal(err)
	}
	if oldRow.ConsumedAt == nil {
		t.Fatal("resend left old challenge active")
	}
}

func TestRegistrationChallengeBoundToEmail(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	emailA := "challenge-owner-" + clockid.New() + "@example.test"
	emailB := "challenge-other-" + clockid.New() + "@example.test"
	status, challenge, _ := rs.requestCode(t, emailA, nil)
	if status != http.StatusOK {
		t.Fatalf("registration code status=%d", status)
	}
	code := rs.mailer.last().Code
	wrongEmail := rs.register(t, emailB, challenge.ChallengeID, code, "Cross Email Merchant", nil)
	wrongEmail.Body.Close()
	if wrongEmail.StatusCode != http.StatusGone {
		t.Fatalf("cross-email challenge status=%d", wrongEmail.StatusCode)
	}
	var users int64
	if err := rs.db.Model(&schema.Users{}).Where("email IN ?", []string{emailA, emailB}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("cross-email attempt created users=%d", users)
	}
	valid := rs.register(t, emailA, challenge.ChallengeID, code, "Cross Email Merchant", nil)
	valid.Body.Close()
	if valid.StatusCode != http.StatusOK {
		t.Fatalf("owner email challenge status=%d", valid.StatusCode)
	}
}

func TestRegistrationWrongAttemptsCommitAndExpire(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	email := "wrong-code-" + clockid.New() + "@example.test"
	_, challenge, _ := rs.requestCode(t, email, nil)
	wrongCode := wrongVerificationCode(rs.mailer.last().Code)
	for i := 0; i < 5; i++ {
		resp := rs.register(t, email, challenge.ChallengeID, wrongCode, "Wrong Attempts", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("wrong attempt %d status=%d body=%s", i+1, resp.StatusCode, raw)
		}
		resp.Body.Close()
	}
	var row schema.RegistrationChallenges
	if err := rs.db.Where("id = ?", challenge.ChallengeID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.FailedAttempts != 5 || row.ConsumedAt != nil {
		t.Fatalf("challenge after wrong attempts %#v", row)
	}
	resp := rs.register(t, email, challenge.ChallengeID, wrongCode, "Wrong Attempts", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("sixth wrong attempt status=%d", resp.StatusCode)
	}
}

func TestRegistrationSendFailureInvalidatesChallenge(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	rs.mailer.mu.Lock()
	rs.mailer.sendErr = errors.New("smtp down")
	rs.mailer.mu.Unlock()
	email := "send-failure-" + clockid.New() + "@example.test"
	status, challenge, _ := rs.requestCode(t, email, nil)
	if status != http.StatusServiceUnavailable || challenge.ChallengeID != "" {
		t.Fatalf("send failure status=%d challenge=%#v", status, challenge)
	}
	var rows int64
	if err := rs.db.Model(&schema.RegistrationChallenges{}).Where("email = ? AND consumed_at IS NOT NULL", email).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("invalidated challenge rows=%d", rows)
	}
}

func TestRegistrationQuotaFailureRollsBackIdentityAndChallenge(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	email := "quota-rollback-" + clockid.New() + "@example.test"
	merchantName := "Rollback Merchant " + clockid.New()
	_, challenge, _ := rs.requestCode(t, email, nil)
	sent := rs.mailer.last()
	rs.failQuota.Store(true)
	failed := rs.register(t, email, challenge.ChallengeID, sent.Code, merchantName, nil)
	if failed.StatusCode != http.StatusInternalServerError {
		raw, _ := io.ReadAll(failed.Body)
		failed.Body.Close()
		t.Fatalf("quota failure status=%d body=%s", failed.StatusCode, raw)
	}
	failed.Body.Close()
	var users, merchants, memberships, sessions, accounts int64
	if err := rs.db.Model(&schema.Users{}).Where("email = ?", email).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Model(&schema.Merchants{}).Where("name = ?", merchantName).Count(&merchants).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Model(&schema.Memberships{}).Where("merchant_id IN (SELECT id FROM merchants WHERE name = ?)", merchantName).Count(&memberships).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Model(&schema.AuthSessions{}).Where("user_id IN (SELECT id FROM users WHERE email = ?)", email).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if err := rs.db.Model(&schema.MerchantQuotaAccounts{}).Where("merchant_id IN (SELECT id FROM merchants WHERE name = ?)", merchantName).Count(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 || merchants != 0 || memberships != 0 || sessions != 0 || accounts != 0 {
		t.Fatalf("rollback rows users=%d merchants=%d memberships=%d sessions=%d accounts=%d", users, merchants, memberships, sessions, accounts)
	}
	var row schema.RegistrationChallenges
	if err := rs.db.Where("id = ?", challenge.ChallengeID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ConsumedAt != nil {
		t.Fatal("quota failure consumed challenge")
	}
	rs.failQuota.Store(false)
	retry := rs.register(t, email, challenge.ChallengeID, sent.Code, merchantName, nil)
	if retry.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(retry.Body)
		retry.Body.Close()
		t.Fatalf("retry after quota failure status=%d body=%s", retry.StatusCode, raw)
	}
	retry.Body.Close()
}

func TestRegistrationConcurrentCodeAdmissionAndReplay(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	email := "concurrent-registration-" + clockid.New() + "@example.test"
	statuses := make(chan int, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, _ := rs.requestCode(t, email, nil)
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	accepted := 0
	limited := 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			accepted++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("concurrent code status=%d", status)
		}
	}
	if accepted != 1 || limited != 11 {
		t.Fatalf("concurrent code accepted=%d limited=%d", accepted, limited)
	}
	var challenge schema.RegistrationChallenges
	if err := rs.db.Where("email = ? AND consumed_at IS NULL", email).Take(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	code := rs.mailer.last().Code
	results := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := rs.register(t, email, challenge.ID, code, "Concurrent Merchant", nil)
			results <- resp.StatusCode
			resp.Body.Close()
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for status := range results {
		if status == http.StatusOK {
			successes++
		} else if status != http.StatusGone {
			t.Fatalf("concurrent register status=%d", status)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent register successes=%d", successes)
	}
	var users int64
	if err := rs.db.Model(&schema.Users{}).Where("email = ?", email).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Fatalf("users=%d", users)
	}
}

func TestSessionReadSurvivesMailerReadError(t *testing.T) {
	rs := newRegistrationServer(t)
	cookies := auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	rs.mailer.mu.Lock()
	rs.mailer.availabilityErr = errors.New("settings unavailable")
	rs.mailer.mu.Unlock()
	resp := rs.do(t, http.MethodGet, "/api/auth/session", "", cookies)
	defer resp.Body.Close()
	var body struct {
		Authenticated         bool `json:"authenticated"`
		RegistrationAvailable bool `json:"registration_available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !body.Authenticated || body.RegistrationAvailable {
		t.Fatalf("session status=%d body=%#v", resp.StatusCode, body)
	}
}

func TestRegistrationPreservesExistingIdentity(t *testing.T) {
	rs := newRegistrationServer(t)
	cookies := auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	status, _, _ := rs.requestCode(t, auth.TestOperatorEmail, cookies)
	if status != http.StatusConflict || rs.mailer.count() != 0 {
		t.Fatalf("authenticated code request status=%d sent=%d", status, rs.mailer.count())
	}
	resp := rs.register(t, auth.TestOperatorEmail, "unused", "123456", "Unwanted Merchant", cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("authenticated registration status=%d", resp.StatusCode)
	}
	status, proof, raw := rs.requestCode(t, auth.TestOperatorEmail, nil)
	if status != http.StatusOK {
		t.Fatalf("code request status=%d body=%s", status, raw)
	}
	resp = rs.register(t, auth.TestOperatorEmail, proof.ChallengeID, rs.mailer.last().Code, "Unwanted Merchant", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("existing email registration status=%d", resp.StatusCode)
	}
	var count int64
	if err := rs.db.Model(&schema.Merchants{}).Where("name = ?", "Unwanted Merchant").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unexpected merchant count=%d err=%v", count, err)
	}
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
}

func TestRegistrationRequiresAvailableMailer(t *testing.T) {
	rs := newRegistrationServer(t)
	_ = auth.MustAuthenticate(t, rs.client, rs.srv.URL)
	rs.mailer.mu.Lock()
	rs.mailer.available = false
	rs.mailer.mu.Unlock()
	status, _, _ := rs.requestCode(t, "unavailable@example.test", nil)
	if status != http.StatusServiceUnavailable || rs.mailer.count() != 0 {
		t.Fatalf("unavailable code request status=%d sent=%d", status, rs.mailer.count())
	}
	resp := rs.register(t, "unavailable@example.test", "unused", "123456", "Unwanted Merchant", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unavailable registration status=%d", resp.StatusCode)
	}
}
