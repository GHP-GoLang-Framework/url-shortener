// Package store holds the persistence layer for the URL shortener: Postgres
// is the source of truth, Redis is a cache-aside layer in front of it.
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	codeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength   = 7
	maxAttempts  = 5
	cacheTTL     = time.Hour
)

// Link is one shortened URL row.
type Link struct {
	Code      string
	TargetURL string
	Clicks    int64
	CreatedAt time.Time
}

// Store wraps the Postgres pool and Redis client the app needs.
type Store struct {
	pg    *pgxpool.Pool
	cache *redis.Client
}

// Open connects to Postgres and Redis and makes sure the schema exists.
func Open(ctx context.Context, pgURL, redisAddr string) (*Store, error) {
	pg, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		return nil, fmt.Errorf("store: connect postgres: %w", err)
	}
	if err := pg.Ping(ctx); err != nil {
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}

	cache := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := cache.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("store: ping redis: %w", err)
	}

	s := &Store{pg: pg, cache: cache}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the Postgres pool and the Redis connection.
func (s *Store) Close() {
	s.pg.Close()
	s.cache.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pg.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS links (
			code       TEXT PRIMARY KEY,
			target_url TEXT NOT NULL,
			clicks     BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS idx_links_created_at ON links (created_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Shorten inserts targetURL under a new random code, retrying on collision,
// and returns the code.
func (s *Store) Shorten(ctx context.Context, targetURL string) (string, error) {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := randomCode()
		if err != nil {
			return "", fmt.Errorf("store: generate code: %w", err)
		}

		_, err = s.pg.Exec(ctx,
			`INSERT INTO links (code, target_url) VALUES ($1, $2)`,
			code, targetURL,
		)
		if err == nil {
			return code, nil
		}
		if !isUniqueViolation(err) {
			return "", fmt.Errorf("store: insert link: %w", err)
		}
		// Collision on code — loop and try a fresh one.
	}
	return "", errors.New("store: could not generate a unique code")
}

// Resolve returns the target URL for code, checking Redis before Postgres
// and backfilling the cache on a miss. found is false when the code does
// not exist. Every resolution also increments the click counter.
func (s *Store) Resolve(ctx context.Context, code string) (targetURL string, found bool, err error) {
	cacheKey := "url:" + code

	cached, err := s.cache.Get(ctx, cacheKey).Result()
	if err == nil {
		s.bumpClicks(ctx, code)
		return cached, true, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", false, fmt.Errorf("store: redis get: %w", err)
	}

	var target string
	err = s.pg.QueryRow(ctx,
		`SELECT target_url FROM links WHERE code = $1`, code,
	).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: select link: %w", err)
	}

	if err := s.cache.Set(ctx, cacheKey, target, cacheTTL).Err(); err != nil {
		return "", false, fmt.Errorf("store: redis set: %w", err)
	}

	s.bumpClicks(ctx, code)
	return target, true, nil
}

// bumpClicks increments the click counter for code. Resolve already
// confirmed the code exists, so a failure here is logged-worthy but not
// fatal to the redirect — it's swallowed by the caller on purpose.
func (s *Store) bumpClicks(ctx context.Context, code string) {
	s.pg.Exec(ctx, `UPDATE links SET clicks = clicks + 1 WHERE code = $1`, code)
}

// Recent returns the most recently created links, newest first.
func (s *Store) Recent(ctx context.Context, limit int) ([]Link, error) {
	rows, err := s.pg.Query(ctx,
		`SELECT code, target_url, clicks, created_at FROM links ORDER BY created_at DESC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: select recent: %w", err)
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.Code, &l.TargetURL, &l.Clicks, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan recent: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// randomCode returns a random codeLength-character string drawn from
// codeAlphabet.
func randomCode() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(buf), nil
}

// isUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505), the only error Shorten's retry loop should absorb.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
