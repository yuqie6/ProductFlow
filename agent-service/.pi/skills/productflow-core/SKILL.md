---
name: productflow-core
description: ProductFlow business authority, live-graph collaboration, global scope, and human confirmation rules.
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
- Only tools present in this turn's tool list exist. Legacy archive list/inspect tools appear only on the history page.

## Product-workflow loop

1. Call `get_product_workflow_context_v1`. `node_catalog.config_fields` is the only inspector write surface. `live_graph` is topology without full config bodies.
2. If intake is empty, load `product-intake`.
3. Use `ask_user` only when a missing fact changes the run or explanation.
4. One reversible edit (one node config, one edge, one rename): `apply_graph_change_set_v1` with exactly one operation.
5. Multi-node reconstructs, bulk deletes, or preset overlays: `propose_graph_change_set_v1`. Do not claim the graph already changed.
6. The user asks to run: `request_workflow_run_v1`. Do not claim the run started.

## Global scope

Do not apply or propose graph changes on a global conversation. Inspect one product with `inspect_global_workflow_context_v1`, then send the user to that product workbench, or call `create_product_workspace_v1` for a new canvas session. Library writes use `propose_global_draft` with `draft_kind=library_organization` only.

## Completion

Stop after a useful answer, a validated graph change, a pending canvas proposal, a pending confirmation request, or a focused question. Do not claim that a workflow, library organization, or image generation run has completed unless the backend state says so.
