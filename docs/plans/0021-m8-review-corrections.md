# M8 review corrections before M8.5 C2

Status: complete. Date: 2026-09-24.
Branch: `codex/m8-review-corrections`; baseline `3509503`.

## Confirmed findings and boundary

- `source.destinationAllowed` unmapped IPs but accepted any `IsPrivate()` IPv6
  address under `allowPrivateNetworks`, including AWS metadata ULA
  `fd00:ec2::254`. IPv4 link-local and `100.100.100.200` were already denied;
  known metadata addresses are now explicitly checked before the private branch.
  Ordinary RFC1918/ULA and the separate loopback opt-in retain their meaning.
  This finite list does not replace cluster egress policy.
- `BuildPoolWithCache` excluded a cache at `ValidatedAt + maxStale`, but the Pool
  controller requeued only by the jittered refresh interval. The refresher
  emits events after attempts, not at passive expiry. The controller now takes
  the earliest accepted cache deadline from the same reader, bounded by the
  normal refresh requeue. On a failed mixed-source snapshot, it rereads each
  HTTP contribution independently so its deadline and source status still
  advance. Expired/missing/corrupt entries supply no future deadline. A
  minimum one-second requeue avoids near-zero deadline loops.

The fixes do not change HWID, cache fingerprint, API, source parsing, managed
activation or Gateway LKG. No C2 protocol support is added.

## Exit evidence

- [x] Controlled address-policy tests cover metadata IPv4/IPv6, mapped IPv4,
  ordinary private/ULA, loopback, DNS hostname, retry and redirect dials.
- [x] Fake-client controller tests cover earliest of multiple deadlines,
  refreshed expiry, mixed valid/expired sources, empty inventory, missing and
  corrupt cache, source configuration change/removal, restart of the reconciler,
  error-path scheduling, bounded requeue and retained Gateway generation.
- [x] Focused race tests, `make check`, docs and vulnerability scan completed;
  final diff reviewed and committed.

Validation: `make fmt`; focused `go test -race -count=1` for source policy and
Pool/Gateway expiry tests; `make check` (full race suite, build, offline docs,
Helm and release checks); `make docs`; and `git diff --check` all passed.
`make vuln` reported zero called vulnerabilities and one imported but uncalled
finding. The first unprivileged focused test attempt could not bind a local
loopback fixture; the same tests passed with permitted local networking.

Kubernetes kind E2E and the full Kubernetes compatibility matrix remain for
maintainer qualification and are not claimed by this correction task.
