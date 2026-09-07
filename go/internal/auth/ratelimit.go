package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultAttemptWindow     = 15 * time.Minute
	defaultAttemptIPMax      = 100
	defaultAttemptSubjectMax = 10
)

// AttemptDecision is the result of one atomic IP+credential budget decision.
type AttemptDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// AttemptLimiter is deliberately small so HTTP tests can use a dedicated
// Redis client without coupling auth handlers to a broker implementation.
type AttemptLimiter interface {
	Allow(ctx context.Context, ip, subject string) (AttemptDecision, error)
}

// RedisAttemptLimiter applies both credential budgets in one Redis Lua call.
// The two counters use the same fixed window and expire automatically.
type RedisAttemptLimiter struct {
	Client     redis.UniversalClient
	Namespace  string
	Window     time.Duration
	IPMax      int
	SubjectMax int
	Now        func() time.Time
}

// RedisAttemptLimiterConfig contains env-only limiter settings.
type RedisAttemptLimiterConfig struct {
	Namespace  string
	Window     time.Duration
	IPMax      int
	SubjectMax int
}

// NewRedisAttemptLimiter parses REDIS_URL and creates a client. The caller
// owns the returned client and should close it during process shutdown.
func NewRedisAttemptLimiter(redisURL string, cfg RedisAttemptLimiterConfig) (*RedisAttemptLimiter, error) {
	if strings.TrimSpace(redisURL) == "" {
		return nil, errors.New("REDIS_URL is required for auth attempt limiting")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	return NewRedisAttemptLimiterClient(redis.NewClient(options), cfg), nil
}

// NewRedisAttemptLimiterClient is useful for focused tests and for callers
// that already own a Redis client.
func NewRedisAttemptLimiterClient(client redis.UniversalClient, cfg RedisAttemptLimiterConfig) *RedisAttemptLimiter {
	if cfg.Namespace == "" {
		cfg.Namespace = "productflow:auth:attempt"
	}
	if cfg.Window <= 0 {
		cfg.Window = defaultAttemptWindow
	}
	if cfg.IPMax <= 0 {
		cfg.IPMax = defaultAttemptIPMax
	}
	if cfg.SubjectMax <= 0 {
		cfg.SubjectMax = defaultAttemptSubjectMax
	}
	return &RedisAttemptLimiter{
		Client: client, Namespace: cfg.Namespace, Window: cfg.Window,
		IPMax: cfg.IPMax, SubjectMax: cfg.SubjectMax,
	}
}

const attemptScript = `
local ip_count = redis.call('INCR', KEYS[1])
if ip_count == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
if ip_count > tonumber(ARGV[2]) then
  local ip_ttl = redis.call('TTL', KEYS[1])
  if ip_ttl < 1 then ip_ttl = 1 end
  return {0, ip_ttl}
end
local subject_count = redis.call('INCR', KEYS[2])
if subject_count == 1 then redis.call('EXPIRE', KEYS[2], ARGV[1]) end
local retry_after = 0
if subject_count > tonumber(ARGV[3]) then
  local ip_ttl = redis.call('TTL', KEYS[1])
  local subject_ttl = redis.call('TTL', KEYS[2])
  retry_after = ip_ttl
  if subject_ttl > retry_after then retry_after = subject_ttl end
  if retry_after < 1 then retry_after = 1 end
  return {0, retry_after}
end
return {1, retry_after}
`

// Allow increments both counters before any password hash work. A Redis
// error is returned to the caller so production can fail closed with 503.
func (l *RedisAttemptLimiter) Allow(ctx context.Context, ip, subject string) (AttemptDecision, error) {
	if l == nil || l.Client == nil {
		return AttemptDecision{}, errors.New("auth attempt limiter is unavailable")
	}
	now := time.Now().UTC()
	if l.Now != nil {
		now = l.Now().UTC()
	}
	windowSeconds := int64(l.Window / time.Second)
	if windowSeconds < 1 {
		windowSeconds = 1
	}
	bucket := now.Unix() / windowSeconds
	remaining := windowSeconds - (now.Unix() % windowSeconds)
	if remaining < 1 {
		remaining = 1
	}
	keys := []string{
		fmt.Sprintf("%s:ip:%s:%d", l.Namespace, digest(ip), bucket),
		fmt.Sprintf("%s:subject:%s:%d", l.Namespace, digest(ip+"\x00"+subject), bucket),
	}
	result, err := l.Client.Eval(ctx, attemptScript, keys,
		remaining, l.IPMax, l.SubjectMax).Result()
	if err != nil {
		return AttemptDecision{}, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return AttemptDecision{}, fmt.Errorf("unexpected auth limiter response %T", result)
	}
	allowed, ok := redisInt64(values[0])
	if !ok {
		return AttemptDecision{}, fmt.Errorf("invalid auth limiter decision %v", values[0])
	}
	retry, ok := redisInt64(values[1])
	if !ok {
		return AttemptDecision{}, fmt.Errorf("invalid auth limiter retry %v", values[1])
	}
	return AttemptDecision{Allowed: allowed == 1, RetryAfter: time.Duration(retry) * time.Second}, nil
}

func redisInt64(value any) (int64, bool) {
	switch value := value.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// ClientIP returns the address used for the IP budget. Forwarded addresses
// are trusted only when the immediate peer and each proxy hop are inside one
// of the explicitly configured CIDRs. A direct caller cannot forge XFF.
func ClientIP(r *http.Request, trustedProxyCIDRs []string) string {
	if r == nil {
		return "unknown"
	}
	remote := parseRequestIP(r.RemoteAddr)
	if !remote.IsValid() {
		return "unknown"
	}
	prefixes := parseTrustedPrefixes(trustedProxyCIDRs)
	if !containsPrefix(prefixes, remote) {
		return remote.String()
	}
	current := remote
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if !containsPrefix(prefixes, current) {
			break
		}
		hop := parseRequestIP(strings.TrimSpace(parts[i]))
		if !hop.IsValid() {
			break
		}
		current = hop
	}
	return current.String()
}

func parseTrustedPrefixes(raw []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}

func containsPrefix(prefixes []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseRequestIP(raw string) netip.Addr {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]"), "[")
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}
