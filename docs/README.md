# Documentation map

Start with [product and scope](product.md), then [architecture and terminology](architecture.md),
then the owning design for your task. [AGENTS.md](../AGENTS.md) is the operating
contract, not a substitute for these documents. No original conversation is needed.

## Sources of truth

| Question | Authoritative location |
| --- | --- |
| What is the product, and what is outside it? | [Product](product.md) |
| What do terms mean, where does code belong, how do boundaries interact? | [Architecture](architecture.md) |
| How should identity, normalization, provenance, and source refresh behave? | [Endpoints and sources](designs/endpoints-and-sources.md) |
| How should probes, history, adaptation, explanation, and metrics work? | [Observations and selection](designs/observations-and-selection.md) |
| How do policy, engine differences, validation, and LKG publication work? | [Policy, renderers, publication](designs/policy-rendering-publication.md) |
| What is proposed for CRDs, ownership, reconciliation, and runtime modes? | [Kubernetes design](designs/kubernetes.md) |
| What is sensitive and which threat boundaries need controls? | [Threat model](security/threat-model.md), reporting in [SECURITY.md](../SECURITY.md) |
| Which decisions are accepted and why? | [ADR index](decisions/README.md) |
| What remains unresolved and when must it be settled? | [Decision queue](decisions/open-questions.md) |
| What should be implemented next and how is completion measured? | [Roadmap](roadmap/README.md) |
| Which ideas are P0 vs future scope? | [Feature catalog](roadmap/features.md) |
| How do I branch, build, validate, commit, and hand off? | [Workflow](development/workflow.md) |
| Which tests and research experiments belong with a change? | [Testing](development/testing.md) |
| How can another agent resume substantial work? | [Execution plans](plans/README.md) |

## Reading status correctly

- **Accepted decision**: a durable architectural constraint recorded in an ADR.
- **Required behavior**: a property an implementation must meet; not evidence that
  the implementation exists.
- **Proposed direction**: the current design hypothesis, to validate before coding
  the affected public/persistent contract.
- **Open question**: unresolved, with a milestone gate in the decision queue.
- **Implemented**: backed by code and validation evidence; current milestone status
  is in the roadmap. This applies to repository tooling and the M1–M5 endpoint,
  source, policy, engine artifact, local publication, observation, probe, history,
  selection and standalone reconciliation domains.
- **Future/experimental**: cataloged beyond the current milestone; not current work.

Priority and maturity are independent. P0 does not mean implemented; an accepted
ADR does not freeze all details in its associated design. Conceptual CLI names,
resource fields, metrics, and package paths are not supported interfaces today.

## Maintaining the map

Update the owning document rather than copying specifications into README,
AGENTS, or plans. Summaries should link back here. If behavior changes a durable
decision, update/supersede its ADR and the current design together. If authoritative
documents conflict, resolve the conflict explicitly before coding affected behavior.

Upstream findings are linked near the decisions they informed, with the research
date/version where relevant. Recheck version-sensitive details before implementation;
an upstream release existing does not make it supported by EgressFox.

The root agent map follows official [Codex AGENTS.md guidance](https://learn.chatgpt.com/docs/agent-configuration/agents-md):
repository instructions are concise and discoverable, with deeper docs linked by
task. Add nested instructions only when local operating rules genuinely differ;
none are needed at foundation stage.
