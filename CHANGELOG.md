# Changelog

All notable changes to EgressFox are recorded here. The first development release
section is prepared for review; no release has been published yet.
The format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/).

Publication dates are recorded by GitHub Releases. A version section is finalized
in the reviewed tagged commit before publication; it does not claim publication.

## [Unreleased]

## [v0.1.0-dev.1]

### Added

- ✨ Added a Kubernetes-independent control plane for canonical endpoint identity,
  provenance-preserving inventories, bounded source snapshots, Mihomo and sing-box
  rendering, native validation, and recoverable publication that preserves the
  last known good configuration.
- ✨ Added bounded engine-backed endpoint observations, persistent SQLite history,
  deterministic adaptive selection, and reconciliation that avoids publishing
  obsolete decisions.
- ✨ Added a namespace-scoped Kubernetes operator with alpha APIs, Helm delivery,
  BYO runtime configuration publication, and an optional single-replica managed
  Gateway with authenticated SOCKS5 access and exact-generation activation.
- ✨ Added release qualification and tag-bound publication tooling for supported
  multi-architecture operator and Gateway artifacts, including notices, SBOMs,
  vulnerability checks, checksums, signatures, and provenance.

### Changed

- ♻️ Defined development and prerelease version identity across operator builds,
  container images, and Helm metadata, together with explicit engine and
  Kubernetes compatibility profiles. Local build identities and release dry-runs
  remain distinct from published releases.

### Security

- 🔒 Prevented a release rerun from overwriting a published GHCR version tag and
  required reviewed changelog notes before publication.
