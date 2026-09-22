package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseVersionContract(t *testing.T) {
	for _, test := range []struct{ value, class, normalized string }{
		{"v0.1.0-dev.1", "dev", "0.1.0-dev.1"},
		{"v0.1.0-alpha.1", "alpha", "0.1.0-alpha.1"},
		{"v0.1.0-beta.1", "beta", "0.1.0-beta.1"},
		{"v0.1.0", "stable", "0.1.0"},
		{"v1.12.34", "stable", "1.12.34"},
	} {
		parsed, err := parseReleaseTag(test.value)
		if err != nil || parsed.Class != test.class || parsed.Normalized != test.normalized {
			t.Errorf("parseReleaseTag(%q) = %#v, %v", test.value, parsed, err)
		}
	}
	for _, value := range []string{"0.1.0-alpha.1", "v0.1.0-rc.1", "v0.01.0", "v0.1", "v0.1.0+build.1", "v0.1.0-dev.0"} {
		if _, err := parseReleaseTag(value); err == nil {
			t.Errorf("invalid version accepted: %s", value)
		}
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
