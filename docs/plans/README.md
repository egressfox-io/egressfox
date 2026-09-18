# Execution plans

Use a plan for work crossing components, changing architecture, introducing a
milestone, or spanning sessions. Tiny localized fixes need only a clear commit/PR.
Plans are durable continuation notes; product/design documents remain authoritative.

| Plan | Status | Purpose |
| --- | --- | --- |
| [0001](0001-repository-bootstrap.md) | Complete | Repository foundation only |
| [0002](0002-endpoint-identity.md) | In progress | M1 endpoint identity and provenance |

Create the next numbered Markdown file using [the template](template.md). Keep it
here as its status changes; no empty active/completed directory hierarchy is needed.
Record the branch, baseline, scope, design gates, checkpoints, tests/results, actual
decisions and remaining work. Use dates and commit IDs where useful; a final commit
need not embed its own impossible-to-know hash. Preserve enough context to resume
without a conversation transcript.

Before a checkpoint commit, update the plan with evidence and next steps. On
completion, mark it complete, update this index and milestone status, and leave
task-owned changes committed according to the [Git contract](../development/workflow.md).
Do not use plans to copy entire designs or hide unresolved decisions as done.
