package auth

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestClientIPTrustsOnlyConfiguredProxyChain(t *testing.T) {
	request := func(remote, forwarded string) *http.Request {
		r := httptestNewRequest(remote)
		r.Header.Set("X-Forwarded-For", forwarded)
		return r
	}
	if got := ClientIP(request("192.0.2.10:1234", "203.0.113.1"), []string{"198.51.100.0/24"}); got != "192.0.2.10" {
		t.Fatalf("direct forged XFF identity=%q", got)
	}
	if got := ClientIP(request("198.51.100.10:1234", "203.0.113.1, 198.51.100.20"), []string{"198.51.100.0/24"}); got != "203.0.113.1" {
		t.Fatalf("trusted proxy identity=%q", got)
	}
}

func TestRedisAttemptLimiterAtomicBudgets(t *testing.T) {
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:16379/1"
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("invalid REDIS_URL: %v", err)
	}
	clientA := redis.NewClient(options)
	clientB := redis.NewClient(options)
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	if err := clientA.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis unavailable: %v", err)
	}
	namespace := "productflow:test:auth-attempt:" + time.Now().UTC().Format("20060102150405.000000000")
	limiterA := NewRedisAttemptLimiterClient(clientA, RedisAttemptLimiterConfig{
		Namespace:  namespace,
		Window:     15 * time.Minute,
		IPMax:      100,
		SubjectMax: 10,
	})
	limiterB := NewRedisAttemptLimiterClient(clientB, RedisAttemptLimiterConfig{
		Namespace:  namespace,
		Window:     15 * time.Minute,
		IPMax:      100,
		SubjectMax: 10,
	})

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	decisions := make([]AttemptDecision, 0, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			limiter := limiterA
			if index%2 == 1 {
				limiter = limiterB
			}
			decision, err := limiter.Allow(context.Background(), "203.0.113.10", "account@example.test")
			if err != nil {
				t.Errorf("Allow: %v", err)
				return
			}
			mu.Lock()
			decisions = append(decisions, decision)
			if decision.Allowed {
				allowed++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if allowed != 10 {
		t.Fatalf("same account/IP allowed=%d want=10", allowed)
	}
	for _, decision := range decisions {
		if !decision.Allowed && decision.RetryAfter < time.Second {
			t.Fatalf("denied decision has RetryAfter=%s", decision.RetryAfter)
		}
	}
	for _, ip := range []string{"203.0.113.11", "203.0.113.12"} {
		for i := 0; i < 10; i++ {
			limiter := limiterA
			if i%2 == 1 {
				limiter = limiterB
			}
			decision, err := limiter.Allow(context.Background(), ip, "account@example.test")
			if err != nil {
				t.Fatal(err)
			}
			if !decision.Allowed {
				t.Fatalf("different IP %s attempt %d denied", ip, i+1)
			}
		}
	}

	ipNamespace := namespace + ":ip-total"
	ipLimiterA := NewRedisAttemptLimiterClient(clientA, RedisAttemptLimiterConfig{Namespace: ipNamespace, Window: 15 * time.Minute, IPMax: 100, SubjectMax: 10})
	ipLimiterB := NewRedisAttemptLimiterClient(clientB, RedisAttemptLimiterConfig{Namespace: ipNamespace, Window: 15 * time.Minute, IPMax: 100, SubjectMax: 10})
	ipAllowed := 0
	for i := 0; i < 100; i++ {
		limiter := ipLimiterA
		if i%2 == 1 {
			limiter = ipLimiterB
		}
		decision, err := limiter.Allow(context.Background(), "203.0.113.50", "account-"+strconv.Itoa(i)+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allowed {
			ipAllowed++
		}
	}
	if ipAllowed != 100 {
		t.Fatalf("same IP different accounts allowed=%d want=100", ipAllowed)
	}
	var ipKeysBefore []string
	var ipCursor uint64
	for {
		keys, next, err := clientA.Scan(context.Background(), ipCursor, ipNamespace+"*", 100).Result()
		if err != nil {
			t.Fatal(err)
		}
		ipKeysBefore = append(ipKeysBefore, keys...)
		ipCursor = next
		if ipCursor == 0 {
			break
		}
	}
	for i := 100; i < 140; i++ {
		limiter := ipLimiterA
		if i%2 == 1 {
			limiter = ipLimiterB
		}
		decision, err := limiter.Allow(context.Background(), "203.0.113.50", "account-"+strconv.Itoa(i)+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allowed {
			t.Fatalf("IP-exhausted random subject %d was allowed", i)
		}
	}
	var ipKeysAfter []string
	ipCursor = 0
	for {
		keys, next, err := clientA.Scan(context.Background(), ipCursor, ipNamespace+"*", 100).Result()
		if err != nil {
			t.Fatal(err)
		}
		ipKeysAfter = append(ipKeysAfter, keys...)
		ipCursor = next
		if ipCursor == 0 {
			break
		}
	}
	if len(ipKeysAfter) != len(ipKeysBefore) {
		t.Fatalf("IP budget exhaustion added keys before=%d after=%d", len(ipKeysBefore), len(ipKeysAfter))
	}

	boundaryNamespace := namespace + ":boundary"
	boundaryNow := time.Unix(29, 0).UTC()
	boundary := NewRedisAttemptLimiterClient(clientA, RedisAttemptLimiterConfig{Namespace: boundaryNamespace, Window: 10 * time.Second, IPMax: 100, SubjectMax: 1})
	boundary.Now = func() time.Time { return boundaryNow }
	if decision, err := boundary.Allow(context.Background(), "203.0.113.60", "boundary@example.test"); err != nil || !decision.Allowed {
		t.Fatalf("boundary first decision=%+v err=%v", decision, err)
	}
	if decision, err := boundary.Allow(context.Background(), "203.0.113.60", "boundary@example.test"); err != nil || decision.Allowed || decision.RetryAfter != time.Second {
		t.Fatalf("boundary denied decision=%+v err=%v", decision, err)
	}
	boundaryNow = time.Unix(30, 0).UTC()
	if decision, err := boundary.Allow(context.Background(), "203.0.113.60", "boundary@example.test"); err != nil || !decision.Allowed {
		t.Fatalf("new window decision=%+v err=%v", decision, err)
	}

	var cursor uint64
	for {
		keys, next, err := clientA.Scan(context.Background(), cursor, namespace+"*", 100).Result()
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			ttl, err := clientA.TTL(context.Background(), key).Result()
			if err != nil {
				t.Fatal(err)
			}
			if ttl <= 0 || ttl > 15*time.Minute {
				t.Fatalf("key %q TTL=%s", key, ttl)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}

func TestRedisAttemptLimiterFailsClosedWithoutClient(t *testing.T) {
	limiter := NewRedisAttemptLimiterClient(nil, RedisAttemptLimiterConfig{})
	if _, err := limiter.Allow(context.Background(), "203.0.113.10", "account@example.test"); err == nil {
		t.Fatal("nil Redis client allowed an attempt")
	}
}

func httptestNewRequest(remote string) *http.Request {
	return &http.Request{RemoteAddr: remote, Header: make(http.Header)}
}
