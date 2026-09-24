# ADR 0020: Subscription identity and source destination policy

Date: 2026-09-24. Status: accepted. Amends ADR 0018.

## Context

M8 generates a deterministic default HWID from Pool UID and source ID. The
assignment survives ordinary HTTP settings and cache changes, but a source rename
or move rotates it. M8 also lets `allowPrivateNetworks` bypass the entire resolved
address classification, including loopback and cloud metadata link-local ranges.

## Decision

The logical HTTP subscription identity is separate from source provenance and
request/cache identity. `http.clientIdentity` is an optional durable continuity
reference. Omitted identity means `legacy:<Pool UID>/<source ID>` and preserves the
exact M8 HWID algorithm for existing resources. An explicit legacy reference can
carry that assignment across a source rename or Pool move. A new `stable:<opaque>`
reference provides continuity for newly configured subscriptions without coupling
the HWID to Pool UID or source ID. Changing the reference deliberately changes
the automatic HWID. The identity value is non-secret and lives in the Kubernetes
spec; the derived HWID needs no SQLite row, random initialization or cache-body
retention. Multiple workers derive the same value. Reusing an identity across
unrelated subscriptions is a configuration error; the adapter rejects duplicates
within one Pool. Cross-Pool identity reuse is an explicit administrator decision.

A profile's explicit `hwid` takes precedence. Removing it restores the automatic
value from the unchanged client identity. Clean mode transmits no HWID but does not
change its assignment. A source removed and later recreated without an explicit
continuity reference is a new logical subscription if its Pool UID or source ID
changed. Cluster migration requires an explicit stable reference or fixed HWID;
there is no implicit recovery from expired/deleted source-cache content.

`allowPrivateNetworks` authorizes ordinary RFC1918 and IPv6 ULA destinations, not
all special-use addresses. Loopback requires the separate explicit `allowLoopback`
option. Link-local, metadata, multicast, unspecified and reserved/control ranges
remain denied. Classification applies to unmapped resolved IPs immediately before
every dial. Redirects retain same-origin limits and are re-resolved/re-authorized.
These are application controls; cluster egress policy remains defense in depth.

## Alternatives

Random assignment in SQLite would add a schema migration, concurrent first-write
path and a retention policy solely for a reproducible value. Deriving identity
from URL or credentials would rotate on ordinary renewal and risk disclosure.
Inferring continuity from matching source configuration would be ambiguous. A
single private-network bypass would continue to expose metadata destinations.

## Consequences

The API adds two optional HTTP source fields and regenerated CRDs; existing objects
and their HWIDs remain compatible. A deliberate source rename/move must first set
`clientIdentity` to the previous effective reference. Cache keys and fingerprints
remain independent, so a format/URL/header change invalidates stale content and
validators without resetting identity. Existing local HTTP tests use explicit
loopback intent; in-cluster private service tests keep `allowPrivateNetworks`.
