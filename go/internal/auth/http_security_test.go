package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestTwoAuthHTTPInstancesShareRedisBudgetAndReadsSurviveRedisClose(t *testing.T) {
	options, stopRedis := securityRedis(t)
	redisA := redis.NewClient(options)
	redisB := redis.NewClient(options)
	t.Cleanup(func() {
		_ = redisA.Close()
		_ = redisB.Close()
	})

	pool, db := testdb.Open(t)
	email := "security-" + randomTestSuffix() + "@example.test"
	password := "password-for-security-test"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	userID := clockid.New()
	createdUserIDs := []string{userID}
	createdMerchantIDs := []string{}
	t.Cleanup(func() {
		if err := db.Where("id IN ?", createdUserIDs).Delete(&schema.Users{}).Error; err != nil {
			t.Errorf("cleanup security users: %v", err)
		}
		if err := db.Where("id IN ?", createdMerchantIDs).Delete(&schema.Merchants{}).Error; err != nil {
			t.Errorf("cleanup security merchants: %v", err)
		}
	})
	now := time.Now().UTC()
	merchant := schema.Merchants{
		ID: clockid.New(), Name: "security-merchant-" + randomTestSuffix(), Status: auth.MerchantStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	createdMerchantIDs = append(createdMerchantIDs, merchant.ID)
	if err := db.Create(&schema.Users{
		ID: userID, Email: email, PasswordHash: string(hash), DisplayName: "security", Status: auth.UserStatusActive,
		MerchantID: &merchant.ID,
		CreatedAt:  now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	namespace := "productflow:test:http-auth:" + randomTestSuffix()
	limiterConfig := auth.RedisAttemptLimiterConfig{Namespace: namespace, Window: 15 * time.Minute, IPMax: 100, SubjectMax: 10}
	limiterA := auth.NewRedisAttemptLimiterClient(redisA, limiterConfig)
	limiterB := auth.NewRedisAttemptLimiterClient(redisB, limiterConfig)
	serverA := newSecurityAuthServer(t, pool, db, limiterA)
	serverB := newSecurityAuthServer(t, pool, db, limiterB)

	loginBody := `{"email":"` + email + `","password":"wrong-password"}`
	statuses := make(chan int, 30)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			baseURL := serverA.URL
			if i%2 == 1 {
				baseURL = serverB.URL
			}
			req, err := http.NewRequest(http.MethodPost, baseURL+"/api/auth/session", strings.NewReader(loginBody))
			if err != nil {
				t.Errorf("new request: %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://web.test")
			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				t.Errorf("login request: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			statuses <- resp.StatusCode
		}(i)
	}
	wg.Wait()
	close(statuses)
	unauthorized, limited := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusUnauthorized:
			unauthorized++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected login status %d", status)
		}
	}
	if unauthorized != 10 || limited != 20 {
		t.Fatalf("shared budget unauthorized=%d limited=%d want 10/20", unauthorized, limited)
	}

	validReq, err := http.NewRequest(http.MethodPost, serverA.URL+"/api/auth/session", strings.NewReader(`{"email":"`+email+`","password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	validReq.Header.Set("Content-Type", "application/json")
	validReq.Header.Set("Origin", "http://web.test")
	validResp, err := (&http.Client{}).Do(validReq)
	if err != nil {
		t.Fatal(err)
	}
	if validResp.StatusCode != http.StatusTooManyRequests {
		validResp.Body.Close()
		t.Fatalf("budget should count successes and be exhausted, got %d", validResp.StatusCode)
	}
	validResp.Body.Close()

	// A fresh subject gets a session before Redis is stopped, then ordinary
	// session reads must continue to use PostgreSQL only.
	readEmail := "read-" + randomTestSuffix() + "@example.test"
	readHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	readUserID := clockid.New()
	createdUserIDs = append(createdUserIDs, readUserID)
	readMerchant := schema.Merchants{
		ID: clockid.New(), Name: "read-merchant-" + randomTestSuffix(), Status: auth.MerchantStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&readMerchant).Error; err != nil {
		t.Fatal(err)
	}
	createdMerchantIDs = append(createdMerchantIDs, readMerchant.ID)
	if err := db.Create(&schema.Users{
		ID: readUserID, Email: readEmail, PasswordHash: string(readHash), DisplayName: "read", Status: auth.UserStatusActive,
		MerchantID: &readMerchant.ID,
		CreatedAt:  now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	readReq, err := http.NewRequest(http.MethodPost, serverA.URL+"/api/auth/session", strings.NewReader(`{"email":"`+readEmail+`","password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	readReq.Header.Set("Content-Type", "application/json")
	readReq.Header.Set("Origin", "http://web.test")
	readResp, err := (&http.Client{}).Do(readReq)
	if err != nil {
		t.Fatal(err)
	}
	if readResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(readResp.Body)
		readResp.Body.Close()
		t.Fatalf("read session login %d %s", readResp.StatusCode, body)
	}
	cookies := readResp.Cookies()
	readResp.Body.Close()
	stopRedis()

	closedReq, err := http.NewRequest(http.MethodPost, serverB.URL+"/api/auth/session", strings.NewReader(`{"email":"`+readEmail+`","password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	closedReq.Header.Set("Content-Type", "application/json")
	closedReq.Header.Set("Origin", "http://web.test")
	closedResp, err := (&http.Client{}).Do(closedReq)
	if err != nil {
		t.Fatal(err)
	}
	if closedResp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(closedResp.Body)
		closedResp.Body.Close()
		t.Fatalf("closed Redis login %d %s", closedResp.StatusCode, body)
	}
	closedResp.Body.Close()
	stateReq, err := http.NewRequest(http.MethodGet, serverA.URL+"/api/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		stateReq.AddCookie(cookie)
	}
	stateResp, err := (&http.Client{}).Do(stateReq)
	if err != nil {
		t.Fatal(err)
	}
	defer stateResp.Body.Close()
	var state struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if stateResp.StatusCode != http.StatusOK || !state.Authenticated {
		t.Fatalf("session read after Redis close status=%d state=%+v", stateResp.StatusCode, state)
	}
}

func newSecurityAuthServer(t *testing.T, pool *pgxpool.Pool, db *gorm.DB, limiter auth.AttemptLimiter) *httptest.Server {
	t.Helper()
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "security-test-session-secret"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	h := auth.HTTP{AdminAccessKey: auth.TestAdminKey, Store: store, DB: db, Service: auth.Service{DB: db}, AttemptLimiter: limiter}
	httpx.AuthenticatedFunc = auth.Authenticated
	engine.Use(h.LoadPrincipal())
	engine.Use(httpx.BrowserStateProtection(httpx.BrowserStateConfig{AllowedOrigins: []string{"http://web.test"}}))
	h.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv
}

func randomTestSuffix() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return clockid.New()
	}
	return hex.EncodeToString(buf[:])
}

func securityRedis(t *testing.T) (*redis.Options, func()) {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is required for the isolated outage test")
	}
	socket := filepath.Join(t.TempDir(), "redis.sock")
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	options := &redis.Options{Network: "unix", Addr: socket, MaxRetries: -1, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond}
	probe := redis.NewClient(options)
	defer probe.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := probe.Ping(context.Background()).Err(); err == nil {
			return options, stop
		}
		if time.Now().After(deadline) {
			t.Fatal("dedicated Redis failed to start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
