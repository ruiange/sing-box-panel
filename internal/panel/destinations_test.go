package panel

import (
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruiange/sing-box-panel/internal/store"
)

func TestDeviceDestinationsRequiresSessionAndScopesHistory(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	const phone = "cefa1281-1f0b-410a-8a5d-72816727f2e0"
	const laptop = "6398fd22-1279-4b13-bf7d-719d24be801c"
	const empty = "90bac3e4-7e70-4b0a-9b44-0be57d0d4261"
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{phone, laptop, empty} {
		if err := db.CreateDevice(ctx, store.Device{
			ID: id, Name: id, Enabled: id != phone, UUID: id, SubscriptionToken: id, CreatedAt: start,
		}); err != nil {
			t.Fatal(err)
		}
	}
	seenAt := start.Add(time.Minute)
	const latestAddress = "news.example:443"
	if err := db.RecordDestinations(ctx, []store.DestinationEvent{
		{DeviceID: phone, ConnectionID: "phone-docs", Address: "docs.example:443", SeenAt: start},
		{DeviceID: phone, ConnectionID: "phone-news", Address: latestAddress, SeenAt: seenAt},
		{DeviceID: laptop, ConnectionID: "laptop-first", Address: "laptop.example:443", SeenAt: seenAt},
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, nil, Config{Password: "test-password", Zone: time.UTC}, slog.Default())
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	client := server.Client()
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	historyURL := server.URL + "/api/devices/" + phone + "/destinations"
	requestPanel(t, ctx, client, http.MethodGet, historyURL, "", http.StatusUnauthorized)
	requestPanel(t, ctx, client, http.MethodPost, server.URL+"/api/auth/login", `{"password":"test-password"}`, http.StatusOK)
	requestPanel(t, ctx, client, http.MethodGet, historyURL+"?limit=101", "", http.StatusBadRequest)
	requestPanel(t, ctx, client, http.MethodGet, historyURL+"?before=invalid", "", http.StatusBadRequest)
	requestPanel(t, ctx, client, http.MethodGet, server.URL+"/api/devices/unknown/destinations", "", http.StatusNotFound)

	data := panelData[store.DestinationPage](t, requestPanel(t, ctx, client, http.MethodGet, historyURL, "", http.StatusOK))
	if len(data.Items) != 2 || data.Items[0].ID != "phone-news" || data.Items[0].Address != latestAddress || !data.Items[0].LastSeenAt.Equal(seenAt) {
		t.Fatalf("unexpected device history: %+v", data)
	}
	limited := panelData[store.DestinationPage](t, requestPanel(t, ctx, client, http.MethodGet, historyURL+"?limit=1", "", http.StatusOK))
	if len(limited.Items) != 1 || limited.Items[0].Address != latestAddress || limited.NextCursor == "" {
		t.Fatalf("unexpected limited device history: %+v", limited)
	}
	earlier := panelData[store.DestinationPage](t, requestPanel(t, ctx, client, http.MethodGet, historyURL+"?limit=1&before="+url.QueryEscape(limited.NextCursor), "", http.StatusOK))
	if len(earlier.Items) != 1 || earlier.Items[0].ID != "phone-docs" || earlier.NextCursor != "" {
		t.Fatalf("unexpected earlier device history: %+v", earlier)
	}
	emptyData := panelData[store.DestinationPage](t, requestPanel(t, ctx, client, http.MethodGet, server.URL+"/api/devices/"+empty+"/destinations", "", http.StatusOK))
	if emptyData.Items == nil || len(emptyData.Items) != 0 {
		t.Fatalf("empty history should return []: %+v", emptyData)
	}
}
