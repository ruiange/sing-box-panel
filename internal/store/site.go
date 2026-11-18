package store

import "context"

func (s *Store) SiteTitle(ctx context.Context) (string, error) {
	var title string
	err := s.db.QueryRowContext(ctx, "SELECT title FROM site_settings WHERE id = 1").Scan(&title)
	return title, err
}

func (s *Store) UpdateSiteTitle(ctx context.Context, title string) (string, error) {
	var saved string
	err := s.db.QueryRowContext(ctx, "UPDATE site_settings SET title = ? WHERE id = 1 RETURNING title", title).Scan(&saved)
	return saved, err
}
