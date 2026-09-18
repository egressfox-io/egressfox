// Command checkdocs checks the repository's inline Markdown links offline.
// It supports ATX headings and fenced code blocks, not arbitrary CommonMark.
package main

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	inlineLink = regexp.MustCompile(`\[[^\]\n]*\]\(([^\s)]+)\)`)
	inlineCode = regexp.MustCompile("`+[^`]*`+")
	heading    = regexp.MustCompile(`^#{1,6} +(.+?)(?: +#+)?$`)
)

func main() {
	problems, err := check(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
	fmt.Println("Documentation links and local anchors OK (offline check).")
}

func check(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	documents := make(map[string][]string)
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".cache", "bin", "dist", "local", "state", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		documents[path] = proseLines(string(data))
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	anchors := make(map[string]map[string]bool)
	for path, lines := range documents {
		anchors[path] = headingAnchors(lines)
	}
	var problems []string
	for _, path := range paths {
		for n, line := range documents[path] {
			for _, match := range inlineLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				if err := checkLink(root, path, strings.Trim(match[1], "<>"), anchors); err != nil {
					relative, _ := filepath.Rel(root, path)
					problems = append(problems, fmt.Sprintf("%s:%d: %s: %v", relative, n+1, match[1], err))
				}
			}
		}
	}
	return problems, nil
}

func checkLink(root, from, link string, anchors map[string]map[string]bool) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	if u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "mailto" {
		return nil // External availability is reviewed separately, never fetched here.
	}
	if u.IsAbs() || u.Host != "" || filepath.IsAbs(u.Path) {
		return fmt.Errorf("use a repository-relative link or an HTTP(S) URL")
	}
	target := from
	if u.Path != "" {
		target = filepath.Join(filepath.Dir(from), filepath.FromSlash(u.Path))
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("link escapes the repository")
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("missing local target: %w", err)
	}
	if u.Fragment != "" && filepath.Ext(target) == ".md" && !anchors[target][u.Fragment] {
		return fmt.Errorf("missing heading anchor %q", u.Fragment)
	}
	return nil
}

// Keep line positions intact while ignoring fenced examples, including Mermaid.
func proseLines(text string) []string {
	lines := strings.Split(text, "\n")
	var fence byte
	var width int
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != 0 {
			lines[i] = ""
			if strings.HasPrefix(trimmed, strings.Repeat(string(fence), width)) && strings.Trim(trimmed, string(fence)) == "" {
				fence = 0
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[0]
			width = len(trimmed) - len(strings.TrimLeft(trimmed, string(fence)))
			lines[i] = ""
		}
	}
	return lines
}

func headingAnchors(lines []string) map[string]bool {
	anchors := make(map[string]bool)
	for _, line := range lines {
		match := heading.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		var slug strings.Builder
		for _, r := range strings.ToLower(match[1]) {
			switch {
			case unicode.IsLetter(r), unicode.IsNumber(r), r == '-', r == '_':
				slug.WriteRune(r)
			case r == ' ':
				slug.WriteByte('-')
			}
		}
		base := slug.String()
		anchor := base
		for n := 1; anchors[anchor]; n++ {
			anchor = fmt.Sprintf("%s-%d", base, n)
		}
		anchors[anchor] = true
	}
	return anchors
}
