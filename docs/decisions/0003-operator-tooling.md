# ADR 0003: Established operator tooling and a deferred alpha scaffold

Date: 2026-09-18. Status: accepted.

## Context

Kubernetes-native reconciliation is core product direction, but the proposed
resources do not yet settle state placement, output ownership, and routing-policy
composition. Generated CRDs are already a public contract, even when called alpha.

## Decision

Use Kubebuilder with controller-runtime for the future Go operator, starting the
API in `v1alpha1`. Keep the proposed `egressfox.io` group and resource responsibilities
in the [Kubernetes design](../designs/kubernetes.md) until the API gate is resolved.
Do not scaffold APIs/controllers during bootstrap. Use the actual pinned CLI when
ready; preserve its generated metadata and supported dependency relationships.

Use focused reconcilers, declarative current-state processing, status subresources,
Conditions, observedGeneration, scoped watches/RBAC, leader-scoped work, and
justified ownership/finalizers. These are architectural requirements, not existing
operator features.

## Alternatives

Handwritten imitation scaffolding loses reliable regeneration. Generating all three
proposed CRDs now would harden assumptions before examples/tests. A custom controller
framework adds maintenance without a product-specific need. Operator SDK is viable
but adds no demonstrated benefit to this Go-first foundation.

## Consequences

M6 includes a real design/scaffold step, not merely filling empty reconciler methods.
Recheck the official [compatibility policy](https://book.kubebuilder.io/versions_compatibility_supportability),
[reconciliation guidance](https://book.kubebuilder.io/reference/good-practices), and
[controller-runtime compatibility](https://github.com/kubernetes-sigs/controller-runtime#compatibility)
before choosing versions. No cluster version support is claimed today.

## Scope clarification (2026-09-23)

[ADR 0015](0015-standalone-runtime-and-engine-packaging.md) clarifies that
"Kubernetes-native" in this record describes the operator frontend and its API,
not a Kubernetes-only product. The shared Go core remains Kubernetes-independent;
standalone operation is an accepted future frontend and is not implemented by this
ADR or by the current operator tooling.
