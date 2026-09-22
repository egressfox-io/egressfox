package buildinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRepository creates a throwaway Git repository containing the release
// manifest the identity resolution reads. It never touches the caller's
// repository.
func fixtureRepository(t *testing.T, developmentVersion string) string {
	t.Helper()
	root := t.TempDir()
	manifest := `{"schemaVersion":1,"release":{"developmentVersion":"` + developmentVersion +
		`","platforms":["linux/amd64"],"kubernetes":{"minimumSupported":"1.32","releaseValidation":["1.32"],"profiles":[]}},"engines":[],"tools":[]}`
	writeFixtureFile(t, filepath.Join(root, "release", "manifest.json"), manifest)
	writeFixtureFile(t, filepath.Join(root, "source.txt"), "fixture\n")
	runGit(t, root, "init", "--quiet", "--initial-branch=main")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "--quiet", "-m", "fixture")
	return root
}

func writeFixtureFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
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
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestResolveIdentityCleanUntaggedBuild(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	revision := runGit(t, root, "rev-parse", "HEAD")

	identity, err := ResolveIdentity(root, "v0.1.0-dev.1")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Tagged || identity.Dirty {
		t.Fatalf("clean untagged identity is tagged=%v dirty=%v", identity.Tagged, identity.Dirty)
	}
	if want := "0.1.0-dev.1+g" + revision[:12]; identity.EffectiveVersion() != want {
		t.Fatalf("clean identity = %q, want %q", identity.EffectiveVersion(), want)
	} else if strings.Contains(identity.EffectiveVersion(), DirtySuffix) {
		t.Fatalf("clean identity = %q must not contain %q", identity.EffectiveVersion(), DirtySuffix)
	}
	if identity.Revision != revision {
		t.Fatalf("identity revision = %q, want %q", identity.Revision, revision)
	}
	if identity.Created == "" {
		t.Fatal("clean identity must carry the commit timestamp")
	}
}

func TestResolveIdentityDirtyUntaggedBuild(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	revision := runGit(t, root, "rev-parse", "HEAD")
	clean, err := ResolveIdentity(root, "v0.1.0-dev.1")
	if err != nil {
		t.Fatal(err)
	}

	writeFixtureFile(t, filepath.Join(root, "source.txt"), "changed\n")
	identity, err := ResolveIdentity(root, "v0.1.0-dev.1")
	if err != nil {
		t.Fatal(err)
	}
	if !identity.Dirty {
		t.Fatal("modified tracked file must mark the tree dirty")
	}
	if want := "0.1.0-dev.1+g" + revision[:12] + DirtySuffix; identity.EffectiveVersion() != want {
		t.Fatalf("dirty identity = %q, want %q", identity.EffectiveVersion(), want)
	}
	if identity.EffectiveVersion() == clean.EffectiveVersion() {
		t.Fatal("dirty build must not claim the clean identity of the same commit")
	}
}

func TestResolveIdentityUntrackedSourceMarksDirty(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	writeFixtureFile(t, filepath.Join(root, "new-source.go"), "package fixture\n")
	identity, err := ResolveIdentity(root, "v0.1.0-dev.1")
	if err != nil {
		t.Fatal(err)
	}
	if !identity.Dirty {
		t.Fatal("untracked non-ignored file must mark the tree dirty")
	}
}

func TestResolveIdentityIgnoredOutputStaysClean(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	writeFixtureFile(t, filepath.Join(root, ".gitignore"), "/dist/\n/.cache/\n")
	writeFixtureFile(t, filepath.Join(root, "dist", "release", "artifact.tar.gz"), "ignored release output\n")
	writeFixtureFile(t, filepath.Join(root, ".cache", "release-tools", "syft"), "ignored tool\n")
	runGit(t, root, "add", ".gitignore")
	runGit(t, root, "commit", "--quiet", "-m", "ignore build output")

	identity, err := ResolveIdentity(root, "v0.1.0-dev.1")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Dirty {
		t.Fatalf("ignored release output must not mark the tree dirty: %s", identity.EffectiveVersion())
	}
}

func TestResolveIdentityTaggedRelease(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	runGit(t, root, "tag", "v0.1.0-dev.1")

	identity, err := ResolveIdentity(root, "v0.1.0-dev.1")
	if err != nil {
		t.Fatal(err)
	}
	if !identity.Tagged {
		t.Fatal("release tag on HEAD must produce a tagged identity")
	}
	if identity.EffectiveVersion() != "0.1.0-dev.1" {
		t.Fatalf("tagged identity = %q, want 0.1.0-dev.1", identity.EffectiveVersion())
	}
	if strings.Contains(identity.EffectiveVersion(), DirtySuffix) {
		t.Fatal("tagged identity must never contain the dirty suffix")
	}
}

func TestResolveIdentityTaggedReleaseRejectsDirtyTree(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	runGit(t, root, "tag", "v0.1.0-dev.1")
	writeFixtureFile(t, filepath.Join(root, "source.txt"), "changed\n")

	if _, err := ResolveIdentity(root, "v0.1.0-dev.1"); err == nil {
		t.Fatal("a tagged release identity must fail closed on a dirty tree")
	}
}

func TestResolveIdentityRejectsAmbiguousTags(t *testing.T) {
	root := fixtureRepository(t, "v0.1.0-dev.1")
	runGit(t, root, "tag", "v0.1.0-dev.1")
	runGit(t, root, "tag", "v0.1.0-dev.2")

	if _, err := ResolveIdentity(root, "v0.1.0-dev.1"); err == nil {
		t.Fatal("multiple release tags on one commit must fail")
	}
}

func TestParseVersionContract(t *testing.T) {
	for _, test := range []struct{ value, class, normalized string }{
		{"v0.1.0-dev.1", "dev", "0.1.0-dev.1"},
		{"v0.1.0-dev.2", "dev", "0.1.0-dev.2"},
		{"v0.1.0-alpha.1", "alpha", "0.1.0-alpha.1"},
		{"v0.1.0-beta.1", "beta", "0.1.0-beta.1"},
		{"v0.1.0", "stable", "0.1.0"},
		{"0.1.0-dev.1", "dev", "0.1.0-dev.1"},
		{"0.1.0", "stable", "0.1.0"},
	} {
		parsed, err := ParseVersion(test.value)
		if err != nil || parsed.Class != test.class || parsed.Normalized != test.normalized {
			t.Errorf("ParseVersion(%q) = %#v, %v", test.value, parsed, err)
		}
	}
	for _, value := range []string{"", "0.1", "v0.1", "v0.01.0", "v0.1.0+build.1", "v0.1.0-rc.1", "v0.1.0-dev.0", "v0.1.0-dev", "0.1.0-dev.1.dirty"} {
		if _, err := ParseVersion(value); err == nil {
			t.Errorf("invalid version accepted: %s", value)
		}
	}
}
