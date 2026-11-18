package panel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDomainHTTPSRestoresAndClosesIndependentlyOfHTTP(t *testing.T) {
	const domain = "panel.example.com"
	const fallback = "http://198.51.100.4:9443"
	const settingsPath = "/api/settings/access"
	now := time.Now()
	cert, key := manualTestPair(t, domain, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(nil, nil, Config{PublicURL: fallback, Password: "test-password"}, log)
	m := &domainAccess{ctx: t.Context(), dir: t.TempDir(), listenAddr: "127.0.0.1:0", log: log}
	service.access = m
	m.handler = service.Handler()

	if err := m.persist(accessConfig{Mode: "manual", Domain: domain, Certificate: string(cert), PrivateKey: string(key)}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(m.dir, "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("certificate settings permissions = %03o", info.Mode().Perm())
	}
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	address := m.listener.Addr().String()
	t.Cleanup(func() {
		if m.server != nil {
			_ = m.server.Close()
		}
	})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("cannot trust test certificate")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}

	request := func(method, route, body string, cookie *http.Cookie, expected int) *http.Response {
		t.Helper()
		r, err := http.NewRequest(method, "https://"+domain+route, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", "https://"+domain)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != expected {
			response.Body.Close()
			t.Fatalf("%s %s = %d, want %d", method, route, response.StatusCode, expected)
		}
		return response
	}
	login := request(http.MethodPost, "/api/auth/login", `{"password":"test-password"}`, nil, http.StatusOK)
	login.Body.Close()
	cookies := login.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("expected a secure session cookie")
	}
	cookie := cookies[0]
	checkStatus := func(mode string) {
		t.Helper()
		response := request(http.MethodGet, settingsPath, "", cookie, http.StatusOK)
		defer response.Body.Close()
		var data struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
			t.Fatal(err)
		}
		if string(data.Data["mode"]) != `"`+mode+`"` || data.Data["privateKey"] != nil || data.Data["certificate"] != nil {
			t.Fatal("unexpected settings status or certificate contents exposed")
		}
	}
	checkStatus("manual")
	failed := request(http.MethodPut, settingsPath, `{"mode":"manual","domain":"wrong.example.com","certificate":"bad","privateKey":"bad"}`, cookie, http.StatusBadRequest)
	failed.Body.Close()
	checkStatus("manual")
	disabled := request(http.MethodPut, settingsPath, `{"mode":"off"}`, cookie, http.StatusOK)
	disabled.Body.Close()
	if conn, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		conn.Close()
		t.Fatal("HTTPS port stayed open after disabling the domain")
	}

	w := httptest.NewRecorder()
	m.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fallback+"/api/auth/session", nil))
	if w.Code != http.StatusOK || service.publicURL() != fallback {
		t.Fatal("original HTTP endpoint no longer works")
	}
	if err := m.restore(); err != nil || m.active.Load() != nil {
		t.Fatalf("disabled domain did not stay off: %v", err)
	}
}
