package releasemanifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryManifest(t *testing.T) {
	manifest, err := Load(filepath.Join("..", "..", "release", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Engines) != 2 || len(manifest.Tools) != 4 {
		t.Fatalf("unexpected release inventory: engines=%d tools=%d", len(manifest.Engines), len(manifest.Tools))
	}
}

func TestFetchRejectsChecksumMismatch(t *testing.T) {
	client := fixtureClient([]byte("different"))
	download := Download{FileName: "tool", URL: "https://fixtures.invalid/tool", SHA256: hex.EncodeToString(make([]byte, sha256.Size)), Format: "raw"}
	output := filepath.Join(t.TempDir(), "tool")
	if err := Fetch(context.Background(), client, download, output, true); err == nil {
		t.Fatal("checksum mismatch succeeded")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("mismatched output exists: %v", err)
	}
}

func TestFetchMaterializesSupportedFormats(t *testing.T) {
	for _, test := range []struct {
		name       string
		format     string
		binaryPath string
		archive    func([]byte) []byte
	}{
		{name: "raw", format: "raw", archive: func(value []byte) []byte { return value }},
		{name: "gzip", format: "gzip", archive: gzipBytes},
		{name: "tar", format: "tar.gz", binaryPath: "folder/tool", archive: tarGzipBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := []byte("verified executable")
			archive := test.archive(want)
			digest := sha256.Sum256(archive)
			output := filepath.Join(t.TempDir(), "nested", "tool")
			download := Download{FileName: "tool", URL: "https://fixtures.invalid/tool", SHA256: hex.EncodeToString(digest[:]), Format: test.format, BinaryPath: test.binaryPath}
			if err := Fetch(context.Background(), fixtureClient(archive), download, output, true); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("output = %q, err=%v", got, err)
			}
			info, _ := os.Stat(output)
			if info.Mode().Perm() != 0o555 {
				t.Fatalf("mode = %o", info.Mode().Perm())
			}
		})
	}
}

func TestArchiveRejectsUnsafePaths(t *testing.T) {
	var value bytes.Buffer
	compressed := gzip.NewWriter(&value)
	archive := tar.NewWriter(compressed)
	_ = archive.WriteHeader(&tar.Header{Name: "../tool", Mode: 0o755, Size: 1})
	_, _ = archive.Write([]byte("x"))
	_ = archive.Close()
	_ = compressed.Close()
	digest := sha256.Sum256(value.Bytes())
	download := Download{FileName: "tool.tar.gz", URL: "https://fixtures.invalid/tool", SHA256: hex.EncodeToString(digest[:]), Format: "tar.gz", BinaryPath: "tool"}
	if err := Fetch(context.Background(), fixtureClient(value.Bytes()), download, filepath.Join(t.TempDir(), "tool"), true); err == nil {
		t.Fatal("unsafe archive succeeded")
	}
}

func TestExtractSourceArchive(t *testing.T) {
	archiveName := filepath.Join(t.TempDir(), "source.tar.gz")
	archiveBytes := sourceTarGzipBytes(t, []tar.Header{
		{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader},
		{Name: "project/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "project/go.mod", Typeflag: tar.TypeReg, Mode: 0o644, Size: 12},
	}, []byte("module test\n"))
	if err := os.WriteFile(archiveName, archiveBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "source")
	if err := ExtractSourceArchive(archiveName, output); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(output, "go.mod")); err != nil || string(content) != "module test\n" {
		t.Fatalf("extracted content = %q, err=%v", content, err)
	}
}

func TestExtractSourceArchiveRejectsTraversal(t *testing.T) {
	archiveName := filepath.Join(t.TempDir(), "source.tar.gz")
	archiveBytes := sourceTarGzipBytes(t, []tar.Header{
		{Name: "project/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "project/../outside", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
	}, []byte("x"))
	if err := os.WriteFile(archiveName, archiveBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExtractSourceArchive(archiveName, filepath.Join(t.TempDir(), "source")); err == nil {
		t.Fatal("source traversal archive succeeded")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func fixtureClient(value []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(value))}, nil
	})}
}

func gzipBytes(value []byte) []byte {
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	_, _ = writer.Write(value)
	_ = writer.Close()
	return output.Bytes()
}

func tarGzipBytes(value []byte) []byte {
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	archive := tar.NewWriter(compressed)
	_ = archive.WriteHeader(&tar.Header{Name: "folder/tool", Mode: 0o755, Size: int64(len(value))})
	_, _ = archive.Write(value)
	_ = archive.Close()
	_ = compressed.Close()
	return output.Bytes()
}

func sourceTarGzipBytes(t *testing.T, headers []tar.Header, value []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	archive := tar.NewWriter(compressed)
	for _, header := range headers {
		if err := archive.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := archive.Write(value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
