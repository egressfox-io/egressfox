# GitHub repository presentation and settings

These recommendations prepare `egressfox-io/egressfox` for its first public push.
They are manual GitHub settings, not claims about current remote configuration.

## Public metadata

**Description**

> Adaptive egress control plane for reliable external connectivity with Mihomo, sing-box, and Kubernetes.

**Website**

Leave the website field empty until an actual maintained public project site or
documentation domain exists. Do not use `egressfox.io` merely because it is the API
domain.

**Topics**

```text
kubernetes egress networking proxy operator golang mihomo sing-box devops
```

Use this restrained set rather than unrelated discovery or VPN-bypass terms.

## Repository features

- **Issues:** enable; the repository contains structured bug and feature forms.
- **Discussions:** leave disabled until maintainers intend to moderate open-ended
  support and community discussion.
- **Projects:** leave disabled until a maintained public planning board exists.
- **Wiki:** disable; versioned documentation in `docs/` is authoritative.

`CHANGELOG.md` is generated from emoji Conventional Commits by explicit release
preparation, then committed before the release tag.
GitHub Releases remain the publication record and carry the release artifacts and
notes extracted from the exact version section.
Internal milestones and release dry-runs do not create published release entries.

## Social preview and branding

No canonical logo or social-preview asset exists. Do not upload an improvised image.
A future preview should be 1280×640 pixels, readable in light and dark contexts, and
contain only the canonical mark, “EgressFox”, the phrase “Adaptive egress control
plane”, and a restrained source → evidence → selection → validated configuration
motif. Establish the logo first so the preview does not create a competing identity.

## Safety and governance settings

Before the first public push or runtime release, maintainers should:

1. Set `main` as the default branch and protect it from force-pushes and deletion.
2. Require the `Repository checks` workflow and `Changelog validation` check for
   pull requests after each has a successful run. Use pull requests for normal work
   and keep approval rules workable for the single-maintainer project; do not require
   a second maintainer's code-owner approval for the sole maintainer's own changes.
3. Keep default Actions permissions read-only and disallow unreviewed third-party
   Actions. Repository workflows already pin Actions by commit SHA.
4. Enable private vulnerability reporting and security-alert notifications as
   described in [SECURITY.md](../../SECURITY.md).
5. Create the protected `release` environment with required reviewers and restrict
   release tags before enabling publication.
6. Confirm GHCR package visibility, retention and repository inheritance before
   publishing the first image. Limit package write/admin access to release maintainers;
   GHCR tag names are not treated as immutable by the workflow. If account settings
   offer tag immutability, enable it for version tags and verify it separately.
7. Review the public Git history and repository contents once more before the initial
   push; do not rewrite authorship merely for presentation.

The changelog workflow is `.github/workflows/changelog.yml`. After it reports its
first successful check, add the exact **`Changelog validation`** status check to the
`main` branch ruleset's required checks. The workflow tests read-only changelog
generation on pull requests and never commits to a branch.
`CODEOWNERS` identifies the current owner for review routing; it does not itself
require approval.

A formal Code of Conduct is intentionally deferred until maintainers designate a
real moderation contact and enforcement process. The repository must not publish an
unreachable address or make an unenforceable promise merely to satisfy a checklist.
