package buildinfo

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// DirtySuffix marks a local development identity whose source tree contains
// changes. It is valid SemVer build metadata, so it never changes the
// precedence of the base development version and never appears in a release
// tag.
const DirtySuffix = ".dirty"

// LocalSuffix marks a build that has no Git identity at all.
const LocalSuffix = "+local"

// releaseTag is the only accepted release tag vocabulary. The leading `v` is
// optional so already-normalized values are accepted as well.
var releaseTag = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(dev|alpha|beta)\.([1-9][0-9]*))?$`)

// Version is a validated official release version.
type Version struct {
	// Normalized is the version without the leading `v`. It is the binary
	// version, OCI version/tag, Helm version/appVersion and default image tag.
	Normalized string
	// Class is "dev", "alpha", "beta" or "stable".
	Class string
}

// ParseVersion validates an official release tag or its normalized form.
func ParseVersion(value string) (Version, error) {
	matches := releaseTag.FindStringSubmatch(value)
	if matches == nil {
		return Version{}, fmt.Errorf("release version %q is not an allowed SemVer tag", value)
	}
	class := "stable"
	if matches[4] != "" {
		class = matches[4]
	}
	return Version{Normalized: strings.TrimPrefix(value, "v"), Class: class}, nil
}

// Identity is the resolved build identity of one source tree.
type Identity struct {
	// Version is the identity without the dirty marker: either the single
	// official release tag on the commit or the configured development version
	// plus build metadata.
	Version string
	// Revision is the full source commit, or empty outside Git.
	Revision string
	// Created is the commit timestamp in RFC3339, or empty outside Git.
	Created string
	// Tagged reports whether Version came from an official release tag on the
	// exact commit.
	Tagged bool
	// Dirty reports whether the working tree contains source changes that are
	// not ignored build output.
	Dirty bool
}

// EffectiveVersion is the version an artifact built from this tree must
// report. A dirty untagged tree can never claim the identity of a clean build
// from the same commit.
func (identity Identity) EffectiveVersion() string {
	if !identity.Dirty {
		return identity.Version
	}
	return identity.Version + DirtySuffix
}

// ResolveIdentity determines the build identity of the repository at root.
// developmentVersion is the planned development version from the release
// manifest. A commit carrying an official release tag wins over the
// development version, but a dirty tree never produces an official identity
// and an untagged commit never claims a released one.
func ResolveIdentity(root, developmentVersion string) (Identity, error) {
	parsed, err := ParseVersion(developmentVersion)
	if err != nil {
		return Identity{}, err
	}
	dirty, err := gitTreeDirty(root)
	if err != nil {
		return Identity{}, err
	}
	revision, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		if dirty {
			return Identity{}, fmt.Errorf("cannot mark a local build dirty without Git identity")
		}
		return Identity{Version: parsed.Normalized + LocalSuffix, Dirty: false}, nil
	}
	identity := Identity{Revision: revision, Dirty: dirty}
	if created, err := gitOutput(root, "show", "-s", "--format=%cI", revision); err == nil {
		identity.Created = created
	}
	tags, err := gitOutput(root, "tag", "--points-at", revision)
	if err != nil {
		return Identity{}, err
	}
	var tagged []Version
	for _, tag := range strings.Fields(tags) {
		if parsed, parseErr := ParseVersion(tag); parseErr == nil {
			tagged = append(tagged, parsed)
		}
	}
	switch {
	case len(tagged) > 1:
		return Identity{}, fmt.Errorf("commit %s carries multiple release tags; a build identity must be unambiguous", revision)
	case len(tagged) == 1:
		if dirty {
			return Identity{}, fmt.Errorf("tagged release version %s cannot be built from a dirty working tree", tagged[0].Normalized)
		}
		identity.Version = tagged[0].Normalized
		identity.Tagged = true
		return identity, nil
	}
	if len(revision) < 12 {
		return Identity{}, fmt.Errorf("Git returned a short revision")
	}
	identity.Version = fmt.Sprintf("%s+g%s", parsed.Normalized, revision[:12])
	return identity, nil
}

// gitTreeDirty reports whether tracked files changed or non-ignored files were
// added. Ignored build and release output never marks the tree dirty.
func gitTreeDirty(root string) (bool, error) {
	output, err := gitOutput(root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return false, err
	}
	return output != "", nil
}

func gitOutput(root string, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read Git identity: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
