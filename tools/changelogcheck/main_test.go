package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func str(value string) *string { return &value }

func TestCheck(t *testing.T) {
	base := "# Changelog\n\n## [Unreleased]\n\n### Added\n- Keep existing entry.\n\n## [0.1.0-alpha.1]\n\n### Fixed\n- Old fix.\n"
	valid := "# Changelog\n\n## [Unreleased]\n\n### Added\n- Keep existing entry.\n- Add a meaningful new capability.\n\n## [0.1.0-alpha.1]\n\n### Fixed\n- Old fix.\n"
	changedEntry := "# Changelog\n\n## [Unreleased]\n\n### Added\n- Keep the existing entry with a meaningful update.\n\n## [0.1.0-alpha.1]\n\n### Fixed\n- Old fix.\n"
	updatedHistoryOnly := "# Changelog\n\n## [Unreleased]\n\n### Added\n- Keep existing entry.\n\n## [0.1.0-alpha.1]\n\n### Fixed\n- Corrected an old release note.\n"
	noBoxes := "## Changelog\n- [ ] CHANGELOG.md updated under Unreleased.\n- [ ] No changelog entry is required.\nReason:\n"
	request := "## Changelog\n- [ ] CHANGELOG.md updated under Unreleased.\n- [x] No changelog entry is required.\nReason:\nTest-only coverage; no shipped behavior changes.\n"
	type testCase struct {
		name       string
		paths      []string
		base, head *string
		body       string
		label      bool
		wantPass   bool
	}
	cases := []testCase{
		{"new meaningful Unreleased entry passes", []string{"internal/endpoint/address.go", "CHANGELOG.md"}, str(base), str(valid), noBoxes, false, true},
		{"meaningful edit to an Unreleased entry passes", []string{"internal/endpoint/address.go", "CHANGELOG.md"}, str(base), str(changedEntry), noBoxes, false, true},
		{"source change plus older-section-only edit fails", []string{"internal/endpoint/address.go", "CHANGELOG.md"}, str(base), str(updatedHistoryOnly), noBoxes, false, false},
		{"required entry missing without exemption fails", []string{"internal/endpoint/address.go"}, str(base), str(base), noBoxes, false, false},
		{"internal documentation-only change is automatically exempt", []string{"docs/development/testing.md"}, str(base), str(base), noBoxes, false, true},
		{"justified authorized exemption passes", []string{"internal/endpoint/address.go"}, str(base), str(base), request, true, true},
		{"unauthorized exemption request fails", []string{"internal/endpoint/address.go"}, str(base), str(base), request, false, false},
		{"contradictory checkbox selections fail even with label", []string{"internal/endpoint/address.go"}, str(base), str(base), "## Changelog\n- [x] CHANGELOG.md updated under Unreleased.\n- [x] No changelog entry is required.\nReason:\nReviewed.\n", true, false},
		{"whitespace-only Unreleased edit fails", []string{"CHANGELOG.md", "internal/endpoint/address.go"}, str(base), str(strings.Replace(base, "- Keep existing entry.", "- Keep existing entry.   ", 1)), noBoxes, false, false},
		{"initial changelog creation with entry passes", []string{"CHANGELOG.md", "internal/endpoint/address.go"}, nil, str("# Changelog\n\n## [Unreleased]\n\n### Added\n- Add the first supported capability.\n"), noBoxes, false, true},
		{"product code plus README is not automatically documentation-only", []string{"internal/endpoint/address.go", "README.md"}, str(base), str(base), noBoxes, false, false},
		{"historical-only changelog edit does not satisfy requirement", []string{"CHANGELOG.md"}, str(base), str(updatedHistoryOnly), noBoxes, false, false},
		{"placeholder entry does not satisfy requirement", []string{"CHANGELOG.md", "internal/endpoint/address.go"}, str(base), str("# Changelog\n\n## [Unreleased]\n\n### Added\n- TODO\n"), noBoxes, false, false},
		{"commented and fenced entries do not satisfy requirement", []string{"CHANGELOG.md", "internal/endpoint/address.go"}, str(base), str("# Changelog\n\n## [Unreleased]\n\n<!--\n### Added\n- Hidden entry.\n-->\n\n```md\n### Added\n- Fenced entry.\n```\n"), noBoxes, false, false},
		{"maintainer label alone is not an exemption request", []string{"internal/endpoint/address.go"}, str(base), str(base), noBoxes, true, false},
		{"authorized request without a reason fails", []string{"internal/endpoint/address.go"}, str(base), str(base), "## Changelog\n- [ ] CHANGELOG.md updated under Unreleased.\n- [x] No changelog entry is required.\nReason:\n", true, false},
		{"Go test-only change is automatically exempt", []string{"internal/endpoint/address_test.go"}, str(base), str(base), noBoxes, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := check(tc.paths, tc.base, tc.head, tc.body, tc.label)
			if gotPass := err == nil; gotPass != tc.wantPass {
				t.Fatalf("check() passed = %t, want %t; error = %v", gotPass, tc.wantPass, err)
			}
		})
	}
}

func TestUnreleasedEntriesPreserveSemanticEmoji(t *testing.T) {
	contents := "# Changelog\n\n## [Unreleased]\n\n### Added\n- ✨ Add managed runtime\n"
	entries, found, err := unreleasedEntries(contents)
	if err != nil {
		t.Fatalf("unreleasedEntries() error = %v", err)
	}
	if !found {
		t.Fatal("unreleasedEntries() did not find the Unreleased section")
	}
	if _, ok := entries["added\x00✨ add managed runtime"]; !ok {
		t.Fatalf("unreleasedEntries() did not preserve the source emoji: %#v", entries)
	}
}

func TestUntrustedPullRequestMetadataIsNeverEvaluated(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, "init", "-q")
	gitTest(t, repo, "config", "user.email", "test@example.invalid")
	gitTest(t, repo, "config", "user.name", "Test")
	writeTestFile(t, repo, "README.md", "base\n")
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-qm", "base")
	base := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))
	writeTestFile(t, repo, "internal/endpoint/address.go", "package endpoint\n")
	gitTest(t, repo, "add", "internal/endpoint/address.go")
	gitTest(t, repo, "commit", "-qm", "head")
	head := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))

	marker := filepath.Join(t.TempDir(), "metadata-executed")
	attack := "; touch " + marker + "; #"
	event := pullRequestEvent{}
	event.PullRequest.Title = attack
	event.PullRequest.Body = attack
	event.PullRequest.Labels = []label{{Name: attack}}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(repo, "event.json")
	if err := os.WriteFile(eventPath, eventJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("CHANGELOG_BASE_SHA", base)
	t.Setenv("CHANGELOG_HEAD_SHA", head)

	if _, err := run(); err == nil {
		t.Fatal("untrusted metadata unexpectedly bypassed the required entry")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untrusted metadata was evaluated; marker stat error = %v", err)
	}
}

func TestRunAcceptsInitialChangelogCreation(t *testing.T) {
	repo := t.TempDir()
	gitTest(t, repo, "init", "-q")
	gitTest(t, repo, "config", "user.email", "test@example.invalid")
	gitTest(t, repo, "config", "user.name", "Test")
	writeTestFile(t, repo, "README.md", "base\n")
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "commit", "-qm", "base")
	base := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))

	writeTestFile(t, repo, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n### Added\n- Add the first documented capability.\n")
	gitTest(t, repo, "add", "CHANGELOG.md")
	gitTest(t, repo, "commit", "-qm", "add initial changelog")
	head := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))
	eventPath := filepath.Join(repo, "event.json")
	if err := os.WriteFile(eventPath, []byte(`{"pull_request":{"body":"","labels":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("CHANGELOG_BASE_SHA", base)
	t.Setenv("CHANGELOG_HEAD_SHA", head)

	message, err := run()
	if err != nil {
		t.Fatalf("run() rejected a valid initial changelog: %v", err)
	}
	if message != "passed (meaningful entry added under Unreleased)" {
		t.Fatalf("run() message = %q, want valid-entry success", message)
	}
}

func gitTest(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return output
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
