# M8 final hardening and subscription identity

Status: complete. Date: 2026-09-24. Branch: `fix/m8-final-hardening`.
Baseline: `8185f56` (M8 and subscription compatibility complete).

## Scope and confirmed findings

The existing M8 cache, conditional refresh, profiles, URI/JSON decoding and
VMess/Shadowsocks renderers are present. Automatic HWID is deterministic from
Pool UID and source ID, so ordinary request changes do not rotate it, but source
renaming does. The source-cache fingerprint intentionally includes effective
headers, URL, format and admission. The current private-network switch allows
every resolved address, including loopback and link-local. macOS native traffic
tests cover VMess/Shadowsocks; the Linux kind managed path used VLESS. The first
Linux VMess attempt confirmed that probe address pinning rebuilt every endpoint
through the VLESS/Trojan-only constructor, leaving VMess/Shadowsocks without
eligible evidence despite admitted source records. The constructor path is now
protocol aware and covered by a focused preservation test.

## Design gates

- Preserve the legacy HWID algorithm for existing sources. Add a durable, explicit
  continuity reference for source renames or moves; source ID/provenance and cache
  key remain separate. No confidential cache retention is needed for identity.
- A source created without a continuity reference uses the existing Pool UID and
  source ID identity. Operators preserve a previous identity explicitly before a
  rename/move. A deliberate reference change rotates automatic HWID; removal of
  a profile override restores the unchanged automatic assignment.
- Private-network permission allows ordinary RFC1918/ULA addresses. Loopback
  needs a separate explicit opt-in; link-local, metadata, multicast and
  unspecified destinations remain prohibited. Authorize every resolved dial
  candidate on every request/retry, including redirects.
- Keep strict explicit formats and private cache fingerprint invalidation. Prove
  previous published artifact LKG survives failed replacement.
- Preserve VMess/Shadowsocks credentials, method, transport and TLS while probe
  execution pins a resolved endpoint address.
- Extend the existing kind harness with controlled VMess and Shadowsocks source,
  both managed engines and authenticated traffic, without a new framework.

## Checkpoints

- [x] Identity API/ADR, legacy migration, override/restoration and wire tests.
- [x] Destination policy and controlled resolution/redirect/retry tests.
- [x] Format/cache/LKG regressions and Linux managed protocol traffic.
- [x] Documentation, full validation, Kubernetes matrix and clean commits.

## Evidence and handoff

The identity continuity reference preserves the exact old HWID with an explicit
legacy reference, and a stable reference covers new portable subscriptions.
Concurrent resolution, rename/move, override/restoration and deliberate reset
tests use controlled wire requests. Source destination classification denies
link-local metadata and special-use addresses even with private permission;
controlled DNS, redirect and retry tests exercise the actual dial path. Format
transition tests prove cache/validator isolation and stable HWID. `make check`
passed, including race tests, generated manifest reproducibility, Helm and docs.
The pinned vulnerability scan reported zero called vulnerabilities. Envtest and
Helm passed on Kubernetes 1.32, 1.34 and 1.37. The complete 1.32 kind scenario
passed: synthetic VMess/TCP with `auto` security and Shadowsocks/TCP with
`aes-128-gcm` carried authenticated SOCKS traffic through both managed Mihomo
and sing-box, and format failure retained active Gateway generations. The
complete 1.34 and 1.37 kind scenarios passed the same assertions. The 1.37
fixture initially failed because BusyBox uClibc ash `read` segfaulted in the
pinned node image; replacing test-only request-header parsing with `awk`
restored the provider, and the full 1.37 scenario passed. Kubernetes 1.32 and
1.34 were then rerun successfully with the final fixture. All kind clusters
were removed. `make k8s-api-compat`, `make e2e-kind K8S_VERSION=1.37`, and
`make k8s-e2e-compat K8S_RELEASE_VALIDATION='1.32 1.34'` passed. `make vuln`
reported zero called vulnerabilities and one imported, uncalled finding.
M8.5 and M9 remain outside scope.
