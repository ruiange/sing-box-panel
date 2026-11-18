package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDestinationHistoryMigratesV4AndKeepsEachConnection(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"phone", "laptop"} {
		if err := s.CreateDevice(ctx, Device{
			ID: id, Name: id, Enabled: true, UUID: id, SubscriptionToken: id, CreatedAt: start,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordTraffic(ctx, start.Add(time.Minute), start, map[string]Counters{"phone": {Upload: 80}}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `
		DROP TABLE site_settings;
		DROP TABLE device_destinations;
		CREATE TABLE device_destinations (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			address TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			PRIMARY KEY (device_id, address)
		) STRICT;
		CREATE INDEX device_destinations_by_recency ON device_destinations(device_id, last_seen_at DESC, address);
		INSERT INTO device_destinations(device_id, address, last_seen_at) VALUES ('phone', 'example.com:443', ?);
		PRAGMA user_version = 4;
	`, start.Format(sortableTime)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if title, err := s.SiteTitle(ctx); err != nil || title != "趣多多流量冠军" {
		t.Fatalf("migration did not initialize the site title: %q (%v)", title, err)
	}
	report, err := s.Traffic(ctx, start.Add(time.Hour), 7, time.UTC)
	if err != nil || report.Totals.All.Upload != 80 {
		t.Fatalf("migration changed traffic: %+v (%v)", report.Totals, err)
	}

	newest := start.Add(3 * time.Minute)
	previous := start.Add(2 * time.Minute)
	events := []DestinationEvent{
		{DeviceID: "phone", ConnectionID: "newest", Address: "EXAMPLE.com.:0443", SeenAt: newest},
		{DeviceID: "phone", ConnectionID: "previous", Address: "example.com:443", SeenAt: previous},
		{DeviceID: "phone", ConnectionID: "newest", Address: "example.com:443", SeenAt: newest},
		{DeviceID: "phone", ConnectionID: "ipv6", Address: "[2001:0db8:0:0:0:0:0:1]:80", SeenAt: previous},
		{DeviceID: "phone", ConnectionID: "invalid-address", Address: "https://example.com:443/account", SeenAt: newest},
		{DeviceID: "phone", ConnectionID: "invalid-id:", Address: "example.com:443", SeenAt: newest},
		{DeviceID: "phone", ConnectionID: "stale", Address: "stale.example:80", SeenAt: start.Add(-time.Minute)},
		{DeviceID: "laptop", ConnectionID: "newest", Address: "198.51.100.1:443", SeenAt: newest},
		{DeviceID: "deleted", ConnectionID: "unknown-device", Address: "deleted.example:443", SeenAt: newest},
	}
	if err := s.RecordDestinations(ctx, events); err != nil {
		t.Fatal(err)
	}
	phone, err := s.ListDestinations(ctx, "phone", 10, "")
	if err != nil || len(phone.Items) != 4 || phone.NextCursor != "" || phone.Items[0].Address != "example.com:443" ||
		!phone.Items[0].LastSeenAt.Equal(newest) || phone.Items[1].Address != "example.com:443" ||
		phone.Items[2].Address != "[2001:db8::1]:80" || phone.Items[3].Address != "example.com:443" ||
		!strings.HasPrefix(phone.Items[3].ID, "legacy:") || !phone.Items[3].LastSeenAt.Equal(start) {
		t.Fatalf("unexpected phone history: %+v (%v)", phone, err)
	}
	laptop, err := s.ListDestinations(ctx, "laptop", 100, "")
	if err != nil || len(laptop.Items) != 1 || laptop.Items[0].Address != "198.51.100.1:443" {
		t.Fatalf("unexpected laptop history: %+v (%v)", laptop, err)
	}
	if err := s.DeleteDevice(ctx, "phone"); err != nil {
		t.Fatal(err)
	}
	phone, err = s.ListDestinations(ctx, "phone", 10, "")
	if err != nil || len(phone.Items) != 0 {
		t.Fatalf("unexpected retained history after deletion: %+v (%v)", phone, err)
	}
}

func TestDestinationHistoryKeepsAndPagesAllConnections(t *testing.T) {
	ctx := t.Context()
	s, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if err := s.CreateDevice(ctx, Device{ID: "phone", Name: "phone", UUID: "phone", SubscriptionToken: "phone", CreatedAt: start}); err != nil {
		t.Fatal(err)
	}
	events := make([]DestinationEvent, 602)
	for i := range events {
		events[i] = DestinationEvent{
			DeviceID: "phone", ConnectionID: fmt.Sprintf("conn-%03d", i), Address: "example.com:443", SeenAt: start.Add(time.Duration(i/2) * time.Millisecond),
		}
	}
	if err := s.RecordDestinations(ctx, events); err != nil {
		t.Fatal(err)
	}
	// Replaying an already recorded connection cannot create another copy.
	if err := s.RecordDestinations(ctx, events[:2]); err != nil {
		t.Fatal(err)
	}
	index := len(events) - 1
	cursor := ""
	for {
		page, err := s.ListDestinations(ctx, "phone", 97, cursor)
		if err != nil || len(page.Items) > 97 {
			t.Fatalf("unexpected page size %d (%v)", len(page.Items), err)
		}
		for _, item := range page.Items {
			if index < 0 || item.ID != events[index].ConnectionID {
				t.Fatalf("out of order or repeated destination %s at index %d", item.ID, index)
			}
			index--
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if index != -1 {
		t.Fatalf("pagination did not return %d older connections", index+1)
	}
}
