package state

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/observation"
)

const schemaVersion = 1

var (
	ErrOpen        = errors.New("history store open failed")
	ErrSchema      = errors.New("history schema is unsupported")
	ErrPersistence = errors.New("history persistence failed")
)

type Retention struct {
	MaxAge    time.Duration
	MaxPerKey int
	MaxRows   int
}

func DefaultRetention() Retention {
	return Retention{MaxAge: 30 * 24 * time.Hour, MaxPerKey: 512, MaxRows: 100_000}
}

func (retention Retention) valid() bool {
	return retention.MaxAge >= time.Minute && retention.MaxAge <= 365*24*time.Hour &&
		retention.MaxPerKey >= 1 && retention.MaxPerKey <= 10_000 &&
		retention.MaxRows >= 1 && retention.MaxRows <= 1_000_000
}

type Store struct {
	database  *sql.DB
	retention Retention
}

func Open(path string, retention Retention) (*Store, error) {
	if !retention.valid() {
		return nil, failure(ErrOpen, "retention")
	}
	absolute, err := filepath.Abs(path)
	if err != nil || filepath.Base(absolute) == "." || filepath.Base(absolute) == string(filepath.Separator) {
		return nil, failure(ErrOpen, "path")
	}
	if err := validatePrivateDirectory(filepath.Dir(absolute)); err != nil {
		return nil, err
	}
	if err := prepareDatabaseFile(absolute); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite3", absolute)
	if err != nil {
		return nil, failure(ErrOpen, "driver")
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	store := &Store{database: database, retention: retention}
	if err := store.initialize(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := os.Chmod(absolute, 0o600); err != nil {
		_ = database.Close()
		return nil, failure(ErrOpen, "permissions")
	}
	return store, nil
}

func (store *Store) String() string { return "SQLite history store path=<redacted>" }

func (store *Store) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	if err := store.database.Close(); err != nil {
		return failure(ErrPersistence, "close")
	}
	return nil
}

func (store *Store) initialize(ctx context.Context) error {
	if err := store.database.PingContext(ctx); err != nil {
		return failure(ErrOpen, "ping")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
	} {
		if _, err := store.database.ExecContext(ctx, statement); err != nil {
			return failure(ErrOpen, "pragma")
		}
	}
	var version int
	if err := store.database.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return failure(ErrOpen, "schema_read")
	}
	if version > schemaVersion || version < 0 {
		return failure(ErrSchema, "future_version")
	}
	if version == schemaVersion {
		return store.verifySchema(ctx)
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return failure(ErrOpen, "migration_begin")
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, schemaV1); err != nil {
		return failure(ErrOpen, "migration_schema")
	}
	if _, err := transaction.ExecContext(ctx, `PRAGMA user_version=1`); err != nil {
		return failure(ErrOpen, "migration_version")
	}
	if err := transaction.Commit(); err != nil {
		return failure(ErrOpen, "migration_commit")
	}
	return store.verifySchema(ctx)
}

func (store *Store) verifySchema(ctx context.Context) error {
	var integrity string
	if err := store.database.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&integrity); err != nil || integrity != "ok" {
		return failure(ErrOpen, "integrity")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT sample_id, endpoint_id, connection_revision, target_id, target_revision,
       vantage_id, probe_kind, engine, engine_version, renderer_schema, media_type,
       started_at_ns, completed_at_ns, duration_ns, outcome, status_code
FROM observations LIMIT 0`)
	if err != nil {
		return failure(ErrOpen, "schema_verify")
	}
	if err := rows.Close(); err != nil {
		return failure(ErrOpen, "schema_close")
	}
	return nil
}

const schemaV1 = `
CREATE TABLE observations (
    sample_id BLOB PRIMARY KEY NOT NULL CHECK(length(sample_id) = 32),
    endpoint_id TEXT NOT NULL,
    connection_revision BLOB NOT NULL CHECK(length(connection_revision) = 32),
    target_id TEXT NOT NULL,
    target_revision BLOB NOT NULL CHECK(length(target_revision) = 32),
    vantage_id TEXT NOT NULL,
    probe_kind INTEGER NOT NULL,
    engine INTEGER NOT NULL,
    engine_version TEXT NOT NULL,
    renderer_schema TEXT NOT NULL,
    media_type TEXT NOT NULL,
    started_at_ns INTEGER NOT NULL,
    completed_at_ns INTEGER NOT NULL,
    duration_ns INTEGER NOT NULL,
    outcome INTEGER NOT NULL,
    status_code INTEGER NOT NULL
);
CREATE INDEX observations_evidence_time ON observations (
    endpoint_id, connection_revision, target_id, target_revision, vantage_id,
    probe_kind, engine, engine_version, renderer_schema, media_type,
    completed_at_ns, sample_id
);
CREATE INDEX observations_completed_time ON observations (completed_at_ns, sample_id);
`

func (store *Store) Append(ctx context.Context, value observation.Observation, now time.Time) error {
	if now.IsZero() {
		return failure(ErrPersistence, "evaluation_time")
	}
	fields, err := persistenceFields(value.Key())
	if err != nil {
		return failure(ErrPersistence, "key")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return failure(ErrPersistence, contextCode(ctx, "begin"))
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `
INSERT OR IGNORE INTO observations (
    sample_id, endpoint_id, connection_revision, target_id, target_revision,
    vantage_id, probe_kind, engine, engine_version, renderer_schema, media_type,
    started_at_ns, completed_at_ns, duration_ns, outcome, status_code
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.SampleIDForPersistence(), fields.endpointID, fields.connectionRevision,
		fields.targetID, fields.targetRevision, fields.vantageID, fields.kind,
		fields.profile.Engine, fields.profile.Version, fields.profile.RendererSchema,
		fields.profile.MediaType, value.StartedAt().UnixNano(), value.CompletedAt().UnixNano(),
		value.Duration().Nanoseconds(), value.Outcome(), value.StatusCode())
	if err != nil {
		return failure(ErrPersistence, contextCode(ctx, "insert"))
	}
	if err := store.prune(ctx, transaction, fields, now.UTC()); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "commit"))
	}
	return nil
}

func (store *Store) prune(ctx context.Context, transaction *sql.Tx, fields keyFields, now time.Time) error {
	cutoff := now.Add(-store.retention.MaxAge).UnixNano()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM observations WHERE completed_at_ns < ?`, cutoff); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "retention_age"))
	}
	arguments := fields.arguments()
	arguments = append(arguments, store.retention.MaxPerKey)
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM observations WHERE sample_id IN (
    SELECT sample_id FROM observations
    WHERE endpoint_id=? AND connection_revision=? AND target_id=? AND target_revision=?
      AND vantage_id=? AND probe_kind=? AND engine=? AND engine_version=?
      AND renderer_schema=? AND media_type=?
    ORDER BY completed_at_ns DESC, sample_id DESC LIMIT -1 OFFSET ?
)`, arguments...); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "retention_key"))
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM observations WHERE sample_id IN (
    SELECT sample_id FROM observations
    ORDER BY completed_at_ns DESC, sample_id DESC LIMIT -1 OFFSET ?
)`, store.retention.MaxRows); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "retention_global"))
	}
	return nil
}

func (store *Store) Load(ctx context.Context, key observation.Key, from, through time.Time) ([]observation.Observation, error) {
	if from.IsZero() || through.IsZero() || through.Before(from) {
		return nil, failure(ErrPersistence, "query_window")
	}
	fields, err := persistenceFields(key)
	if err != nil {
		return nil, failure(ErrPersistence, "key")
	}
	arguments := fields.arguments()
	arguments = append(arguments, from.UTC().UnixNano(), through.UTC().UnixNano())
	rows, err := store.database.QueryContext(ctx, `
SELECT sample_id, started_at_ns, completed_at_ns, duration_ns, outcome, status_code
FROM observations
WHERE endpoint_id=? AND connection_revision=? AND target_id=? AND target_revision=?
  AND vantage_id=? AND probe_kind=? AND engine=? AND engine_version=?
  AND renderer_schema=? AND media_type=?
  AND completed_at_ns >= ? AND completed_at_ns <= ?
ORDER BY completed_at_ns ASC, sample_id ASC`, arguments...)
	if err != nil {
		return nil, failure(ErrPersistence, contextCode(ctx, "query"))
	}
	defer rows.Close()
	values := make([]observation.Observation, 0)
	for rows.Next() {
		var sampleID []byte
		var startedNS, completedNS, durationNS int64
		var outcome, statusCode int
		if err := rows.Scan(&sampleID, &startedNS, &completedNS, &durationNS, &outcome, &statusCode); err != nil {
			return nil, failure(ErrPersistence, "query_scan")
		}
		value, err := observation.New(observation.Params{
			Key: key, StartedAt: time.Unix(0, startedNS).UTC(), CompletedAt: time.Unix(0, completedNS).UTC(),
			Duration: time.Duration(durationNS), Outcome: observation.Outcome(outcome), StatusCode: statusCode,
		})
		if err != nil || !bytes.Equal(sampleID, value.SampleIDForPersistence()) {
			return nil, failure(ErrPersistence, "corrupt_row")
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, failure(ErrPersistence, contextCode(ctx, "query_rows"))
	}
	return values, nil
}

func (store *Store) Summarize(ctx context.Context, key observation.Key, now time.Time, window, freshness time.Duration) (observation.Summary, error) {
	values, err := store.Load(ctx, key, now.Add(-window), now)
	if err != nil {
		return observation.Summary{}, err
	}
	summary, err := observation.Summarize(key, values, now, window, freshness)
	if err != nil {
		return observation.Summary{}, failure(ErrPersistence, "summary")
	}
	return summary, nil
}

func (store *Store) Count(ctx context.Context) (int, error) {
	var count int
	if err := store.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations`).Scan(&count); err != nil {
		return 0, failure(ErrPersistence, contextCode(ctx, "count"))
	}
	return count, nil
}

type keyFields struct {
	endpointID         string
	connectionRevision []byte
	targetID           string
	targetRevision     []byte
	vantageID          string
	kind               observation.Kind
	profile            artifact.Profile
}

func persistenceFields(key observation.Key) (keyFields, error) {
	connectionRevision, err := key.Connection().Revision().RevealForPersistence()
	if err != nil {
		return keyFields{}, err
	}
	targetRevision, err := key.Target().Revision().RevealForPersistence()
	if err != nil {
		return keyFields{}, err
	}
	return keyFields{
		endpointID: key.Connection().ID().String(), connectionRevision: connectionRevision,
		targetID: key.Target().ID().String(), targetRevision: targetRevision,
		vantageID: key.Vantage().String(), kind: key.Kind(), profile: key.Profile(),
	}, nil
}

func (fields keyFields) arguments() []any {
	return []any{
		fields.endpointID, fields.connectionRevision, fields.targetID, fields.targetRevision,
		fields.vantageID, fields.kind, fields.profile.Engine, fields.profile.Version,
		fields.profile.RendererSchema, fields.profile.MediaType,
	}
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return failure(ErrOpen, "directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return failure(ErrOpen, "directory_permissions")
	}
	return nil
}

func prepareDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if createErr != nil {
			return failure(ErrOpen, "create")
		}
		if closeErr := file.Close(); closeErr != nil {
			return failure(ErrOpen, "create_close")
		}
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return failure(ErrOpen, "unsafe_path")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return failure(ErrOpen, "file_permissions")
	}
	return nil
}

type Failure struct {
	kind error
	code string
}

func (failureValue *Failure) Error() string {
	return fmt.Sprintf("%s code=%s", failureValue.kind, failureValue.code)
}
func (failureValue *Failure) Unwrap() error { return failureValue.kind }
func (failureValue *Failure) Code() string  { return failureValue.code }
func failure(kind error, code string) error { return &Failure{kind: kind, code: code} }

func contextCode(ctx context.Context, fallback string) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	return fallback
}
