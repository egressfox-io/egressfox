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

GitHub Releases are the changelog for the initial alpha series. Do not create an
artificial changelog for internal milestones. Add a `CHANGELOG.md` later only if the
project adopts a maintained changelog process distinct from release notes.

## Social preview and branding

No canonical logo or social-preview asset exists. Do not upload an improvised image.
A future preview should be 1280×640 pixels, readable in light and dark contexts, and
contain only the canonical mark, “EgressFox”, the phrase “Adaptive egress control
plane”, and a restrained source → evidence → selection → validated configuration
motif. Establish the logo first so the preview does not create a competing identity.

## Safety and governance settings

Before the first public push or runtime release, maintainers should:

1. Set `main` as the default branch and protect it from force-pushes and deletion.
2. Require the `Repository checks` workflow for pull requests after its first
   successful public run; require review rather than direct pushes for normal work.
3. Keep default Actions permissions read-only and disallow unreviewed third-party
   Actions. Repository workflows already pin Actions by commit SHA.
4. Enable private vulnerability reporting and security-alert notifications as
   described in [SECURITY.md](../../SECURITY.md).
5. Create the protected `release` environment with required reviewers and restrict
   release tags before enabling publication.
6. Confirm GHCR package visibility and retention before publishing the first image.
7. Review the public Git history and repository contents once more before the initial
   push; do not rewrite authorship merely for presentation.

A formal Code of Conduct is intentionally deferred until maintainers designate a
real moderation contact and enforcement process. The repository must not publish an
unreachable address or make an unenforceable promise merely to satisfy a checklist.
