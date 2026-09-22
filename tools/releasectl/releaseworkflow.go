package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// defaultReleaseWorkflow is the protected publication workflow that must keep
// satisfying the release-immutability contract.
const defaultReleaseWorkflow = ".github/workflows/release.yml"

// ghReleaseCommand matches every GitHub release mutation or query in the
// workflow, so an unrecognized one is reported instead of ignored.
var ghReleaseCommand = regexp.MustCompile(`gh (release [a-z-]+)`)

type workflowCheck struct {
	name      string
	required  []*regexp.Regexp
	forbidden []*regexp.Regexp
}

// releaseWorkflowChecks is the durable publication contract: publication is
// tag-bound and approved, refuses to run twice for one version, and never
// rewrites existing release assets. The checks intentionally match the
// workflow text instead of a parsed representation so any unrecognized rewrite
// fails closed.
func releaseWorkflowChecks() []workflowCheck {
	return []workflowCheck{
		{
			name: "publication is protected, tag-bound and clean",
			required: []*regexp.Regexp{
				regexp.MustCompile(`(?m)^  publish:$`),
				regexp.MustCompile(`(?m)^    environment: release$`),
				regexp.MustCompile(`(?m)^          test "\$GITHUB_REF_TYPE" = tag$`),
				regexp.MustCompile(`(?m)^          test "\$GITHUB_REF_NAME" = "\$VERSION"$`),
				regexp.MustCompile(`(?m)^          test -z "\$\(git status --porcelain\)"$`),
				regexp.MustCompile(`(?m)^        run: make release-dry-run$`),
			},
		},
		{
			name: "an existing release or asset is never overwritten",
			required: []*regexp.Regexp{
				regexp.MustCompile(`(?m)^          if gh release view "\$VERSION" >/dev/null 2>&1; then$`),
				regexp.MustCompile(`(?m)^            echo "release \$VERSION already exists; published versions are immutable" >&2$`),
				regexp.MustCompile(`(?m)^            exit 1$`),
				regexp.MustCompile(`(?m)^          gh release create "\$VERSION" --verify-tag --generate-notes \$prerelease$`),
				regexp.MustCompile(`(?m)^          gh release upload "\$VERSION" dist/release/\*$`),
			},
			forbidden: []*regexp.Regexp{
				regexp.MustCompile(`--clobber`),
				regexp.MustCompile(`gh release delete`),
				regexp.MustCompile(`gh release edit`),
			},
		},
	}
}

// checkReleaseWorkflowText validates one workflow file against the publication
// contract. Every release write must be reachable only after the immutability
// check refused an existing release.
func checkReleaseWorkflowText(content string) error {
	publish := strings.Index(content, "  publish:")
	if publish < 0 {
		return fmt.Errorf("release workflow has no protected publish job")
	}
	publishJob := content[publish:]
	for _, check := range releaseWorkflowChecks() {
		for _, required := range check.required {
			if !required.MatchString(publishJob) {
				return fmt.Errorf("release workflow no longer satisfies %q: missing %s", check.name, required.String())
			}
		}
		for _, forbidden := range check.forbidden {
			if forbidden.MatchString(publishJob) {
				return fmt.Errorf("release workflow no longer satisfies %q: found %s", check.name, forbidden.String())
			}
		}
	}
	commands := map[string]int{}
	for _, match := range ghReleaseCommand.FindAllStringSubmatch(publishJob, -1) {
		commands[match[1]]++
	}
	if commands["release create"] != 1 || commands["release upload"] != 1 {
		return fmt.Errorf("release workflow must create and upload a release exactly once per run, found %s", sortedCommands(commands))
	}
	if commands["release view"] != 1 {
		return fmt.Errorf("release workflow must check for an existing release exactly once per run, found %s", sortedCommands(commands))
	}
	view := strings.Index(publishJob, `gh release view "$VERSION"`)
	refuse := strings.Index(publishJob, "already exists")
	create := strings.Index(publishJob, "gh release create")
	upload := strings.Index(publishJob, "gh release upload")
	if view < 0 || refuse < view || create < refuse || upload < create {
		return fmt.Errorf("release workflow must refuse an existing release before creating or uploading assets")
	}
	return nil
}

func sortedCommands(commands map[string]int) string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, fmt.Sprintf("%s=%d", name, commands[name]))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func releaseGuard(arguments []string) error {
	flags := flag.NewFlagSet("release-guard", flag.ContinueOnError)
	workflow := flags.String("workflow", defaultReleaseWorkflow, "protected publication workflow")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("release-guard accepts no positional arguments")
	}
	content, err := os.ReadFile(*workflow)
	if err != nil {
		return fmt.Errorf("read release workflow: %w", err)
	}
	if err := checkReleaseWorkflowText(string(content)); err != nil {
		return err
	}
	fmt.Printf("release workflow satisfies the publication contract: %s\n", *workflow)
	return nil
}
