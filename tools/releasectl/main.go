package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egressfox-io/egressfox/internal/buildinfo"
	"github.com/egressfox-io/egressfox/internal/releasemanifest"
)

const defaultManifest = "release/manifest.json"

func main() {
	if len(os.Args) < 2 {
		fail("usage: releasectl <validate|validate-version|build-version|release-guard|kubernetes|metadata|fetch-engine|prepare-engine-source|overrides|engine-build|fetch-sources|fetch-licenses|install-tool>")
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
	case "validate-version":
		err = validateVersion(os.Args[2:])
	case "build-version":
		var version string
		if version, err = buildVersion(os.Args[2:]); err == nil {
			fmt.Println(version)
		}
	case "release-guard":
		err = releaseGuard(os.Args[2:])
	case "kubernetes":
		err = kubernetes(os.Args[2:])
	case "metadata":
		err = metadata(os.Args[2:])
	case "fetch-engine":
		err = fetchEngine(os.Args[2:])
	case "prepare-engine-source":
		err = prepareEngineSource(os.Args[2:])
	case "overrides":
		err = overrides(os.Args[2:])
	case "engine-build":
		err = engineBuild(os.Args[2:])
	case "fetch-sources":
		err = fetchSources(os.Args[2:])
	case "fetch-licenses":
		err = fetchLicenses(os.Args[2:])
	case "install-tool":
		err = installTool(os.Args[2:])
	default:
		err = fmt.Errorf("unknown releasectl command %q", os.Args[1])
	}
	if err != nil {
		fail(err.Error())
	}
}

func validate(arguments []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	root := flags.String("root", "", "optional repository root for consistency checks")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("validate accepts no positional arguments")
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	if *root != "" {
		if err := validateRepository(*root, manifest); err != nil {
			return err
		}
	}
	fmt.Printf("release manifest valid: %d engines, %d tools, %d platforms\n", len(manifest.Engines), len(manifest.Tools), len(manifest.Release.Platforms))
	return nil
}

func validateRepository(root string, manifest releasemanifest.Manifest) error {
	notices, err := os.ReadFile(filepath.Join(root, "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		return fmt.Errorf("read third-party notices: %w", err)
	}
	for _, engine := range manifest.Engines {
		for _, required := range []string{engine.Name, engine.Version, engine.Source.Commit, engine.Source.Archive.FileName, engine.LicenseFile.FileName, engine.License} {
			if !strings.Contains(string(notices), required) {
				return fmt.Errorf("third-party notices omit %s metadata %q", engine.Name, required)
			}
		}
	}
	values, err := os.ReadFile(filepath.Join(root, "charts", "egressfox", "values.yaml"))
	if err != nil {
		return fmt.Errorf("read chart values: %w", err)
	}
	if !regexp.MustCompile(`(?m)^\s*tag:\s*""\s*$`).Match(values) {
		return fmt.Errorf("chart image tag must default to appVersion")
	}
	chart, err := os.ReadFile(filepath.Join(root, "charts", "egressfox", "Chart.yaml"))
	if err != nil {
		return fmt.Errorf("read chart metadata: %w", err)
	}
	developmentVersion := strings.TrimPrefix(manifest.Release.DevelopmentVersion, "v")
	if !regexp.MustCompile(`(?m)^version:\s*`+regexp.QuoteMeta(developmentVersion)+`\s*$`).Match(chart) ||
		!regexp.MustCompile(`(?m)^appVersion:\s*"`+regexp.QuoteMeta(developmentVersion)+`"\s*$`).Match(chart) {
		return fmt.Errorf("chart version and appVersion must match release development version %s", developmentVersion)
	}
	goModule, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	goMatch := regexp.MustCompile(`(?m)^go ([0-9]+\.[0-9]+\.[0-9]+)$`).FindSubmatch(goModule)
	if len(goMatch) != 2 {
		return fmt.Errorf("go.mod must declare an exact Go patch version")
	}
	dockerfile, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		return fmt.Errorf("read Dockerfile: %w", err)
	}
	for _, engine := range manifest.Engines {
		if engine.Build.GoVersion != string(goMatch[1]) {
			return fmt.Errorf("engine %s Go version differs from go.mod", engine.Name)
		}
	}
	if !strings.Contains(string(dockerfile), "golang:"+string(goMatch[1])+"-") {
		return fmt.Errorf("Dockerfile Go base differs from go.mod")
	}
	return nil
}

type releaseVersion = buildinfo.Version

func parseReleaseTag(value string) (releaseVersion, error) {
	return buildinfo.ParseVersion(value)
}

func validateVersion(arguments []string) error {
	flags := flag.NewFlagSet("validate-version", flag.ContinueOnError)
	version := flags.String("version", "", "vX.Y.Z[-dev.N|-alpha.N|-beta.N] release tag")
	revision := flags.String("revision", "", "40-character source commit")
	created := flags.String("created", "", "RFC3339 creation time")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	normalized, err := checkReleaseVersion(*version, *revision, *created)
	if err != nil {
		return err
	}
	fmt.Println(normalized)
	return nil
}

func checkReleaseVersion(version, revision, created string) (string, error) {
	parsed, err := parseReleaseTag(version)
	if err != nil {
		return "", err
	}
	decoded, err := hex.DecodeString(revision)
	if err != nil || len(decoded) != 20 || revision != strings.ToLower(revision) {
		return "", fmt.Errorf("release revision is not a full lowercase commit SHA")
	}
	if _, err := time.Parse(time.RFC3339, created); err != nil {
		return "", fmt.Errorf("release creation time is not RFC3339: %w", err)
	}
	return parsed.Normalized, nil
}

// buildVersion resolves the identity an artifact built from root must report.
// buildVersion resolves the identity an artifact built from root must report.
// --require-clean makes release qualification fail closed on any non-ignored
// source change, and --version requires the resolved version to be that planned
// release version. An untagged candidate commit is allowed here: pre-publication
// qualification does not need the Git tag, which only publication requires.
func buildVersion(arguments []string) (string, error) {
	flags := flag.NewFlagSet("build-version", flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	manifestName := flags.String("manifest", defaultManifest, "release manifest relative to root")
	developmentVersion := flags.String("development-version", "", "planned development version; defaults to the release manifest value")
	expected := flags.String("version", "", "required planned release version; any other resolved version fails")
	requireClean := flags.Bool("require-clean", false, "fail unless the source tree has no non-ignored changes")
	if err := flags.Parse(arguments); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("build-version accepts no positional arguments")
	}
	declared := *developmentVersion
	if declared == "" {
		manifest, err := releasemanifest.Load(filepath.Join(*root, *manifestName))
		if err != nil {
			return "", err
		}
		declared = manifest.Release.DevelopmentVersion
	}
	identity, err := buildinfo.ResolveIdentity(*root, declared)
	if err != nil {
		return "", err
	}
	if *requireClean && identity.Dirty {
		return "", fmt.Errorf("release qualification requires a clean working tree; the build identity would be %s", identity.EffectiveVersion())
	}
	if *expected != "" {
		wanted, err := buildinfo.ParseVersion(*expected)
		if err != nil {
			return "", err
		}
		// An untagged candidate commit appends build metadata; that metadata does
		// not change version precedence. A commit whose release tag disagrees with
		// the requested version is a real conflict and fails.
		resolved := strings.SplitN(identity.Version, "+", 2)[0]
		switch {
		case resolved == wanted.Normalized:
		case identity.Tagged:
			return "", fmt.Errorf("release version conflict: requested %s but commit %s carries the release tag %s", wanted.Normalized, identity.Revision, identity.Version)
		default:
			return "", fmt.Errorf("release version mismatch: requested %s but the planned release version is %s (resolved identity %s)", wanted.Normalized, resolved, identity.EffectiveVersion())
		}
	}
	return identity.EffectiveVersion(), nil
}

func kubernetes(arguments []string) error {
	flags := flag.NewFlagSet("kubernetes", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	minor := flags.String("version", "", "supported Kubernetes minor")
	platform := flags.String("platform", releasemanifest.HostPlatform(), "host platform")
	field := flags.String("field", "", "minimum-supported, release-validation, envtest-version, envtest-sha512, envtest-url, or kind-node-image")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	compatibility := manifest.Release.Kubernetes
	switch *field {
	case "minimum-supported":
		fmt.Println(compatibility.MinimumSupported)
		return nil
	case "release-validation":
		fmt.Println(strings.Join(compatibility.ReleaseValidation, " "))
		return nil
	}
	profile, err := compatibility.Profile(*minor)
	if err != nil {
		return err
	}
	switch *field {
	case "envtest-version":
		fmt.Println(profile.EnvtestVersion)
	case "envtest-sha512":
		digest, ok := profile.EnvtestSHA512[*platform]
		if !ok {
			return fmt.Errorf("Kubernetes %s has no envtest asset for %s", profile.Minor, *platform)
		}
		fmt.Println(digest)
	case "envtest-url":
		fmt.Printf("https://github.com/kubernetes-sigs/controller-tools/releases/download/envtest-v%s/envtest-v%s-%s.tar.gz\n", profile.EnvtestVersion, profile.EnvtestVersion, strings.ReplaceAll(*platform, "/", "-"))
	case "kind-node-image":
		fmt.Println(profile.KindNodeImage)
	default:
		return fmt.Errorf("unsupported Kubernetes metadata field %q", *field)
	}
	return nil
}

func metadata(arguments []string) error {
	flags := flag.NewFlagSet("metadata", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	toolName := flags.String("tool", "", "tool name")
	field := flags.String("field", "version", "metadata field")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	tool, err := manifest.Tool(*toolName)
	if err != nil {
		return err
	}
	if *field != "version" {
		return fmt.Errorf("unsupported metadata field %q", *field)
	}
	fmt.Println(tool.Version)
	return nil
}

func fetchEngine(arguments []string) error {
	flags := flag.NewFlagSet("fetch-engine", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	name := flags.String("engine", "", "engine name")
	platform := flags.String("platform", "", "target platform")
	output := flags.String("output", "", "output executable")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	engine, err := manifest.Engine(*name)
	if err != nil {
		return err
	}
	download, ok := engine.UpstreamAssets[*platform]
	if !ok || *output == "" {
		return fmt.Errorf("engine %s has no artifact for %q or output is empty", engine.Name, *platform)
	}
	return releasemanifest.Fetch(context.Background(), nil, download, *output, true)
}

func prepareEngineSource(arguments []string) error {
	flags := flag.NewFlagSet("prepare-engine-source", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	name := flags.String("engine", "", "engine name")
	output := flags.String("output-dir", "", "new source directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	engine, err := manifest.Engine(*name)
	if err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("output directory is required")
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		return fmt.Errorf("create source parent directory: %w", err)
	}
	temporary, err := os.MkdirTemp(filepath.Dir(*output), ".egressfox-source-*")
	if err != nil {
		return fmt.Errorf("create source temporary directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	archive := filepath.Join(temporary, engine.Source.Archive.FileName)
	if err := releasemanifest.Fetch(context.Background(), nil, engine.Source.Archive, archive, false); err != nil {
		return err
	}
	if err := releasemanifest.ExtractSourceArchive(archive, *output); err != nil {
		return err
	}
	if engine.Build.OverlayDirectory != "" {
		if err := copyOverlay(engine.Build.OverlayDirectory, *output); err != nil {
			return err
		}
	}
	changes := fmt.Sprintf("# EgressFox engine build changes\n\nUpstream: %s %s (%s)\nEgressFox build revision: %d\n\n- Minimum dependency requirements are raised by the exact entries in release/manifest.json; Go minimal version selection may choose a higher requirement from the source graph.\n- The resolved go.mod and go.sum in this archive record the actual graph.\n- The release build is static, stripped, and limited to the feature tags in that manifest.\n", engine.Source.Repository, engine.Source.Tag, engine.Source.Commit, engine.Build.Revision)
	if engine.Name == "sing-box" {
		changes += "- The command is branded `egressfox-engine-s`; it is not endorsed by or associated with the upstream application.\n"
	}
	if err := os.WriteFile(filepath.Join(*output, "EGRESSFOX-CHANGES.md"), []byte(changes), 0o644); err != nil {
		return fmt.Errorf("write engine change notice: %w", err)
	}
	return nil
}

func copyOverlay(source, destination string) error {
	return filepath.WalkDir(source, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if strings.HasSuffix(target, ".overlay") {
			target = strings.TrimSuffix(target, ".overlay")
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("overlay contains non-regular file %s", name)
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
}

func overrides(arguments []string) error {
	flags := flag.NewFlagSet("overrides", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	name := flags.String("engine", "", "engine name")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	engine, err := manifest.Engine(*name)
	if err != nil {
		return err
	}
	for _, override := range engine.Build.DependencyOverrides {
		fmt.Println(override)
	}
	return nil
}

func engineBuild(arguments []string) error {
	flags := flag.NewFlagSet("engine-build", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	name := flags.String("engine", "", "engine name")
	field := flags.String("field", "", "package, tags, revision, version, or binary")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	engine, err := manifest.Engine(*name)
	if err != nil {
		return err
	}
	switch *field {
	case "package":
		fmt.Println(engine.Build.Package)
	case "tags":
		fmt.Println(strings.Join(engine.Build.Tags, ","))
	case "revision":
		fmt.Println(engine.Build.Revision)
	case "version":
		fmt.Println(engine.Version)
	case "binary":
		fmt.Println(engine.Build.BinaryName)
	default:
		return fmt.Errorf("unsupported engine build field %q", *field)
	}
	return nil
}

func fetchSources(arguments []string) error {
	return fetchEngineMaterials("fetch-sources", arguments, true)
}

func fetchLicenses(arguments []string) error {
	return fetchEngineMaterials("fetch-licenses", arguments, false)
}

func fetchEngineMaterials(command string, arguments []string, includeSources bool) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	output := flags.String("output-dir", "", "source/license output directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("output directory is required")
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	for _, engine := range manifest.Engines {
		downloads := []releasemanifest.Download{engine.LicenseFile}
		if includeSources {
			downloads = append([]releasemanifest.Download{engine.Source.Archive}, downloads...)
		}
		for _, download := range downloads {
			if err := releasemanifest.Fetch(context.Background(), nil, download, filepath.Join(*output, download.FileName), false); err != nil {
				return err
			}
		}
	}
	return nil
}

func installTool(arguments []string) error {
	flags := flag.NewFlagSet("install-tool", flag.ContinueOnError)
	manifestName := flags.String("manifest", defaultManifest, "release manifest")
	name := flags.String("tool", "", "tool name")
	platform := flags.String("platform", releasemanifest.HostPlatform(), "host platform")
	output := flags.String("output", "", "output executable")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	manifest, err := releasemanifest.Load(*manifestName)
	if err != nil {
		return err
	}
	tool, err := manifest.Tool(*name)
	if err != nil {
		return err
	}
	download, ok := tool.Assets[*platform]
	if !ok || *output == "" {
		return fmt.Errorf("tool %s has no artifact for %q or output is empty", tool.Name, *platform)
	}
	return releasemanifest.Fetch(context.Background(), nil, download, *output, true)
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
