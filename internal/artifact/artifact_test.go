package artifact_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
)

type checker struct {
	id  string
	err error
}

func (c checker) Check(context.Context, artifact.Candidate) (artifact.Evidence, error) {
	return artifact.Evidence{ValidatorID: c.id}, c.err
}

func TestCandidateValidationAndReceiptAreExact(t *testing.T) {
	t.Parallel()
	first := mustCandidate(t, artifact.SingBox1141, []byte(`{"password":"first"}`))
	second := mustCandidate(t, artifact.SingBox1141, []byte(`{"password":"second"}`))
	validated, err := artifact.Validate(context.Background(), first, checker{id: "test/sing-box/1.14.1"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := artifact.Validate(context.Background(), second, checker{id: "test/sing-box/1.14.1"})
	if err != nil {
		t.Fatal(err)
	}
	if validated.Equal(other) {
		t.Fatal("different exact bytes shared a generation")
	}
	receipt, err := validated.ProtectedReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if profile, ok := artifact.MatchesProtectedReceipt(first.Reveal(), receipt); !ok || profile != artifact.SingBox1141 {
		t.Fatal("receipt did not match exact bytes")
	}
	if _, ok := artifact.MatchesProtectedReceipt(second.Reveal(), receipt); ok {
		t.Fatal("receipt matched different bytes")
	}
}

func TestArtifactFormattingDoesNotLeak(t *testing.T) {
	t.Parallel()
	const secret = "artifact-secret-canary"
	candidate := mustCandidate(t, artifact.Mihomo11931, []byte("password: "+secret))
	validated, err := artifact.Validate(context.Background(), candidate, checker{id: "test/mihomo/1.19.31"})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{candidate, validated} {
		formatted := fmt.Sprintf("%v %+v %#v", value, value, value)
		encoded, marshalErr := json.Marshal(value)
		if strings.Contains(formatted+string(encoded)+fmt.Sprint(marshalErr), secret) {
			t.Fatal("artifact formatting leaked content")
		}
	}
}

func TestNativeCheckerUsesPinnedProfileAndSanitizesFailure(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-sing-box")
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo 'sing-box version 1.14.1'; exit 0; fi\necho native-secret-canary >&2\nexit 9\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	native, err := artifact.NewNativeChecker(artifact.SingBox1141, binary, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	candidate := mustCandidate(t, artifact.SingBox1141, []byte(`{"password":"native-secret-canary"}`))
	_, err = artifact.Validate(context.Background(), candidate, native)
	if !errors.Is(err, artifact.ErrValidation) || strings.Contains(err.Error(), "native-secret-canary") {
		t.Fatalf("native error = %v", err)
	}
}

func TestNativeCheckerRejectsNearVersionMatch(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-sing-box")
	script := "#!/bin/sh\necho 'sing-box version 1.14.10'\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	native, err := artifact.NewNativeChecker(artifact.SingBox1141, binary, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	candidate := mustCandidate(t, artifact.SingBox1141, []byte(`{"password":"version-secret-canary"}`))
	_, err = artifact.Validate(context.Background(), candidate, native)
	var validationError *artifact.ValidationError
	if !errors.As(err, &validationError) || validationError.Code() != "version_mismatch" || strings.Contains(err.Error(), "version-secret-canary") {
		t.Fatalf("native error = %v", err)
	}
}

func mustCandidate(t testing.TB, profile artifact.Profile, content []byte) artifact.Candidate {
	t.Helper()
	value, err := artifact.NewCandidate(profile, content)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
