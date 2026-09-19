package publish

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/egressfox-io/egressfox/internal/artifact"
)

type acceptChecker struct{}

func (acceptChecker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: "test/sing-box/1.14.1"}, nil
}

func TestFilePublicationReplaceNoOpAndPermissions(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	first := validated(t, []byte("first-secret-artifact"))
	result, err := publisher.Publish(context.Background(), first)
	if err != nil || !result.Changed {
		t.Fatalf("first publish = %v, %v", result, err)
	}
	assertFile(t, target, []byte("first-secret-artifact"), 0o600)
	result, err = publisher.Publish(context.Background(), first)
	if err != nil || result.Changed {
		t.Fatalf("no-op publish = %v, %v", result, err)
	}
	second := validated(t, []byte("second-secret-artifact"))
	result, err = publisher.Publish(context.Background(), second)
	if err != nil || !result.Changed {
		t.Fatalf("replacement = %v, %v", result, err)
	}
	assertFile(t, target, []byte("second-secret-artifact"), 0o600)
	assertFile(t, publisher.previousPath(), []byte("first-secret-artifact"), 0o600)
	if _, err := os.Stat(publisher.journalPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal remained after commit")
	}
}

func TestPublicationFailurePreservesKnownGood(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	first := validated(t, []byte("known-good"))
	if _, err := publisher.Publish(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	originalRename := publisher.rename
	publisher.rename = func(oldPath, newPath string) error {
		if newPath == target {
			return errors.New("injected rename failure")
		}
		return originalRename(oldPath, newPath)
	}
	if _, err := publisher.Publish(context.Background(), validated(t, []byte("candidate"))); !errors.Is(err, ErrPublication) {
		t.Fatalf("publish error = %v", err)
	}
	assertFile(t, target, []byte("known-good"), 0o600)
	if _, ok := artifact.MatchesProtectedReceipt([]byte("known-good"), mustRead(t, publisher.receiptPath())); !ok {
		t.Fatal("known-good receipt changed")
	}
}

func TestReceiptSyncFailureRestoresKnownGoodPair(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	first := validated(t, []byte("known-good"))
	if _, err := publisher.Publish(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	originalSync := publisher.syncDir
	failed := false
	publisher.syncDir = func(path string) error {
		if !failed {
			content, contentErr := os.ReadFile(target)
			receipt, receiptErr := os.ReadFile(publisher.receiptPath())
			if contentErr == nil && receiptErr == nil {
				if _, matches := artifact.MatchesProtectedReceipt(content, receipt); matches && string(content) == "candidate" {
					failed = true
					return errors.New("injected receipt sync failure")
				}
			}
		}
		return originalSync(path)
	}
	if _, err := publisher.Publish(context.Background(), validated(t, []byte("candidate"))); !errors.Is(err, ErrPublication) {
		t.Fatalf("publish error = %v", err)
	}
	if !failed {
		t.Fatal("receipt sync failure was not injected")
	}
	assertFile(t, target, []byte("known-good"), 0o600)
	if _, ok := artifact.MatchesProtectedReceipt([]byte("known-good"), mustRead(t, publisher.receiptPath())); !ok {
		t.Fatal("known-good target and receipt were not restored")
	}
}

func TestJournalRecoveryCompletesCommittedTarget(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), validated(t, []byte("old"))); err != nil {
		t.Fatal(err)
	}
	desired := validated(t, []byte("committed-before-crash"))
	journal, _ := desired.ProtectedReceipt()
	if err := os.WriteFile(publisher.journalPath(), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, desired.Reveal(), 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	result, err := restarted.Publish(context.Background(), desired)
	if err != nil || result.Changed {
		t.Fatalf("recovered publish = %v, %v", result, err)
	}
	if _, ok := artifact.MatchesProtectedReceipt(desired.Reveal(), mustRead(t, restarted.receiptPath())); !ok {
		t.Fatal("recovery did not complete receipt")
	}
}

func TestPublisherRejectsUnsafeOwnershipAndCancellation(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	if err := os.WriteFile(target, []byte("user-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), validated(t, []byte("candidate"))); !errors.Is(err, ErrOwnership) {
		t.Fatalf("unmanaged error = %v", err)
	}
	if got := mustRead(t, target); string(got) != "user-owned" {
		t.Fatal("unmanaged target changed")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := publisher.Publish(ctx, validated(t, []byte("candidate"))); !errors.Is(err, ErrPublication) {
		t.Fatalf("cancel error = %v", err)
	}
	if _, err := publisher.Publish(context.Background(), artifact.Validated{}); !errors.Is(err, ErrPublication) {
		t.Fatalf("zero artifact error = %v", err)
	}
}

func TestPublisherRejectsUnsafeFilesystemLayout(t *testing.T) {
	t.Parallel()
	t.Run("missing directory", func(t *testing.T) {
		_, err := NewFilePublisher(filepath.Join(t.TempDir(), "missing", "config.json"))
		if !errors.Is(err, ErrTarget) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("public directory", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := NewFilePublisher(filepath.Join(directory, "config.json"))
		if !errors.Is(err, ErrTarget) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("symlink target", func(t *testing.T) {
		directory := privateTempDir(t)
		realTarget := filepath.Join(directory, "real.json")
		if err := os.WriteFile(realTarget, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "config.json")
		if err := os.Symlink(realTarget, link); err != nil {
			t.Fatal(err)
		}
		_, err := NewFilePublisher(link)
		if !errors.Is(err, ErrTarget) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("public managed file", func(t *testing.T) {
		directory := privateTempDir(t)
		target := filepath.Join(directory, "config.json")
		publisher, err := NewFilePublisher(target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := publisher.Publish(context.Background(), validated(t, []byte("secret"))); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := publisher.Publish(context.Background(), validated(t, []byte("other"))); !errors.Is(err, ErrTarget) {
			t.Fatalf("error = %v", err)
		}
		if got := string(mustRead(t, target)); got != "secret" {
			t.Fatal("unsafe target changed")
		}
	})
}

func TestRecoveryRemovesOrphanTemporaryFiles(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	orphan := filepath.Join(directory, ".config.json.egressfox-candidate-orphan")
	if err := os.WriteFile(orphan, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), validated(t, []byte("candidate"))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan temporary file remained")
	}
}

func TestPublisherSerializesConcurrentAttempts(t *testing.T) {
	t.Parallel()
	directory := privateTempDir(t)
	target := filepath.Join(directory, "config.json")
	publisher, err := NewFilePublisher(target)
	if err != nil {
		t.Fatal(err)
	}
	values := []artifact.Validated{validated(t, []byte("first")), validated(t, []byte("second"))}
	var wait sync.WaitGroup
	errorsSeen := make(chan error, len(values))
	for _, value := range values {
		wait.Add(1)
		go func(value artifact.Validated) {
			defer wait.Done()
			_, err := publisher.Publish(context.Background(), value)
			errorsSeen <- err
		}(value)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	content := mustRead(t, target)
	if _, ok := artifact.MatchesProtectedReceipt(content, mustRead(t, publisher.receiptPath())); !ok {
		t.Fatal("concurrent result is inconsistent")
	}
}

func privateTempDir(t testing.TB) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}
func validated(t testing.TB, content []byte) artifact.Validated {
	t.Helper()
	candidate, err := artifact.NewCandidate(artifact.SingBox1141, content)
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.Validate(context.Background(), candidate, acceptChecker{})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustRead(t testing.TB, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func assertFile(t testing.TB, path string, want []byte, mode os.FileMode) {
	t.Helper()
	got := mustRead(t, path)
	if string(got) != string(want) {
		t.Fatalf("file %s content mismatch", filepath.Base(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("mode = %o, want %o", info.Mode().Perm(), mode)
	}
}
