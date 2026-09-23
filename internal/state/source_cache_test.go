package state_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/state"
	_ "github.com/ncruces/go-sqlite3/driver"
)

func TestSourceCacheCommitReopenIntegrityAndPrune(t *testing.T) {
	t.Parallel()
	path := filepath.Join(privateTempDir(t), "state.db")
	store, err := state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	fingerprint := sha256.Sum256([]byte("private context"))
	entry := state.SourceCache{Key: "pool/main", Fingerprint: fingerprint, Body: []byte("trojan://secret@example.com:443"), ETag: `"provider"`, AcceptedAt: now, ValidatedAt: now}
	if err := store.SaveSourceCache(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, found, err := store.LoadSourceCache(ctx, entry.Key, fingerprint)
	if err != nil || !found || string(got.Body) != string(entry.Body) || got.ETag != entry.ETag {
		t.Fatalf("reopen = %+v, %v, %v", got, found, err)
	}
	wrong := sha256.Sum256([]byte("rotated"))
	if _, found, err := store.LoadSourceCache(ctx, entry.Key, wrong); found || err != nil {
		t.Fatalf("rotated = %v, %v", found, err)
	}
	if err := store.ValidateSourceCache(ctx, entry.Key, fingerprint, now.Add(time.Hour), `"provider-v2"`, ""); err != nil {
		t.Fatal(err)
	}
	got, found, err = store.LoadSourceCache(ctx, entry.Key, fingerprint)
	if err != nil || !found || !got.ValidatedAt.Equal(now.Add(time.Hour)) || !got.AcceptedAt.Equal(now) || got.ETag != `"provider-v2"` {
		t.Fatalf("validation = %+v, %v, %v", got, found, err)
	}
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE http_source_cache SET body=? WHERE source_key=?`, []byte("malformed"), entry.Key); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	if _, _, err := store.LoadSourceCache(ctx, entry.Key, fingerprint); !errors.Is(err, state.ErrPersistence) {
		t.Fatalf("corruption = %v", err)
	}
	if err := store.PruneSourceCache(ctx, map[string]struct{}{}, now.Add(9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LoadSourceCache(ctx, entry.Key, fingerprint); found || err != nil {
		t.Fatalf("prune = %v, %v", found, err)
	}
	empty := state.SourceCache{Key: "pool/empty", Fingerprint: fingerprint, AcceptedAt: now, ValidatedAt: now}
	if err := store.SaveSourceCache(ctx, empty); err != nil {
		t.Fatalf("allowed empty cache: %v", err)
	}
	if got, found, err := store.LoadSourceCache(ctx, empty.Key, fingerprint); err != nil || !found || len(got.Body) != 0 {
		t.Fatalf("empty cache = %+v, %v, %v", got, found, err)
	}
	database, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE http_source_cache SET validated_at_ns=accepted_at_ns-1 WHERE source_key=?`, empty.Key); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	if _, _, err := store.LoadSourceCache(ctx, empty.Key, fingerprint); !errors.Is(err, state.ErrPersistence) {
		t.Fatalf("metadata corruption = %v", err)
	}
}
