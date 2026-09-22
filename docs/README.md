# Documentation map

Start with [product and scope](product.md), then [architecture and terminology](architecture.md).
Operators can go directly to the Kubernetes or release guides; contributors should
read the owning design and accepted decisions for the area they plan to change.

## Common paths

| Goal | Start here |
| --- | --- |
| Understand the product boundary | [Product](product.md) and [architecture](architecture.md) |
| Evaluate or operate the Kubernetes integration | [Kubernetes operator guide](operations/kubernetes.md) and [samples](../config/samples/README.md) |
| Verify a release or supported version | [Release guide](operations/releasing.md), [versioning](operations/versioning.md), and [Kubernetes compatibility](operations/kubernetes-compatibility.md) |
| Report or assess security concerns | [Security policy](../SECURITY.md) and [threat model](security/threat-model.md) |
| Contribute a change | [Contributing](../CONTRIBUTING.md), [workflow](development/workflow.md), and [testing](development/testing.md) |
| Review notable project changes | [Changelog](../CHANGELOG.md) |
| Configure the public GitHub repository | [Repository settings](operations/github-repository.md) |

## Sources of truth

| Question | Authoritative location |
| --- | --- |
| What is the product, and what is outside it? | [Product](product.md) |
| What do terms mean, where does code belong, how do boundaries interact? | [Architecture](architecture.md) |
| What are the future standalone process, runtime and packaging boundaries? | [Runtime and standalone design](designs/runtime-and-standalone.md) |
| How do artifacts, external publication, candidate composition and managed activation relate? | [Artifact publication and composition](designs/artifact-publication-and-composition.md) |
| How should identity, normalization, provenance, and source refresh behave? | [Endpoints and sources](designs/endpoints-and-sources.md) |
| How should probes, history, adaptation, explanation, and metrics work? | [Observations and selection](designs/observations-and-selection.md) |
| How do policy, engine differences, validation, and LKG publication work? | [Policy, renderers, publication](designs/policy-rendering-publication.md) |
| How do CRDs, ownership, reconciliation, and runtime modes work? | [Kubernetes design](designs/kubernetes.md), [operator guide](operations/kubernetes.md) |
| What is sensitive and which threat boundaries need controls? | [Threat model](security/threat-model.md), reporting in [SECURITY.md](../SECURITY.md) |
| Which decisions are accepted and why? | [ADR index](decisions/README.md) |
| What remains unresolved and when must it be settled? | [Decision queue](decisions/open-questions.md) |
| What should be implemented next and how is completion measured? | [Roadmap](roadmap/README.md) |
| What is the P1 objective and milestone sequence? | [P1 roadmap](roadmap/p1.md) |
| Which ideas are P0 vs future scope? | [Feature catalog](roadmap/features.md) |
| How do I branch, build, validate, commit, and hand off? | [Workflow](development/workflow.md) |
| Which tests and research experiments belong with a change? | [Testing](development/testing.md) |
| How is substantial work planned and resumed? | [Execution plans](plans/README.md) |
| How is a release built, accepted, published, and verified? | [Release guide](operations/releasing.md) |
| What version forms are valid and how does identity flow through artifacts? | [Versioning](operations/versioning.md) |
| Which Kubernetes versions are qualified and how are they tested? | [Kubernetes compatibility](operations/kubernetes-compatibility.md) |

## Reading status correctly

- **Accepted decision**: a durable architectural constraint recorded in an ADR.
- **Required behavior**: a property an implementation must meet; not evidence that
  the implementation exists.
- **Proposed direction**: the current design hypothesis, to validate before coding
  the affected public/persistent contract.
- **Open question**: unresolved, with a milestone gate in the decision queue.
- **Implemented**: backed by code and validation evidence; current milestone status
  is in the roadmap. This applies to repository tooling and the M1–M7 endpoint,
  source, policy, engine artifact, local publication, observation, probe, history,
  selection, reconciliation, Kubernetes operator, Secret publication and managed
  runtime/activation domains. The standalone product CLI/runtime wrapper and the
  broader publisher family remain future architecture.
- **Future/experimental**: cataloged beyond the current milestone; not current work.

Priority and maturity are independent. P0 does not mean implemented; an accepted
ADR does not freeze all details in its associated design. Conceptual CLI names,
resource fields, metrics, and package paths are not supported interfaces today.

## Maintaining the map

Update the owning document rather than copying specifications into the README or
execution plans. Summaries should link back here. If behavior changes a durable
decision, update/supersede its ADR and the current design together. If authoritative
documents conflict, resolve the conflict explicitly before coding affected behavior.

Upstream findings are linked near the decisions they informed, with the research
date/version where relevant. Recheck version-sensitive details before implementation;
an upstream release existing does not make it supported by EgressFox.

`AGENTS.md` contains concise repository instructions for automated coding tools; it
does not replace the human contribution guide or the authoritative project docs.
