package httpx

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// BrowserStateConfig describes the exact origins allowed to send browser
// state-changing requests. Origins are scheme/host/port values from env; the
// request Host header is never used to build this set.
type BrowserStateConfig struct {
	AllowedOrigins []string
	InternalToken  string
}

// BrowserStateProtection validates the source of browser state changes. It
// leaves reads alone and lets internal bearer requests reach their dedicated
// token middleware before deciding authorization. An invalid bearer token is
// still rejected by that middleware; it does not authorize a browser route.
func BrowserStateProtection(cfg BrowserStateConfig) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, raw := range cfg.AllowedOrigins {
		if origin, ok := canonicalOrigin(raw); ok {
			allowed[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin, hasOrigin := requestOrigin(c.Request)
		if hasOrigin {
			if _, ok := allowed[origin]; ok {
				setCORSHeaders(c, origin)
			}
		}

		if c.Request.Method == http.MethodOptions {
			if !hasOrigin || !originAllowed(origin, allowed) {
				AbortDetail(c, http.StatusForbidden, "请求来源不受信任")
				return
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-ProductFlow-Merchant-Id")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		if browserStateMutation(c.Request) && !internalBearerRequest(c.Request, cfg.InternalToken) && (!hasOrigin || !originAllowed(origin, allowed)) {
			AbortDetail(c, http.StatusForbidden, "请求来源不受信任")
			return
		}
		c.Next()
	}
}

func browserStateMutation(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func internalBearerRequest(r *http.Request, expected string) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/internal/") {
		return false
	}
	if strings.TrimSpace(expected) == "" {
		return false
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	token = strings.TrimSpace(token)
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func originAllowed(origin string, allowed map[string]struct{}) bool {
	_, ok := allowed[origin]
	return ok
}

func setCORSHeaders(c *gin.Context, origin string) {
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Vary", "Origin")
}

func requestOrigin(r *http.Request) (string, bool) {
	if raw := r.Header.Values("Origin"); len(raw) > 0 {
		// Origin has precedence even when malformed or explicitly null.
		if len(raw) != 1 {
			return "", false
		}
		origin, valid := canonicalOrigin(strings.TrimSpace(raw[0]))
		return origin, valid
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		parsed, err := url.Parse(referer)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "", false
		}
		origin, ok := canonicalOrigin(parsed.Scheme + "://" + parsed.Host)
		return origin, ok
	}
	return "", false
}

func canonicalOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	// url.Parse accepts a host with whitespace or an invalid port; Hostname and
	// Port force the same scheme/host/port shape used by browser Origin.
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	if strings.ContainsAny(u.Host, " \t\r\n,") {
		return "", false
	}
	port := u.Port()
	if strings.Contains(u.Host, ":") && port == "" && !strings.Contains(u.Host, "]") {
		return "", false
	}
	if port != "" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		return strings.ToLower(u.Scheme) + "://" + host + ":" + port, true
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return strings.ToLower(u.Scheme) + "://" + host, true
}
