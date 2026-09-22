package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// defaultReleaseWorkflow is the protected publication workflow that must keep
// satisfying the release-immutability contract.
const defaultReleaseWorkflow = ".github/workflows/release.yml"

// ghReleaseCommand matches every GitHub release mutation or query in a publish
// job, so an unrecognized one is reported instead of ignored.
var ghReleaseCommand = regexp.MustCompile(`gh release [a-z-]+`)

// yamlKey matches a `key:` or `key: value` line at one indentation depth, with
// optionally quoted keys.
var yamlKey = regexp.MustCompile(`^["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?:\s*(.*)$`)

// publishStep is one workflow step: the shell it runs, the label that
// introduced it, and any step-level condition.
type publishStep struct {
	label     string
	condition string
	script    string
	command   string
}

// shell is the step's shell text, including an inline run command.
func (step publishStep) shell() string {
	if step.script != "" {
		return step.script
	}
	return step.command
}

// releaseWorkflowChecks the publication contract of the protected publish job.
//
// Publication must stay strictly tag-bound, approved, single-shot and
// non-destructive:
//
//   - the job is gated by the publish input and the protected release
//     environment, and holds the package/attestation write permission the
//     signing and provenance steps need;
//   - validation happens on the exact tag with a clean tree before publication;
//   - an existing GitHub Release is refused explicitly before any release is
//     created, and assets are uploaded only once, without clobbering, deleting
//     or editing anything.
//
// The checks read structure and shell tokens rather than exact line layout, so
// reindenting, requoting, comment or flag-order changes never fail CI while a
// lost guarantee does.
func releaseWorkflowChecks(content string) error {
	if !strings.Contains(content, "group: release-publication") ||
		!strings.Contains(content, "cancel-in-progress: false") ||
		!strings.Contains(content, "queue: max") {
		return fmt.Errorf("release workflow must serialize publication without canceling an active run")
	}
	publish, err := parseReleaseJob(content)
	if err != nil {
		return err
	}
	for _, forbidden := range []string{"gh release delete", "gh release edit", "--clobber"} {
		if strings.Contains(publish.raw, forbidden) {
			return fmt.Errorf("protected publish job must never use %q", forbidden)
		}
	}
	for _, requirement := range []struct {
		name string
		ok   func(map[string]string) bool
	}{
		{"run only when publication was requested", func(values map[string]string) bool {
			return strings.Contains(values["if"], "inputs.publish == true")
		}},
		{"run in the protected release environment", func(values map[string]string) bool {
			return values["environment"] == "release"
		}},
		{"hold packages: write", func(values map[string]string) bool { return values["packages"] == "write" }},
		{"hold id-token: write", func(values map[string]string) bool { return values["id-token"] == "write" }},
		{"hold attestations: write", func(values map[string]string) bool { return values["attestations"] == "write" }},
		{"hold contents: write", func(values map[string]string) bool { return values["contents"] == "write" }},
	} {
		if !requirement.ok(publish.values) {
			return fmt.Errorf("protected publish job must %s", requirement.name)
		}
	}
	var scripts []string
	for _, step := range publish.steps {
		if step.condition != "" {
			return fmt.Errorf("protected publish job must not gate a step with %q", step.condition)
		}
		if shell := step.shell(); shell != "" {
			scripts = append(scripts, shell)
		}
	}
	if len(scripts) == 0 {
		return fmt.Errorf("protected publish job has no runnable step")
	}
	joined := strings.Join(scripts, "\n")
	// The actual registry write must be dominated by both remote checks and
	// changelog extraction. A check after image publication is too late.
	preflight := strings.Index(joined, "python3 hack/release-preflight.py")
	push := strings.Index(joined, "docker buildx build --platform")
	notes := strings.Index(joined, "python3 hack/release-notes.py")
	if preflight < 0 || push < 0 || notes < 0 || preflight > push || notes > push {
		return fmt.Errorf("remote release preflight and reviewed notes must precede the image push")
	}
	if strings.Count(joined[:push], "python3 hack/release-preflight.py") < 2 {
		return fmt.Errorf("remote release preflight must be repeated immediately before image push")
	}
	if strings.Contains(joined, "--generate-notes") || !strings.Contains(joined, "--notes-file dist/release-notes.md") {
		return fmt.Errorf("GitHub Release must use the reviewed changelog notes")
	}
	for _, requirement := range []struct {
		name string
		text string
	}{
		{"select an exact tag", `test "$GITHUB_REF_TYPE" = tag`},
		{"bind the version to that tag", `test "$GITHUB_REF_NAME" = "$VERSION"`},
		{"refuse a dirty source tree", `test -z "$(git status --porcelain)"`},
		{"construct release files before writing", "make release-dry-run"},
	} {
		if !strings.Contains(joined, requirement.text) {
			return fmt.Errorf("protected publish job must %s: missing %q", requirement.name, requirement.text)
		}
	}
	create := strings.Join(matchingScripts(scripts, `gh release create "$VERSION"`), "\n")
	upload := strings.Join(matchingScripts(scripts, `gh release upload "$VERSION"`), "\n")
	view := strings.Join(matchingScripts(scripts, `gh release view "$VERSION"`), "\n")
	if create == "" || upload == "" || view == "" {
		return fmt.Errorf("protected publish job must query, create and upload exactly one release")
	}
	for _, command := range ghReleaseCommand.FindAllString(joined, -1) {
		switch command {
		case "gh release view", "gh release create", "gh release upload":
		default:
			return fmt.Errorf("protected publish job must not run %q", command)
		}
	}
	if count := strings.Count(create, `gh release create "$VERSION"`); count != 1 {
		return fmt.Errorf("protected publish job must create the release exactly once, found %d", count)
	}
	if count := strings.Count(upload, `gh release upload "$VERSION"`); count != 1 {
		return fmt.Errorf("protected publish job must upload release files exactly once, found %d", count)
	}
	if !strings.Contains(create, "--verify-tag") {
		return fmt.Errorf("protected publish job must require the existing tag when creating the release")
	}
	if !strings.Contains(upload, "dist/release/") {
		return fmt.Errorf("protected publish job must upload the constructed release files")
	}
	refuse := strings.Join(matchingScripts(scripts, "already exists"), "\n")
	for _, requirement := range []struct {
		name string
		text string
	}{
		{"query the target release", `gh release view "$VERSION"`},
		{"stop when the version is already published", "exit 1"},
	} {
		if !strings.Contains(refuse, requirement.text) {
			return fmt.Errorf("protected publish job must %s before writing: missing %q", requirement.name, requirement.text)
		}
	}
	if strings.Index(joined, "already exists") > strings.Index(joined, `gh release create "$VERSION"`) {
		return fmt.Errorf("protected publish job must refuse an existing release before creating one")
	}
	if strings.Index(joined, `gh release create "$VERSION"`) > strings.Index(joined, `gh release upload "$VERSION"`) {
		return fmt.Errorf("protected publish job must create the release before uploading files")
	}
	return nil
}

func matchingScripts(scripts []string, text string) []string {
	var matches []string
	for _, script := range scripts {
		if strings.Contains(script, text) {
			matches = append(matches, script)
		}
	}
	return matches
}

// releaseJob is the parsed shape of the protected publication job. raw keeps
// the job text for token checks, values holds its direct `key: value` scalar
// settings, and steps holds one shell script per run block.
type releaseJob struct {
	raw    string
	values map[string]string
	steps  []publishStep
}

// parseReleaseJob extracts the publish job with indentation-aware scanning. It
// does not depend on any particular indentation width, quoting style,
// block-scalar modifier or key order. YAML features it does not recognize
// degrade to absent entries, and the required guarantees then fail closed.
func parseReleaseJob(content string) (releaseJob, error) {
	lines := strings.Split(content, "\n")
	// Anchor on the jobs section so an input or workflow name that happens to be
	// called `publish` is never mistaken for the job.
	jobs := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "jobs:" {
			jobs = index
			break
		}
	}
	if jobs < 0 {
		return releaseJob{}, fmt.Errorf("release workflow has no jobs section")
	}
	start := -1
	for index := jobs + 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "publish:" {
			start = index
			break
		}
	}
	if start < 0 {
		return releaseJob{}, fmt.Errorf("release workflow has no publish job")
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		line := lines[index]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			end = index
			break
		}
	}
	job := releaseJob{raw: strings.Join(lines[start:end], "\n"), values: map[string]string{}}
	// Record shell blocks first so their contents are never read as YAML keys.
	skip := make(map[int]bool)
	for index := start + 1; index < end; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if !strings.HasPrefix(trimmed, "run:") {
			continue
		}
		script, next := blockScalar(lines, index, end)
		if indent := indentOf(lines[index]); indent >= 0 {
			for blockLine := index; blockLine < next; blockLine++ {
				if blockLine == index || strings.TrimSpace(lines[blockLine]) == "" || indentOf(lines[blockLine]) > indent {
					skip[blockLine] = true
				}
			}
		}
		job.steps = append(job.steps, publishStep{
			label:     stepField(lines, start, index, "name"),
			condition: stepField(lines, start, index, "if"),
			script:    script,
			command:   trimmed,
		})
		index = next - 1
	}
	// Collect scalar settings at any depth outside those blocks, so nested
	// mappings such as permissions become visible and key order is irrelevant.
	for index := start + 1; index < end; index++ {
		if skip[index] {
			continue
		}
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		match := yamlKey.FindStringSubmatch(trimmed)
		if match == nil || match[2] == "" {
			continue
		}
		job.values[unquoteYAML(match[1])] = unquoteYAML(match[2])
	}
	return job, nil
}

// blockScalar reads the block scalar introduced at the run key, returning its
// normalized script and the first line after the block.
func blockScalar(lines []string, key, end int) (string, int) {
	keyIndent := indentOf(lines[key])
	next := key + 1
	if keyIndent < 0 {
		return "", next
	}
	var body []string
	for ; next < end; next++ {
		line := lines[next]
		if strings.TrimSpace(line) == "" {
			body = append(body, "")
			continue
		}
		if indentOf(line) <= keyIndent {
			break
		}
		body = append(body, line)
	}
	return strings.TrimSpace(normalizeWhitespace(strings.Join(body, "\n"))), next
}

// stepField finds a scalar field of the step that owns the run key, such as its
// name or condition, by scanning the preceding lines at the same indentation.
func stepField(lines []string, jobStart, runIndex int, field string) string {
	runIndent := indentOf(lines[runIndex])
	if runIndent < 0 {
		return ""
	}
	for index := runIndex - 1; index > jobStart; index-- {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || indentOf(line) < runIndent {
			return ""
		}
		if indentOf(line) != runIndent {
			continue
		}
		// The run key may share its line with the step list marker.
		trimmed = strings.TrimPrefix(trimmed, "- ")
		if value, ok := strings.CutPrefix(trimmed, field+":"); ok {
			return unquoteYAML(strings.TrimSpace(value))
		}
	}
	return ""
}

func indentOf(line string) int {
	spaces := 0
	for _, character := range line {
		switch character {
		case ' ':
			spaces++
		case '\t':
			spaces += 2
		default:
			return spaces
		}
	}
	return -1
}

func unquoteYAML(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
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
	if err := releaseWorkflowChecks(string(content)); err != nil {
		return err
	}
	fmt.Printf("release workflow satisfies the publication contract: %s\n", *workflow)
	return nil
}
