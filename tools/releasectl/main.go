package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/egressfox-io/egressfox/internal/releasemanifest"
)

const defaultManifest = "release/manifest.json"

func main() {
	if len(os.Args) < 2 {
		fail("usage: releasectl <validate|fetch-engine|fetch-sources|install-tool>")
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
	case "fetch-engine":
		err = fetchEngine(os.Args[2:])
	case "fetch-sources":
		err = fetchSources(os.Args[2:])
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
	fmt.Printf("release manifest valid: %d engines, %d tools, %d platforms\n", len(manifest.Engines), len(manifest.Tools), len(manifest.Release.Platforms))
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
	download, ok := engine.Assets[*platform]
	if !ok || *output == "" {
		return fmt.Errorf("engine %s has no artifact for %q or output is empty", engine.Name, *platform)
	}
	return releasemanifest.Fetch(context.Background(), nil, download, *output, true)
}

func fetchSources(arguments []string) error {
	flags := flag.NewFlagSet("fetch-sources", flag.ContinueOnError)
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
		for _, download := range []releasemanifest.Download{engine.Source.Archive, engine.LicenseFile} {
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
