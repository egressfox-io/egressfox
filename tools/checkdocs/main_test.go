package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"relative file", "[guide](docs/guide.md)", ""},
		{"anchor", "[guide](docs/guide.md#setup)", ""},
		{"duplicate heading", "[guide](docs/guide.md#setup-1)", ""},
		{"same document", "# Start\n[here](#start)", ""},
		{"encoded path", "[guide](docs/a%20b.md)", ""},
		{"external", "[site](https://example.com/not-fetched#section)", ""},
		{"code fence", "````md\n[example](missing.md)\n```\n[example](missing.md)\n````", ""},
		{"tilde fence", "~~~md\n[example](missing.md)\n~~~", ""},
		{"inline code", "`[example](missing.md)`", ""},
		{"missing file", "[bad](missing.md)", "missing local target"},
		{"missing anchor", "[bad](docs/guide.md#missing)", "missing heading anchor"},
		{"outside repository", "[bad](../outside.md)", "escapes the repository"},
		{"absolute path", "[bad](/tmp/private.md)", "repository-relative"},
		{"file URI", "[bad](file:///tmp/private.md)", "repository-relative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{
				"README.md": tt.body, "docs/guide.md": "# Setup\n## Setup\n", "docs/a b.md": "# Space\n",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			problems, err := check(root)
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" && len(problems) != 0 {
				t.Fatalf("unexpected problems: %v", problems)
			}
			if tt.want != "" && (len(problems) != 1 || !strings.Contains(problems[0], tt.want)) {
				t.Fatalf("got %v; want one problem containing %q", problems, tt.want)
			}
		})
	}
}
