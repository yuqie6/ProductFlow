package httpx

import (
	"crypto/sha256"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/sessions"
)

const SessionCookieName = "session"
const sessionContextKey = "productflow.session"

type SessionConfig struct {
	Secret string
	Secure bool
}

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

func CurrentSession(c *gin.Context) *sessions.Session {
	value, ok := c.Get(sessionContextKey)
	if !ok {
		return nil
	}
	sess, _ := value.(*sessions.Session)
	return sess
}

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

func ReplaceSession(c *gin.Context, values map[any]any) error {
	sess := CurrentSession(c)
	if sess == nil {
		return nil
	}
	sess.Values = values
	sess.Options.MaxAge = 14 * 24 * 60 * 60
	return sess.Save(c.Request, c.Writer)
}

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
