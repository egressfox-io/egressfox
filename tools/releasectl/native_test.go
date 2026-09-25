package main

import (
	"os"
	"path/filepath"
	"testing"
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
