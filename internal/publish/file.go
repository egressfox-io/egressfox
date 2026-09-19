package publish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egressfox-io/egressfox/internal/artifact"
)

var (
	ErrTarget      = errors.New("invalid publication target")
	ErrOwnership   = errors.New("publication ownership conflict")
	ErrPublication = errors.New("artifact publication failed")
)

type Result struct {
	Profile artifact.Profile
	Changed bool
}

func (r Result) String() string {
	return fmt.Sprintf("publication result profile=%s changed=%t", r.Profile, r.Changed)
}

type FilePublisher struct {
	mu      sync.Mutex
	target  string
	rename  func(string, string) error
	syncDir func(string) error
}

func NewFilePublisher(target string) (*FilePublisher, error) {
	absolute, err := filepath.Abs(target)
	if err != nil || filepath.Base(absolute) == "." || filepath.Base(absolute) == string(filepath.Separator) {
		return nil, targetError("invalid_path")
	}
	publisher := &FilePublisher{target: filepath.Clean(absolute), rename: os.Rename, syncDir: syncDirectory}
	if err := publisher.validateDirectory(); err != nil {
		return nil, err
	}
	return publisher, nil
}

func (p *FilePublisher) String() string { return "file publisher target=<redacted>" }

// CurrentReceipt returns protected evidence for the currently owned LKG after
// applying the same crash recovery and ownership checks as publication.
func (p *FilePublisher) CurrentReceipt() (artifact.Receipt, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.validateDirectory(); err != nil {
		return artifact.Receipt{}, false, err
	}
	if err := p.recover(); err != nil {
		return artifact.Receipt{}, false, err
	}
	_, encoded, exists, err := p.currentOwned()
	if err != nil || !exists {
		return artifact.Receipt{}, exists, err
	}
	receipt, err := artifact.RestoreReceipt(encoded)
	if err != nil {
		return artifact.Receipt{}, false, ownershipError("receipt_decode")
	}
	return receipt, true, nil
}

func (p *FilePublisher) Publish(ctx context.Context, validated artifact.Validated) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.validateDirectory(); err != nil {
		return Result{}, err
	}
	if err := p.recover(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, publicationError("cancelled")
	}

	current, currentReceipt, exists, err := p.currentOwned()
	if err != nil {
		return Result{}, err
	}
	desired := validated.Reveal()
	desiredReceipt, err := validated.ProtectedReceipt()
	if err != nil {
		return Result{}, publicationError("receipt_encode")
	}
	if exists && bytes.Equal(current, desired) {
		if profile, ok := artifact.MatchesProtectedReceipt(current, currentReceipt); ok && profile == validated.Profile() {
			return Result{Profile: profile, Changed: false}, nil
		}
		return Result{}, ownershipError("receipt_mismatch")
	}

	stage, err := p.stage("candidate", desired)
	if err != nil {
		return Result{}, publicationError("stage")
	}
	defer os.Remove(stage)
	if exists {
		backup, backupErr := p.stage("previous", current)
		if backupErr != nil {
			return Result{}, publicationError("backup_stage")
		}
		defer os.Remove(backup)
		if err := p.rename(backup, p.previousPath()); err != nil {
			return Result{}, publicationError("backup_commit")
		}
		if err := p.syncDir(filepath.Dir(p.target)); err != nil {
			return Result{}, publicationError("backup_sync")
		}
	}
	if err := p.writeAtomic(p.journalPath(), desiredReceipt); err != nil {
		return Result{}, publicationError("journal")
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(p.journalPath())
		_ = p.syncDir(filepath.Dir(p.target))
		return Result{}, publicationError("cancelled")
	}
	if err := p.rename(stage, p.target); err != nil {
		_ = os.Remove(p.journalPath())
		_ = p.syncDir(filepath.Dir(p.target))
		return Result{}, publicationError("target_commit")
	}
	if err := p.syncDir(filepath.Dir(p.target)); err != nil {
		return Result{}, p.rollback(exists, currentReceipt, "target_sync")
	}
	readback, err := readRegular(p.target, len(desired)+1)
	if err != nil || !bytes.Equal(readback, desired) {
		return Result{}, p.rollback(exists, currentReceipt, "readback")
	}
	if err := p.writeAtomic(p.receiptPath(), desiredReceipt); err != nil {
		return Result{}, p.rollback(exists, currentReceipt, "receipt_commit")
	}
	if err := os.Remove(p.journalPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, publicationError("journal_cleanup")
	}
	if err := p.syncDir(filepath.Dir(p.target)); err != nil {
		return Result{}, publicationError("receipt_sync")
	}
	return Result{Profile: validated.Profile(), Changed: true}, nil
}

func (p *FilePublisher) validateDirectory() error {
	directory := filepath.Dir(p.target)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return targetError("directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return targetError("directory_permissions")
	}
	for _, path := range []string{p.target, p.receiptPath(), p.previousPath(), p.journalPath()} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return targetError("unsafe_path")
		}
		if info.Mode().Perm()&0o077 != 0 {
			return targetError("file_permissions")
		}
	}
	return nil
}

func (p *FilePublisher) recover() error {
	journal, err := readRegular(p.journalPath(), 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return p.cleanupTemps()
	}
	if err != nil {
		return ownershipError("journal")
	}
	content, targetErr := readRegular(p.target, 64<<20)
	if targetErr == nil {
		if _, ok := artifact.MatchesProtectedReceipt(content, journal); ok {
			if err := p.writeAtomic(p.receiptPath(), journal); err != nil {
				return publicationError("recovery_receipt")
			}
		}
	}
	if err := os.Remove(p.journalPath()); err != nil {
		return publicationError("recovery_cleanup")
	}
	if err := p.syncDir(filepath.Dir(p.target)); err != nil {
		return publicationError("recovery_sync")
	}
	return p.cleanupTemps()
}

func (p *FilePublisher) currentOwned() ([]byte, []byte, bool, error) {
	content, err := readRegular(p.target, 64<<20)
	if errors.Is(err, os.ErrNotExist) {
		if _, receiptErr := os.Lstat(p.receiptPath()); receiptErr == nil {
			return nil, nil, false, ownershipError("orphan_receipt")
		}
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, targetError("target_read")
	}
	receipt, err := readRegular(p.receiptPath(), 64<<10)
	if err != nil {
		return nil, nil, false, ownershipError("unmanaged_target")
	}
	if _, ok := artifact.MatchesProtectedReceipt(content, receipt); !ok {
		return nil, nil, false, ownershipError("receipt_mismatch")
	}
	return content, receipt, true, nil
}

func (p *FilePublisher) stage(kind string, content []byte) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(p.target), "."+filepath.Base(p.target)+".egressfox-"+kind+"-")
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		if !ok {
			_ = file.Close()
			_ = os.Remove(name)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func (p *FilePublisher) writeAtomic(path string, content []byte) error {
	stage, err := p.stage("metadata", content)
	if err != nil {
		return err
	}
	defer os.Remove(stage)
	if err := p.rename(stage, path); err != nil {
		return err
	}
	return p.syncDir(filepath.Dir(p.target))
}

func (p *FilePublisher) rollback(hadPrevious bool, previousReceipt []byte, code string) error {
	recovered := true
	if hadPrevious {
		if err := p.rename(p.previousPath(), p.target); err != nil {
			recovered = false
		}
		if err := p.writeAtomic(p.receiptPath(), previousReceipt); err != nil {
			recovered = false
		}
	} else {
		if err := os.Remove(p.target); err != nil && !errors.Is(err, os.ErrNotExist) {
			recovered = false
		}
		if err := os.Remove(p.receiptPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			recovered = false
		}
	}
	_ = os.Remove(p.journalPath())
	_ = p.syncDir(filepath.Dir(p.target))
	if !recovered {
		return publicationError("rollback_failed")
	}
	return publicationError(code)
}

func (p *FilePublisher) cleanupTemps() error {
	entries, err := os.ReadDir(filepath.Dir(p.target))
	if err != nil {
		return publicationError("temp_scan")
	}
	prefix := "." + filepath.Base(p.target) + ".egressfox-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) && entry.Type().IsRegular() {
			_ = os.Remove(filepath.Join(filepath.Dir(p.target), entry.Name()))
		}
	}
	return nil
}

func (p *FilePublisher) receiptPath() string  { return p.target + ".egressfox-receipt" }
func (p *FilePublisher) previousPath() string { return p.target + ".egressfox-previous" }
func (p *FilePublisher) journalPath() string  { return p.target + ".egressfox-journal" }

func readRegular(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, targetError("unsafe_path")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(content) > limit {
		return nil, targetError("file_too_large")
	}
	return content, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

type Failure struct {
	kind error
	code string
}

func (e *Failure) Error() string         { return fmt.Sprintf("%s (%s)", e.kind, e.code) }
func (e *Failure) Unwrap() error         { return e.kind }
func (e *Failure) Code() string          { return e.code }
func targetError(code string) error      { return &Failure{ErrTarget, code} }
func ownershipError(code string) error   { return &Failure{ErrOwnership, code} }
func publicationError(code string) error { return &Failure{ErrPublication, code} }
