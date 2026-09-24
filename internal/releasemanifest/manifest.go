package releasemanifest

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
)

const SchemaVersion = 1

type Manifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Release       Release  `json:"release"`
	Engines       []Engine `json:"engines"`
	Tools         []Tool   `json:"tools"`
}

type Release struct {
	DevelopmentVersion string                  `json:"developmentVersion"`
	Platforms          []string                `json:"platforms"`
	Kubernetes         KubernetesCompatibility `json:"kubernetes"`
}

type KubernetesCompatibility struct {
	MinimumSupported  string              `json:"minimumSupported"`
	ReleaseValidation []string            `json:"releaseValidation"`
	Profiles          []KubernetesProfile `json:"profiles"`
}

type KubernetesProfile struct {
	Minor          string            `json:"minor"`
	EnvtestVersion string            `json:"envtestVersion"`
	EnvtestSHA512  map[string]string `json:"envtestSHA512"`
	KindNodeImage  string            `json:"kindNodeImage"`
}

type Engine struct {
	Name           string              `json:"name"`
	Version        string              `json:"version"`
	Profile        string              `json:"profile"`
	License        string              `json:"license"`
	Build          EngineBuild         `json:"build"`
	Source         Source              `json:"source"`
	LicenseFile    Download            `json:"licenseFile"`
	UpstreamAssets map[string]Download `json:"upstreamAssets"`
}

type EngineBuild struct {
	Revision            int      `json:"revision"`
	GoVersion           string   `json:"goVersion"`
	Package             string   `json:"package"`
	BinaryName          string   `json:"binaryName"`
	OverlayDirectory    string   `json:"overlayDirectory"`
	Tags                []string `json:"tags"`
	DependencyOverrides []string `json:"dependencyOverrides"`
}

type Source struct {
	Repository string   `json:"repository"`
	Tag        string   `json:"tag"`
	Commit     string   `json:"commit"`
	Archive    Download `json:"archive"`
}

type Tool struct {
	Name    string              `json:"name"`
	Version string              `json:"version"`
	Assets  map[string]Download `json:"assets"`
}

type Download struct {
	FileName   string `json:"fileName"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Format     string `json:"format"`
	BinaryPath string `json:"binaryPath,omitempty"`
}

func Load(fileName string) (Manifest, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return Manifest{}, fmt.Errorf("open release manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("release manifest contains trailing JSON")
		}
		return fmt.Errorf("decode trailing release manifest data: %w", err)
	}
	return nil
}

func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported release manifest schema %d", manifest.SchemaVersion)
	}
	if !validDevelopmentVersion(manifest.Release.DevelopmentVersion) || !slices.Equal(manifest.Release.Platforms, []string{"linux/amd64", "linux/arm64"}) {
		return errors.New("release contract must list linux/amd64 and linux/arm64 in order")
	}
	if err := manifest.Release.Kubernetes.Validate(); err != nil {
		return fmt.Errorf("Kubernetes compatibility contract: %w", err)
	}
	expected := map[string]artifact.Profile{"mihomo": artifact.Mihomo11931, "sing-box": artifact.SingBox1141}
	expectedBuildRevision := map[string]int{"mihomo": 1, "sing-box": 3}
	seenEngines := make(map[string]bool)
	for _, engine := range manifest.Engines {
		profile, ok := expected[engine.Name]
		if !ok || seenEngines[engine.Name] {
			return fmt.Errorf("unexpected or duplicate engine %q", engine.Name)
		}
		seenEngines[engine.Name] = true
		if engine.Version != profile.Version || engine.Profile != profile.RendererSchema || engine.License == "" {
			return fmt.Errorf("engine %s does not match the compiled compatibility profile", engine.Name)
		}
		if engine.Build.Revision != expectedBuildRevision[engine.Name] || engine.Build.GoVersion != "1.27.1" || engine.Build.Package == "" ||
			!safeArchivePath(engine.Build.BinaryName) || strings.Contains(engine.Build.BinaryName, "/") ||
			(engine.Build.OverlayDirectory != "" && !safeArchivePath(engine.Build.OverlayDirectory)) ||
			!slices.IsSorted(engine.Build.DependencyOverrides) {
			return fmt.Errorf("engine %s has an invalid build contract", engine.Name)
		}
		for _, override := range engine.Build.DependencyOverrides {
			parts := strings.Split(override, "@")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("engine %s has invalid dependency override %q", engine.Name, override)
			}
		}
		if !validHTTPS(engine.Source.Repository) || engine.Source.Tag == "" || !validCommit(engine.Source.Commit) {
			return fmt.Errorf("engine %s has invalid source provenance", engine.Name)
		}
		if err := validateDownload(engine.Source.Archive, false); err != nil {
			return fmt.Errorf("engine %s source: %w", engine.Name, err)
		}
		if err := validateDownload(engine.LicenseFile, false); err != nil {
			return fmt.Errorf("engine %s license: %w", engine.Name, err)
		}
		for _, platform := range manifest.Release.Platforms {
			download, ok := engine.UpstreamAssets[platform]
			if !ok {
				return fmt.Errorf("engine %s is missing %s", engine.Name, platform)
			}
			if err := validateDownload(download, true); err != nil {
				return fmt.Errorf("engine %s %s: %w", engine.Name, platform, err)
			}
		}
		if len(engine.UpstreamAssets) != len(manifest.Release.Platforms) {
			return fmt.Errorf("engine %s advertises an unsupported platform", engine.Name)
		}
	}
	if len(seenEngines) != len(expected) {
		return errors.New("release manifest must contain both supported engines")
	}
	seenTools := make(map[string]bool)
	for _, tool := range manifest.Tools {
		if tool.Name == "" || tool.Version == "" || seenTools[tool.Name] {
			return fmt.Errorf("invalid or duplicate release tool %q", tool.Name)
		}
		seenTools[tool.Name] = true
		for _, platform := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
			download, ok := tool.Assets[platform]
			if !ok {
				return fmt.Errorf("release tool %s is missing %s", tool.Name, platform)
			}
			if err := validateDownload(download, true); err != nil {
				return fmt.Errorf("release tool %s %s: %w", tool.Name, platform, err)
			}
		}
		if len(tool.Assets) != 4 {
			return fmt.Errorf("release tool %s advertises an unsupported platform", tool.Name)
		}
	}
	for _, required := range []string{"syft", "grype", "cosign", "helm"} {
		if !seenTools[required] {
			return fmt.Errorf("release manifest is missing tool %s", required)
		}
	}
	return nil
}

func (compatibility KubernetesCompatibility) Validate() error {
	if compatibility.MinimumSupported == "" || len(compatibility.Profiles) == 0 || len(compatibility.ReleaseValidation) == 0 {
		return errors.New("must declare minimum support, profiles, and release validation")
	}
	profiles := make(map[string]KubernetesProfile, len(compatibility.Profiles))
	for _, profile := range compatibility.Profiles {
		if !validKubernetesMinor(profile.Minor) || profile.EnvtestVersion != profile.Minor+".0" || profiles[profile.Minor].Minor != "" {
			return fmt.Errorf("invalid or duplicate profile %q", profile.Minor)
		}
		if !validDigest(profile.KindNodeImage, "@sha256:", sha256.Size) {
			return fmt.Errorf("profile %s has an unpinned kind node image", profile.Minor)
		}
		for _, platform := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
			digest := profile.EnvtestSHA512[platform]
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != sha512.Size || digest != strings.ToLower(digest) {
				return fmt.Errorf("profile %s has invalid envtest SHA-512 for %s", profile.Minor, platform)
			}
		}
		if len(profile.EnvtestSHA512) != 4 {
			return fmt.Errorf("profile %s has an unsupported envtest platform", profile.Minor)
		}
		profiles[profile.Minor] = profile
	}
	if _, ok := profiles[compatibility.MinimumSupported]; !ok {
		return fmt.Errorf("minimum supported Kubernetes %s has no profile", compatibility.MinimumSupported)
	}
	seenValidation := make(map[string]bool, len(compatibility.ReleaseValidation))
	for _, minor := range compatibility.ReleaseValidation {
		if _, ok := profiles[minor]; !ok || seenValidation[minor] {
			return fmt.Errorf("release validation Kubernetes %s has no profile", minor)
		}
		seenValidation[minor] = true
	}
	if !slices.IsSorted(compatibility.ReleaseValidation) {
		return errors.New("release validation Kubernetes profiles must be sorted")
	}
	return nil
}

func (compatibility KubernetesCompatibility) Profile(minor string) (KubernetesProfile, error) {
	for _, profile := range compatibility.Profiles {
		if profile.Minor == minor {
			return profile, nil
		}
	}
	return KubernetesProfile{}, fmt.Errorf("unsupported Kubernetes version %q", minor)
}

func validDevelopmentVersion(value string) bool {
	return regexp.MustCompile(`^v0\.[1-9][0-9]*\.[0-9]+-dev\.[1-9][0-9]*$`).MatchString(value)
}

func validKubernetesMinor(value string) bool {
	return regexp.MustCompile(`^1\.[0-9]+$`).MatchString(value)
}

func validDigest(value, separator string, size int) bool {
	parts := strings.Split(value, separator)
	if len(parts) != 2 || parts[0] == "" || parts[1] != strings.ToLower(parts[1]) {
		return false
	}
	decoded, err := hex.DecodeString(parts[1])
	return err == nil && len(decoded) == size
}

func validateDownload(download Download, executable bool) error {
	if download.FileName == "" || path.Base(download.FileName) != download.FileName || !validHTTPS(download.URL) {
		return errors.New("download filename or URL is invalid")
	}
	decoded, err := hex.DecodeString(download.SHA256)
	if err != nil || len(decoded) != sha256.Size || download.SHA256 != strings.ToLower(download.SHA256) {
		return errors.New("download SHA-256 is invalid")
	}
	if download.Format != "raw" && download.Format != "gzip" && download.Format != "tar.gz" {
		return fmt.Errorf("unsupported download format %q", download.Format)
	}
	if download.Format == "tar.gz" && executable && !safeArchivePath(download.BinaryPath) {
		return errors.New("archive binary path is invalid")
	}
	if download.Format != "tar.gz" && download.BinaryPath != "" {
		return errors.New("binary path is only valid for tar.gz downloads")
	}
	return nil
}

func validHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func validCommit(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeArchivePath(value string) bool {
	trimmed := strings.TrimSuffix(value, "/")
	return trimmed != "" && !strings.HasPrefix(trimmed, "/") && path.Clean(trimmed) == trimmed && !strings.HasPrefix(trimmed, "../")
}

func (manifest Manifest) Engine(name string) (Engine, error) {
	for _, engine := range manifest.Engines {
		if engine.Name == name {
			return engine, nil
		}
	}
	return Engine{}, fmt.Errorf("unknown engine %q", name)
}

func (manifest Manifest) Tool(name string) (Tool, error) {
	for _, tool := range manifest.Tools {
		if tool.Name == name {
			return tool, nil
		}
	}
	return Tool{}, fmt.Errorf("unknown release tool %q", name)
}

func HostPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func Fetch(ctx context.Context, client *http.Client, download Download, output string, executable bool) error {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute, CheckRedirect: secureRedirects}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, download.URL, nil)
	if err != nil {
		return fmt.Errorf("prepare download: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", download.FileName, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected HTTP status %d", download.FileName, response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".egressfox-download-*")
	if err != nil {
		return fmt.Errorf("create temporary download: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, 512<<20)); err != nil {
		temporary.Close()
		return fmt.Errorf("read %s: %w", download.FileName, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", download.FileName, err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != download.SHA256 {
		return fmt.Errorf("checksum mismatch for %s", download.FileName)
	}
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o555
	}
	if err := materialize(temporaryName, download, output, mode); err != nil {
		return err
	}
	return nil
}

func ExtractSourceArchive(archiveName, output string) error {
	if err := os.Mkdir(output, 0o755); err != nil {
		return fmt.Errorf("create source directory: %w", err)
	}
	file, err := os.Open(archiveName)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open source gzip stream: %w", err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var root string
	var files int
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read source archive: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !safeArchivePath(name) {
			return errors.New("source archive contains an unsafe path")
		}
		parts := strings.SplitN(name, "/", 2)
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return errors.New("source archive has multiple roots")
		}
		if len(parts) == 1 {
			if header.Typeflag != tar.TypeDir && !(strings.HasSuffix(header.Name, "/") && header.Size == 0) {
				return fmt.Errorf("source archive root is not a directory: type=%d name=%q size=%d", header.Typeflag, header.Name, header.Size)
			}
			continue
		}
		relative := parts[1]
		if !safeArchivePath(relative) {
			return errors.New("source archive contains an unsafe relative path")
		}
		target := filepath.Join(output, filepath.FromSlash(relative))
		files++
		if files > 50_000 {
			return errors.New("source archive contains too many entries")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create source directory: %w", err)
			}
		case tar.TypeReg:
			total += header.Size
			if header.Size < 0 || header.Size > 256<<20 || total > 1<<30 {
				return errors.New("source archive exceeds size limits")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create source parent: %w", err)
			}
			mode := os.FileMode(0o644)
			if header.Mode&0o111 != 0 {
				mode = 0o755
			}
			outputFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return fmt.Errorf("create source file: %w", err)
			}
			_, copyErr := io.CopyN(outputFile, reader, header.Size)
			closeErr := outputFile.Close()
			if copyErr != nil {
				return fmt.Errorf("extract source file: %w", copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close source file: %w", closeErr)
			}
		default:
			return fmt.Errorf("source archive contains unsupported entry type %d", header.Typeflag)
		}
	}
	if root == "" || files == 0 {
		return errors.New("source archive is empty")
	}
	return nil
}

func secureRedirects(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 || request.URL.Scheme != "https" {
		return errors.New("unsafe download redirect")
	}
	return nil
}

func materialize(archiveName string, download Download, output string, mode os.FileMode) error {
	source, err := os.Open(archiveName)
	if err != nil {
		return fmt.Errorf("open verified download: %w", err)
	}
	defer source.Close()
	var reader io.Reader = source
	if download.Format == "gzip" || download.Format == "tar.gz" {
		compressed, err := gzip.NewReader(source)
		if err != nil {
			return fmt.Errorf("open %s gzip stream: %w", download.FileName, err)
		}
		defer compressed.Close()
		reader = compressed
	}
	if download.Format == "tar.gz" {
		reader, err = archiveFile(tar.NewReader(reader), download.BinaryPath)
		if err != nil {
			return fmt.Errorf("extract %s: %w", download.FileName, err)
		}
	}
	return writeAtomic(output, reader, mode)
}

func archiveFile(reader *tar.Reader, wanted string) (io.Reader, error) {
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("binary is absent from archive")
		}
		if err != nil {
			return nil, err
		}
		if !safeArchivePath(header.Name) {
			return nil, errors.New("archive contains an unsafe path")
		}
		if header.Name == wanted {
			if header.Typeflag != tar.TypeReg || header.Size > 256<<20 {
				return nil, errors.New("archive binary is not a bounded regular file")
			}
			return io.LimitReader(reader, header.Size), nil
		}
	}
}

func writeAtomic(output string, reader io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".egressfox-output-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := io.Copy(temporary, reader); err != nil {
		temporary.Close()
		return fmt.Errorf("write output: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set output mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(name, output); err != nil {
		return fmt.Errorf("install output: %w", err)
	}
	return nil
}
