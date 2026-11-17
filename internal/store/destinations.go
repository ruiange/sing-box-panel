package store

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxDestinationsPerPage = 100

var destinationLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var connectionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var legacyConnectionIDPattern = regexp.MustCompile(`^legacy:[0-9]+$`)
var ErrInvalidDestinationCursor = errors.New("invalid destination history cursor")

type DestinationEvent struct {
	DeviceID     string
	ConnectionID string
	Address      string
	SeenAt       time.Time
}

type Destination struct {
	ID         string    `json:"id"`
	Address    string    `json:"address"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type DestinationPage struct {
	Items      []Destination `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

func destinationAddress(raw string) (string, bool) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" || len(port) > 5 {
		return "", false
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return "", false
		}
		host = address.Unmap().String()
	} else {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if len(host) > 253 {
			return "", false
		}
		for _, label := range strings.Split(host, ".") {
			if !destinationLabel.MatchString(label) {
				return "", false
			}
		}
	}
	return net.JoinHostPort(host, strconv.FormatUint(number, 10)), true
}

// RecordDestinations records each connection once, even when the stream
// replays it. Separate connections to the same address stay separate.
func (s *Store) RecordDestinations(ctx context.Context, events []DestinationEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insert, err := tx.PrepareContext(ctx, `
		INSERT INTO device_destinations(device_id, connection_id, address, last_seen_at)
		SELECT id, ?, ?, ? FROM devices WHERE id = ? AND substr(created_at, 1, 23) <= substr(?, 1, 23)
		ON CONFLICT (device_id, connection_id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, event := range events {
		address, valid := destinationAddress(event.Address)
		if !valid || event.SeenAt.IsZero() || !connectionIDPattern.MatchString(event.ConnectionID) {
			continue
		}
		// The core reports timestamps with millisecond resolution.
		seenAt := event.SeenAt.UTC().Format(sortableTime)
		if _, err := insert.ExecContext(ctx, event.ConnectionID, address, seenAt, event.DeviceID, seenAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListDestinations(ctx context.Context, deviceID string, limit int, before string) (DestinationPage, error) {
	limit = min(max(limit, 1), MaxDestinationsPerPage)
	query := `SELECT connection_id, address, last_seen_at FROM device_destinations WHERE device_id = ?`
	args := []any{deviceID}
	if before != "" {
		seenAt, connectionID, err := decodeDestinationCursor(before)
		if err != nil {
			return DestinationPage{}, err
		}
		query += ` AND (last_seen_at, connection_id) < (?, ?)`
		args = append(args, seenAt, connectionID)
	}
	query += ` ORDER BY last_seen_at DESC, connection_id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, append(args, limit+1)...)
	if err != nil {
		return DestinationPage{}, err
	}
	defer rows.Close()

	page := DestinationPage{Items: make([]Destination, 0, limit)}
	for rows.Next() {
		if len(page.Items) == limit {
			last := page.Items[limit-1]
			page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.LastSeenAt.Format(sortableTime) + "\n" + last.ID))
			break
		}
		var item Destination
		var lastSeenAt string
		if err := rows.Scan(&item.ID, &item.Address, &lastSeenAt); err != nil {
			return DestinationPage{}, err
		}
		item.LastSeenAt, err = time.Parse(time.RFC3339Nano, lastSeenAt)
		if err != nil {
			return DestinationPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func decodeDestinationCursor(value string) (string, string, error) {
	if len(value) > 256 {
		return "", "", ErrInvalidDestinationCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", "", ErrInvalidDestinationCursor
	}
	seenAt, connectionID, valid := strings.Cut(string(raw), "\n")
	if !valid || (!connectionIDPattern.MatchString(connectionID) && !legacyConnectionIDPattern.MatchString(connectionID)) {
		return "", "", ErrInvalidDestinationCursor
	}
	parsed, err := time.Parse(sortableTime, seenAt)
	if err != nil || parsed.UTC().Format(sortableTime) != seenAt {
		return "", "", ErrInvalidDestinationCursor
	}
	return seenAt, connectionID, nil
}
