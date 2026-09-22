// Command changelogcheck verifies that a pull request adds a meaningful entry to
// CHANGELOG.md or qualifies for a narrow automatic or maintainer-authorized
// exemption. It reads pull_request metadata as data and never evaluates it in a
// shell.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
)

const exemptionLabel = "no-changelog"

var (
	commitPattern         = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
	sectionPattern        = regexp.MustCompile(`^##\s+\[Unreleased\]\s*$`)
	versionSectionPattern = regexp.MustCompile(`(?m)^## \[(v\d+\.\d+\.\d+(?:-(?:dev|alpha|beta)\.\d+)?)\]$`)
	majorHeadingPattern   = regexp.MustCompile(`^##(?:\s|$)`)
	categoryPattern       = regexp.MustCompile(`^###\s+(.+?)\s*$`)
	bulletPattern         = regexp.MustCompile(`^[-*+]\s+(.+?)\s*$`)
	checkboxPattern       = regexp.MustCompile(`^\s*-\s+\[([ xX])\]\s+(.+?)\s*$`)
	reasonPattern         = regexp.MustCompile(`(?i)^\s*reason:\s*(.*)$`)
	commentPattern        = regexp.MustCompile(`(?s)<!--.*?-->`)
	placeholderPattern    = regexp.MustCompile(`(?i)^(?:tbd|todo|fixme|wip|none|n/?a|no changes?(?: yet)?|no notable changes|describe (?:the )?change|placeholder|\[.+\])\s*[.!?]*$`)
)

var standardCategories = map[string]struct{}{
	"added":      {},
	"changed":    {},
	"deprecated": {},
	"removed":    {},
	"fixed":      {},
	"security":   {},
}

type label struct {
	Name string `json:"name"`
}

type pullRequestEvent struct {
	PullRequest struct {
		Title  string  `json:"title"`
		Body   string  `json:"body"`
		Labels []label `json:"labels"`
	} `json:"pull_request"`
}

type result struct {
	Message string
}

func main() {
	message, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "changelog check: "+err.Error())
		os.Exit(1)
	}
	fmt.Println("changelog check: " + message)
}

func run() (string, error) {
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	base := os.Getenv("CHANGELOG_BASE_SHA")
	head := os.Getenv("CHANGELOG_HEAD_SHA")
	if eventPath == "" || base == "" || head == "" {
		return "", errors.New("GITHUB_EVENT_PATH, CHANGELOG_BASE_SHA, and CHANGELOG_HEAD_SHA are required")
	}
	if !commitPattern.MatchString(base) || !commitPattern.MatchString(head) {
		return "", errors.New("base and head must be full Git commit IDs")
	}

	eventFile, err := os.Open(eventPath)
	if err != nil {
		return "", fmt.Errorf("read pull request event: %w", err)
	}
	defer eventFile.Close()
	var event pullRequestEvent
	decoder := json.NewDecoder(io.LimitReader(eventFile, 1<<20))
	if err := decoder.Decode(&event); err != nil {
		return "", fmt.Errorf("decode pull request event: %w", err)
	}

	paths, err := changedPaths(base, head)
	if err != nil {
		return "", err
	}
	baseLog, err := fileAt(base, "CHANGELOG.md")
	if err != nil {
		return "", err
	}
	headLog, err := fileAt(head, "CHANGELOG.md")
	if err != nil {
		return "", err
	}

	passed, err := check(paths, baseLog, headLog, event.PullRequest.Body, hasExemptionLabel(event.PullRequest.Labels))
	if err != nil {
		return "", err
	}
	return passed.Message, nil
}

func changedPaths(base, head string) ([]string, error) {
	if err := requireCommit(base); err != nil {
		return nil, fmt.Errorf("read pull request base: %w", err)
	}
	if err := requireCommit(head); err != nil {
		return nil, fmt.Errorf("read pull request head: %w", err)
	}
	output, err := git("diff", "--name-only", "--no-renames", "-z", base, head, "--")
	if err != nil {
		return nil, fmt.Errorf("compare pull request revisions: %w", err)
	}
	if len(output) == 0 {
		return nil, nil
	}
	parts := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			paths = append(paths, string(part))
		}
	}
	return paths, nil
}

func requireCommit(revision string) error {
	if !commitPattern.MatchString(revision) {
		return errors.New("revision is not a full Git commit ID")
	}
	if _, err := git("cat-file", "-e", revision+"^{commit}"); err != nil {
		return errors.New("revision is unavailable in the checkout")
	}
	return nil
}

func fileAt(revision, name string) (*string, error) {
	if err := requireCommit(revision); err != nil {
		return nil, err
	}
	object := revision + ":" + name
	if _, err := git("cat-file", "-e", object); err != nil {
		return nil, nil
	}
	contents, err := git("show", object)
	if err != nil {
		return nil, fmt.Errorf("read %s from pull request revision: %w", name, err)
	}
	value := string(contents)
	return &value, nil
}

func git(args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	if stderr.Len() == 0 {
		return nil, err
	}
	return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
}

func check(paths []string, baseLog, headLog *string, body string, authorized bool) (result, error) {
	updatedChecked, updatedUnchecked := checkboxStates(body, "CHANGELOG.md updated with release notes.")
	if !updatedChecked && !updatedUnchecked {
		updatedChecked, updatedUnchecked = checkboxStates(body, "CHANGELOG.md updated under Unreleased.")
	}
	exemptChecked, exemptUnchecked := checkboxStates(body, "No changelog entry is required.")
	if (updatedChecked && updatedUnchecked) || (exemptChecked && exemptUnchecked) || (updatedChecked && exemptChecked) {
		return result{}, errors.New("the changelog checklist is contradictory; select exactly one option")
	}

	delta, err := unreleasedDelta(baseLog, headLog)
	if err != nil {
		return result{}, err
	}
	if !delta {
		delta, err = preparedReleaseDelta(baseLog, headLog)
		if err != nil {
			return result{}, err
		}
	}
	if updatedChecked && !delta {
		return result{}, errors.New("the checklist says CHANGELOG.md was updated, but no meaningful new entry was found")
	}
	if exemptChecked && delta {
		return result{}, errors.New("the checklist requests no changelog entry, but a meaningful Unreleased entry is present; select only one option")
	}
	if exemptChecked {
		if !reasonProvided(body) {
			return result{}, errors.New("a changelog exemption request needs a brief reason after `Reason:` in the pull request description")
		}
		if !authorized {
			return result{}, errors.New("the exemption request is not authorized; a maintainer must apply the `no-changelog` label after review")
		}
		return result{Message: "passed (maintainer-authorized no-changelog exemption)"}, nil
	}
	if delta {
		return result{Message: "passed (meaningful changelog entry added)"}, nil
	}
	if automaticallyExempt(paths) {
		return result{Message: "passed (only narrowly classified internal documentation or Go test files changed)"}, nil
	}
	return result{}, errors.New("a changelog entry is required: add a meaningful entry under CHANGELOG.md's ## [Unreleased], or request an exemption with a reason and the maintainer-applied `no-changelog` label")
}

// A reviewed release candidate moves notes into a new exact-version section
// before its tag is created. This is an entry, not a changelog exemption.
func preparedReleaseDelta(baseLog, headLog *string) (bool, error) {
	if headLog == nil {
		return false, nil
	}
	base := ""
	if baseLog != nil {
		base = *baseLog
	}
	for _, match := range versionSectionPattern.FindAllStringIndex(*headLog, -1) {
		heading := (*headLog)[match[0]:match[1]]
		if strings.Contains(base, heading) {
			continue
		}
		section := "## [Unreleased]" + (*headLog)[match[1]:]
		entries, _, err := unreleasedEntries(section)
		if err != nil {
			return false, err
		}
		if len(entries) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func unreleasedDelta(baseLog, headLog *string) (bool, error) {
	if headLog == nil {
		return false, nil
	}
	headEntries, hasHeadSection, err := unreleasedEntries(*headLog)
	if err != nil {
		return false, fmt.Errorf("parse pull request CHANGELOG.md: %w", err)
	}
	if !hasHeadSection {
		return false, nil
	}
	baseEntries := map[string]struct{}{}
	if baseLog != nil {
		var hasBaseSection bool
		baseEntries, hasBaseSection, err = unreleasedEntries(*baseLog)
		if err != nil {
			return false, fmt.Errorf("parse base CHANGELOG.md: %w", err)
		}
		if !hasBaseSection {
			baseEntries = map[string]struct{}{}
		}
	}
	for entry := range headEntries {
		if _, existed := baseEntries[entry]; !existed {
			return true, nil
		}
	}
	return false, nil
}

func unreleasedEntries(contents string) (map[string]struct{}, bool, error) {
	lines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
	entries := map[string]struct{}{}
	inSection := false
	found := false
	category := ""
	inComment := false
	var fence byte
	fenceWidth := 0
	for _, raw := range lines {
		line := strings.TrimSpace(stripHTMLComments(raw, &inComment))
		if fence != 0 {
			if closesFence(line, fence, fenceWidth) {
				fence = 0
				fenceWidth = 0
			}
			continue
		}
		if marker, width, ok := openingFence(line); ok {
			fence = marker
			fenceWidth = width
			continue
		}
		if sectionPattern.MatchString(line) {
			if found {
				return nil, false, errors.New("more than one ## [Unreleased] section")
			}
			found = true
			inSection = true
			category = ""
			continue
		}
		if inSection && majorHeadingPattern.MatchString(line) {
			inSection = false
		}
		if !inSection {
			continue
		}
		if match := categoryPattern.FindStringSubmatch(line); match != nil {
			category = strings.ToLower(strings.TrimSpace(match[1]))
			continue
		}
		match := bulletPattern.FindStringSubmatch(line)
		if match == nil || category == "" {
			continue
		}
		if _, ok := standardCategories[category]; !ok {
			continue
		}
		text := meaningfulText(match[1])
		if text == "" || placeholderPattern.MatchString(text) {
			continue
		}
		key := category + "\x00" + strings.ToLower(strings.Join(strings.Fields(text), " "))
		entries[key] = struct{}{}
	}
	return entries, found, nil
}

func stripHTMLComments(line string, inComment *bool) string {
	var visible strings.Builder
	for len(line) > 0 {
		if *inComment {
			end := strings.Index(line, "-->")
			if end < 0 {
				return visible.String()
			}
			*inComment = false
			line = line[end+3:]
			continue
		}
		start := strings.Index(line, "<!--")
		if start < 0 {
			visible.WriteString(line)
			break
		}
		visible.WriteString(line[:start])
		*inComment = true
		line = line[start+4:]
	}
	return visible.String()
}

func openingFence(line string) (byte, int, bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	width := 0
	for width < len(line) && line[width] == line[0] {
		width++
	}
	return line[0], width, width >= 3
}

func closesFence(line string, marker byte, width int) bool {
	count := 0
	for count < len(line) && line[count] == marker {
		count++
	}
	return count >= width && strings.TrimSpace(line[count:]) == ""
}

func meaningfulText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "`*_~ ")
	return strings.TrimSpace(text)
}

func checkboxStates(body, labelText string) (checked, unchecked bool) {
	for _, raw := range strings.Split(body, "\n") {
		match := checkboxPattern.FindStringSubmatch(raw)
		if match == nil || strings.TrimSpace(match[2]) != labelText {
			continue
		}
		if strings.EqualFold(match[1], "x") {
			checked = true
		} else {
			unchecked = true
		}
	}
	return checked, unchecked
}

func reasonProvided(body string) bool {
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		match := reasonPattern.FindStringSubmatch(raw)
		if match == nil {
			continue
		}
		parts := []string{match[1]}
		parts = append(parts, lines[i+1:]...)
		reason := strings.TrimSpace(commentPattern.ReplaceAllString(strings.Join(parts, "\n"), " "))
		reason = strings.TrimSpace(strings.Trim(reason, "`*_~ "))
		return reason != "" && !placeholderPattern.MatchString(reason)
	}
	return false
}

func hasExemptionLabel(labels []label) bool {
	for _, item := range labels {
		if item.Name == exemptionLabel {
			return true
		}
	}
	return false
}

func automaticallyExempt(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, name := range paths {
		if !isInternalDocumentation(name) && !strings.HasSuffix(path.Base(name), "_test.go") {
			return false
		}
	}
	return true
}

func isInternalDocumentation(name string) bool {
	clean := path.Clean(name)
	switch clean {
	case "AGENTS.md", "CONTRIBUTING.md", "MAINTAINERS.md", "docs/README.md", ".github/pull_request_template.md":
		return true
	}
	for _, prefix := range []string{"docs/plans/", "docs/decisions/", "docs/development/", ".github/ISSUE_TEMPLATE/"} {
		if strings.HasPrefix(clean, prefix) && strings.HasSuffix(strings.ToLower(clean), ".md") {
			return true
		}
	}
	return false
}
