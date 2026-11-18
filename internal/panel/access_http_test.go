package panel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAccessSettingsRequireSessionAndProtectPEMUpload(t *testing.T) {
	const fallback = "http://198.51.100.4:9443"
	const endpoint = "/api/settings/access"
	const manual = `{"mode":"manual","domain":"panel.example.com","certificate":"not a certificate","privateKey":"never sent over HTTP"}`
	s := NewService(nil, nil, Config{PublicURL: fallback, Password: "test-password"}, nil)
	s.access = &domainAccess{}
	handler := s.Handler()

	call := func(method, path, body string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
		t.Helper()
		// httptest.NewRequest uses a remote address outside loopback.
		req := httptest.NewRequest(method, fallback+path, strings.NewReader(body))
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", fallback)
		}
		if method == http.MethodPut {
			// The fallback public HTTP listener must not trust client headers.
			req.Header.Set("X-Forwarded-Proto", "https")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var envelope struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Message == "" {
			t.Fatalf("%s %s didn't return a JSON envelope: %v", method, path, err)
		}
		expectedCode := status
		if status < 400 {
			expectedCode = 0
		}
		if w.Code != status || envelope.Code != expectedCode {
			t.Fatalf("%s %s: expected HTTP %d (code %d), got HTTP %d (code %d): %s", method, path, status, expectedCode, w.Code, envelope.Code, envelope.Message)
		}
		return w
	}

	call(http.MethodGet, endpoint, "", nil, http.StatusUnauthorized)
	call(http.MethodPut, endpoint, manual, nil, http.StatusUnauthorized)
	login := call(http.MethodPost, "/api/auth/login", `{"password":"test-password"}`, nil, http.StatusOK).Result()
	defer login.Body.Close()
	cookies := login.Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("expected the default HTTP login cookie, got %d cookies", len(cookies))
	}
	cookie := cookies[0]
	call(http.MethodPut, endpoint, manual, cookie, http.StatusForbidden)
	settings := call(http.MethodGet, endpoint, "", cookie, http.StatusOK)
	var state struct {
		Data struct {
			ManualUploadAllowed *bool `json:"manualUploadAllowed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(settings.Body.Bytes(), &state); err != nil || state.Data.ManualUploadAllowed == nil || *state.Data.ManualUploadAllowed {
		t.Fatalf("plain HTTP settings incorrectly allow manual PEM upload: %v", err)
	}
}

func TestDirectHTTPAcceptsSameOriginLoginAndLiveTrafficOnAlternateIP(t *testing.T) {
	const ipv4 = "http://1.1.1.1:19443"
	const ipv6 = "http://[2606:4700:4700::1111]:19443"
	const otherOrigin = "http://9.9.9.9:19443"

	for _, tc := range []struct {
		name, configured, alternate string
	}{
		{"IPv4", ipv6, ipv4},
		{"IPv6", ipv4, ipv6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(nil, nil, Config{
				PublicURL:  tc.configured,
				Listen:     ":19443",
				Password:   "test-password",
				DirectHTTP: true,
			}, nil)
			server := httptest.NewServer(s.Handler())
			t.Cleanup(server.Close)
			t.Cleanup(s.CloseLiveTraffic)
			// Route the public test addresses to a loopback listener while keeping the original Host.
			dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			transport := &http.Transport{DialContext: dial}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			login := func(target, origin string, status int) []*http.Cookie {
				t.Helper()
				req, err := http.NewRequest(http.MethodPost, target+"/api/auth/login", strings.NewReader(`{"password":"test-password"}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", origin)
				reply, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer reply.Body.Close()
				if reply.StatusCode != status {
					t.Fatalf("login to %s from %s returned HTTP %d; want %d", target, origin, reply.StatusCode, status)
				}
				return reply.Cookies()
			}

			login(tc.alternate, otherOrigin, http.StatusForbidden)
			for _, address := range []string{"http://panel.example.com:19443", "http://10.0.0.1:19443", "http://1.1.1.1:19444"} {
				login(address, address, http.StatusForbidden)
			}
			cookies := login(tc.alternate, tc.alternate, http.StatusOK)
			if len(cookies) != 1 || cookies[0].Secure {
				t.Fatal("alternate IP login did not issue an HTTP session cookie")
			}

			address := "ws" + strings.TrimPrefix(tc.alternate, "http") + "/api/traffic/live"
			dialer := &websocket.Dialer{NetDialContext: dial, HandshakeTimeout: 3 * time.Second}
			headers := http.Header{"Cookie": {cookies[0].Name + "=" + cookies[0].Value}, "Origin": {otherOrigin}}
			conn, reply, err := dialer.Dial(address, headers)
			if reply != nil {
				reply.Body.Close()
			}
			if err == nil {
				conn.Close()
				t.Fatal("cross-origin WebSocket connected to the alternate IP")
			}
			if reply == nil || reply.StatusCode != http.StatusForbidden {
				t.Fatalf("unexpected cross-origin WebSocket rejection: %v", err)
			}
			headers.Set("Origin", tc.alternate)
			conn, _, err = dialer.Dial(address, headers)
			if err != nil {
				t.Fatalf("alternate IP live traffic failed to connect: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
		})
	}
}

func TestAccessDomainAcceptsSameOriginLoginAndLiveTraffic(t *testing.T) {
	const domain = "panel.example.com"
	const origin = "https://" + domain
	const fallback = "http://198.51.100.4:9443"
	now := time.Now()
	certPEM, keyPEM := manualTestPair(t, domain, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth)
	cert, err := parseManualCertificate(certPEM, keyPEM, domain)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(nil, nil, Config{PublicURL: fallback, Password: "test-password"}, nil)
	s.access = &domainAccess{}
	s.access.active.Store(&domainSite{mode: "manual", domain: domain, certificate: &cert})
	server := httptest.NewUnstartedServer(s.Handler())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(s.CloseLiveTraffic)
	roots := x509.NewCertPool()
	roots.AddCert(cert.Leaf)
	tlsConfig := &tls.Config{RootCAs: roots}
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	transport := &http.Transport{DialContext: dial, TLSClientConfig: tlsConfig}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	login := func(requestOrigin string, status int) []*http.Cookie {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, origin+"/api/auth/login", strings.NewReader(`{"password":"test-password"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", requestOrigin)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("login from %s returned HTTP %d; want %d", requestOrigin, response.StatusCode, status)
		}
		return response.Cookies()
	}
	login(fallback, http.StatusForbidden)
	cookies := login(origin, http.StatusOK)
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("domain login did not issue a secure session cookie")
	}

	dialer := &websocket.Dialer{NetDialContext: dial, TLSClientConfig: tlsConfig, HandshakeTimeout: 3 * time.Second}
	headers := http.Header{"Cookie": {cookies[0].Name + "=" + cookies[0].Value}, "Origin": {fallback}}
	address := "wss://" + domain + "/api/traffic/live"
	conn, reply, err := dialer.Dial(address, headers)
	if reply != nil {
		reply.Body.Close()
	}
	if err == nil {
		conn.Close()
		t.Fatal("fallback origin opened a WebSocket on the HTTPS domain")
	}
	if reply == nil || reply.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected cross-origin WebSocket rejection: %v", err)
	}
	headers.Set("Origin", origin)
	conn, _, err = dialer.Dial(address, headers)
	if err != nil {
		t.Fatalf("domain live traffic failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
}
