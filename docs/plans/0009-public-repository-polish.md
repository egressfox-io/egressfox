# Public repository polish

Status: complete. Prepared/completed: 2026-09-21.
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
- [x] Complete tracked-tree and history hygiene/secret audits; record any blockers.
- [x] Redesign the README and improve public documentation/example navigation.
- [x] Add concise issue forms, support guidance and a lightweight PR template.
- [x] Record GitHub metadata/settings recommendations and deliberate deferrals.
- [x] Run documentation, repository and template validation; review public claims.
- [x] Commit coherent checkpoints and leave a clean reviewable branch.

## Results and evidence

The root README now leads with the adaptive egress control-plane identity, a compact
architecture diagram, an implementation-backed capability matrix, explicit P0
limits, a source-based evaluation path, a concise Kubernetes example and curated
security/development navigation. It does not imply that a release or public image
already exists. Human contribution, support and security documents now provide
separate, consistent routes; GitHub issue forms collect sanitized operational
context, and the PR template remains short.

Tracked-tree and all-commit strong-pattern scans found no private-key blocks or
common GitHub, AWS, OpenAI or Slack token formats. No tracked machine-specific home
path, archive, database, editor directory, `.DS_Store` or build output exists. Test
credentials are conspicuously synthetic and use reserved example domains. Existing
Git author names/email addresses are ordinary authorship metadata and were not
rewritten. The local `Archive.zip`, macOS metadata, build outputs and release caches
remain untracked and ignored; the pre-existing archive was not deleted or modified.

No canonical logo or social-preview asset exists, so no visual identity was
invented. A future asset brief and copy-paste repository description/topics are in
the GitHub repository settings guide. GitHub Releases remain the first-alpha
changelog; a Code of Conduct remains deferred until maintainers designate a real
moderation contact and enforcement path.

Validation passed with `make docs`, `make release-validate`, `make check`,
`make vuln`, Ruby parsing of every issue-form YAML file, `git diff --check`, current
tree/history secret-pattern scans and tracked-artifact/path audits. Sixteen sampled
authoritative external documentation and upstream release links returned HTTP 200.
The vulnerability scan found zero reachable vulnerabilities and disclosed one
imported-package finding with no called vulnerable symbol.

Manual work is limited to the GitHub settings documented in
`docs/operations/github-repository.md`: public metadata, default-branch protection,
required checks/review, private vulnerability reporting, protected release
environment and GHCR policy. No remote setting, push, tag or release was changed.
