package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// identityFixture creates a throwaway Git repository. The development version
// is supplied explicitly, so the fixture needs no release manifest.
func identityFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"add", "."},
		{"commit", "--quiet", "-m", "fixture"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		command.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=fixture",
			"GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture",
			"GIT_COMMITTER_EMAIL=fixture@example.invalid",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
			"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
	}
	return root
}

func fixtureGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func resolveFixtureVersion(t *testing.T, root string, arguments ...string) (string, error) {
	t.Helper()
	return buildVersion(append([]string{"--root", root, "--development-version", "v0.1.0-dev.1"}, arguments...))
}

func TestBuildVersionCleanAndDirtyIdentity(t *testing.T) {
	root := identityFixture(t)
	revision := fixtureGit(t, root, "rev-parse", "HEAD")
	wantClean := "0.1.0-dev.1+g" + revision[:12]

	clean, err := resolveFixtureVersion(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if clean != wantClean {
		t.Fatalf("clean build version = %q, want %q", clean, wantClean)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirty, err := resolveFixtureVersion(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if want := wantClean + ".dirty"; dirty != want {
		t.Fatalf("dirty build version = %q, want %q", dirty, want)
	}
}

func TestBuildVersionRequireCleanRejectsDirtyTree(t *testing.T) {
	root := identityFixture(t)
	if _, err := resolveFixtureVersion(t, root, "--require-clean"); err != nil {
		t.Fatalf("clean tree must be accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new-source.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := resolveFixtureVersion(t, root, "--require-clean")
	if err == nil {
		t.Fatalf("dirty tree must fail release qualification, got %q", identity)
	}
	if !strings.Contains(err.Error(), "clean working tree") {
		t.Fatalf("error must name the clean-tree requirement: %v", err)
	}
}

func TestBuildVersionRequiresExactVersion(t *testing.T) {
	root := identityFixture(t)
	revision := fixtureGit(t, root, "rev-parse", "HEAD")
	wantUntagged := "0.1.0-dev.1+g" + revision[:12]

	for _, version := range []string{"0.1.0-dev.1", "v0.1.0-dev.1"} {
		identity, err := resolveFixtureVersion(t, root, "--version", version)
		if err != nil {
			t.Fatalf("requested %s: %v", version, err)
		}
		if identity != wantUntagged {
			t.Fatalf("requested %s resolved to %q, want %q", version, identity, wantUntagged)
		}
	}
	for _, version := range []string{"0.1.0-dev.2", "0.1.0"} {
		if identity, err := resolveFixtureVersion(t, root, "--version", version); err == nil {
			t.Fatalf("untagged commit must not satisfy requested version %s, got %q", version, identity)
		}
	}
	if _, err := resolveFixtureVersion(t, root, "--version", "0.1.0-dev.1.dirty"); err == nil {
		t.Fatal("build metadata must not be accepted as a release version")
	}
}

func TestBuildVersionTaggedReleaseIsExact(t *testing.T) {
	root := identityFixture(t)
	command := exec.Command("git", "-C", root, "tag", "v0.1.0-dev.1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git tag: %v\n%s", err, output)
	}
	identity, err := resolveFixtureVersion(t, root, "--version", "0.1.0-dev.1", "--require-clean")
	if err != nil {
		t.Fatal(err)
	}
	if identity != "0.1.0-dev.1" {
		t.Fatalf("tagged build version = %q, want 0.1.0-dev.1", identity)
	}
	if _, err := resolveFixtureVersion(t, root, "--version", "0.1.0-dev.2"); err == nil {
		t.Fatal("a tagged commit must reject a different requested version")
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if identity, err := resolveFixtureVersion(t, root, "--require-clean"); err == nil {
		t.Fatalf("tagged release on a dirty tree must fail, got %q", identity)
	}
}

// TestBuildVersionRepositoryIdentity pins the shipped command to the real
// repository, so a regression in the Makefile wiring is caught by `make test`.
func TestBuildVersionRepositoryIdentity(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	status, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Skipf("Git state unavailable: %v", err)
	}
	identity, err := buildVersion([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSpace(string(status))
	switch {
	case tree == "" && strings.HasSuffix(identity, ".dirty"):
		t.Fatalf("clean repository identity = %q, want no .dirty suffix", identity)
	case tree != "" && !strings.HasSuffix(identity, ".dirty"):
		t.Fatalf("dirty repository identity = %q, want a .dirty suffix", identity)
	}
	if !strings.HasPrefix(identity, "0.1.0-dev.1") {
		t.Fatalf("repository identity = %q, want the planned development version 0.1.0-dev.1", identity)
	}
}
