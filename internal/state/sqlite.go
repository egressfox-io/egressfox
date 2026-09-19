package state

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
	"github.com/egressfox-io/egressfox/internal/selection"
)

const schemaVersion = 2

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
	if version == 0 {
		if _, err := transaction.ExecContext(ctx, schemaV1); err != nil {
			return failure(ErrOpen, "migration_schema_v1")
		}
		version = 1
	}
	if version == 1 {
		if _, err := transaction.ExecContext(ctx, schemaV2); err != nil {
			return failure(ErrOpen, "migration_schema_v2")
		}
		version = 2
	}
	if _, err := transaction.ExecContext(ctx, `PRAGMA user_version=2`); err != nil {
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
	checkpointRows, err := store.database.QueryContext(ctx, `SELECT scope_id, slot, payload, updated_at_ns FROM selection_checkpoints LIMIT 0`)
	if err != nil {
		return failure(ErrOpen, "selection_schema_verify")
	}
	if err := checkpointRows.Close(); err != nil {
		return failure(ErrOpen, "selection_schema_close")
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

const schemaV2 = `
CREATE TABLE selection_checkpoints (
    scope_id TEXT NOT NULL,
    slot INTEGER NOT NULL CHECK(slot IN (0, 1)),
    payload BLOB NOT NULL CHECK(length(payload) BETWEEN 1 AND 4194304),
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (scope_id, slot)
);
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

const (
	checkpointCommitted = 0
	checkpointPending   = 1
)

type checkpointPayload struct {
	Scope             string               `json:"scope"`
	TargetID          string               `json:"target_id"`
	TargetRevision    []byte               `json:"target_revision"`
	Vantage           string               `json:"vantage"`
	Kind              observation.Kind     `json:"kind"`
	Profile           artifact.Profile     `json:"profile"`
	PolicyFingerprint []byte               `json:"policy_fingerprint"`
	Members           []checkpointMember   `json:"members"`
	Cooldowns         []checkpointCooldown `json:"cooldowns"`
	Receipt           []byte               `json:"receipt"`
}

type checkpointMember struct {
	EndpointID string `json:"endpoint_id"`
	Revision   []byte `json:"connection_revision"`
	SelectedAt int64  `json:"selected_at_ns"`
}

type checkpointCooldown struct {
	EndpointID string `json:"endpoint_id"`
	Revision   []byte `json:"connection_revision"`
	Until      int64  `json:"until_ns"`
}

type storedCheckpoint struct {
	state   selection.State
	receipt artifact.Receipt
}

// StageDecision durably records a decision that may only become committed after
// its exact protected artifact receipt is observed at the publisher boundary.
func (store *Store) StageDecision(ctx context.Context, value selection.State, receipt artifact.Receipt, now time.Time) error {
	if err := value.Validate(); err != nil || now.IsZero() {
		return failure(ErrPersistence, "decision_state")
	}
	payload, err := encodeCheckpoint(value, receipt)
	if err != nil || len(payload) > 4<<20 {
		return failure(ErrPersistence, "decision_encode")
	}
	if _, err := store.database.ExecContext(ctx, `
INSERT INTO selection_checkpoints(scope_id, slot, payload, updated_at_ns)
VALUES(?, ?, ?, ?)
ON CONFLICT(scope_id, slot) DO UPDATE SET payload=excluded.payload, updated_at_ns=excluded.updated_at_ns`,
		value.Scope, checkpointPending, payload, now.UTC().UnixNano()); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "decision_stage"))
	}
	return nil
}

// CommitDecision promotes the pending checkpoint only when it names the exact
// currently published artifact receipt supplied by the caller.
func (store *Store) CommitDecision(ctx context.Context, scope string, current artifact.Receipt) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return failure(ErrPersistence, contextCode(ctx, "decision_commit_begin"))
	}
	defer transaction.Rollback()
	pending, found, err := loadCheckpoint(ctx, transaction, scope, checkpointPending)
	if err != nil || !found || !pending.receipt.Equal(current) {
		return failure(ErrPersistence, "decision_commit_mismatch")
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM selection_checkpoints WHERE scope_id=? AND slot=?`, scope, checkpointCommitted); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "decision_commit_replace"))
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE selection_checkpoints SET slot=? WHERE scope_id=? AND slot=?`, checkpointCommitted, scope, checkpointPending); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "decision_commit_promote"))
	}
	if err := transaction.Commit(); err != nil {
		return failure(ErrPersistence, contextCode(ctx, "decision_commit"))
	}
	return nil
}

// RecoverDecision resolves a pending filesystem/database crash window and returns
// committed state only when it matches the publisher's current protected receipt.
func (store *Store) RecoverDecision(ctx context.Context, scope string, current artifact.Receipt, currentExists bool) (*selection.State, error) {
	if !safeCheckpointScope(scope) {
		return nil, failure(ErrPersistence, "decision_scope")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, failure(ErrPersistence, contextCode(ctx, "decision_recover_begin"))
	}
	defer transaction.Rollback()
	pending, pendingFound, err := loadCheckpoint(ctx, transaction, scope, checkpointPending)
	if err != nil {
		return nil, err
	}
	if pendingFound && currentExists && pending.receipt.Equal(current) {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM selection_checkpoints WHERE scope_id=? AND slot=?`, scope, checkpointCommitted); err != nil {
			return nil, failure(ErrPersistence, "decision_recover_replace")
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE selection_checkpoints SET slot=? WHERE scope_id=? AND slot=?`, checkpointCommitted, scope, checkpointPending); err != nil {
			return nil, failure(ErrPersistence, "decision_recover_promote")
		}
	} else if pendingFound {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM selection_checkpoints WHERE scope_id=? AND slot=?`, scope, checkpointPending); err != nil {
			return nil, failure(ErrPersistence, "decision_recover_discard")
		}
	}
	committed, found, err := loadCheckpoint(ctx, transaction, scope, checkpointCommitted)
	if err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, failure(ErrPersistence, contextCode(ctx, "decision_recover_commit"))
	}
	if !found || !currentExists || !committed.receipt.Equal(current) {
		return nil, nil
	}
	value := committed.state
	return &value, nil
}

type checkpointQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadCheckpoint(ctx context.Context, query checkpointQuery, scope string, slot int) (storedCheckpoint, bool, error) {
	var payload []byte
	err := query.QueryRowContext(ctx, `SELECT payload FROM selection_checkpoints WHERE scope_id=? AND slot=?`, scope, slot).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return storedCheckpoint{}, false, nil
	}
	if err != nil {
		return storedCheckpoint{}, false, failure(ErrPersistence, contextCode(ctx, "decision_load"))
	}
	value, err := decodeCheckpoint(payload)
	if err != nil {
		return storedCheckpoint{}, false, failure(ErrPersistence, "decision_corrupt")
	}
	return value, true, nil
}

func encodeCheckpoint(value selection.State, receipt artifact.Receipt) ([]byte, error) {
	targetRevision, err := value.Context.Target.Revision().RevealForPersistence()
	if err != nil {
		return nil, err
	}
	receiptBytes, err := receipt.RevealForPersistence()
	if err != nil {
		return nil, err
	}
	payload := checkpointPayload{Scope: value.Scope, TargetID: value.Context.Target.ID().String(), TargetRevision: targetRevision,
		Vantage: value.Context.Vantage.String(), Kind: value.Context.Kind, Profile: value.Context.Profile,
		PolicyFingerprint: append([]byte(nil), value.PolicyFingerprint[:]...), Receipt: receiptBytes}
	for _, member := range value.Members {
		revision, err := member.Connection.Revision().RevealForPersistence()
		if err != nil {
			return nil, err
		}
		payload.Members = append(payload.Members, checkpointMember{member.Connection.ID().String(), revision, member.SelectedAt.UTC().UnixNano()})
	}
	for _, cooldown := range value.Cooldowns {
		revision, err := cooldown.Connection.Revision().RevealForPersistence()
		if err != nil {
			return nil, err
		}
		payload.Cooldowns = append(payload.Cooldowns, checkpointCooldown{cooldown.Connection.ID().String(), revision, cooldown.Until.UTC().UnixNano()})
	}
	return json.Marshal(payload)
}

func decodeCheckpoint(encoded []byte) (storedCheckpoint, error) {
	var payload checkpointPayload
	if len(encoded) == 0 || len(encoded) > 4<<20 || json.Unmarshal(encoded, &payload) != nil || len(payload.PolicyFingerprint) != 32 || len(payload.Members) > 10_000 || len(payload.Cooldowns) > 10_000 {
		return storedCheckpoint{}, errors.New("invalid decision checkpoint")
	}
	targetID, err := observation.NewTargetID(payload.TargetID)
	if err != nil {
		return storedCheckpoint{}, err
	}
	target, err := observation.RestoreTargetRef(targetID, payload.TargetRevision)
	if err != nil {
		return storedCheckpoint{}, err
	}
	vantage, err := observation.NewVantageID(payload.Vantage)
	if err != nil {
		return storedCheckpoint{}, err
	}
	selectionContext, err := selection.NewContext(target, vantage, payload.Kind, payload.Profile)
	if err != nil {
		return storedCheckpoint{}, err
	}
	value := selection.State{Scope: payload.Scope, Context: selectionContext}
	copy(value.PolicyFingerprint[:], payload.PolicyFingerprint)
	for _, stored := range payload.Members {
		connection, err := restoreConnection(stored.EndpointID, stored.Revision)
		if err != nil || stored.SelectedAt == 0 {
			return storedCheckpoint{}, errors.New("invalid decision member")
		}
		value.Members = append(value.Members, selection.Member{Connection: connection, SelectedAt: time.Unix(0, stored.SelectedAt).UTC()})
	}
	for _, stored := range payload.Cooldowns {
		connection, err := restoreConnection(stored.EndpointID, stored.Revision)
		if err != nil || stored.Until == 0 {
			return storedCheckpoint{}, errors.New("invalid decision cooldown")
		}
		value.Cooldowns = append(value.Cooldowns, selection.Cooldown{Connection: connection, Until: time.Unix(0, stored.Until).UTC()})
	}
	if err := value.Validate(); err != nil {
		return storedCheckpoint{}, err
	}
	receipt, err := artifact.RestoreReceipt(payload.Receipt)
	if err != nil {
		return storedCheckpoint{}, err
	}
	return storedCheckpoint{state: value, receipt: receipt}, nil
}

func restoreConnection(rawID string, rawRevision []byte) (observation.ConnectionRef, error) {
	id, err := endpoint.ParseID(rawID)
	if err != nil {
		return observation.ConnectionRef{}, err
	}
	revision, err := endpoint.RestoreRevision(rawRevision)
	if err != nil {
		return observation.ConnectionRef{}, err
	}
	return observation.RestoreConnectionRef(id, revision)
}

func safeCheckpointScope(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '/' || character == '_' || character == '-') {
			return false
		}
	}
	return true
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
