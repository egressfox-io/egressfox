# Public repository polish

Status: in progress. Prepared/started: 2026-09-21.
Branch/baseline: `docs/public-repository-polish` from `e21752f`.

## Objective and boundaries

Prepare the completed P0 and release-hardened repository for its first public GitHub
push. Improve the root landing page, human contributor navigation, community intake,
examples, repository hygiene and GitHub-setting recommendations without changing
product behavior, publishing anything or beginning P1.

The repository remains pre-release alpha. No public image, chart, release, website or
support SLA is implied. Existing architecture, security, compatibility and release
decisions remain authoritative.

## Initial findings and decisions

- The implementation and roadmap support the complete P0 pipeline, exact Mihomo
  1.19.31 and sing-box-compatible 1.14.1 profiles, Kubernetes 1.37.0, and a
  namespace-scoped BYO operator. The README is accurate but reads like a milestone
  summary and does not provide a compact local-build evaluation path.
- No repository logo, documentation image or social-preview asset exists. This task
  will not invent branding; it will recommend a future 1280×640 social preview.
- GitHub CI/release workflows and a lowercase pull request template already exist.
  Add focused issue forms and support routing; improve rather than duplicate the PR
  template. Defer a Code of Conduct until maintainers choose a real enforcement
  contact instead of inventing one.
- GitHub Releases remain the changelog authority for the first alpha. Do not create
  artificial historical release notes before a public release.
- A pre-existing untracked `Archive.zip` is a 48 MB local repository archive that
  includes build output and macOS metadata. Preserve it locally but ensure it is
  ignored and cannot enter the public push accidentally.

## Checkpoints

- [x] Reconstruct Git, documentation, implementation, release and community state.
- [ ] Complete tracked-tree and history hygiene/secret audits; record any blockers.
- [ ] Redesign the README and improve public documentation/example navigation.
- [ ] Add concise issue forms, support guidance and a lightweight PR template.
- [ ] Record GitHub metadata/settings recommendations and deliberate deferrals.
- [ ] Run documentation, repository and template validation; review public claims.
- [ ] Commit coherent checkpoints and leave a clean reviewable branch.

## Resume and handoff

Continue with hygiene and claim audits before editing public prose. Preserve P0/P1
boundaries, avoid remote side effects, and update this record with validation results
and manual GitHub actions before completion.
