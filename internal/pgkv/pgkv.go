// Package pgkv keeps what used to live in Redis in Postgres (board card openpak/prds#20): the device
// link codes (shared with the Juxtaposition UI's Lite service, which writes the same ui_kv rows) and
// the book of display names. Rows that expire carry expires_at; reads ignore expired rows and a
// sweep removes them.
package pgkv

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS ui_kv (
	key        text PRIMARY KEY,
	value      text NOT NULL,
	expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS ui_kv_expires ON ui_kv (expires_at);
CREATE TABLE IF NOT EXISTS openverse_names (
	hash  text NOT NULL,
	field text NOT NULL,
	value text NOT NULL,
	PRIMARY KEY (hash, field)
);`

// Store implements linkcodes.KV and names.Backend.
type Store struct{ pool *pgxpool.Pool }

// Open connects, creates the tables if missing and starts the expiry sweep (every 10 minutes).
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool}
	go func() {
		for range time.Tick(10 * time.Minute) {
			_, _ = s.Sweep(context.Background())
		}
	}()
	return s, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Sweep deletes expired rows and reports how many.
func (s *Store) Sweep(ctx context.Context) (int64, error) {
	t, err := s.pool.Exec(ctx, "DELETE FROM ui_kv WHERE expires_at < now()")
	return t.RowsAffected(), err
}

// Get implements linkcodes.KV: unknown and expired both come back as not found.
func (s *Store) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.pool.QueryRow(ctx, "SELECT value FROM ui_kv WHERE key = $1 AND expires_at > now()", key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// Set implements linkcodes.KV; ex is the lifetime in seconds.
func (s *Store) Set(ctx context.Context, key, value string, ex int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ui_kv (key, value, expires_at) VALUES ($1, $2, now() + make_interval(secs => $3))
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at`, key, value, float64(ex))
	return err
}

// HSet implements names.Backend.
func (s *Store) HSet(ctx context.Context, key, field, value string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO openverse_names (hash, field, value) VALUES ($1, $2, $3)
		 ON CONFLICT (hash, field) DO UPDATE SET value = EXCLUDED.value`, key, field, value)
	return err
}

// HMGet implements names.Backend; a missing field is absent from the result.
func (s *Store) HMGet(ctx context.Context, key string, fields []string) (map[string]string, error) {
	out := map[string]string{}
	if len(fields) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT field, value FROM openverse_names WHERE hash = $1 AND field = ANY($2)", key, fields)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f, v string
		if err := rows.Scan(&f, &v); err != nil {
			return nil, err
		}
		out[f] = v
	}
	return out, rows.Err()
}
