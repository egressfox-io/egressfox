package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publicationStep is the immutability-critical part of the protected workflow.
// The guard tests mutate copies of it, never the repository file.
const publicationStep = `      - name: Verify selected ref and version
        run: |
          if [ "$PUBLISH" = true ]; then
            test "$GITHUB_REF_TYPE" = tag
            test "$GITHUB_REF_NAME" = "$VERSION"
          fi
      - name: Create release and upload verified files
        run: |
          case "$VERSION" in
            *-dev.*|*-alpha.*|*-beta.*) prerelease=--prerelease ;;
            *) prerelease= ;;
          esac
          # A published version is immutable.
          if gh release view "$VERSION" >/dev/null 2>&1; then
            echo "release $VERSION already exists; published versions are immutable" >&2
            echo "publish a new version such as the next -dev.N snapshot instead" >&2
            exit 1
          fi
          gh release create "$VERSION" --verify-tag --generate-notes $prerelease
          gh release upload "$VERSION" dist/release/*
`

func publicationWorkflow() string {
	return `name: Release validation and publication

on:
  workflow_dispatch:

jobs:
  publish:
    name: Publish signed release
    runs-on: ubuntu-24.04
    environment: release
    env:
      VERSION: ${{ inputs.version }}
    steps:
      - name: Recheck trusted tag
        run: |
          test "$GITHUB_REF_TYPE" = tag
          test "$GITHUB_REF_NAME" = "$VERSION"
          test -z "$(git status --porcelain)"
      - name: Construct release files without registry writes
        run: make release-dry-run
` + publicationStep
}

func TestReleaseWorkflowSatisfiesPublicationContract(t *testing.T) {
	if err := checkReleaseWorkflowText(publicationWorkflow()); err != nil {
		t.Fatalf("publication workflow must satisfy the immutability contract: %v", err)
	}
	actual, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseWorkflowText(string(actual)); err != nil {
		t.Fatalf("repository release workflow must satisfy the immutability contract: %v", err)
	}
}

func TestReleaseWorkflowRejectsAssetClobbering(t *testing.T) {
	clobbering := strings.Replace(publicationWorkflow(),
		`gh release upload "$VERSION" dist/release/*`,
		`gh release upload "$VERSION" dist/release/* --clobber`, 1)
	if err := checkReleaseWorkflowText(clobbering); err == nil {
		t.Fatal("workflow that clobbers existing assets must be rejected")
	}
}

func TestReleaseWorkflowRejectsSilentOverwrite(t *testing.T) {
	// The original pre-merge shape: create only when missing, then always
	// upload with clobbering.
	overwriting := strings.Replace(publicationWorkflow(), publicationStep,
		`      - name: Create release and upload verified files
        run: |
          case "$VERSION" in
            *-dev.*|*-alpha.*|*-beta.*) prerelease=--prerelease ;;
            *) prerelease= ;;
          esac
          gh release view "$VERSION" >/dev/null 2>&1 || \
            gh release create "$VERSION" --verify-tag --generate-notes $prerelease
          gh release upload "$VERSION" dist/release/* --clobber
`, 1)
	if err := checkReleaseWorkflowText(overwriting); err == nil {
		t.Fatal("workflow that overwrites an existing release must be rejected")
	}
}

func TestReleaseWorkflowRejectsMissingImmutabilityCheck(t *testing.T) {
	mutate := func(t *testing.T, content, from, to string) string {
		t.Helper()
		if !strings.Contains(content, from) {
			t.Fatalf("fixture no longer contains %q", from)
		}
		return strings.Replace(content, from, to, 1)
	}
	for name, mutateFixture := range map[string]func(*testing.T, string) string{
		"no existing-release check": func(t *testing.T, content string) string {
			return mutate(t, content, `          if gh release view "$VERSION" >/dev/null 2>&1; then
            echo "release $VERSION already exists; published versions are immutable" >&2
            echo "publish a new version such as the next -dev.N snapshot instead" >&2
            exit 1
          fi
`, "")
		},
		"release recreated by deletion": func(t *testing.T, content string) string {
			return mutate(t, content, `          gh release create "$VERSION"`,
				`          gh release delete "$VERSION" --yes || true
          gh release create "$VERSION"`)
		},
		"tag check replaced by a branch check": func(t *testing.T, content string) string {
			return mutate(t, content, `          test "$GITHUB_REF_TYPE" = tag`, `          test "$GITHUB_REF_TYPE" = branch`)
		},
		"version not bound to the selected ref": func(t *testing.T, content string) string {
			return mutate(t, content, `          test "$GITHUB_REF_NAME" = "$VERSION"`, "")
		},
		"unprotected environment": func(t *testing.T, content string) string {
			return mutate(t, content, "    environment: release\n", "")
		},
		"dirty publication allowed": func(t *testing.T, content string) string {
			return mutate(t, content, `          test -z "$(git status --porcelain)"`, "")
		},
		"release qualification skipped": func(t *testing.T, content string) string {
			return mutate(t, content, "        run: make release-dry-run\n", "        run: echo skipped\n")
		},
		"asset upload removed": func(t *testing.T, content string) string {
			return mutate(t, content, `          gh release upload "$VERSION" dist/release/*`, "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkReleaseWorkflowText(mutateFixture(t, publicationWorkflow())); err == nil {
				t.Fatalf("workflow without %s must be rejected", name)
			}
		})
	}
}

func TestCopyOverlay(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "file.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyOverlay(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "nested", "file.go"))
	if err != nil || string(content) != "package fixture\n" {
		t.Fatalf("overlay content=%q err=%v", content, err)
	}
}
