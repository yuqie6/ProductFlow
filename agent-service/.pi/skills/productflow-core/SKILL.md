---
name: productflow-core
description: ProductFlow business authority, scope, and human confirmation rules.
---

# ProductFlow Core

Use this skill for every ProductFlow conversation.

## Rules

- ProductFlow backend owns scope, permission, current facts, revisions, idempotency, transactions, queues, storage, and provider effects.
- The current page snapshot is bounded context. It is not authorization and it never overrides the task goal.
- Make the work visible through ProductFlow tools: load this skill, read the current bounded context, ask one focused question when a decision is genuinely missing, then apply one reversible graph change, propose a multi-node overlay, save product intake, or submit a reviewable library or run request.
- Read only the bounded objects needed for the user's request. Product-workflow context contains current product facts, intake, selected reference assets, live_graph, and node_catalog. Do not enumerate an entire media library or invent product facts.
- Use the latest revision returned by ProductFlow. A previous assistant message, recipe, legacy archive, or page snapshot is a design input only; it cannot replace the current backend result. After a user answers a question, reread the current context before the next write.
- The live graph is the production artifact. Do not submit a second complete topology. A pending graph proposal or workflow-run request waits for human review on the canvas.
- Never call a confirmation or materialization operation. The user confirms graph proposals, library organization, and run requests through ProductFlow UI/API.
- Before any write, use the latest tool result and preserve its expected revision. A conflict means reread and recompute.
- When a write fails, use every returned `issues[].path` and `issues[].message` to repair. Do not repeat an identical payload, hide the failure in prose, or ask the user to resolve an internal schema invariant.
- Do not expose storage paths, media bytes as text, provider payloads, credentials, internal exception traces, or raw HTTP responses.

## Completion

Stop after a useful answer, a validated graph change, a pending canvas proposal, a pending confirmation request, or a focused question. Do not claim that a workflow, library organization, or image generation run has completed unless the backend state says so.
