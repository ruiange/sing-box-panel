package panel

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	cookieName      = "sing_box_panel_session"
	sessionLifetime = 12 * time.Hour
)

type authenticator struct {
	mu              sync.Mutex
	password        [sha256.Size]byte
	sessions        map[[sha256.Size]byte]time.Time
	attempts        map[string][]time.Time
	secure          bool
	trustLocalProxy bool
}

func newAuthenticator(password string, secure, trustLocalProxy bool) *authenticator {
	return &authenticator{
		password: sha256.Sum256([]byte(password)), secure: secure,
		sessions: make(map[[sha256.Size]byte]time.Time),
		attempts: make(map[string][]time.Time), trustLocalProxy: trustLocalProxy,
	}
}

func (a *authenticator) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &request) {
		return
	}
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	key := a.loginSource(r)
	cutoff := now.Add(-time.Minute)
	for source, attempts := range a.attempts {
		if !attempts[len(attempts)-1].After(cutoff) {
			delete(a.attempts, source)
		}
	}
	if len(a.attempts) >= 2048 && a.attempts[key] == nil {
		key = "overflow"
	}
	attempts := a.attempts[key]
	for len(attempts) > 0 && !attempts[0].After(cutoff) {
		attempts = attempts[1:]
	}
	if len(attempts) >= 10 {
		w.Header().Set("Retry-After", "60")
		fail(w, http.StatusTooManyRequests, "登录尝试过多，请稍后再试")
		return
	}
	provided := sha256.Sum256([]byte(request.Password))
	if subtle.ConstantTimeCompare(a.password[:], provided[:]) != 1 {
		a.attempts[key] = append(attempts, now)
		fail(w, http.StatusUnauthorized, "管理员密码不正确")
		return
	}
	for token, expires := range a.sessions {
		if !expires.After(now) {
			delete(a.sessions, token)
		}
	}
	if current, err := r.Cookie(cookieName); err == nil {
		delete(a.sessions, sha256.Sum256([]byte(current.Value)))
	}
	if len(a.sessions) >= 256 {
		fail(w, http.StatusServiceUnavailable, "管理员会话过多，请退出其他会话后再试")
		return
	}
	token, err := randomToken()
	if err != nil {
		fail(w, http.StatusInternalServerError, "无法创建登录会话")
		return
	}
	delete(a.attempts, key)
	a.sessions[sha256.Sum256([]byte(token))] = now.Add(sessionLifetime)
	a.setCookie(w, r, token, int(sessionLifetime.Seconds()))
	respond(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *authenticator) loginSource(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if a.trustLocalProxy && isLoopback(host) {
		if forwarded, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil && forwarded.Zone() == "" {
			return forwarded.Unmap().String()
		}
	}
	return host
}

func (a *authenticator) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, sha256.Sum256([]byte(cookie.Value)))
		a.mu.Unlock()
	}
	a.setCookie(w, r, "", -1)
	respond(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (a *authenticator) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: value, Path: "/", MaxAge: maxAge,
		Secure: a.secure || r.TLS != nil, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func (a *authenticator) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) != 43 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[sha256.Sum256([]byte(cookie.Value))].After(time.Now())
}

func (a *authenticator) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			fail(w, http.StatusUnauthorized, "请先登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomToken() (string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(secret[:]), nil
}
