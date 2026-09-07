package auth

import (
	"net/http"
	"sync"
	"time"
)

// loginLimiter 是进程内登录节流：按客户端 IP 计失败次数。
type loginLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	maxFail int
	hits    map[string][]time.Time
}

func newLoginLimiter(window time.Duration, maxFail int) *loginLimiter {
	return &loginLimiter{
		window:  window,
		maxFail: maxFail,
		hits:    map[string][]time.Time{},
	}
}

func (l *loginLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, ts := range l.hits[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	l.hits[key] = kept
	return len(kept) < l.maxFail
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.hits[key], now)
}

func clientKey(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
