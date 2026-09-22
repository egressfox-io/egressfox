# Execution plans

Use a plan for work crossing components, changing architecture, introducing a
milestone, or spanning sessions. Tiny localized fixes need only a clear commit/PR.
Plans are durable continuation notes; product/design documents remain authoritative.

| Plan | Status | Purpose |
| --- | --- | --- |
| [0001](0001-repository-bootstrap.md) | Complete | Repository foundation only |
| [0002](0002-endpoint-identity.md) | Complete | M1 endpoint identity and provenance |
| [0003](0003-source-inventory.md) | Complete | M2 safe source-to-inventory pipeline |
| [0004](0004-validated-publication.md) | Complete | M3 validated engine artifacts and recoverable file publication |
| [0005](0005-observations-history.md) | Complete | M4 bounded observations and persistent history |
| [0006](0006-adaptive-reconciliation.md) | Complete | M5 adaptive standalone reconciliation |
| [0007](0007-kubernetes-operator.md) | Complete | M6 Kubernetes BYO operator and Secret publication |
| [0008](0008-release-hardening.md) | Complete | First alpha release hardening and Q12 resolution |
| [0009](0009-public-repository-polish.md) | Complete | First-public-push presentation, community files and hygiene |
| [0010](0010-p1-product-roadmap.md) | Complete | P1 product objective, milestones, API gates and implementation order |
| [0011](0011-managed-gateway.md) | Complete | M7 managed gateway and exact-generation activation |
| [0012](0012-development-versioning-kubernetes-compatibility.md) | Complete | Development versioning and Kubernetes compatibility |

Create the next numbered Markdown file using [the template](template.md). Keep it
here as its status changes; no empty active/completed directory hierarchy is needed.
Record the branch, baseline, scope, design gates, checkpoints, tests/results, actual
decisions and remaining work. Use dates and commit IDs where useful; a final commit
need not embed its own impossible-to-know hash. Preserve enough context for another
contributor to resume without private task context.

Before a checkpoint commit, update the plan with evidence and next steps. On
completion, mark it complete, update this index and milestone status, and leave
task-owned changes committed according to the [Git contract](../development/workflow.md).
Do not use plans to copy entire designs or hide unresolved decisions as done.
