# Runtime, standalone and publication architecture documentation

Status: active. Date: 2026-09-22. Branch: `docs/runtime-standalone-architecture`.
Baseline: `c961b5b`.

## Objective and boundaries

Record the accepted post-P1 control-plane architecture before implementation. Keep
current M1–M7 and release behavior explicit, preserve the M8–M12 order, and make the
standalone/runtime/publication work a discoverable successor to P1.

This is documentation and design only. It does not change Go behavior, CRDs,
generated manifests, Helm, Dockerfiles, release metadata/tooling, workflows, engine
builds/overlays, or any P1 implementation. No tag, release, push, or publication is
in scope.

## Baseline and discovery

- The checkout was clean on `main` at `c961b5b`; a dedicated branch was created
  before substantive edits.
- Read root `AGENTS.md`, the development workflow, product/architecture maps,
  current P0/P1 roadmaps, ADR and plan indexes, ADRs 0001, 0002, 0008–0014, open
  questions, endpoint/source, observation/selection, publication and Kubernetes
  designs, threat model, release/versioning guides and test strategy.
- Inspected command entry points, core package placement, current engine build
  metadata and overlays, OCI Dockerfile, Make/release tooling, current Gateway API,
  operator/controller activation, Helm and generated CRDs to establish shipped
  behavior only.

### Current reality and contradictions found

- M1–M5 provide the Kubernetes-independent Go core; M6 adds the namespace-scoped
  BYO operator and owned Secret publication; M7 adds explicit managed engines and
  exact-generation activation. M8 resilient HTTP source refresh remains next.
- The only product executable is currently `egressfox-operator`. There is no
  `egressfox` product CLI, standalone daemon entry point, or `egressfox-runtime`.
  M7 starts the selected engine directly. The fixed `egressfox-healthcheck` proves
  local authenticated SOCKS negotiation and has a narrow readiness purpose.
- The current OCI image contains the operator, health helper, Mihomo binary at
  `/usr/local/libexec/egressfox/mihomo`, and the sing-box-derived
  `egressfox-engine-s`. Current Mihomo build metadata uses `with_gvisor`; sing-box
  has no build tags and has a reviewed branding overlay. Full upstream production
  feature parity has not been established. No build inputs are changed by this work.
- M7 already uses immutable exact-generation Secrets, a stable Service, one replica,
  `maxUnavailable: 0`, `maxSurge: 1`, and previous-ready-generation retention on
  rollout failure. Readiness currently verifies the local authenticated SOCKS
  handshake, not arbitrary destination health. Future requirements refine the
  activation contract without rewriting M7 history.
- Current BYO API requires `outputSecretName`; managed mode has an internal
  generation Secret. No `EgressOutput`, Vault/S3 publisher, or reusable sink exists.
- The README/product wording presents standalone operation and engine ownership as
  current; the feature catalog still describes a Rust/Ratatui primary CLI as
  deferred; the roadmap does not lead from M12 to the accepted standalone track.
  P1's one-pool wording and output deferrals need future-composition clarification.

## Durable decisions to record

- EgressFox is an adaptive egress control plane with Kubernetes and standalone as
  intended deployment frontends over one shared Go core. Engines remain the traffic
  dataplane; bounded policy routing intent is rendered to engines.
- Future process/package roles: Go `egressfox`, Kubernetes `egressfox-operator`,
  thin execution/lifecycle `egressfox-runtime`, and private managed binaries
  `egressfox-engine-m` / `egressfox-engine-s`; public engine choices remain Mihomo
  and sing-box.
- Future standalone UX is full single-file by default, with separately executed
  release-bundled engine/runtime payloads materialized lazily and safely. A future
  canonical OCI image contains thin control-plane/runtime roles and one set of
  canonical engine/runtime bytes. No hidden engine download is allowed.
- A validated artifact is independent of managed activation and zero-or-many
  external publications. Exact private content equality is distinct from a safe
  public generation/revision. Publishers consume the same validated bytes and are
  deployment-independent.
- Future external outputs begin with Kubernetes Secret, Vault, S3-compatible object
  storage, filesystem and stdout. A future `EgressOutput` is independent of basic
  managed Gateway activation. Current output-Secret and internal M7 generation
  behavior remain documented until deliberate API migration. No `EgressSink` is
  planned.
- `ProxyPool` remains a leaf inventory that may combine multiple sources. Future
  candidate composition consumes multiple Pool/Profile inputs in policy/selection;
  pools do not recursively contain pools.
- Equivalent deterministic desired artifacts do not create a new artifact
  generation or runtime rollout. Reconciliation is change-driven and repair-driven;
  missing/drifted destinations may be repaired with the same generation.
- Healthy LKG remains active until a validated replacement is runtime-ready; failed
  replacement and external-output failure preserve it. Kubernetes rollout is
  surge-first, service identity and client credentials are stable across artifact
  changes, and existing sessions are drained best-effort without a promise that
  every long-lived connection survives process replacement.

## Historical decisions and transition

ADR 0012 remains authority for current engine redistribution, GPL/source/notices,
SBOM, scanning, signing, provenance and protected publication. ADR 0013 remains
historical and current authority for the implemented M7 exact-generation managed
activation. ADR 0014 remains authority for release identity and Kubernetes
qualification. ADRs 0015–0017 add future product/runtime, artifact/publication and
composition decisions; they do not rewrite those records or claim future packaging
and outputs already ship. ADRs 0002 and 0003 receive dated scope clarifications for
the Go standalone frontend and Kubernetes product wording. No decision is
superseded.

## Checkpoints

- [x] Verify baseline, branch, instructions, document authority and implementation
  facts; record this plan before architecture edits.
- [x] Add durable ADR and focused runtime/standalone and artifact/publication design
  authority; update architecture and security boundaries.
- [x] Reconcile README/product, P1/feature/roadmap, open questions, AGENTS and
  documentation indexes while preserving current behavior and milestone order.
- [x] Review all affected Markdown for contradictions and links; run `make check`,
  documentation validation and `git diff --check`; record exact outcomes.
- [x] Commit coherent documentation checkpoints and finish on a clean branch.

## Progress and evidence

The first design checkpoint now records ADRs 0015–0017 and the detailed
`runtime-and-standalone.md` and `artifact-publication-and-composition.md` designs.
`architecture.md`, current publication/Kubernetes designs, threat model, ADR index,
documentation map, and ADR 0002/0003 scope notes link the authority while retaining
M7 and release facts. A second documentation checkpoint reconciles README/product,
P1/feature/roadmap, root agent guidance, decision queue and release wording. It
preserves the M8–M12 implementation order and records S1–S4 as undated post-P1
stages only. The initial plan and architecture checkpoints are committed as
`a2f7301` and `2db3788`; this final checkpoint completes the product/roadmap
reconciliation and execution record.

## Unresolved details intentionally left open

- Exact standalone command syntax, flags, config schema and precedence, output
  format/exit codes, persistent-history opt-in and thin-build capability reporting.
- Runtime CLI and exact signal/readiness/lifecycle contract; secure cache path and
  extraction cleanup/retention details; canonical artifact build mechanics.
- Exact `EgressOutput` schema and migration from Gateway `outputSecretName`,
  required/optional semantics, Vault/S3 auth/key/version/atomicity/retry/receipt and
  retention behavior, and whether a reusable connection object is ever justified.
- Candidate-group/filter/profile reference schema, validation and direct/block
  composition details.
- Exact candidate readiness/activation status, listener handoff, drain timeouts and
  engine signal behavior; protocols whose sessions cannot drain.
- Whether present engine build profiles differ from upstream production feature
  profiles; that requires future version-specific research and qualification.

## Validation and handoff

- `GOCACHE=/private/tmp/egressfox-docs-gocache make docs` — passed; offline links
  and local anchors are valid.
- `GOCACHE=/private/tmp/egressfox-check-gocache make check` — passed, including
  formatting, generated-file consistency, vet, race tests, builds, documentation,
  Helm lint/template, release-manifest/workflow guards and whitespace checks. The
  first sandboxed attempt could not bind localhost test sockets; the same command
  passed when rerun with the permission those local tests require.
- `git diff --check` — passed; no generated tracked API/CRD changes resulted.
- No Go, API/CRD, Helm, Dockerfile, release manifest/tooling, workflow, engine build
  or overlay, runtime, M8, release, tag, push or publication changes were made.
- Documentation and plan work is committed on
  `docs/runtime-standalone-architecture`; the final tree is clean.

No Kubernetes envtest/kind qualification or online vulnerability scan was run; no
claim about those checks is made.
