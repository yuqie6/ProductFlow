package httpx

import (
	"crypto/sha256"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/sessions"
)

// SessionCookieName 是用户会话 cookie 名，HTTP 合同固定为 "session"。
// 用户身份由会话里的 auth_session_id 引用数据库会话；不要改成 token / jwt。
const SessionCookieName = "session"
const sessionContextKey = "productflow.session"

// SessionConfig 配置 gorilla CookieStore：Secret 做 SHA-256 密钥，Secure 控制 cookie Secure 位。
type SessionConfig struct {
	Secret string // 做 SHA-256 后当 cookie 密钥，来自 SESSION_SECRET
	Secure bool   // cookie Secure 位，来自 SESSION_COOKIE_SECURE
}

// NewCookieStore 用 SHA-256(Secret) 做 cookie 密钥；MaxAge 14 天，HttpOnly，SameSite=Lax。
func NewCookieStore(cfg SessionConfig) *sessions.CookieStore {
	sum := sha256.Sum256([]byte(cfg.Secret))
	store := sessions.NewCookieStore(sum[:])
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   14 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cfg.Secure,
	}
	return store
}

// Session 从 cookie 加载会话并放入 Gin context；解码失败时新建空会话。
func Session(store *sessions.CookieStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, err := store.Get(c.Request, SessionCookieName)
		if err != nil {
			sess, _ = store.New(c.Request, SessionCookieName)
		}
		c.Set(sessionContextKey, sess)
		c.Next()
	}
}

// CurrentSession 返回中间件挂上的会话；未挂 Session 时为 nil。
func CurrentSession(c *gin.Context) *sessions.Session {
	value, ok := c.Get(sessionContextKey)
	if !ok {
		return nil
	}
	sess, _ := value.(*sessions.Session)
	return sess
}

// SessionBool 读取会话里的 bool；无会话、缺键或类型不对时返回 false。
func SessionBool(c *gin.Context, key string) bool {
	sess := CurrentSession(c)
	if sess == nil {
		return false
	}
	raw, ok := sess.Values[key]
	if !ok {
		return false
	}
	flag, _ := raw.(bool)
	return flag
}

// SessionString 读取会话里的 string；无会话、缺键或类型不对时返回空串。
func SessionString(c *gin.Context, key string) string {
	sess := CurrentSession(c)
	if sess == nil {
		return ""
	}
	raw, ok := sess.Values[key]
	if !ok {
		return ""
	}
	value, _ := raw.(string)
	return value
}

// SetSessionValue 写入单个键并 Save。无会话时静默成功（返回 nil）。
func SetSessionValue(c *gin.Context, key string, value any) error {
	sess := CurrentSession(c)
	if sess == nil {
		return nil
	}
	if sess.Options.MaxAge < 0 {
		sess.Options.MaxAge = 14 * 24 * 60 * 60
	}
	sess.Values[key] = value
	return sess.Save(c.Request, c.Writer)
}

// ReplaceSession 用 values 整表替换会话并 Save。无会话时静默成功（返回 nil）。
func ReplaceSession(c *gin.Context, values map[any]any) error {
	sess := CurrentSession(c)
	if sess == nil {
		return nil
	}
	sess.Values = values
	sess.Options.MaxAge = 14 * 24 * 60 * 60
	return sess.Save(c.Request, c.Writer)
}

// ClearSession 清空值并把 MaxAge 设为 -1，同时再 SetCookie 过期。无会话时静默成功。
//
// sess.Save 失败仍会 SetCookie 过期并返回该 error。
func ClearSession(c *gin.Context) error {
	sess := CurrentSession(c)
	if sess == nil {
		return nil
	}
	sess.Values = map[any]any{}
	sess.Options.MaxAge = -1
	err := sess.Save(c.Request, c.Writer)
	c.SetCookie(SessionCookieName, "", -1, "/", "", sess.Options.Secure, true)
	return err
}
