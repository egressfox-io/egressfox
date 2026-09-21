package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAlphaVersionContract(t *testing.T) {
	for _, value := range []string{"v0.1.0-alpha.1", "v0.0.0-alpha.0", "v0.12.34-alpha.56"} {
		if !alphaVersion.MatchString(value) {
			t.Errorf("valid version rejected: %s", value)
		}
	}
	for _, value := range []string{"0.1.0-alpha.1", "v1.0.0", "v0.1.0", "v0.1.0-beta.1", "v0.01.0-alpha.1"} {
		if alphaVersion.MatchString(value) {
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
