package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publicationStep is the immutability-critical part of the protected workflow.
// The guard tests mutate copies of it, never the repository file.
const publicationStep = `      - name: Create release and upload verified files
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
          gh release create "$VERSION" --verify-tag --notes-file dist/release-notes.md $prerelease
          gh release upload "$VERSION" dist/release/*
`

func publicationWorkflow() string {
	return `name: Release validation and publication

on:
  workflow_dispatch:

concurrency:
  group: release-publication
  cancel-in-progress: false
  queue: max

jobs:
  validate:
    name: Non-publishing release validation
    runs-on: ubuntu-24.04
    steps:
      - name: Verify selected ref and version
        run: |
          if [ "$PUBLISH" = true ]; then
            test "$GITHUB_REF_TYPE" = tag
            test "$GITHUB_REF_NAME" = "$VERSION"
          fi
  publish:
    name: Publish signed release
    if: ${{ inputs.publish == true }}
    runs-on: ubuntu-24.04
    environment: release
    permissions:
      contents: write
      packages: write
      id-token: write
      attestations: write
    steps:
      - name: Recheck trusted tag
        run: |
          test "$GITHUB_REF_TYPE" = tag
          test "$GITHUB_REF_NAME" = "$VERSION"
          test -z "$(git status --porcelain)"
      - name: Construct release files without registry writes
        run: make release-dry-run
      - name: Prepare remote publication
        run: |
          python3 hack/release-notes.py --version "$VERSION" --output dist/release-notes.md
          python3 hack/release-preflight.py
      - name: Publish image
        run: |
          python3 hack/release-preflight.py
          docker buildx build --platform linux/amd64,linux/arm64 --push .
` + publicationStep
}

// formatTolerantPublicationWorkflow is the same contract written with harmless
// layout differences: two-space indentation, quoted scalars, reordered release
// flags and a different block-scalar modifier.
func formatTolerantPublicationWorkflow() string {
	return `name: Release validation and publication
concurrency:
  group: release-publication
  cancel-in-progress: false
  queue: max
jobs:
  publish:
    name: Publish signed release
    environment: "release"
    if: ${{ inputs.publish == true }}
    permissions:
      packages: write
      "id-token": write
      contents: 'write'
      attestations: write
    runs-on: ubuntu-24.04
    steps:
    - name: Check the exact tag and a clean tree
      run: |-
        test "$GITHUB_REF_TYPE" = tag
        test "$GITHUB_REF_NAME" = "$VERSION"
        test -z "$(git status --porcelain)"
    - name: Qualify
      run: make release-dry-run
    - name: Check
      run: |
        python3 hack/release-notes.py --version "$VERSION" --output dist/release-notes.md
        python3 hack/release-preflight.py
    - name: Image
      run: |
        python3 hack/release-preflight.py
        docker buildx build --platform linux/amd64,linux/arm64 --push .
    - name: Publish
      run: |
        if gh release view "$VERSION" >/dev/null 2>&1; then
          echo "release $VERSION already exists; published versions are immutable" >&2
          exit 1
        fi
        gh release create "$VERSION" --notes-file dist/release-notes.md --verify-tag $prerelease
        gh release upload "$VERSION" dist/release/*
`
}

func TestReleaseWorkflowSatisfiesPublicationContract(t *testing.T) {
	if err := releaseWorkflowChecks(publicationWorkflow()); err != nil {
		t.Fatalf("publication workflow must satisfy the immutability contract: %v", err)
	}
	actual, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseWorkflowChecks(string(actual)); err != nil {
		t.Fatalf("repository release workflow must satisfy the immutability contract: %v", err)
	}
}

func TestReleaseWorkflowCheckoutsFetchFullHistory(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(content)
	for _, job := range []string{"validate", "publish"} {
		t.Run(job, func(t *testing.T) {
			start := strings.Index(workflow, "\n  "+job+":\n")
			if start < 0 {
				t.Fatalf("missing %s job", job)
			}
			steps := workflow[start:]
			if job == "validate" {
				steps = strings.SplitN(steps, "\n  publish:\n", 2)[0]
			}
			checkout := strings.Index(steps, "      - uses: actions/checkout@")
			if checkout < 0 {
				t.Fatal("missing checkout step")
			}
			step := steps[checkout:]
			if next := strings.Index(step[1:], "\n      - "); next >= 0 {
				step = step[:next+1]
			}
			if !strings.Contains(step, "\n          fetch-depth: 0\n") {
				t.Fatal("checkout must fetch full history and tags for changelog verification")
			}
		})
	}
}

// TestReleaseWorkflowAcceptsHarmlessFormattingChanges keeps CI independent of
// YAML layout: reindenting, requoting, reordering flags or adding comments must
// never be reported as a lost release guarantee.
func TestReleaseWorkflowAcceptsHarmlessFormattingChanges(t *testing.T) {
	if err := releaseWorkflowChecks(formatTolerantPublicationWorkflow()); err != nil {
		t.Fatalf("formatting changes must not break the contract check: %v", err)
	}
	commented := strings.Replace(publicationWorkflow(), "          gh release upload",
		"          # keep assets immutable\n          gh release upload", 1)
	if err := releaseWorkflowChecks(commented); err != nil {
		t.Fatalf("added comments must not break the contract check: %v", err)
	}
}

func TestReleaseWorkflowRejectsAssetClobbering(t *testing.T) {
	clobbering := strings.Replace(publicationWorkflow(),
		`gh release upload "$VERSION" dist/release/*`,
		`gh release upload "$VERSION" dist/release/* --clobber`, 1)
	if err := releaseWorkflowChecks(clobbering); err == nil {
		t.Fatal("workflow that clobbers existing assets must be rejected")
	}
}

func TestReleaseWorkflowRejectsImageBeforePreflight(t *testing.T) {
	original := publicationWorkflow()
	image := "      - name: Publish image\n        run: |\n          python3 hack/release-preflight.py\n          docker buildx build --platform linux/amd64,linux/arm64 --push .\n"
	if !strings.Contains(original, image) {
		t.Fatal("missing image fixture")
	}
	changed := strings.Replace(original, image, "", 1)
	changed = strings.Replace(changed, "      - name: Prepare remote publication", image+"      - name: Prepare remote publication", 1)
	if err := releaseWorkflowChecks(changed); err == nil {
		t.Fatal("image publication before preflight must fail")
	}
}

func TestReleaseWorkflowRejectsConcurrentPublication(t *testing.T) {
	changed := strings.Replace(publicationWorkflow(), "  group: release-publication", "  group: release-${{ inputs.version }}", 1)
	if err := releaseWorkflowChecks(changed); err == nil {
		t.Fatal("independent version groups must not publish concurrently")
	}
}

func TestReleaseWorkflowNeverCommitsPreparation(t *testing.T) {
	changed := strings.Replace(publicationWorkflow(), "        run: make release-dry-run", "        run: make release-prepare", 1)
	if err := releaseWorkflowChecks(changed); err == nil {
		t.Fatal("public release workflow must never prepare a changelog commit")
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
	if err := releaseWorkflowChecks(overwriting); err == nil {
		t.Fatal("workflow that overwrites an existing release must be rejected")
	}
}

func TestReleaseWorkflowRejectsMissingImmutabilityGuarantee(t *testing.T) {
	mutate := func(t *testing.T, content, from, to string) string {
		t.Helper()
		if !strings.Contains(content, from) {
			t.Fatalf("fixture no longer contains %q", from)
		}
		mutated := strings.Replace(content, from, to, 1)
		if mutated == content {
			t.Fatalf("mutation %q had no effect", from)
		}
		return mutated
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
		"release deleted before recreation": func(t *testing.T, content string) string {
			return mutate(t, content, `          gh release create "$VERSION"`,
				`          gh release delete "$VERSION" --yes || true
          gh release create "$VERSION"`)
		},
		"tag check replaced by a branch check": func(t *testing.T, content string) string {
			return mutate(t, content, `          test "$GITHUB_REF_TYPE" = tag
          test "$GITHUB_REF_NAME" = "$VERSION"
          test -z "$(git status --porcelain)"`,
				`          test "$GITHUB_REF_TYPE" = branch
          test "$GITHUB_REF_NAME" = "$VERSION"
          test -z "$(git status --porcelain)"`)
		},
		"version not bound to the selected ref": func(t *testing.T, content string) string {
			return mutate(t, content, `          test "$GITHUB_REF_TYPE" = tag
          test "$GITHUB_REF_NAME" = "$VERSION"
          test -z "$(git status --porcelain)"`,
				`          test "$GITHUB_REF_TYPE" = tag
          test -z "$(git status --porcelain)"`)
		},
		"unprotected environment": func(t *testing.T, content string) string {
			return mutate(t, content, "    environment: release\n", "")
		},
		"job no longer requires the publish input": func(t *testing.T, content string) string {
			return mutate(t, content, "    if: ${{ inputs.publish == true }}\n", "")
		},
		"signing permissions withdrawn": func(t *testing.T, content string) string {
			return mutate(t, content, "      id-token: write\n", "")
		},
		"dirty publication allowed": func(t *testing.T, content string) string {
			return mutate(t, content, `          test -z "$(git status --porcelain)"`, "          true")
		},
		"release qualification skipped": func(t *testing.T, content string) string {
			return mutate(t, content, "        run: make release-dry-run\n", "        run: echo skipped\n")
		},
		"existing tag not required": func(t *testing.T, content string) string {
			return mutate(t, content, " --verify-tag", "")
		},
		"release write made conditional": func(t *testing.T, content string) string {
			return mutate(t, content, `      - name: Create release and upload verified files
        run: |`, `      - name: Create release and upload verified files
        if: always()
        run: |`)
		},
		"asset upload removed": func(t *testing.T, content string) string {
			return mutate(t, content, `          gh release upload "$VERSION" dist/release/*`, "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := releaseWorkflowChecks(mutateFixture(t, publicationWorkflow())); err == nil {
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
