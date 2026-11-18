package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrafficGenerationsAcrossLocalMidnight(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "数据 #?.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	zone := time.FixedZone("UTC+08", 8*60*60)
	beforeMidnight := time.Date(2026, 11, 30, 15, 59, 59, 0, time.UTC)
	started := beforeMidnight.Add(-time.Minute)
	for _, d := range []Device{
		{ID: "phone", Name: "手机", Enabled: true, UUID: "phone-key", SubscriptionToken: "phone-token", CreatedAt: started},
		{ID: "pc", Name: "电脑", Enabled: true, UUID: "pc-key", SubscriptionToken: "pc-token", CreatedAt: started},
	} {
		if err := s.CreateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	record := func(at, start time.Time, values map[string]Counters) {
		t.Helper()
		if err := s.RecordTraffic(ctx, at, start, values, zone); err != nil {
			t.Fatal(err)
		}
	}

	record(beforeMidnight, started, map[string]Counters{"phone": {Upload: 250, Download: 900}})
	record(beforeMidnight, started, map[string]Counters{"phone": {Upload: 300, Download: 950}})
	record(beforeMidnight.Add(time.Second), started.Add(time.Second), nil)
	record(beforeMidnight.Add(2*time.Second), started.Add(-time.Second), map[string]Counters{
		"phone": {Upload: 350, Download: 950},
		"pc":    {Upload: 8, Download: 40},
	})

	// The next process can already have higher counters than the old one.
	newStart := beforeMidnight.Add(3 * time.Second)
	record(newStart.Add(time.Second), newStart, map[string]Counters{"phone": {Upload: 500, Download: 1_000}})
	if err := s.ResetTrafficBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	record(newStart.Add(2*time.Second), newStart.Add(time.Second), map[string]Counters{"phone": {Upload: 25, Download: 125}})

	// Restarting the collector itself must preserve its cursor.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	record(newStart.Add(3*time.Second), newStart.Add(time.Second), map[string]Counters{"phone": {Upload: 25, Download: 125}})

	result, err := s.Traffic(ctx, newStart.Add(3*time.Second), 7, zone)
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.Today != (Usage{Upload: 633, Download: 1215}) || result.Totals.Month != result.Totals.Today {
		t.Fatalf("today/month: %+v", result.Totals)
	}
	if result.Totals.All != (Usage{Upload: 883, Download: 2115}) {
		t.Fatalf("total: %+v", result.Totals.All)
	}
	if len(result.Daily) != 7 || result.Daily[5] != (DailyUsage{Date: "2026-11-30", Usage: Usage{Upload: 250, Download: 900}}) {
		t.Fatalf("daily: %+v", result.Daily)
	}
	if len(result.Devices) != 2 || result.Devices[0].ID != "pc" || result.Devices[0].All != (Usage{Upload: 8, Download: 40}) {
		t.Fatalf("devices: %+v", result.Devices)
	}
	phone, err := s.GetDevice(ctx, "phone")
	if err != nil {
		t.Fatal(err)
	}
	phone.Enabled = false
	if err := s.UpdateDevice(ctx, phone); err != nil {
		t.Fatal(err)
	}
	scoped, err := s.DeviceTraffic(ctx, newStart.Add(3*time.Second), 7, zone, "phone")
	if err != nil || scoped.Totals.All != (Usage{Upload: 875, Download: 2075}) || len(scoped.Devices) != 1 || scoped.Devices[0].ID != "phone" || scoped.Daily[6].Usage != (Usage{Upload: 625, Download: 1175}) {
		t.Fatalf("scoped history is mixed or lost: %+v, %v", scoped, err)
	}
	if _, err := s.DeviceTraffic(ctx, newStart, 7, zone, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing device: %v", err)
	}

	body, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(body), `"upload":"883"`) {
		t.Fatalf("traffic JSON: %s (%v)", body, err)
	}
}

func TestTrafficRollbackLeavesCursorUnchanged(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	start := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		err := s.CreateDevice(ctx, Device{ID: id, Name: id, Enabled: true, UUID: id, SubscriptionToken: id, CreatedAt: start})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordTraffic(ctx, start.Add(2*time.Second), start, map[string]Counters{
		"a": {Upload: 90}, "b": {Download: -1},
	}, time.UTC); err == nil {
		t.Fatal("accepted an invalid sample")
	}
	result, err := s.Traffic(ctx, start.Add(2*time.Second), 7, time.UTC)
	if err != nil || result.CollectedAt != nil || result.Totals.All != (Usage{}) {
		t.Fatalf("partial sample persisted: %+v (%v)", result, err)
	}
	if err := s.RecordTraffic(ctx, start.Add(2*time.Second), start, map[string]Counters{
		"a": {Upload: 90}, "b": {Download: 1},
	}, time.UTC); err != nil {
		t.Fatal(err)
	}
	result, err = s.Traffic(ctx, start.Add(2*time.Second), 7, time.UTC)
	if err != nil || result.Totals.All != (Usage{Upload: 90, Download: 1}) {
		t.Fatalf("retry was not applied: %+v (%v)", result, err)
	}
}

func TestDeviceActivityPersistsAfterMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	start := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	phone := Device{ID: "phone", Name: "手机", Enabled: true, UUID: "phone-key", SubscriptionToken: "phone-token", CreatedAt: start}
	pc := Device{ID: "pc", Name: "电脑", Enabled: true, UUID: "pc-key", SubscriptionToken: "pc-token", CreatedAt: start}
	for _, device := range []Device{phone, pc} {
		if err := s.CreateDevice(ctx, device); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordTraffic(ctx, start.Add(time.Minute), start, map[string]Counters{phone.ID: {Upload: 40}}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTraffic(ctx, start.Add(2*time.Minute), start, map[string]Counters{phone.ID: {Upload: 75}}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `
		DROP TABLE site_settings;
		DROP TABLE device_destinations;
		DROP TABLE device_activity;
		PRAGMA user_version = 2;
	`); err != nil {
		t.Fatal(err)
	}
	reopen := func() {
		t.Helper()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		opened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		s = opened
	}
	readActivity := func() map[string]time.Time {
		t.Helper()
		activity, err := s.LastDeviceActivity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return activity
	}
	reopen()
	if activity := readActivity(); len(activity) != 0 {
		t.Fatalf("migration fabricated activity: %+v", activity)
	}
	history, err := s.HourlyTraffic(ctx, start.Add(2*time.Minute), 7, 6, time.UTC, phone.ID)
	if err != nil || history.Totals.All.Upload != 75 || len(history.Hourly) != 1 || history.Hourly[0].Upload != 35 {
		t.Fatalf("migration changed daily or hourly traffic: %+v (%v)", history, err)
	}

	first := start.Add(3 * time.Minute)
	lastPhone := first.Add(-time.Nanosecond)
	if err := s.RecordTrafficWithActivity(ctx, first, start, map[string]Counters{phone.ID: {Upload: 85}}, time.UTC, map[string]time.Time{
		phone.ID: lastPhone, pc.ID: first.Add(time.Second), "absent": first,
	}); err != nil {
		t.Fatal(err)
	}
	if activity := readActivity(); len(activity) != 1 || !activity[phone.ID].Equal(lastPhone) {
		t.Fatalf("unexpected first activity: %+v", activity)
	}
	second := first.Add(15 * time.Second)
	if err := s.RecordTrafficWithActivity(ctx, second, start, nil, time.UTC, map[string]time.Time{
		phone.ID: lastPhone.Add(-time.Nanosecond), pc.ID: second,
	}); err != nil {
		t.Fatal(err)
	}
	phone.Enabled = false
	if err := s.UpdateDevice(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetTrafficBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	reopen()
	if activity := readActivity(); len(activity) != 2 || !activity[phone.ID].Equal(lastPhone) || !activity[pc.ID].Equal(second) {
		t.Fatalf("activity was overwritten or lost: %+v", activity)
	}
	if err := s.DeleteDevice(ctx, pc.ID); err != nil {
		t.Fatal(err)
	}
	if activity := readActivity(); len(activity) != 1 || !activity[phone.ID].Equal(lastPhone) {
		t.Fatalf("deleted device activity was retained: %+v", activity)
	}
}

func TestFailedActivityWriteRollsBackTraffic(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	if err := s.CreateDevice(ctx, Device{ID: "phone", Name: "手机", Enabled: true, UUID: "key", SubscriptionToken: "token", CreatedAt: start}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TRIGGER fail_activity BEFORE INSERT ON device_activity
		BEGIN SELECT RAISE(ABORT, 'activity write failed'); END;
	`); err != nil {
		t.Fatal(err)
	}
	sample := start.Add(time.Second)
	counters := map[string]Counters{"phone": {Upload: 55}}
	activity := map[string]time.Time{"phone": sample}
	if err := s.RecordTrafficWithActivity(ctx, sample, start, counters, time.UTC, activity); err == nil {
		t.Fatal("accepted a failed activity write")
	}
	traffic, err := s.Traffic(ctx, sample, 7, time.UTC)
	if err != nil || traffic.CollectedAt != nil || traffic.Totals.All != (Usage{}) {
		t.Fatalf("partial sample persisted: %+v (%v)", traffic, err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER fail_activity;`); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTrafficWithActivity(ctx, sample, start, counters, time.UTC, activity); err != nil {
		t.Fatal(err)
	}
	traffic, err = s.Traffic(ctx, sample, 7, time.UTC)
	if err != nil || traffic.Totals.All.Upload != 55 {
		t.Fatalf("traffic retry was not applied: %+v (%v)", traffic, err)
	}
	stored, err := s.LastDeviceActivity(ctx)
	if err != nil || !stored["phone"].Equal(sample) {
		t.Fatalf("activity retry was not applied: %+v (%v)", stored, err)
	}
}

func TestHourlyTrafficStartsAtMigrationBaseline(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	zone := time.FixedZone("UTC+05:30", 5*60*60+30*60)
	start := time.Date(2026, 11, 15, 11, 0, 0, 0, zone)
	for _, id := range []string{"phone", "pc"} {
		if err := s.CreateDevice(ctx, Device{ID: id, Name: id, Enabled: true, UUID: id, SubscriptionToken: id, CreatedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	record := func(at time.Time, counters map[string]Counters) {
		t.Helper()
		if err := s.RecordTraffic(ctx, at, start, counters, zone); err != nil {
			t.Fatal(err)
		}
	}

	oldSample := start.Add(105 * time.Minute)
	record(oldSample, map[string]Counters{"phone": {Upload: 100, Download: 200}})
	if _, err := s.db.ExecContext(ctx, `
		DROP TABLE site_settings;
		DROP TABLE device_destinations;
		DROP TABLE device_activity;
		DROP TABLE traffic_hourly;
		DROP TABLE traffic_observed_hours;
		DROP TABLE traffic_hourly_state;
		PRAGMA user_version = 1;
	`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	baseline := oldSample.Add(3 * time.Minute)
	record(baseline, map[string]Counters{"phone": {Upload: 130, Download: 240}})
	first, err := s.HourlyTraffic(ctx, baseline, 7, 6, zone, "phone")
	if err != nil || first.HourlySince == nil || !first.HourlySince.Equal(baseline) || len(first.Hourly) != 0 || first.Totals.All != (Usage{Upload: 130, Download: 240}) {
		t.Fatalf("migration baseline: %+v (%v)", first, err)
	}

	record(baseline.Add(time.Minute), nil)
	record(start.Add(2*time.Hour), map[string]Counters{
		"phone": {Upload: 155, Download: 300},
		"pc":    {Upload: 3, Download: 8},
	})
	// Duplicate snapshots must leave both hourly and daily totals unchanged.
	record(start.Add(2*time.Hour), map[string]Counters{"phone": {Upload: 999, Download: 999}})
	now := start.Add(4 * time.Hour)
	record(now, nil)

	phone, err := s.HourlyTraffic(ctx, now, 7, 6, zone, "phone")
	if err != nil || len(phone.Hourly) != 3 {
		t.Fatalf("scoped hourly: %+v (%v)", phone, err)
	}
	for i, want := range []struct {
		hour  int
		usage Usage
	}{{12, Usage{}}, {13, Usage{Upload: 25, Download: 60}}, {15, Usage{}}} {
		point := phone.Hourly[i]
		if point.At.Hour() != want.hour || point.At.Minute() != 0 || point.Usage != want.usage {
			t.Errorf("hour %d: got %+v, want %+v", i, point, want)
		}
	}
	all, err := s.HourlyTraffic(ctx, now, 7, 6, zone, "")
	if err != nil || len(all.Hourly) != 3 || all.Hourly[1].Usage != (Usage{Upload: 28, Download: 68}) || all.Totals.All != (Usage{Upload: 158, Download: 308}) {
		t.Fatalf("combined hours: %+v (%v)", all, err)
	}
	encoded, err := json.Marshal(phone.Hourly[1])
	if err != nil || string(encoded) != `{"at":"2026-11-15T13:00:00+05:30","upload":"25","download":"60"}` {
		t.Fatalf("hourly JSON: %s (%v)", encoded, err)
	}
}

func TestHourlyTrafficSeparatesRepeatedLocalHour(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 4, 59, 0, 0, time.UTC)
	device := Device{ID: "phone", Name: "phone", Enabled: true, UUID: "key", SubscriptionToken: "token", CreatedAt: start}
	if err := s.CreateDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	for i, sample := range []time.Time{start, start.Add(2 * time.Minute), start.Add(62 * time.Minute)} {
		if err := s.RecordTraffic(ctx, sample, start, map[string]Counters{device.ID: {Upload: int64(i)}}, zone); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.HourlyTraffic(ctx, start.Add(62*time.Minute), 7, 6, zone, device.ID)
	if err != nil || len(result.Hourly) != 2 {
		t.Fatalf("repeated hour: %+v (%v)", result, err)
	}
	for i, want := range []string{"2026-11-01T01:00:00-04:00", "2026-11-01T01:00:00-05:00"} {
		if point := result.Hourly[i]; point.At.Format(time.RFC3339) != want || point.Usage.Upload != 1 {
			t.Errorf("point %d: got %+v, want %s with one byte", i, point, want)
		}
	}
}

func TestOpenRejectsRedirectedDatabaseFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.db")
	if err := os.WriteFile(target, []byte("unrelated data"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "panel.db")
	for _, candidate := range []string{path, path + "-journal", path + "-wal", path + "-shm"} {
		if err := os.Symlink(target, candidate); err != nil {
			t.Fatal(err)
		}
		if s, err := Open(path); err == nil {
			s.Close()
			t.Fatalf("accepted redirected database file %s", candidate)
		}
		if err := os.Remove(candidate); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("target permissions changed: %v (%v)", info, err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions: %v (%v)", info, err)
	}
}
