package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type accountServer struct {
	*authServer
	mailer *fakeRegistrationMailer
	clock  *registrationClock
}

func newAccountServer(t *testing.T) *accountServer {
	t.Helper()
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_account_0908_%d", time.Now().UnixNano()))
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "account-test-session-secret"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	mailer := &fakeRegistrationMailer{available: true}
	clock := &registrationClock{now: time.Now().UTC()}
	h := auth.HTTP{
		Store:  store,
		Mailer: mailer,
		DB:     gdb,
		Service: auth.Service{
			DB:                        gdb,
			Now:                       clock.Now,
			RecoveryChallengeIDSecret: "account-test-recovery-id-secret",
		},
		AttemptLimiter: registrationAllowLimiter{},
	}
	httpx.AuthenticatedFunc = auth.Authenticated
	engine.Use(h.LoadPrincipal())
	h.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &accountServer{
		authServer: &authServer{srv: srv, client: &http.Client{}, db: gdb},
		mailer:     mailer,
		clock:      clock,
	}
}

func seedAccountUser(t *testing.T, as *accountServer, suffix, password string) schema.Users {
	t.Helper()
	now := time.Now().UTC()
	merchant := schema.Merchants{
		ID: clockid.New(), Name: "账户商家-" + suffix, Status: auth.MerchantStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := as.db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	// Register the merchant cleanup before seedDirectUser's user cleanup so the
	// user row is removed before its owning merchant.
	t.Cleanup(func() { _ = as.db.Where("id = ?", merchant.ID).Delete(&schema.Merchants{}).Error })
	email := suffix + "@example.test"
	return seedDirectUser(t, as.db, merchant.ID, email, password, "账户用户-"+suffix, false)
}

func accountResponse(t *testing.T, resp *http.Response) auth.AccountView {
	t.Helper()
	defer resp.Body.Close()
	var view auth.AccountView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

func assertRecoveryError(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || body["code"] != "invalid_recovery_code" || body["detail"] != "验证码无效或已过期，请重新申请" {
		t.Fatalf("recovery error status=%d body=%#v", resp.StatusCode, body)
	}
}

func recoveryRequest(t *testing.T, as *accountServer, email string) (int, map[string]any, []byte) {
	t.Helper()
	resp := as.do(t, http.MethodPost, "/api/auth/password-recovery/request", fmt.Sprintf(`{"email":%q}`, email), nil)
	defer resp.Body.Close()
	raw := make([]byte, 0, 256)
	buf := make([]byte, 256)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			raw = append(raw, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body, raw
}

func recoveryConfirm(t *testing.T, as *accountServer, email, challengeID, code, password string) *http.Response {
	t.Helper()
	return as.do(t, http.MethodPost, "/api/auth/password-recovery/confirm", fmt.Sprintf(
		`{"email":%q,"challenge_id":%q,"code":%q,"new_password":%q}`,
		email, challengeID, code, password), nil)
}

func TestAccountOwnProfileAndSessions(t *testing.T) {
	as := newAccountServer(t)
	userA := seedAccountUser(t, as, "account-a-"+clockid.New(), "account-password-a")
	userB := seedAccountUser(t, as, "account-b-"+clockid.New(), "account-password-b")
	cookiesA1 := loginDirectUser(t, as.authServer, userA.Email, "account-password-a")
	_ = loginDirectUser(t, as.authServer, userA.Email, "account-password-a")
	cookiesB := loginDirectUser(t, as.authServer, userB.Email, "account-password-b")

	viewResp := as.do(t, http.MethodGet, "/api/account", "", cookiesA1)
	if viewResp.StatusCode != http.StatusOK {
		t.Fatalf("account get status=%d", viewResp.StatusCode)
	}
	view := accountResponse(t, viewResp)
	if view.User.ID != userA.ID || view.User.Email != userA.Email || view.Merchant == nil || view.Merchant.ID != *userA.MerchantID {
		t.Fatalf("account view=%#v", view)
	}

	updated := as.do(t, http.MethodPatch, "/api/account", `{"display_name":"  修改后的账号  "}`, cookiesA1)
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("account patch status=%d", updated.StatusCode)
	}
	updatedView := accountResponse(t, updated)
	if updatedView.User.DisplayName != "修改后的账号" {
		t.Fatalf("updated account view=%#v", updatedView)
	}
	unknownField := as.do(t, http.MethodPatch, "/api/account", `{"display_name":"ok","email":"other@example.test"}`, cookiesA1)
	if unknownField.StatusCode != http.StatusBadRequest {
		unknownField.Body.Close()
		t.Fatalf("unknown account field status=%d", unknownField.StatusCode)
	}
	unknownField.Body.Close()

	list := as.do(t, http.MethodGet, "/api/account/sessions?limit=1", "", cookiesA1)
	if list.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(list.Body)
		list.Body.Close()
		t.Fatalf("session list status=%d body=%s", list.StatusCode, raw)
	}
	var first auth.AccountSessionPage
	if err := json.NewDecoder(list.Body).Decode(&first); err != nil {
		list.Body.Close()
		t.Fatal(err)
	}
	list.Body.Close()
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first session page=%#v", first)
	}
	second := as.do(t, http.MethodGet, "/api/account/sessions?limit=1&after="+url.QueryEscape(*first.NextCursor), "", cookiesA1)
	if second.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(second.Body)
		second.Body.Close()
		t.Fatalf("second session list status=%d body=%s", second.StatusCode, raw)
	}
	var secondPage auth.AccountSessionPage
	if err := json.NewDecoder(second.Body).Decode(&secondPage); err != nil {
		second.Body.Close()
		t.Fatal(err)
	}
	second.Body.Close()
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID >= first.Items[0].ID || secondPage.Items[0].Current == first.Items[0].Current {
		t.Fatalf("second session page=%#v", secondPage)
	}

	currentID := first.Items[0].ID
	if secondPage.Items[0].Current {
		currentID = secondPage.Items[0].ID
	}

	var userBSession schema.AuthSessions
	if err := as.db.Where("user_id = ?", userB.ID).Order("created_at DESC").Take(&userBSession).Error; err != nil {
		t.Fatal(err)
	}
	foreign := as.do(t, http.MethodDelete, "/api/account/sessions/"+userBSession.ID, "", cookiesA1)
	if foreign.StatusCode != http.StatusNotFound {
		foreign.Body.Close()
		t.Fatalf("foreign session revoke status=%d", foreign.StatusCode)
	}
	foreign.Body.Close()
	bList := as.do(t, http.MethodGet, "/api/account/sessions", "", cookiesB)
	if bList.StatusCode != http.StatusOK {
		bList.Body.Close()
		t.Fatalf("other account session list status=%d", bList.StatusCode)
	}
	var bPage auth.AccountSessionPage
	if err := json.NewDecoder(bList.Body).Decode(&bPage); err != nil {
		bList.Body.Close()
		t.Fatal(err)
	}
	bList.Body.Close()
	if len(bPage.Items) != 1 || bPage.Items[0].ID != userBSession.ID {
		t.Fatalf("foreign revoke changed other account=%#v", bPage)
	}

	var userASessions []schema.AuthSessions
	if err := as.db.Where("user_id = ?", userA.ID).Order("created_at DESC").Find(&userASessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(userASessions) != 2 {
		t.Fatalf("account sessions=%#v", userASessions)
	}
	var revokeID string
	for _, session := range userASessions {
		if session.ID != currentID {
			revokeID = session.ID
		}
	}
	revoke := as.do(t, http.MethodDelete, "/api/account/sessions/"+revokeID, "", cookiesA1)
	if revoke.StatusCode != http.StatusOK {
		revoke.Body.Close()
		t.Fatalf("own session revoke status=%d", revoke.StatusCode)
	}
	revoke.Body.Close()
	repeat := as.do(t, http.MethodDelete, "/api/account/sessions/"+revokeID, "", cookiesA1)
	if repeat.StatusCode != http.StatusOK {
		repeat.Body.Close()
		t.Fatalf("repeat own session revoke status=%d", repeat.StatusCode)
	}
	repeat.Body.Close()

	current := as.do(t, http.MethodDelete, "/api/account/sessions/"+currentID, "", cookiesA1)
	if current.StatusCode != http.StatusOK {
		current.Body.Close()
		t.Fatalf("current session revoke status=%d", current.StatusCode)
	}
	current.Body.Close()
	afterCurrent := as.do(t, http.MethodGet, "/api/account", "", cookiesA1)
	if afterCurrent.StatusCode != http.StatusUnauthorized {
		afterCurrent.Body.Close()
		t.Fatalf("revoked current session account status=%d", afterCurrent.StatusCode)
	}
	afterCurrent.Body.Close()
}

func TestPasswordChangeRevokesSessionsAndSerializesLogin(t *testing.T) {
	as := newAccountServer(t)
	user := seedAccountUser(t, as, "password-race-"+clockid.New(), "old-password-ok")
	cookies := loginDirectUser(t, as.authServer, user.Email, "old-password-ok")
	recoverySvc := auth.Service{
		DB:                        as.db,
		Now:                       as.clock.Now,
		RecoveryChallengeIDSecret: "account-test-recovery-id-secret",
	}
	oldRecovery, err := recoverySvc.CreatePasswordRecoveryChallenge(context.Background(), user.Email)
	if err != nil {
		t.Fatal(err)
	}

	changed := as.do(t, http.MethodPost, "/api/account/password", `{"current_password":"old-password-ok","new_password":"new-password-ok"}`, cookies)
	if changed.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(changed.Body)
		changed.Body.Close()
		t.Fatalf("password change status=%d body=%s", changed.StatusCode, raw)
	}
	changed.Body.Close()
	oldSession := as.do(t, http.MethodGet, "/api/account", "", cookies)
	if oldSession.StatusCode != http.StatusUnauthorized {
		oldSession.Body.Close()
		t.Fatalf("old session after password change status=%d", oldSession.StatusCode)
	}
	oldSession.Body.Close()
	var active int64
	if err := as.db.Model(&schema.AuthSessions{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("active sessions after password change=%d", active)
	}
	var oldRecoveryRow schema.PasswordRecoveryChallenges
	if err := as.db.Where("id = ?", oldRecovery.ID).Take(&oldRecoveryRow).Error; err != nil {
		t.Fatal(err)
	}
	if oldRecoveryRow.ConsumedAt == nil {
		t.Fatal("password change left an active recovery challenge")
	}
	oldLogin := as.do(t, http.MethodPost, "/api/auth/session", fmt.Sprintf(`{"email":%q,"password":"old-password-ok"}`, user.Email), nil)
	if oldLogin.StatusCode != http.StatusUnauthorized {
		oldLogin.Body.Close()
		t.Fatalf("old password login status=%d", oldLogin.StatusCode)
	}
	oldLogin.Body.Close()
	newLogin := as.do(t, http.MethodPost, "/api/auth/session", fmt.Sprintf(`{"email":%q,"password":"new-password-ok"}`, user.Email), nil)
	if newLogin.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(newLogin.Body)
		newLogin.Body.Close()
		t.Fatalf("new password login status=%d body=%s", newLogin.StatusCode, raw)
	}
	newLogin.Body.Close()

	// Race the same old credential against a password change. Whichever side
	// acquires User first, no old-password session may remain active afterwards.
	raceUser := seedAccountUser(t, as, "password-race-concurrent-"+clockid.New(), "race-old-password")
	_ = loginDirectUser(t, as.authServer, raceUser.Email, "race-old-password")
	svc := auth.Service{DB: as.db, Now: as.clock.Now}
	var wg sync.WaitGroup
	loginErrors := make(chan error, 13)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := svc.Login(context.Background(), raceUser.Email, "race-old-password")
			loginErrors <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		loginErrors <- svc.ChangePassword(context.Background(), raceUser.ID, "race-old-password", "race-new-password")
	}()
	wg.Wait()
	close(loginErrors)
	for err := range loginErrors {
		if err != nil {
			var appErr apperr.Error
			if !errors.As(err, &appErr) || appErr.Status != http.StatusUnauthorized {
				t.Fatalf("password/login race error=%v", err)
			}
		}
	}
	if err := as.db.Model(&schema.AuthSessions{}).Where("user_id = ? AND revoked_at IS NULL", raceUser.ID).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("old sessions survived password/login race=%d", active)
	}
	if _, _, err := svc.Login(context.Background(), raceUser.Email, "race-old-password"); err == nil {
		t.Fatal("old password still logs in after race")
	}
	if _, _, err := svc.Login(context.Background(), raceUser.Email, "race-new-password"); err != nil {
		t.Fatalf("new password failed after race: %v", err)
	}
}

func TestPasswordRecoveryIsNonEnumeratingAndPersistsAttempts(t *testing.T) {
	as := newAccountServer(t)
	user := seedAccountUser(t, as, "recovery-"+clockid.New(), "recovery-old-password")
	status, known, raw := recoveryRequest(t, as, user.Email)
	if status != http.StatusAccepted || known["challenge_id"] == "" || known["expires_in_seconds"] != float64(600) || known["resend_after_seconds"] != float64(60) {
		t.Fatalf("known recovery status=%d body=%s", status, raw)
	}
	knownID := known["challenge_id"].(string)
	knownCode := as.mailer.last().Code
	if len(knownCode) != 6 {
		t.Fatalf("known recovery code=%q", knownCode)
	}
	var knownRow schema.PasswordRecoveryChallenges
	if err := as.db.Where("id = ?", knownID).Take(&knownRow).Error; err != nil {
		t.Fatal(err)
	}
	// A known address in the resend window keeps the same code and challenge
	// id while refreshing the persisted ten-minute expiry promised by the wire.
	knownMailCount := as.mailer.count()
	as.clock.Advance(30 * time.Second)
	knownRepeatStatus, knownRepeat, knownRepeatRaw := recoveryRequest(t, as, user.Email)
	if knownRepeatStatus != http.StatusAccepted || knownRepeat["challenge_id"] != knownID ||
		knownRepeat["expires_in_seconds"] != float64(600) || as.mailer.count() != knownMailCount {
		t.Fatalf("known repeat recovery status=%d body=%s sent=%d", knownRepeatStatus, knownRepeatRaw, as.mailer.count())
	}
	if err := as.db.Where("id = ?", knownID).Take(&knownRow).Error; err != nil {
		t.Fatal(err)
	}
	remaining := knownRow.ExpiresAt.Sub(as.clock.Now())
	if remaining < 10*time.Minute-time.Second || remaining > 10*time.Minute+time.Second {
		t.Fatalf("known repeat expiry remaining=%s", remaining)
	}

	beforeUnknownMail := as.mailer.count()
	unknownEmail := "stable-unknown-" + clockid.New() + "@example.test"
	unknownStatus, unknown, unknownRaw := recoveryRequest(t, as, unknownEmail)
	if unknownStatus != http.StatusAccepted || unknown["challenge_id"] == "" || unknown["expires_in_seconds"] != float64(600) || unknown["resend_after_seconds"] != float64(60) {
		t.Fatalf("unknown recovery status=%d body=%s", unknownStatus, unknownRaw)
	}
	unknownRepeatStatus, unknownRepeat, unknownRepeatRaw := recoveryRequest(t, as, unknownEmail)
	if unknownRepeatStatus != http.StatusAccepted || unknownRepeat["challenge_id"] == "" {
		t.Fatalf("unknown repeat recovery status=%d body=%s", unknownRepeatStatus, unknownRepeatRaw)
	}
	// Repeat the same unknown address. Its stable opaque placeholder has the
	// same equality behavior as a known challenge inside the resend window.
	if unknown["challenge_id"] != unknownRepeat["challenge_id"] {
		t.Fatalf("unknown repeat recovery first=%s second=%s", unknownRaw, unknownRepeatRaw)
	}
	if as.mailer.count() != beforeUnknownMail {
		t.Fatalf("unknown recovery sent mail count=%d want=%d", as.mailer.count(), beforeUnknownMail)
	}
	var unknownRows int64
	if err := as.db.Model(&schema.PasswordRecoveryChallenges{}).Where("id = ?", unknown["challenge_id"]).Count(&unknownRows).Error; err != nil {
		t.Fatal(err)
	}
	if unknownRows != 0 {
		t.Fatalf("unknown recovery persisted challenge=%v", unknown["challenge_id"])
	}

	if err := as.db.Model(&schema.Users{}).Where("id = ?", user.ID).Update("status", auth.UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	disabledStatus, disabled, disabledRaw := recoveryRequest(t, as, user.Email)
	if disabledStatus != http.StatusAccepted || disabled["challenge_id"] == "" || disabled["expires_in_seconds"] != float64(600) || disabled["resend_after_seconds"] != float64(60) {
		t.Fatalf("disabled recovery status=%d body=%s", disabledStatus, disabledRaw)
	}
	disabledRepeatStatus, disabledRepeat, disabledRepeatRaw := recoveryRequest(t, as, user.Email)
	if disabledRepeatStatus != http.StatusAccepted || disabledRepeat["challenge_id"] != disabled["challenge_id"] {
		t.Fatalf("disabled repeat recovery status=%d body=%s", disabledRepeatStatus, disabledRepeatRaw)
	}
	if as.mailer.count() != beforeUnknownMail {
		t.Fatalf("disabled recovery sent mail count=%d want=%d", as.mailer.count(), beforeUnknownMail)
	}
	if err := as.db.Model(&schema.Users{}).Where("id = ?", user.ID).Update("status", auth.UserStatusActive).Error; err != nil {
		t.Fatal(err)
	}

	// The disabled requests consumed the prior challenge. Once the account is
	// active again, a new request creates and delivers a fresh challenge.
	repeatStatus, repeat, repeatRaw := recoveryRequest(t, as, user.Email)
	if repeatStatus != http.StatusAccepted || repeat["challenge_id"] != knownID ||
		as.mailer.count() != beforeUnknownMail+1 {
		t.Fatalf("recovery resend gate status=%d body=%s sent=%d", repeatStatus, repeatRaw, as.mailer.count())
	}

	wrong := wrongVerificationCode(knownCode)
	for i := 0; i < 5; i++ {
		resp := recoveryConfirm(t, as, user.Email, knownID, wrong, "recovery-new-password")
		assertRecoveryError(t, resp)
	}
	if err := as.db.Where("id = ?", knownID).Take(&knownRow).Error; err != nil {
		t.Fatal(err)
	}
	if knownRow.FailedAttempts != 5 || knownRow.ConsumedAt != nil {
		t.Fatalf("failed attempts row=%#v", knownRow)
	}
	assertRecoveryError(t, recoveryConfirm(t, as, user.Email, knownID, wrong, "recovery-new-password"))

	// A malformed/fabricated challenge and a challenge belonging to a different
	// email use the same public error, including after the account is disabled.
	assertRecoveryError(t, recoveryConfirm(t, as, "unknown-"+clockid.New()+"@example.test", clockid.New(), "123456", "recovery-new-password"))
	assertRecoveryError(t, recoveryConfirm(t, as, "other-"+clockid.New()+"@example.test", knownID, knownCode, "recovery-new-password"))
	if err := as.db.Model(&schema.Users{}).Where("id = ?", user.ID).Update("status", auth.UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	assertRecoveryError(t, recoveryConfirm(t, as, user.Email, knownID, knownCode, "recovery-new-password"))
	if err := as.db.Model(&schema.Users{}).Where("id = ?", user.ID).Update("status", auth.UserStatusActive).Error; err != nil {
		t.Fatal(err)
	}

	// Advance past the resend gate to create a new challenge, then expire it.
	as.clock.Advance(61 * time.Second)
	status, fresh, freshRaw := recoveryRequest(t, as, user.Email)
	if status != http.StatusAccepted || fresh["challenge_id"] == "" {
		t.Fatalf("fresh recovery status=%d body=%s", status, freshRaw)
	}
	freshID := fresh["challenge_id"].(string)
	freshCode := as.mailer.last().Code
	as.clock.Advance(10*time.Minute + time.Second)
	assertRecoveryError(t, recoveryConfirm(t, as, user.Email, freshID, freshCode, "recovery-new-password"))

	// A single SMTP failure is accepted with the same 202 shape but leaves no
	// active credential and exposes neither the attempted code nor transport error.
	as.clock.Advance(61 * time.Second)
	as.mailer.mu.Lock()
	as.mailer.sendErr = errors.New("smtp unavailable")
	as.mailer.mu.Unlock()
	failStatus, failed, failedRaw := recoveryRequest(t, as, user.Email)
	as.mailer.mu.Lock()
	as.mailer.sendErr = nil
	as.mailer.mu.Unlock()
	if failStatus != http.StatusAccepted || failed["challenge_id"] == "" || failed["expires_in_seconds"] != float64(600) || failed["resend_after_seconds"] != float64(60) {
		t.Fatalf("failed delivery status=%d body=%s", failStatus, failedRaw)
	}
	var activeRecovery int64
	if err := as.db.Model(&schema.PasswordRecoveryChallenges{}).Where("user_id = ? AND consumed_at IS NULL", user.ID).Count(&activeRecovery).Error; err != nil {
		t.Fatal(err)
	}
	if activeRecovery != 0 {
		t.Fatalf("failed delivery left active recovery challenge=%d", activeRecovery)
	}
	assertRecoveryError(t, recoveryConfirm(t, as, user.Email, failed["challenge_id"].(string), as.mailer.last().Code, "recovery-new-password"))

	// Global SMTP unavailability is checked before account lookup and is the one
	// recovery request failure that is intentionally visible as 503.
	as.mailer.mu.Lock()
	as.mailer.available = false
	as.mailer.mu.Unlock()
	unavailableStatus, _, _ := recoveryRequest(t, as, "another-"+clockid.New()+"@example.test")
	if unavailableStatus != http.StatusServiceUnavailable {
		t.Fatalf("unavailable SMTP status=%d", unavailableStatus)
	}
}

func TestPasswordRecoverySuccessConsumesAndRevokesSessions(t *testing.T) {
	as := newAccountServer(t)
	user := seedAccountUser(t, as, "recovery-success-"+clockid.New(), "recovery-success-old")
	cookies := loginDirectUser(t, as.authServer, user.Email, "recovery-success-old")
	status, body, raw := recoveryRequest(t, as, user.Email)
	if status != http.StatusAccepted {
		t.Fatalf("recovery request status=%d body=%s", status, raw)
	}
	challengeID := body["challenge_id"].(string)
	code := as.mailer.last().Code
	confirmed := recoveryConfirm(t, as, user.Email, challengeID, code, "recovery-success-new")
	if confirmed.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(confirmed.Body)
		confirmed.Body.Close()
		t.Fatalf("recovery confirm status=%d body=%s", confirmed.StatusCode, raw)
	}
	confirmed.Body.Close()
	oldSession := as.do(t, http.MethodGet, "/api/account", "", cookies)
	if oldSession.StatusCode != http.StatusUnauthorized {
		oldSession.Body.Close()
		t.Fatalf("session after recovery status=%d", oldSession.StatusCode)
	}
	oldSession.Body.Close()
	var challenge schema.PasswordRecoveryChallenges
	if err := as.db.Where("id = ?", challengeID).Take(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	if challenge.ConsumedAt == nil {
		t.Fatal("recovery challenge was not consumed")
	}
	assertRecoveryError(t, recoveryConfirm(t, as, user.Email, challengeID, code, "recovery-success-newer"))
	oldLogin := as.do(t, http.MethodPost, "/api/auth/session", fmt.Sprintf(`{"email":%q,"password":"recovery-success-old"}`, user.Email), nil)
	if oldLogin.StatusCode != http.StatusUnauthorized {
		oldLogin.Body.Close()
		t.Fatalf("old password after recovery status=%d", oldLogin.StatusCode)
	}
	oldLogin.Body.Close()
	newLogin := as.do(t, http.MethodPost, "/api/auth/session", fmt.Sprintf(`{"email":%q,"password":"recovery-success-new"}`, user.Email), nil)
	if newLogin.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(newLogin.Body)
		newLogin.Body.Close()
		t.Fatalf("new password after recovery status=%d body=%s", newLogin.StatusCode, raw)
	}
	newLogin.Body.Close()
}

func TestPasswordRecoverySMTPFailureCleansUpAfterRequestCancellation(t *testing.T) {
	as := newAccountServer(t)
	user := seedAccountUser(t, as, "recovery-cancelled-smtp-"+clockid.New(), "recovery-cancelled-password")
	sendStarted := make(chan struct{})
	sendReturned := make(chan struct{})
	as.mailer.mu.Lock()
	as.mailer.resetSend = func(ctx context.Context, _, _ string) error {
		close(sendStarted)
		<-ctx.Done()
		close(sendReturned)
		return ctx.Err()
	}
	as.mailer.mu.Unlock()
	defer func() {
		as.mailer.mu.Lock()
		as.mailer.resetSend = nil
		as.mailer.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, as.srv.URL+"/api/auth/password-recovery/request", strings.NewReader(fmt.Sprintf(`{"email":%q}`, user.Email)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	requestDone := make(chan error, 1)
	go func() {
		resp, err := as.client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-sendStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP send did not start")
	}
	cancel()
	select {
	case <-sendReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP send did not observe request cancellation")
	}
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled recovery request did not finish")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		var active int64
		if err := as.db.Model(&schema.PasswordRecoveryChallenges{}).Where("user_id = ? AND consumed_at IS NULL", user.ID).Count(&active).Error; err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled SMTP failure left active recovery challenge=%d", active)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPasswordRecoveryDeliveryCleanupBindsToIssuedCode(t *testing.T) {
	as := newAccountServer(t)
	user := seedAccountUser(t, as, "recovery-versioned-cleanup-"+clockid.New(), "recovery-versioned-password")
	svc := auth.Service{
		DB:                        as.db,
		Now:                       as.clock.Now,
		RecoveryChallengeIDSecret: "account-test-recovery-id-secret",
	}
	first, err := svc.CreatePasswordRecoveryChallenge(context.Background(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	as.clock.Advance(61 * time.Second)
	second, err := svc.CreatePasswordRecoveryChallenge(context.Background(), user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("replacement changed stable challenge id first=%q second=%q", first.ID, second.ID)
	}
	if first.CodeHash == "" || second.CodeHash == "" || first.CodeHash == second.CodeHash {
		t.Fatalf("replacement code hashes first=%q second=%q", first.CodeHash, second.CodeHash)
	}
	if err := svc.InvalidatePasswordRecoveryChallenge(context.Background(), first.ID, first.CodeHash); err != nil {
		t.Fatal(err)
	}
	var row schema.PasswordRecoveryChallenges
	if err := as.db.Where("id = ?", second.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ConsumedAt != nil || row.CodeHash != second.CodeHash {
		t.Fatalf("late cleanup consumed replacement row=%#v", row)
	}
}

func TestAccountRoutesRequireAuthentication(t *testing.T) {
	as := newAccountServer(t)
	for _, route := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/account", ""},
		{http.MethodPatch, "/api/account", `{"display_name":"x"}`},
		{http.MethodPost, "/api/account/password", `{"current_password":"old","new_password":"new-password"}`},
		{http.MethodGet, "/api/account/sessions", ""},
		{http.MethodDelete, "/api/account/sessions/" + clockid.New(), ""},
	} {
		resp := as.do(t, route.method, route.path, route.body, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			resp.Body.Close()
			t.Fatalf("unauthenticated %s %s status=%d", route.method, route.path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestAccountLogoutFailureRemainsRetryable(t *testing.T) {
	as := newAccountServer(t)
	user := seedAccountUser(t, as, "logout-"+clockid.New(), "logout-password")
	cookies := loginDirectUser(t, as.authServer, user.Email, "logout-password")
	const callback = "test:fail-session-revoke"
	if err := as.db.Callback().Update().Before("gorm:update").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "auth_sessions" {
			db.AddError(errors.New("injected revoke failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { as.db.Callback().Update().Remove(callback) })
	failed := as.do(t, http.MethodDelete, "/api/auth/session", "", cookies)
	if failed.StatusCode != http.StatusInternalServerError {
		t.Fatalf("failed logout status=%d", failed.StatusCode)
	}
	for _, cookie := range failed.Cookies() {
		if cookie.Name == "session" && cookie.MaxAge < 0 {
			t.Fatal("failed logout cleared retryable session cookie")
		}
	}
	failed.Body.Close()
	if err := as.db.Callback().Update().Remove(callback); err != nil {
		t.Fatal(err)
	}
	stillValid := as.do(t, http.MethodGet, "/api/account", "", cookies)
	if stillValid.StatusCode != http.StatusOK {
		t.Fatalf("retryable session status=%d", stillValid.StatusCode)
	}
	stillValid.Body.Close()
	retry := as.do(t, http.MethodDelete, "/api/auth/session", "", cookies)
	if retry.StatusCode != http.StatusOK {
		t.Fatalf("logout retry status=%d", retry.StatusCode)
	}
	retry.Body.Close()
	revoked := as.do(t, http.MethodGet, "/api/account", "", cookies)
	if revoked.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logout retry did not revoke session: %d", revoked.StatusCode)
	}
	revoked.Body.Close()
}
