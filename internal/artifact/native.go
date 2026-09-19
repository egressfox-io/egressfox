package artifact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var ErrValidation = errors.New("artifact validation failed")

type NativeChecker struct {
	profile Profile
	binary  string
	timeout time.Duration
}

func NewNativeChecker(profile Profile, binary string, timeout time.Duration) (NativeChecker, error) {
	if !filepath.IsAbs(binary) || timeout <= 0 {
		return NativeChecker{}, errors.New("native checker requires an absolute binary path and positive timeout")
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o111 == 0 {
		return NativeChecker{}, errors.New("native checker binary must be an executable regular file")
	}
	if profile != Mihomo11931 && profile != SingBox1141 {
		return NativeChecker{}, errors.New("native checker profile is unsupported")
	}
	return NativeChecker{profile: profile, binary: binary, timeout: timeout}, nil
}

func (checker NativeChecker) Check(ctx context.Context, candidate Candidate) (Evidence, error) {
	if candidate.profile != checker.profile {
		return Evidence{}, validationFailure(checker.profile, "profile_mismatch")
	}
	ctx, cancel := context.WithTimeout(ctx, checker.timeout)
	defer cancel()
	versionArgs := []string{"version"}
	if checker.profile.Engine == EngineMihomo {
		versionArgs = []string{"-v"}
	}
	versionOutput, err := checker.run(ctx, "", versionArgs...)
	if err != nil {
		return Evidence{}, validationFailure(checker.profile, contextCode(ctx, "version_failed"))
	}
	if !strings.Contains(versionOutput, checker.profile.Version) {
		return Evidence{}, validationFailure(checker.profile, "version_mismatch")
	}
	directory, err := os.MkdirTemp("", "egressfox-validator-")
	if err != nil {
		return Evidence{}, validationFailure(checker.profile, "workspace_failed")
	}
	defer os.RemoveAll(directory)
	if err := os.Chmod(directory, 0o700); err != nil {
		return Evidence{}, validationFailure(checker.profile, "workspace_failed")
	}
	extension := ".json"
	if checker.profile.Engine == EngineMihomo {
		extension = ".yaml"
	}
	configPath := filepath.Join(directory, "candidate"+extension)
	if err := os.WriteFile(configPath, candidate.content, 0o600); err != nil {
		return Evidence{}, validationFailure(checker.profile, "workspace_failed")
	}
	var args []string
	if checker.profile.Engine == EngineMihomo {
		args = []string{"-t", "-f", configPath, "-d", directory}
	} else {
		args = []string{"check", "-c", configPath, "-D", directory}
	}
	if _, err := checker.run(ctx, directory, args...); err != nil {
		return Evidence{}, validationFailure(checker.profile, contextCode(ctx, "candidate_rejected"))
	}
	return Evidence{ValidatorID: "native/" + checker.profile.Engine.String() + "/" + checker.profile.Version}, nil
}

func (checker NativeChecker) run(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, checker.binary, args...)
	command.Dir = directory
	home := directory
	if home == "" {
		home = os.TempDir()
	}
	command.Env = []string{"HOME=" + home, "TMPDIR=" + home, "LANG=C", "LC_ALL=C"}
	output := &limitedBuffer{limit: 16 << 10}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	return output.String(), err
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if remaining := w.limit - w.buffer.Len(); remaining > 0 {
		if len(p) > remaining {
			_, _ = w.buffer.Write(p[:remaining])
		} else {
			_, _ = w.buffer.Write(p)
		}
	}
	return len(p), nil
}
func (w *limitedBuffer) String() string { return w.buffer.String() }

type ValidationError struct {
	profile Profile
	code    string
}

func validationFailure(profile Profile, code string) error { return &ValidationError{profile, code} }
func (e *ValidationError) Error() string {
	return fmt.Sprintf("artifact validation failed profile=%s code=%s", e.profile, e.code)
}
func (e *ValidationError) Unwrap() error { return ErrValidation }
func (e *ValidationError) Code() string  { return e.code }
func contextCode(ctx context.Context, fallback string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	return fallback
}
