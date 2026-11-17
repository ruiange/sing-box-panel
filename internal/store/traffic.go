package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Usage struct {
	Upload   int64 `json:"upload,string"`
	Download int64 `json:"download,string"`
}

type Counters = Usage

type UsagePeriods struct {
	Today Usage `json:"today"`
	Month Usage `json:"month"`
	All   Usage `json:"all"`
}

type DailyUsage struct {
	Date string `json:"date"`
	Usage
}

type HourlyUsage struct {
	At time.Time `json:"at"`
	Usage
}

type DeviceUsage struct {
	ID string `json:"id"`
	UsagePeriods
}

type Traffic struct {
	TimeZone        string        `json:"timeZone"`
	CollectedAt     *time.Time    `json:"collectedAt"`
	CollectorStatus string        `json:"collectorStatus"`
	Totals          UsagePeriods  `json:"totals"`
	Daily           []DailyUsage  `json:"daily"`
	Hourly          []HourlyUsage `json:"hourly"`
	HourlySince     *time.Time    `json:"hourlySince"`
	Devices         []DeviceUsage `json:"devices"`
}

const generationTolerance = 2 * time.Second

// RecordTraffic assigns byte deltas to the local day and hour of collection.
// The cursor and both totals are committed together so retries cannot count
// bytes twice.
func (s *Store) RecordTraffic(ctx context.Context, collectedAt, startedAt time.Time, counters map[string]Counters, zone *time.Location) error {
	return s.RecordTrafficWithActivity(ctx, collectedAt, startedAt, counters, zone, nil)
}

// RecordTrafficWithActivity also saves timestamps measured by the live sampler.
func (s *Store) RecordTrafficWithActivity(ctx context.Context, collectedAt, startedAt time.Time, counters map[string]Counters, zone *time.Location, activity map[string]time.Time) error {
	if zone == nil || collectedAt.IsZero() || startedAt.IsZero() || startedAt.After(collectedAt.Add(generationTolerance)) {
		return errors.New("invalid traffic snapshot timestamps or time zone")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	previousStart, previousCollection, err := readCollectorState(ctx, tx)
	if err != nil {
		return err
	}
	if previousCollection != nil && !collectedAt.After(*previousCollection) {
		return nil
	}

	// Uptime is estimated to whole seconds; keep the first estimate for the
	// current generation so tolerance does not drift over time.
	if previousStart == nil || startedAt.Sub(*previousStart).Abs() > generationTolerance {
		if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_cursor"); err != nil {
			return err
		}
	} else {
		startedAt = *previousStart
	}

	local := collectedAt.In(zone)
	hour, err := recordObservedHour(ctx, tx, local)
	if err != nil {
		return err
	}
	day := local.Format(time.DateOnly)
	for id, current := range counters {
		if err := applyCounter(ctx, tx, id, day, hour, current); err != nil {
			return err
		}
	}
	if err := recordDeviceActivity(ctx, tx, collectedAt, activity); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE collector_state SET started_at = ?, collected_at = ? WHERE id = 1",
		startedAt.UTC().Format(time.RFC3339Nano), collectedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func recordDeviceActivity(ctx context.Context, tx *sql.Tx, collectedAt time.Time, activity map[string]time.Time) error {
	for id, at := range activity {
		if at.IsZero() || at.After(collectedAt) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO device_activity(device_id, last_active_at) SELECT id, ? FROM devices WHERE id = ?
			ON CONFLICT(device_id) DO UPDATE SET last_active_at = excluded.last_active_at
			WHERE excluded.last_active_at > device_activity.last_active_at
		`, at.UTC().Format(sortableTime), id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LastDeviceActivity(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT device_id, last_active_at FROM device_activity")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activity := make(map[string]time.Time)
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, err
		}
		activity[id] = at
	}
	return activity, rows.Err()
}

func recordObservedHour(ctx context.Context, tx *sql.Tx, local time.Time) (time.Time, error) {
	updated, err := tx.ExecContext(ctx, "UPDATE traffic_hourly_state SET since = ? WHERE id = 1 AND since IS NULL", local.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return time.Time{}, err
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 0 {
		// The first post-migration snapshot provides a baseline. Its delta
		// still belongs in the existing daily totals, but may predate hourly collection.
		return time.Time{}, err
	}
	hour := hourStart(local)
	_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO traffic_observed_hours(hour_start) VALUES (?)", hour.Unix())
	return hour, err
}

func hourStart(local time.Time) time.Time {
	elapsed := time.Duration(local.Minute())*time.Minute + time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
	start := local.Add(-elapsed)
	if offsetStart, _ := local.ZoneBounds(); !offsetStart.IsZero() && start.Before(offsetStart) {
		// An offset change can split a local hour. Preserve the valid start
		// of each part, including both occurrences of a repeated hour.
		return offsetStart
	}
	return start
}

func applyCounter(ctx context.Context, tx *sql.Tx, id, day string, hour time.Time, current Counters) error {
	var previous Counters
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(c.upload, 0), COALESCE(c.download, 0)
		FROM devices d LEFT JOIN traffic_cursor c ON c.device_id = d.id
		WHERE d.id = ?
	`, id).Scan(&previous.Upload, &previous.Download)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Upload < 0 || current.Download < 0 {
		return fmt.Errorf("invalid negative counter for device %q", id)
	}
	if current == previous {
		return nil
	}
	delta := Usage{
		Upload:   counterDelta(previous.Upload, current.Upload),
		Download: counterDelta(previous.Download, current.Download),
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO traffic_daily(device_id, day, upload, download) VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id, day) DO UPDATE SET
			upload = upload + excluded.upload,
			download = download + excluded.download
	`, id, day, delta.Upload, delta.Download); err != nil {
		return err
	}
	if !hour.IsZero() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO traffic_hourly(device_id, hour_start, upload, download) VALUES (?, ?, ?, ?)
			ON CONFLICT(device_id, hour_start) DO UPDATE SET
				upload = upload + excluded.upload,
				download = download + excluded.download
		`, id, hour.Unix(), delta.Upload, delta.Download); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO traffic_cursor(device_id, upload, download) VALUES (?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET upload = excluded.upload, download = excluded.download
	`, id, current.Upload, current.Download)
	return err
}

func counterDelta(previous, current int64) int64 {
	if current < previous {
		return current
	}
	return current - previous
}

// ResetTrafficBaseline starts a fresh counter generation after a confirmed
// reload without changing previously recorded usage or the last collection time.
func (s *Store) ResetTrafficBaseline(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_cursor"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE collector_state SET started_at = NULL WHERE id = 1"); err != nil {
		return err
	}
	return tx.Commit()
}

func readCollectorState(ctx context.Context, tx *sql.Tx) (startedAt, collectedAt *time.Time, err error) {
	var start, collection sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT started_at, collected_at FROM collector_state WHERE id = 1").Scan(&start, &collection); err != nil {
		return nil, nil, err
	}
	startedAt, err = parseOptionalTime(start)
	if err != nil {
		return nil, nil, err
	}
	collectedAt, err = parseOptionalTime(collection)
	return
}

func parseOptionalTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid {
		return nil, nil
	}
	result, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Store) Traffic(ctx context.Context, now time.Time, days int, zone *time.Location) (Traffic, error) {
	return s.traffic(ctx, now, days, 0, zone, "")
}

func (s *Store) DeviceTraffic(ctx context.Context, now time.Time, days int, zone *time.Location, id string) (Traffic, error) {
	if id == "" {
		return Traffic{}, ErrNotFound
	}
	return s.traffic(ctx, now, days, 0, zone, id)
}

func (s *Store) HourlyTraffic(ctx context.Context, now time.Time, days, hours int, zone *time.Location, deviceID string) (Traffic, error) {
	return s.traffic(ctx, now, days, hours, zone, deviceID)
}

func (s *Store) traffic(ctx context.Context, now time.Time, days, hours int, zone *time.Location, deviceID string) (Traffic, error) {
	if zone == nil || days < 1 || days > 366 || hours < 0 || hours > 72 {
		return Traffic{}, errors.New("invalid traffic window or time zone")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Traffic{}, err
	}
	defer tx.Rollback()

	_, lastCollection, err := readCollectorState(ctx, tx)
	if err != nil {
		return Traffic{}, err
	}
	local := now.In(zone)
	today := local.Format(time.DateOnly)
	monthStart := local.AddDate(0, 0, 1-local.Day()).Format(time.DateOnly)
	daily, err := queryDaily(ctx, tx, local, days, deviceID)
	if err != nil {
		return Traffic{}, err
	}
	devices, totals, err := queryDeviceUsage(ctx, tx, today, monthStart, deviceID)
	if err != nil {
		return Traffic{}, err
	}
	if deviceID != "" && len(devices) == 0 {
		return Traffic{}, ErrNotFound
	}
	var rawHourlySince sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT since FROM traffic_hourly_state WHERE id = 1").Scan(&rawHourlySince); err != nil {
		return Traffic{}, err
	}
	hourlySince, err := parseOptionalTime(rawHourlySince)
	if err != nil {
		return Traffic{}, err
	}
	if hourlySince != nil {
		*hourlySince = hourlySince.In(zone)
	}
	hourly := make([]HourlyUsage, 0)
	if hours != 0 {
		hourly, err = queryHourly(ctx, tx, local, hours, deviceID)
		if err != nil {
			return Traffic{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Traffic{}, err
	}
	status := "waiting"
	if lastCollection != nil {
		status = "ok"
	}
	return Traffic{
		TimeZone:        zone.String(),
		CollectedAt:     lastCollection,
		CollectorStatus: status,
		Totals:          totals,
		Daily:           daily,
		Hourly:          hourly,
		HourlySince:     hourlySince,
		Devices:         devices,
	}, nil
}

func queryHourly(ctx context.Context, tx *sql.Tx, local time.Time, hours int, deviceID string) ([]HourlyUsage, error) {
	last := hourStart(local)
	first := last
	for i := 1; i < hours; i++ {
		first = hourStart(first.Add(-time.Nanosecond))
	}
	var args []any
	query := `SELECT h.hour_start, COALESCE(SUM(t.upload), 0), COALESCE(SUM(t.download), 0)
		FROM traffic_observed_hours h LEFT JOIN traffic_hourly t ON t.hour_start = h.hour_start`
	if deviceID != "" {
		query += " AND t.device_id = ?"
		args = append(args, deviceID)
	}
	args = append(args, first.Unix(), last.Unix())
	rows, err := tx.QueryContext(ctx, query+" WHERE h.hour_start BETWEEN ? AND ? GROUP BY h.hour_start ORDER BY h.hour_start", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hourly := make([]HourlyUsage, 0, hours)
	for rows.Next() {
		var epoch int64
		var usage Usage
		if err := rows.Scan(&epoch, &usage.Upload, &usage.Download); err != nil {
			return nil, err
		}
		hourly = append(hourly, HourlyUsage{At: time.Unix(epoch, 0).In(local.Location()), Usage: usage})
	}
	return hourly, rows.Err()
}

func queryDaily(ctx context.Context, tx *sql.Tx, local time.Time, days int, deviceID string) ([]DailyUsage, error) {
	firstDay := local.AddDate(0, 0, 1-days)
	query := `SELECT day, SUM(upload), SUM(download) FROM traffic_daily WHERE day BETWEEN ? AND ?`
	args := []any{firstDay.Format(time.DateOnly), local.Format(time.DateOnly)}
	if deviceID != "" {
		query += " AND device_id = ?"
		args = append(args, deviceID)
	}
	rows, err := tx.QueryContext(ctx, query+" GROUP BY day ORDER BY day", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string]Usage, days)
	for rows.Next() {
		var date string
		var usage Usage
		if err := rows.Scan(&date, &usage.Upload, &usage.Download); err != nil {
			return nil, err
		}
		byDate[date] = usage
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	daily := make([]DailyUsage, 0, days)
	for offset := 0; offset < days; offset++ {
		date := firstDay.AddDate(0, 0, offset).Format(time.DateOnly)
		daily = append(daily, DailyUsage{Date: date, Usage: byDate[date]})
	}
	return daily, nil
}

func queryDeviceUsage(ctx context.Context, tx *sql.Tx, today, monthStart, deviceID string) ([]DeviceUsage, UsagePeriods, error) {
	query := `SELECT d.id,
			COALESCE(SUM(CASE WHEN t.day = ? THEN t.upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN t.day = ? THEN t.download ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN t.day BETWEEN ? AND ? THEN t.upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN t.day BETWEEN ? AND ? THEN t.download ELSE 0 END), 0),
			COALESCE(SUM(t.upload), 0),
			COALESCE(SUM(t.download), 0)
		FROM devices d LEFT JOIN traffic_daily t ON t.device_id = d.id`
	args := []any{today, today, monthStart, today, monthStart, today}
	if deviceID != "" {
		query += " WHERE d.id = ?"
		args = append(args, deviceID)
	}
	rows, err := tx.QueryContext(ctx, query+" GROUP BY d.id ORDER BY d.created_at, d.id", args...)
	if err != nil {
		return nil, UsagePeriods{}, err
	}
	defer rows.Close()

	var totals UsagePeriods
	devices := make([]DeviceUsage, 0)
	for rows.Next() {
		var device DeviceUsage
		if err := rows.Scan(
			&device.ID, &device.Today.Upload, &device.Today.Download,
			&device.Month.Upload, &device.Month.Download, &device.All.Upload, &device.All.Download,
		); err != nil {
			return nil, UsagePeriods{}, err
		}
		if err := addUsage(&totals, device.UsagePeriods); err != nil {
			return nil, UsagePeriods{}, err
		}
		devices = append(devices, device)
	}
	return devices, totals, rows.Err()
}

func addUsage(total *UsagePeriods, add UsagePeriods) error {
	for _, period := range []struct {
		target *Usage
		value  Usage
	}{{&total.Today, add.Today}, {&total.Month, add.Month}, {&total.All, add.All}} {
		period.target.Upload += period.value.Upload
		period.target.Download += period.value.Download
		if period.target.Upload < 0 || period.target.Download < 0 {
			return errors.New("traffic totals exceed supported range")
		}
	}
	return nil
}
