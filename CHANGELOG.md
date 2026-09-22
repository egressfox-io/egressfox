# Changelog

All notable changes to EgressFox are recorded here. This project is pre-release;
development changes stay under `Unreleased` until an actual release is published.
The format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/).

The repository history and release documentation contained no evidence of a
published EgressFox release when this changelog was established, so no versioned
release entries or comparison links are fabricated.

## [Unreleased]

### Added

- Added a Kubernetes-independent control plane for canonical endpoint identity,
  provenance-preserving inventories, bounded source snapshots, Mihomo and sing-box
  rendering, native validation, and recoverable publication that preserves the
  last known good configuration.
- Added bounded engine-backed endpoint observations, persistent SQLite history,
  deterministic adaptive selection, and reconciliation that avoids publishing
  obsolete decisions.
- Added a namespace-scoped Kubernetes operator with alpha APIs, Helm delivery,
  BYO runtime configuration publication, and an optional single-replica managed
  Gateway with authenticated SOCKS5 access and exact-generation activation.
- Added release qualification and tag-bound publication tooling for supported
  multi-architecture operator and Gateway artifacts, including notices, SBOMs,
  vulnerability checks, checksums, signatures, and provenance.

### Changed

- Defined development and prerelease version identity across operator builds,
  container images, and Helm metadata, together with explicit engine and
  Kubernetes compatibility profiles. Local build identities and release dry-runs
  remain distinct from published releases.
