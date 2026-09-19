package state_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/state"
)

func TestStoreInitializeReopenAndReplay(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	path := filepath.Join(directory, "history.db")
	store, err := state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)
	key := testKey(t, "credential", "service", "https://example.com/health", "host-a")
	values := []observation.Observation{
		testObservation(t, key, now.Add(-3*time.Minute), 30*time.Millisecond, observation.OutcomeSuccess, 204),
		testObservation(t, key, now.Add(-time.Minute), 70*time.Millisecond, observation.OutcomeUnexpectedResponse, 503),
	}
	for _, value := range values {
		if err := store.Append(context.Background(), value, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Append(context.Background(), values[0], now); err != nil {
		t.Fatal(err)
	}
	if count, err := store.Count(context.Background()); err != nil || count != len(values) {
		t.Fatalf("count = %d, %v", count, err)
	}
	before, err := store.Summarize(context.Background(), key, now, 10*time.Minute, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v, %v", info.Mode().Perm(), err)
	}

	reopened, err := state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Summarize(context.Background(), key, now, 10*time.Minute, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || after.Samples != 2 || after.Successes != 1 || !after.Fresh {
		t.Fatalf("restart replay changed summary: before=%+v after=%+v", before, after)
	}
}

func TestStoreSeparatesConnectionTargetAndVantage(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, state.DefaultRetention())
	now := time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)
	keys := []observation.Key{
		testKey(t, "old", "service-a", "https://example.com/", "host-a"),
		testKey(t, "new", "service-a", "https://example.com/", "host-a"),
		testKey(t, "old", "service-b", "https://example.net/", "host-a"),
		testKey(t, "old", "service-a", "https://example.com/", "host-b"),
	}
	for index, key := range keys {
		value := testObservation(t, key, now.Add(time.Duration(index)*time.Second), time.Millisecond, observation.OutcomeSuccess, 204)
		if err := store.Append(context.Background(), value, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range keys {
		values, err := store.Load(context.Background(), key, now.Add(-time.Minute), now.Add(time.Minute))
		if err != nil || len(values) != 1 {
			t.Fatalf("isolated load count = %d, %v", len(values), err)
		}
	}
}

func TestRetentionBoundsAgeKeyAndGlobalRows(t *testing.T) {
	t.Parallel()
	retention := state.Retention{MaxAge: time.Minute, MaxPerKey: 2, MaxRows: 3}
	store := openTestStore(t, retention)
	now := time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)
	first := testKey(t, "first", "service-a", "https://example.com/", "host")
	second := testKey(t, "second", "service-b", "https://example.net/", "host")
	for index := 0; index < 3; index++ {
		value := testObservation(t, first, now.Add(time.Duration(index-2)*10*time.Second), time.Millisecond, observation.OutcomeSuccess, 204)
		if err := store.Append(context.Background(), value, now); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := store.Load(context.Background(), first, now.Add(-time.Minute), now)
	if err != nil || len(loaded) != 2 || !loaded[0].CompletedAt().Equal(now.Add(-10*time.Second)) {
		t.Fatalf("per-key retention = %v, %v", loaded, err)
	}
	for index := 0; index < 2; index++ {
		value := testObservation(t, second, now.Add(time.Duration(index+1)*time.Second), time.Millisecond, observation.OutcomeSuccess, 204)
		if err := store.Append(context.Background(), value, now.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := store.Count(context.Background()); err != nil || count != 3 {
		t.Fatalf("global retention count = %d, %v", count, err)
	}
	old := testObservation(t, second, now.Add(-2*time.Minute), time.Millisecond, observation.OutcomeSuccess, 204)
	if err := store.Append(context.Background(), old, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	values, err := store.Load(context.Background(), second, now.Add(-3*time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if value.CompletedAt().Before(now.Add(-time.Minute)) {
			t.Fatal("age retention kept an expired observation")
		}
	}
}

func TestStoreRejectsFutureSchemaAndUnsafePaths(t *testing.T) {
	t.Parallel()
	t.Run("future schema", func(t *testing.T) {
		directory := privateTempDir(t)
		path := filepath.Join(directory, "future.db")
		database, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`PRAGMA user_version=2`); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := state.Open(path, state.DefaultRetention()); !errors.Is(err, state.ErrSchema) {
			t.Fatalf("future schema error = %v", err)
		}
	})
	t.Run("public directory", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := state.Open(filepath.Join(directory, "history.db"), state.DefaultRetention()); !errors.Is(err, state.ErrOpen) {
			t.Fatalf("public directory error = %v", err)
		}
	})
	t.Run("invalid current schema", func(t *testing.T) {
		directory := privateTempDir(t)
		path := filepath.Join(directory, "invalid.db")
		database, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`PRAGMA user_version=1`); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := state.Open(path, state.DefaultRetention()); !errors.Is(err, state.ErrOpen) {
			t.Fatalf("invalid schema error = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		directory := privateTempDir(t)
		realPath := filepath.Join(directory, "real.db")
		if err := os.WriteFile(realPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(directory, "link.db")
		if err := os.Symlink(realPath, linkPath); err != nil {
			t.Fatal(err)
		}
		if _, err := state.Open(linkPath, state.DefaultRetention()); !errors.Is(err, state.ErrOpen) {
			t.Fatalf("symlink error = %v", err)
		}
	})
}

func TestStoreCancellationConcurrencyAndSecretBoundary(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	path := filepath.Join(directory, "history.db")
	store, err := state.Open(path, state.DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)
	key := testKey(t, "database-secret-canary", "private-service", "https://example.com/?token=database-target-canary", "host")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Append(cancelled, testObservation(t, key, now, time.Millisecond, observation.OutcomeSuccess, 204), now); !errors.Is(err, state.ErrPersistence) {
		t.Fatalf("cancelled append = %v", err)
	}

	const count = 32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, count)
	values := make([]observation.Observation, count)
	for index := range values {
		values[index] = testObservation(t, key, now.Add(time.Duration(index)*time.Millisecond), time.Millisecond, observation.OutcomeSuccess, 204)
	}
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errorsSeen <- store.Append(context.Background(), values[index], now.Add(time.Minute))
		}(index)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"history.db", "history.db-wal", "history.db-shm"} {
		content, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			t.Fatal(readErr)
		}
		if bytes.Contains(content, []byte("database-secret-canary")) || bytes.Contains(content, []byte("database-target-canary")) {
			t.Fatalf("%s contains raw secret-bearing input", name)
		}
	}
}

func BenchmarkStoreAppendAndLoadWindow(b *testing.B) {
	store := openTestStore(b, state.Retention{MaxAge: 24 * time.Hour, MaxPerKey: 512, MaxRows: 10_000})
	base := time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)
	key := testKey(b, "benchmark-credential", "benchmark-target", "https://example.test/", "benchmark-host")
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		completed := base.Add(time.Duration(index) * time.Millisecond)
		value := testObservation(b, key, completed, time.Millisecond, observation.OutcomeSuccess, 204)
		if err := store.Append(context.Background(), value, completed); err != nil {
			b.Fatal(err)
		}
		if _, err := store.Load(context.Background(), key, completed.Add(-time.Minute), completed); err != nil {
			b.Fatal(err)
		}
	}
}

func openTestStore(t testing.TB, retention state.Retention) *state.Store {
	t.Helper()
	directory := privateTempDir(t)
	store, err := state.Open(filepath.Join(directory, "history.db"), retention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func privateTempDir(t testing.TB) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func testKey(t testing.TB, credential, targetID, rawURL, vantageID string) observation.Key {
	t.Helper()
	address, err := endpoint.NewAddress("edge.example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := endpoint.NewTrojanCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	tls, err := endpoint.NewTLS("edge.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := endpoint.NewConfiguration(endpoint.ProtocolTrojan, address, secret, endpoint.NewTCPTransport(), tls)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := observation.NewConnectionRef(configuration.Identity())
	if err != nil {
		t.Fatal(err)
	}
	identifier, err := observation.NewTargetID(targetID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := observation.NewHTTPTarget(identifier, rawURL, 204, 5*time.Second, observation.HTTPOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vantage, err := observation.NewVantageID(vantageID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := observation.NewKey(connection, target.Ref(), vantage, observation.KindHTTPGet, artifact.SingBox1141)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testObservation(t testing.TB, key observation.Key, completed time.Time, duration time.Duration, outcome observation.Outcome, status int) observation.Observation {
	t.Helper()
	value, err := observation.New(observation.Params{
		Key: key, StartedAt: completed.Add(-duration), CompletedAt: completed,
		Duration: duration, Outcome: outcome, StatusCode: status,
	})
	if err != nil {
		t.Fatalf("construct observation: %v", err)
	}
	return value
}

func TestStoreFormattingRedactsPath(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, state.DefaultRetention())
	if formatted := fmt.Sprintf("%v", store); formatted != "SQLite history store path=<redacted>" {
		t.Fatalf("store formatting = %q", formatted)
	}
}
