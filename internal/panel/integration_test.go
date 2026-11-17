package panel

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

// TestDeviceTrafficThroughReality checks the live APIs and verifies that actual
// VLESS sessions increment separate device counters across a managed reload.
// It uses an opt-in HTTPS target, never an unrelated service by default.
func TestDeviceTrafficThroughReality(t *testing.T) {
	binary, target := os.Getenv("TEST_SING_BOX_BINARY"), os.Getenv("TEST_REALITY_TARGET")
	if binary == "" || target == "" {
		t.Skip("set TEST_SING_BOX_BINARY and TEST_REALITY_TARGET to run the real proxy test")
	}
	targetURL, err := url.Parse(target)
	if err != nil || targetURL.Scheme != "https" || targetURL.Hostname() == "" || targetURL.Port() != "" {
		t.Fatal("TEST_REALITY_TARGET must be an HTTPS URL on port 443")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	corePort, statsPort := freePort(t), freePort(t)
	coreConfig, coreLog, corePID := filepath.Join(dir, "node.json"), filepath.Join(dir, "node.log"), filepath.Join(dir, "node.pid")
	start := filepath.Join(dir, "reload.sh")
	const script = `#!/bin/sh
set -eu
if [ -f "$3" ]; then
    kill -HUP "$(cat "$3")"
else
    "$1" run -c "$2" >"$4" 2>&1 </dev/null &
    echo "$!" >"$3"
fi
`
	if err := os.WriteFile(start, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if raw, err := os.ReadFile(corePID); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGTERM)
			}
		}
		if t.Failed() {
			if data, err := os.ReadFile(coreLog); err == nil {
				t.Logf("core log: %s", data)
			}
		}
	})
	managed, err := node.New(node.Config{
		BinaryPath: binary, ConfigPath: coreConfig, StatsAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(statsPort)),
		Listen: "127.0.0.1", ListenPort: corePort, ServerName: targetURL.Hostname(),
		HandshakeServer: targetURL.Hostname(), HandshakePort: 443,
		RealityPrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), RealityShortID: "35e0cd09e8f42b80",
		ReloadCommand: []string{start, binary, coreConfig, corePID, coreLog},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()
	db, err := store.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backendURL := "http://" + listener.Addr().String()
	const password = "integration-password"
	service := NewService(db, managed, Config{
		PublicURL: backendURL, Zone: time.UTC, Password: password, WebDir: dir,
		Endpoint: Endpoint{Host: "127.0.0.1", Port: corePort, ServerName: targetURL.Hostname(),
			PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: "35e0cd09e8f42b80"},
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewUnstartedServer(service.Handler())
	_ = httpServer.Listener.Close()
	httpServer.Listener = listener
	httpServer.Start()
	defer httpServer.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	fetch := func(method, path, body string, status int) []byte {
		t.Helper()
		return requestPanel(t, ctx, client, method, backendURL+path, body, status)
	}
	fetch(http.MethodGet, "/api/devices", "", http.StatusUnauthorized)
	fetch(http.MethodPost, "/api/auth/login", `{"password":"`+password+`"}`, http.StatusOK)
	fetch(http.MethodGet, "/docs", "", http.StatusOK)
	fetch(http.MethodGet, "/openapi.yaml", "", http.StatusOK)
	phone := panelData[store.Device](t, fetch(http.MethodPost, "/api/devices", `{"name":"手机"}`, http.StatusCreated))
	pc := panelData[store.Device](t, fetch(http.MethodPost, "/api/devices", `{"name":"电脑"}`, http.StatusCreated))
	if phone.ID == pc.ID || phone.Name == pc.Name {
		t.Fatal("devices were not created independently")
	}
	phoneURL := panelData[map[string]string](t, fetch(http.MethodGet, "/api/devices/"+phone.ID+"/subscription", "", http.StatusOK))["url"]
	pcURL := panelData[map[string]string](t, fetch(http.MethodGet, "/api/devices/"+pc.ID+"/subscription", "", http.StatusOK))["url"]
	if phoneURL == pcURL {
		t.Fatal("devices share a subscription token")
	}
	qrPath := "/api/devices/" + phone.ID + "/subscription-qr?format=shadowrocket"
	if image := fetch(http.MethodGet, qrPath, "", http.StatusOK); !bytes.HasPrefix(image, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("subscription QR did not return an image")
	}
	fetch(http.MethodGet, "/api/devices/"+pc.ID+"/subscription-qr?format=invalid", "", http.StatusBadRequest)

	phoneConfig := requestPanel(t, ctx, client, http.MethodGet, phoneURL+"/sing-box", "", http.StatusOK)
	pcConfig := requestPanel(t, ctx, client, http.MethodGet, pcURL+"/sing-box", "", http.StatusOK)
	requestPanel(t, ctx, client, http.MethodGet, phoneURL+"/clash", "", http.StatusOK)
	requestPanel(t, ctx, client, http.MethodGet, pcURL+"/v2ray", "", http.StatusOK)

	proxyRequest(t, ctx, binary, dir, "phone", target, phoneConfig, true)
	if err := service.CollectBeforeShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	first := panelData[store.Traffic](t, fetch(http.MethodGet, "/api/traffic?days=7", "", http.StatusOK))
	phoneBytes, pcBytes := deviceUsage(t, first, phone.ID), deviceUsage(t, first, pc.ID)
	if phoneBytes.Download == 0 || phoneBytes.Upload == 0 || pcBytes != (store.Usage{}) {
		t.Fatalf("first request attributed incorrectly: phone=%+v pc=%+v", phoneBytes, pcBytes)
	}
	proxyRequest(t, ctx, binary, dir, "pc", target, pcConfig, true)
	if err := service.CollectBeforeShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	second := panelData[store.Traffic](t, fetch(http.MethodGet, "/api/traffic?days=30", "", http.StatusOK))
	pcBytes = deviceUsage(t, second, pc.ID)
	if pcBytes.Upload == 0 || pcBytes.Download == 0 || deviceUsage(t, second, phone.ID) != phoneBytes {
		t.Fatal("second device transferred bytes under another device")
	}
	phoneReport := panelData[store.Traffic](t, fetch(http.MethodGet, "/api/traffic?days=30&deviceId="+phone.ID, "", http.StatusOK))
	if phoneReport.Totals.All != phoneBytes || len(phoneReport.Devices) != 1 || phoneReport.Devices[0].ID != phone.ID || phoneReport.Daily[len(phoneReport.Daily)-1].Usage != phoneBytes {
		t.Fatalf("single-device traffic was not isolated: %+v", phoneReport)
	}
	fetch(http.MethodGet, "/api/traffic?days=7&deviceId=missing", "", http.StatusNotFound)

	rotated := panelData[map[string]string](t, fetch(http.MethodPost, "/api/devices/"+pc.ID+"/rotate-subscription", "", http.StatusOK))["url"]
	requestPanel(t, ctx, client, http.MethodGet, pcURL+"/sing-box", "", http.StatusNotFound)
	requestPanel(t, ctx, client, http.MethodGet, rotated+"/sing-box", "", http.StatusOK)
	fetch(http.MethodPatch, "/api/devices/"+phone.ID, `{"enabled":false}`, http.StatusOK)
	requestPanel(t, ctx, client, http.MethodGet, phoneURL+"/sing-box", "", http.StatusNotFound)
	proxyRequest(t, ctx, binary, dir, "phone-disabled", target, phoneConfig, false)
	proxyRequest(t, ctx, binary, dir, "pc-after-reload", target, pcConfig, true)
	if err := service.CollectBeforeShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	third := panelData[store.Traffic](t, fetch(http.MethodGet, "/api/traffic?days=7", "", http.StatusOK))
	if latest := deviceUsage(t, third, pc.ID); latest.Download <= pcBytes.Download || latest.Upload <= pcBytes.Upload || deviceUsage(t, third, phone.ID) != phoneBytes {
		t.Fatalf("history after reload or device disable is wrong: %+v", third.Devices)
	}
	fetch(http.MethodPost, "/api/auth/logout", "", http.StatusOK)
	fetch(http.MethodGet, "/api/traffic", "", http.StatusUnauthorized)
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func panelData[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var envelope struct {
		Code int
		Data T
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Code != 0 {
		t.Fatalf("unexpected API response: %v / code %d", err, envelope.Code)
	}
	return envelope.Data
}

func requestPanel(t *testing.T, ctx context.Context, client *http.Client, method, target, body string, status int) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	result, err := client.Do(request)
	if err != nil {
		// URLs include subscription credentials, so do not print the URL.
		t.Fatalf("%s request failed: %T", method, err)
	}
	defer result.Body.Close()
	raw, err := io.ReadAll(result.Body)
	if err != nil || result.StatusCode != status {
		t.Fatalf("%s status: got %d, want %d (body read error %v)", method, result.StatusCode, status, err)
	}
	return raw
}

func deviceUsage(t *testing.T, report store.Traffic, id string) store.Usage {
	t.Helper()
	for _, device := range report.Devices {
		if device.ID == id {
			return device.All
		}
	}
	t.Fatal("device missing in usage report")
	return store.Usage{}
}

func proxyRequest(t *testing.T, ctx context.Context, binary, dir, label, target string, raw []byte, expectSuccess bool) {
	t.Helper()
	var config struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Route     map[string]any   `json:"route"`
		DNS       json.RawMessage  `json:"dns,omitempty"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || len(config.Inbounds) != 1 {
		t.Fatalf("invalid sing-box subscription: %v", err)
	}
	local := freePort(t)
	config.Inbounds[0]["listen_port"] = local
	file := filepath.Join(dir, label+".json")
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "run", "-c", file)
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("%s client log: %s", label, logs.String())
		}
	}()
	address := fmt.Sprintf("127.0.0.1:%d", local)
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp4", address, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatal("client mixed listener did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: address})}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if response != nil {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if expectSuccess && (err != nil || response.StatusCode != http.StatusOK) {
		t.Fatalf("%s could not forward HTTPS: %v", label, err)
	}
	if !expectSuccess && err == nil {
		t.Fatalf("%s forwarded HTTPS despite device disable", label)
	}
}
