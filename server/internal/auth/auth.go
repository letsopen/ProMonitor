package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Store 是鉴权所需的最小数据接口（由 *store.Store 满足）
type Store interface {
	EnsureAdmin(ctx context.Context, username, hash string) error
	GetAdminHash(ctx context.Context, username string) (string, error)
	SetAdminPassword(ctx context.Context, username, hash string) error
}

const sessionCookie = "pm_session"

// Auth 负责单管理员登录、会话签名与中间件
type Auth struct {
	store  Store
	user   string
	secret string
}

func New(s Store, user, secret string) *Auth {
	return &Auth{store: s, user: user, secret: secret}
}

// Seed 首次启动时写入种子管理员（若已存在则跳过）
func (a *Auth) Seed(password string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.store.EnsureAdmin(context.Background(), a.username(), string(h))
}

func (a *Auth) username() string { return a.user }

// Login 校验凭据并返回签名会话 token
func (a *Auth) Login(username, password string) (string, error) {
	if username != a.user {
		return "", errors.New("unauthorized")
	}
	h, err := a.store.GetAdminHash(context.Background(), username)
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte(password)) != nil {
		return "", errors.New("unauthorized")
	}
	return a.sign(username), nil
}

// ChangePassword 校验旧密码后更新
func (a *Auth) ChangePassword(oldP, newP string) error {
	h, err := a.store.GetAdminHash(context.Background(), a.user)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte(oldP)) != nil {
		return errors.New("old password incorrect")
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(newP), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.store.SetAdminPassword(context.Background(), a.user, string(nh))
}

func (a *Auth) sign(username string) string {
	exp := time.Now().Add(7 * 24 * time.Hour).Unix()
	body := username + ":" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(a.secret))
	mac.Write([]byte(body))
	return body + ":" + hex.EncodeToString(mac.Sum(nil))
}

func (a *Auth) verify(token string) (string, bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(a.secret))
	mac.Write([]byte(parts[0] + ":" + parts[1]))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(parts[2])) {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	if parts[0] != a.user {
		return "", false
	}
	return parts[0], true
}

// RequireAdmin 是服务端鉴权中间件（真拦截点）
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, ok := a.verify(c.Value); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) WriteCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
}

func (a *Auth) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
