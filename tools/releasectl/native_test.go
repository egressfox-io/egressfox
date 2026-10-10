package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestNativePreparedSourceDigestRejectsMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	if err := os.WriteFile(path, []byte("module example.test/engine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := digestTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".egressfox-input"), []byte("receipt"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := digestTree(root)
	if err != nil || second != first {
		t.Fatalf("receipt changed source digest: %v", err)
	}
	if err := os.WriteFile(path, []byte("module example.test/changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := digestTree(root)
	if err != nil || third == first {
		t.Fatalf("source mutation retained digest: %v", err)
	}
}

// A prepared engine must retain the patched requirement when the Go command
// runs from its module directory, regardless of the upstream's older minimum.
func TestPreparedEngineRequiresPatchedToolchain(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	content := "module example.test/engine\n\ngo 1.26.0\n\ntoolchain go1.27.1\n\nrequire example.test/library v1.2.3\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pinEngineGoVersion(root, "1.27.2"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	module, err := modfile.Parse(path, updated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if module.Go.Version != "1.27.2" || module.Toolchain.Name != "go1.27.2" {
		t.Fatalf("prepared source permits an older toolchain: %s", updated)
	}
	if len(module.Require) != 1 || module.Require[0].Mod.Path != "example.test/library" || module.Require[0].Mod.Version != "v1.2.3" {
		t.Fatalf("toolchain update changed upstream dependencies: %s", updated)
	}
}

func TestPreparedEngineRejectsNewerMinimumGoVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	content := "module example.test/engine\n\ngo 1.28.0\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pinEngineGoVersion(root, "1.27.2"); err == nil {
		t.Fatal("newer upstream minimum was silently downgraded")
	}
	updated, err := os.ReadFile(path)
	if err != nil || string(updated) != content {
		t.Fatalf("refused toolchain update modified source: %v", err)
	}
}
