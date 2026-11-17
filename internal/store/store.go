package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("device not found")

type Store struct {
	db *sql.DB
}

type Device struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Enabled           bool      `json:"enabled"`
	UUID              string    `json:"-"`
	SubscriptionToken string    `json:"-"`
	CreatedAt         time.Time `json:"createdAt"`
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := prepareFile(absolutePath); err != nil {
		return nil, fmt.Errorf("prepare SQLite file: %w", err)
	}

	params := url.Values{"mode": {"rw"}}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "busy_timeout(5000)")
	dsn := url.URL{Scheme: "file", Path: absolutePath, RawQuery: params.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare SQLite schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func prepareFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("database directory must be a real directory writable only by its owner")
	}

	// Check for redirected SQLite files before any open or recovery operation.
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("database file %q must be a regular file", path+suffix)
		}
		if suffix != "" && info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("database file %q must be private", path+suffix)
		}
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	entry, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !entry.Mode().IsRegular() || !os.SameFile(entry, opened) {
		return errors.New("database file was replaced while opening")
	}
	if opened.Mode().Perm() != 0600 {
		return file.Chmod(0600)
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		_, err = tx.ExecContext(ctx, `
			CREATE TABLE devices (
				id TEXT PRIMARY KEY NOT NULL,
				name TEXT NOT NULL,
				enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
				uuid TEXT NOT NULL UNIQUE,
				subscription_token TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL
			) STRICT;
			CREATE TABLE traffic_cursor (
				device_id TEXT PRIMARY KEY NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
				upload INTEGER NOT NULL CHECK (upload >= 0),
				download INTEGER NOT NULL CHECK (download >= 0)
			) STRICT;
			CREATE TABLE traffic_daily (
				device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
				day TEXT NOT NULL,
				upload INTEGER NOT NULL CHECK (upload >= 0),
				download INTEGER NOT NULL CHECK (download >= 0),
				PRIMARY KEY (device_id, day)
			) STRICT;
			CREATE INDEX traffic_daily_by_day ON traffic_daily(day);
			CREATE TABLE collector_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				started_at TEXT,
				collected_at TEXT
			) STRICT;
			INSERT INTO collector_state(id) VALUES (1);
			PRAGMA user_version = 1;
		`)
		if err != nil {
			return err
		}
		fallthrough
	case 1:
		_, err = tx.ExecContext(ctx, `
			CREATE TABLE traffic_observed_hours (
				hour_start INTEGER PRIMARY KEY
			) STRICT;
			CREATE TABLE traffic_hourly (
				device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
				hour_start INTEGER NOT NULL REFERENCES traffic_observed_hours(hour_start),
				upload INTEGER NOT NULL CHECK (upload >= 0),
				download INTEGER NOT NULL CHECK (download >= 0),
				PRIMARY KEY (device_id, hour_start)
			) STRICT;
			CREATE INDEX traffic_hourly_by_hour ON traffic_hourly(hour_start);
			CREATE TABLE traffic_hourly_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				since TEXT
			) STRICT;
			INSERT INTO traffic_hourly_state(id) VALUES (1);
			PRAGMA user_version = 2;
		`)
		if err != nil {
			return err
		}
		fallthrough
	case 2:
		_, err = tx.ExecContext(ctx, `
			CREATE TABLE device_activity (
				device_id TEXT PRIMARY KEY NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
				last_active_at TEXT NOT NULL
			) STRICT;
			PRAGMA user_version = 3;
		`)
		if err != nil {
			return err
		}
		fallthrough
	case 3:
		_, err = tx.ExecContext(ctx, `
			CREATE TABLE device_destinations (
				device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
				address TEXT NOT NULL,
				last_seen_at TEXT NOT NULL,
				PRIMARY KEY (device_id, address)
			) STRICT;
			CREATE INDEX device_destinations_by_recency ON device_destinations(device_id, last_seen_at DESC, address);
			PRAGMA user_version = 4;
		`)
		if err != nil {
			return err
		}
		fallthrough
	case 4:
		_, err = tx.ExecContext(ctx, `
			CREATE TABLE device_destinations_new (
				device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
				connection_id TEXT NOT NULL,
				address TEXT NOT NULL,
				last_seen_at TEXT NOT NULL,
				PRIMARY KEY (device_id, connection_id)
			) STRICT;
			INSERT INTO device_destinations_new (device_id, connection_id, address, last_seen_at)
			SELECT device_id, 'legacy:' || rowid, address, last_seen_at FROM device_destinations;
			DROP TABLE device_destinations;
			ALTER TABLE device_destinations_new RENAME TO device_destinations;
			CREATE INDEX device_destinations_by_recency ON device_destinations(device_id, last_seen_at DESC, connection_id DESC);
			PRAGMA user_version = 5;
		`)
		if err != nil {
			return err
		}
	case 5:
	default:
		return fmt.Errorf("unsupported schema version %d", version)
	}
	return tx.Commit()
}

const deviceFields = "id, name, enabled, uuid, subscription_token, created_at"
const sortableTime = "2006-01-02T15:04:05.000000000Z07:00"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDevice(row rowScanner) (Device, error) {
	var device Device
	var createdAt string
	if err := row.Scan(&device.ID, &device.Name, &device.Enabled, &device.UUID, &device.SubscriptionToken, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Device{}, ErrNotFound
		}
		return Device{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Device{}, err
	}
	device.CreatedAt = parsed
	return device, nil
}

func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+deviceFields+" FROM devices ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := make([]Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, id string) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, "SELECT "+deviceFields+" FROM devices WHERE id = ?", id))
}

func (s *Store) GetDeviceBySubscriptionToken(ctx context.Context, token string) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx, "SELECT "+deviceFields+" FROM devices WHERE subscription_token = ?", token))
}

func (s *Store) CreateDevice(ctx context.Context, device Device) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices(id, name, enabled, uuid, subscription_token, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, device.ID, device.Name, device.Enabled, device.UUID, device.SubscriptionToken, device.CreatedAt.UTC().Format(sortableTime))
	return err
}

func (s *Store) UpdateDevice(ctx context.Context, device Device) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE devices
		SET name = ?, enabled = ?, uuid = ?, subscription_token = ?
		WHERE id = ?
	`, device.Name, device.Enabled, device.UUID, device.SubscriptionToken, device.ID)
	return affectedDevice(result, err)
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM devices WHERE id = ?", id)
	return affectedDevice(result, err)
}

func affectedDevice(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
