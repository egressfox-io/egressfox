package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egressfox-io/egressfox/internal/releasemanifest"
)

const nativeRoot = ".cache/native-engines"

type nativeReceipt struct {
	Schema        int      `json:"schema"`
	Engine        string   `json:"engine"`
	Version       string   `json:"version"`
	Profile       string   `json:"profile"`
	Revision      int      `json:"revision"`
	SourceCommit  string   `json:"sourceCommit"`
	Tags          []string `json:"tags"`
	Overrides     []string `json:"overrides"`
	GOOS          string   `json:"goos"`
	GOARCH        string   `json:"goarch"`
	GoVersion     string   `json:"goVersion"`
	InputsSHA256  string   `json:"inputsSHA256"`
	ModulesSHA256 string   `json:"modulesSHA256"`
	BinarySHA256  string   `json:"binarySHA256"`
}

type nativeSourceReceipt struct {
	InputsSHA256 string `json:"inputsSHA256"`
	TreeSHA256   string `json:"treeSHA256"`
}

func nativeBuild(arguments []string) error {
	flags := flag.NewFlagSet("native-build", flag.ContinueOnError)
	arch := flags.String("arch", runtime.GOARCH, "darwin target architecture (arm64 or amd64)")
	rebuild := flags.Bool("rebuild", false, "rebuild even if the checked receipt matches")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("native-build accepts no positional arguments")
	}
	if err := nativePlatform(*arch); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(defaultManifest)
	if err != nil {
		return err
	}
	goCommand := os.Getenv("GO")
	if goCommand == "" {
		goCommand = "go"
	}
	goVersion, err := goOutput("", goCommand, "env", "GOVERSION")
	if err != nil {
		return err
	}
	for _, engine := range manifest.Engines {
		if goVersion != "go"+engine.Build.GoVersion {
			return fmt.Errorf("%s requires Go %s; found %s", engine.Name, engine.Build.GoVersion, goVersion)
		}
		if err := buildNativeEngine(engine, *arch, goVersion, goCommand, *rebuild); err != nil {
			return err
		}
	}
	return nil
}

func nativeVerify(arguments []string) error {
	flags := flag.NewFlagSet("native-verify", flag.ContinueOnError)
	arch := flags.String("arch", runtime.GOARCH, "darwin target architecture (arm64 or amd64)")
	mihomo := flags.String("mihomo", "", "optional Mihomo binary path")
	singbox := flags.String("sing-box", "", "optional sing-box binary path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("native-verify accepts no positional arguments")
	}
	if err := nativePlatform(*arch); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(defaultManifest)
	if err != nil {
		return err
	}
	paths := map[string]string{"mihomo": *mihomo, "sing-box": *singbox}
	for _, engine := range manifest.Engines {
		path := paths[engine.Name]
		if path == "" {
			path = nativeBinaryPath(engine, *arch)
		}
		if err := verifyNativeEngine(engine, *arch, path); err != nil {
			return fmt.Errorf("%s native profile invalid: %w; rebuild with make engines-native TARGETARCH=%s", engine.Name, err, *arch)
		}
		fmt.Printf("verified %s %s (%s), darwin/%s: %s\n", engine.Name, engine.Version, engine.Profile, *arch, path)
	}
	return nil
}

func nativePlatform(arch string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("native engine workflow requires a macOS host")
	}
	if arch != "arm64" && arch != "amd64" {
		return fmt.Errorf("unsupported Darwin architecture %q", arch)
	}
	return nil
}

func nativeBinaryPath(engine releasemanifest.Engine, arch string) string {
	return filepath.Join(nativeRoot, "darwin", arch, engine.Build.BinaryName)
}

func nativeInputDigest(engine releasemanifest.Engine) (string, error) {
	hash := sha256.New()
	encoded, err := json.Marshal(engine)
	if err != nil {
		return "", err
	}
	hash.Write(encoded)
	// Source preparation and the native builder are inputs, as well as overlays.
	for _, root := range []string{"tools/releasectl", "internal/releasemanifest", engine.Build.OverlayDirectory} {
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("non-regular build input %s", path)
			}
			if root != engine.Build.OverlayDirectory && !strings.HasSuffix(path, ".go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash.Write([]byte(path))
			hash.Write([]byte{0})
			hash.Write(content)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func buildNativeEngine(engine releasemanifest.Engine, arch, goVersion, goCommand string, rebuild bool) error {
	inputs, err := nativeInputDigest(engine)
	if err != nil {
		return err
	}
	path := nativeBinaryPath(engine, arch)
	if !rebuild && verifyNativeEngine(engine, arch, path) == nil {
		fmt.Printf("cached %s darwin/%s: %s\n", engine.Name, arch, path)
		return nil
	}
	sourceParent := filepath.Join(nativeRoot, "sources")
	if err := os.MkdirAll(sourceParent, 0o755); err != nil {
		return err
	}
	sourceDir := filepath.Join(sourceParent, engine.Name+"-"+inputs)
	ready := false
	if content, err := os.ReadFile(filepath.Join(sourceDir, ".egressfox-input")); err == nil {
		var receipt nativeSourceReceipt
		if json.Unmarshal(content, &receipt) == nil && receipt.InputsSHA256 == inputs {
			actual, err := digestTree(sourceDir)
			ready = err == nil && actual == receipt.TreeSHA256
		}
	}
	if !ready {
		if err := os.RemoveAll(sourceDir); err != nil {
			return err
		}
		if err := prepareEngineSource([]string{"--engine", engine.Name, "--output-dir", sourceDir}); err != nil {
			return err
		}
		for _, override := range engine.Build.DependencyOverrides {
			if err := runGo(sourceDir, goCommand, "mod", "edit", "-require="+override); err != nil {
				return err
			}
		}
		if err := runGo(sourceDir, goCommand, "mod", "tidy"); err != nil {
			return err
		}
		tree, err := digestTree(sourceDir)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(nativeSourceReceipt{InputsSHA256: inputs, TreeSHA256: tree})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(sourceDir, ".egressfox-input"), encoded, 0o644); err != nil {
			return err
		}
	}
	modules, err := digestFiles(filepath.Join(sourceDir, "go.mod"), filepath.Join(sourceDir, "go.sum"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".engine-*")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	temporary.Close()
	defer os.Remove(tempPath)
	buildOutput, err := filepath.Abs(tempPath)
	if err != nil {
		return err
	}
	ldflags := "-s -w -buildid= -X " + nativeVersionSymbol(engine.Name) + "=" + engine.Version
	args := []string{"build", "-trimpath", "-buildvcs=false", "-tags=" + strings.Join(engine.Build.Tags, ","), "-ldflags=" + ldflags, "-o", buildOutput, engine.Build.Package}
	if err := runGoWithEnv(sourceDir, goCommand, []string{"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=0", "GOFLAGS="}, args...); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o755); err != nil {
		return err
	}
	binaryDigest, err := digestFiles(tempPath)
	if err != nil {
		return err
	}
	receipt := nativeReceipt{Schema: 1, Engine: engine.Name, Version: engine.Version, Profile: engine.Profile, Revision: engine.Build.Revision, SourceCommit: engine.Source.Commit, Tags: engine.Build.Tags, Overrides: engine.Build.DependencyOverrides, GOOS: "darwin", GOARCH: arch, GoVersion: goVersion, InputsSHA256: inputs, ModulesSHA256: modules, BinarySHA256: binaryDigest}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".receipt.json", append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	if err := verifyNativeEngine(engine, arch, path); err != nil {
		return err
	}
	fmt.Printf("built %s darwin/%s (%s): %s\n", engine.Name, arch, engine.Profile, path)
	return nil
}

func nativeVersionSymbol(name string) string {
	if name == "mihomo" {
		return "github.com/metacubex/mihomo/constant.Version"
	}
	return "github.com/sagernet/sing-box/constant.Version"
}

func verifyNativeEngine(engine releasemanifest.Engine, arch, path string) error {
	inputs, err := nativeInputDigest(engine)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("binary is missing or not executable")
	}
	content, err := os.ReadFile(path + ".receipt.json")
	if err != nil {
		return errors.New("build receipt is missing")
	}
	var receipt nativeReceipt
	if err := json.Unmarshal(content, &receipt); err != nil {
		return errors.New("build receipt is invalid")
	}
	if receipt.Schema != 1 || receipt.Engine != engine.Name || receipt.Version != engine.Version || receipt.Profile != engine.Profile || receipt.Revision != engine.Build.Revision || receipt.SourceCommit != engine.Source.Commit || receipt.GOOS != "darwin" || receipt.GOARCH != arch || receipt.GoVersion != "go"+engine.Build.GoVersion || receipt.InputsSHA256 != inputs || strings.Join(receipt.Tags, ",") != strings.Join(engine.Build.Tags, ",") || strings.Join(receipt.Overrides, ",") != strings.Join(engine.Build.DependencyOverrides, ",") {
		return errors.New("receipt does not match the current manifest or build inputs")
	}
	binaryDigest, err := digestFiles(path)
	if err != nil || binaryDigest != receipt.BinarySHA256 {
		return errors.New("binary checksum differs from receipt")
	}
	machFile, err := macho.Open(path)
	if err != nil {
		return errors.New("binary is not a Darwin executable")
	}
	machFile.Close()
	build, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Go build information: %w", err)
	}
	if build.GoVersion != receipt.GoVersion {
		return errors.New("Go compiler version differs from receipt")
	}
	settings := map[string]string{}
	for _, setting := range build.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != "darwin" || settings["GOARCH"] != arch || settings["CGO_ENABLED"] != "0" || settings["-tags"] != strings.Join(engine.Build.Tags, ",") {
		return errors.New("binary platform or feature tags differ from manifest")
	}
	if arch == runtime.GOARCH {
		argument := "version"
		if engine.Name == "mihomo" {
			argument = "-v"
		}
		output, err := exec.Command(path, argument).CombinedOutput()
		if err != nil || !strings.Contains(string(output), engine.Version) {
			return errors.New("executable does not report the pinned version")
		}
		if engine.Name == "sing-box" {
			if !strings.Contains(string(output), "Tags: "+strings.Join(engine.Build.Tags, ",")) {
				return errors.New("sing-box does not report required build tags")
			}
		}
	}
	return nil
}

func digestFiles(paths ...string) (string, error) {
	hash := sha256.New()
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(hash, file); err != nil {
			file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestTree(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == ".egressfox-input" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular prepared source %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hash.Write([]byte(relative))
		hash.Write([]byte{0})
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(hash, file); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func goOutput(directory, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Go command failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func runGo(directory, command string, args ...string) error {
	return runGoWithEnv(directory, command, nil, args...)
}

func runGoWithEnv(directory, command string, extra []string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), command, args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), extra...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Go %s failed: %w", args[0], err)
	}
	return nil
}
