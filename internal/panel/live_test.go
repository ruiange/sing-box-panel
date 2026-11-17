package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

const (
	liveTestDevice = "cefa1281-1f0b-410a-8a5d-72816727f2e0"
	liveTestIdle   = "6398fd22-1279-4b13-bf7d-719d24be801c"
)

func TestLiveRatesRespectCounterResetsAndWindow(t *testing.T) {
	service := NewService(nil, nil, Config{}, nil)
	now := time.Now()
	start := now.Add(-time.Hour)
	ids := []string{liveTestDevice, liveTestIdle}
	service.live.record(liveTestSnapshot(now.Add(-2*time.Second), start, 20, 70), ids)
	service.live.record(liveTestSnapshot(now, start, 280, 90), ids)
	frame, _ := service.live.frame(liveTestDevice, now)
	if frame.At == nil || frame.Devices[liveTestDevice] != (speed{Upload: 130, Download: 10}) || len(frame.History) != 1 {
		t.Fatalf("unexpected speed frame: %+v", frame)
	}
	if rate, exists := frame.Devices[liveTestIdle]; !exists || rate != (speed{}) {
		t.Fatalf("idle device should have a valid zero rate: %+v", frame.Devices)
	}
	if active := frame.Presence[liveTestDevice]; active.State != "active" || active.LastActiveAt == nil || !active.LastActiveAt.Equal(now) {
		t.Fatalf("traffic did not activate device: %+v", active)
	}
	if idle := frame.Presence[liveTestIdle]; idle.State != "idle" || idle.LastActiveAt != nil {
		t.Fatalf("idle device with no traffic should have no activity timestamp: %+v", idle)
	}
	service.live.record(liveTestSnapshot(now.Add(2*time.Second), start, 10, 20), ids)
	frame, _ = service.live.frame(liveTestDevice, now.Add(2*time.Second))
	if _, exists := frame.Devices[liveTestDevice]; exists || len(frame.History) != 1 {
		t.Fatalf("counter went backwards but produced a point: %+v", frame)
	}
	if unknown := frame.Presence[liveTestDevice]; unknown.State != "unknown" || unknown.LastActiveAt == nil || !unknown.LastActiveAt.Equal(now) {
		t.Fatalf("counter reset should leave presence unknown and preserve last activity: %+v", unknown)
	}
	service.live.invalidate()
	frame, _ = service.live.frame(liveTestDevice, now.Add(3*time.Second))
	if frame.At != nil || len(frame.History) != 1 {
		t.Fatalf("outage should drop the current rate and retain recent history: %+v", frame)
	}
	frame, _ = service.live.frame(liveTestDevice, now.Add(61*time.Second))
	if len(frame.History) != 0 {
		t.Fatalf("stale history was returned: %+v", frame)
	}
}

func TestDevicePresenceExpiresAfterOneMinute(t *testing.T) {
	service := NewService(nil, nil, Config{}, nil)
	now := time.Now()
	start := now.Add(-time.Hour)
	active := []string{liveTestDevice}
	service.live.record(liveTestSnapshot(now.Add(-2*time.Second), start, 0, 0), active)
	service.live.record(liveTestSnapshot(now, start, 0, 10), active)
	for elapsed := liveInterval; elapsed <= liveWindow+liveInterval; elapsed += liveInterval {
		at := now.Add(elapsed)
		service.live.record(liveTestSnapshot(at, start, 0, 10), active)
		frame, _ := service.live.frame("", at)
		presence := frame.Presence[liveTestDevice]
		want := "active"
		if elapsed > liveWindow {
			want = "idle"
		}
		if presence.State != want || presence.LastActiveAt == nil || !presence.LastActiveAt.Equal(now) {
			t.Fatalf("presence after %v: got %+v, want %s with original timestamp", elapsed, presence, want)
		}
	}
	frame, _ := service.live.frame("", now.Add(liveWindow+liveInterval+liveMaxGap+time.Second))
	if frame.Presence[liveTestDevice].State != "unknown" {
		t.Fatalf("stale collection must report unknown: %+v", frame)
	}
}

func TestDevicePresenceRestoredAfterPanelRestart(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now().UTC()
	if err := db.CreateDevice(ctx, store.Device{
		ID: liveTestDevice, Name: "test device", Enabled: true, UUID: liveTestDevice,
		SubscriptionToken: "test-token", CreatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	start := now.Add(-time.Hour)
	managed := &liveTestNode{snapshot: liveTestSnapshot(now.Add(-2*time.Second), start, 0, 0)}
	cfg := Config{PollInterval: 15 * time.Second, Zone: time.UTC}
	service := NewService(db, managed, cfg, nil)
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	managed.snapshot = liveTestSnapshot(now, start, 100, 0)
	service.mu.Lock()
	err = service.sampleLocked(ctx, false)
	service.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	managed.snapshot = liveTestSnapshot(now.Add(2*time.Second), start, 100, 0)
	if err := service.CollectBeforeShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(db, managed, cfg, nil)
	if err := restarted.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	frame, _ := restarted.live.frame("", now.Add(2*time.Second))
	if presence := frame.Presence[liveTestDevice]; presence.State != "unknown" || presence.LastActiveAt == nil || !presence.LastActiveAt.Equal(now) {
		t.Fatalf("restart should recover last activity while awaiting a valid sample: %+v", presence)
	}
}

type liveTestNode struct {
	snapshot node.Snapshot
}

func (*liveTestNode) Apply(context.Context, []node.Device) (bool, error) { return false, nil }
func (n *liveTestNode) Collect(context.Context) (node.Snapshot, error)   { return n.snapshot, nil }

func TestLiveWebSocketSubscriptionAndSession(t *testing.T) {
	const origin = "http://panel.test"
	service := NewService(nil, nil, Config{PublicURL: origin, Password: "test-password"}, nil)
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	address := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/traffic/live"
	if conn, httpResponse, err := websocket.DefaultDialer.Dial(address, http.Header{"Origin": {origin}}); err == nil {
		conn.Close()
		t.Fatal("anonymous client upgraded")
	} else if httpResponse == nil || httpResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous handshake response: %v", err)
	} else {
		httpResponse.Body.Close()
	}

	login, err := server.Client().Post(server.URL+"/api/auth/login", "application/json", strings.NewReader(`{"password":"test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if login.StatusCode != http.StatusOK || len(login.Cookies()) != 1 {
		t.Fatalf("login status: %d", login.StatusCode)
	}
	cookie := login.Cookies()[0]
	headers := http.Header{"Origin": {"http://another.test"}, "Cookie": {cookie.Name + "=" + cookie.Value}}
	if conn, httpResponse, err := websocket.DefaultDialer.Dial(address, headers); err == nil {
		conn.Close()
		t.Fatal("cross-origin client upgraded")
	} else if httpResponse == nil || httpResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin handshake response: %v", err)
	} else {
		httpResponse.Body.Close()
	}

	now := time.Now()
	start := now.Add(-time.Hour)
	active := []string{liveTestDevice}
	service.live.record(liveTestSnapshot(now.Add(-2*time.Second), start, 0, 0), active)
	service.live.record(liveTestSnapshot(now, start, 40, 20), active)
	headers.Set("Origin", origin)
	conn, _, err := websocket.DefaultDialer.Dial(address, headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Code int       `json:"code"`
		Data liveFrame `json:"data"`
	}
	if err := conn.ReadJSON(&result); err != nil || result.Code != 0 || result.Data.At == nil || result.Data.Devices[liveTestDevice].Upload != 20 || result.Data.Presence[liveTestDevice].State != "active" {
		t.Fatalf("initial frame: %+v, %v", result, err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "subscribe", "deviceId": liveTestDevice}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&result); err != nil || result.Data.DeviceID != liveTestDevice || len(result.Data.History) != 1 || result.Data.History[0].Download != 10 {
		t.Fatalf("selected device frame: %+v, %v", result, err)
	}
	logout, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	logout.AddCookie(cookie)
	reply, err := server.Client().Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	reply.Body.Close()
	service.live.invalidate()
	if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, liveSessionExpired) {
		t.Fatalf("logged-out socket did not expire: %v", err)
	}
}

func liveTestSnapshot(at, startedAt time.Time, upload, download int64) node.Snapshot {
	return node.Snapshot{
		ReadAt: at, StartedAt: startedAt,
		Counters: map[string]node.Counters{liveTestDevice: {Upload: upload, Download: download}},
	}
}
