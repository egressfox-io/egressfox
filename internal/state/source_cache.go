package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

const maxCachedBody = 4 << 20

// SourceCache contains private source data. Never log or serialize it into status.
type SourceCache struct {
	Key          string
	Fingerprint  [32]byte
	Body         []byte
	ContentType  string
	ETag         string
	LastModified string
	AcceptedAt   time.Time
	ValidatedAt  time.Time
}

func (store *Store) LoadSourceCache(ctx context.Context, key string, fingerprint [32]byte) (SourceCache, bool, error) {
	var entry SourceCache
	var storedFingerprint, digest []byte
	var accepted, validated int64
	err := store.database.QueryRowContext(ctx, `SELECT fingerprint, body, body_digest, content_type, etag, last_modified, accepted_at_ns, validated_at_ns FROM http_source_cache WHERE source_key=?`, key).Scan(&storedFingerprint, &entry.Body, &digest, &entry.ContentType, &entry.ETag, &entry.LastModified, &accepted, &validated)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceCache{}, false, nil
	}
	if err != nil {
		return SourceCache{}, false, failure(ErrPersistence, "cache_read")
	}
	if len(storedFingerprint) != 32 || string(storedFingerprint) != string(fingerprint[:]) {
		return SourceCache{}, false, nil
	}
	computed := sha256.Sum256(entry.Body)
	if len(entry.Body) > maxCachedBody || len(digest) != 32 || string(computed[:]) != string(digest) || accepted <= 0 || validated < accepted || len(entry.ETag) > 1024 || len(entry.LastModified) > 1024 {
		return SourceCache{}, false, failure(ErrPersistence, "cache_corrupt")
	}
	entry.Key, entry.Fingerprint = key, fingerprint
	entry.AcceptedAt, entry.ValidatedAt = time.Unix(0, accepted).UTC(), time.Unix(0, validated).UTC()
	return entry, true, nil
}

func (store *Store) SaveSourceCache(ctx context.Context, entry SourceCache) error {
	if entry.Key == "" || len(entry.Body) > maxCachedBody || entry.AcceptedAt.IsZero() || entry.ValidatedAt.Before(entry.AcceptedAt) || len(entry.ETag) > 1024 || len(entry.LastModified) > 1024 {
		return failure(ErrPersistence, "cache_invalid")
	}
	digest := sha256.Sum256(entry.Body)
	body := entry.Body
	if body == nil {
		body = []byte{}
	}
	_, err := store.database.ExecContext(ctx, `INSERT INTO http_source_cache (source_key, fingerprint, body, body_digest, content_type, etag, last_modified, accepted_at_ns, validated_at_ns) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(source_key) DO UPDATE SET fingerprint=excluded.fingerprint, body=excluded.body, body_digest=excluded.body_digest, content_type=excluded.content_type, etag=excluded.etag, last_modified=excluded.last_modified, accepted_at_ns=excluded.accepted_at_ns, validated_at_ns=excluded.validated_at_ns`, entry.Key, entry.Fingerprint[:], body, digest[:], entry.ContentType, entry.ETag, entry.LastModified, entry.AcceptedAt.UnixNano(), entry.ValidatedAt.UnixNano())
	if err != nil {
		return failure(ErrPersistence, "cache_write")
	}
	return nil
}

func (store *Store) ValidateSourceCache(ctx context.Context, key string, fingerprint [32]byte, at time.Time, etag, lastModified string) error {
	if len(etag) > 1024 || len(lastModified) > 1024 {
		return failure(ErrPersistence, "cache_invalid_validator")
	}
	result, err := store.database.ExecContext(ctx, `UPDATE http_source_cache SET validated_at_ns=?, etag=CASE WHEN ?='' THEN etag ELSE ? END, last_modified=CASE WHEN ?='' THEN last_modified ELSE ? END WHERE source_key=? AND fingerprint=?`, at.UnixNano(), etag, etag, lastModified, lastModified, key, fingerprint[:])
	if err != nil {
		return failure(ErrPersistence, "cache_validate")
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return failure(ErrPersistence, "cache_missing")
	}
	return nil
}

func (store *Store) SourceCacheVersion(ctx context.Context, key string) ([32]byte, time.Time, bool, error) {
	var digest []byte
	var body []byte
	var validated int64
	err := store.database.QueryRowContext(ctx, `SELECT body, body_digest, validated_at_ns FROM http_source_cache WHERE source_key=?`, key).Scan(&body, &digest, &validated)
	if errors.Is(err, sql.ErrNoRows) {
		return [32]byte{}, time.Time{}, false, nil
	}
	if err != nil || len(body) > maxCachedBody || len(digest) != 32 || validated <= 0 {
		return [32]byte{}, time.Time{}, false, failure(ErrPersistence, "cache_version")
	}
	computed := sha256.Sum256(body)
	if string(computed[:]) != string(digest) {
		return [32]byte{}, time.Time{}, false, failure(ErrPersistence, "cache_corrupt")
	}
	var result [32]byte
	copy(result[:], digest)
	return result, time.Unix(0, validated).UTC(), true, nil
}

func (store *Store) DeleteSourceCache(ctx context.Context, key string) error {
	if _, err := store.database.ExecContext(ctx, `DELETE FROM http_source_cache WHERE source_key=?`, key); err != nil {
		return failure(ErrPersistence, "cache_delete")
	}
	return nil
}

func (store *Store) PruneSourceCache(ctx context.Context, live map[string]struct{}, now time.Time) error {
	rows, err := store.database.QueryContext(ctx, `SELECT source_key, validated_at_ns FROM http_source_cache`)
	if err != nil {
		return failure(ErrPersistence, "cache_prune_read")
	}
	var stale []string
	for rows.Next() {
		var key string
		var validated int64
		if err := rows.Scan(&key, &validated); err != nil {
			rows.Close()
			return failure(ErrPersistence, "cache_prune_scan")
		}
		if _, ok := live[key]; !ok || !now.Before(time.Unix(0, validated).Add(8*24*time.Hour)) {
			stale = append(stale, key)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return failure(ErrPersistence, "cache_prune_iterate")
	}
	if err := rows.Close(); err != nil {
		return failure(ErrPersistence, "cache_prune_close")
	}
	for _, key := range stale {
		if err := store.DeleteSourceCache(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
