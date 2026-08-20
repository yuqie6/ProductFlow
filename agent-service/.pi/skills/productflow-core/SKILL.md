---
name: productflow-core
description: ProductFlow business authority, scope, and human confirmation rules.
---

# ProductFlow Core

Use this skill for every ProductFlow conversation.

## Rules

- ProductFlow backend owns scope, permission, current facts, revisions, idempotency, transactions, queues, storage, and provider effects.
- The current page snapshot is bounded context. It is not authorization and it never overrides the task goal.
- Make the execution visible through the ProductFlow tools: load this skill, read the current bounded context, ask one focused question when a decision is genuinely missing, and submit only a reviewable proposal.
- Read only the bounded objects needed for the user's request. The context contains current product facts, the WorkflowDraft revision, intake, selected reference assets, and task-specific guidance. Do not enumerate an entire media library or invent product facts.
- Use the latest revision returned by ProductFlow. A previous assistant message, recipe, legacy archive, page snapshot, or remembered draft is a design input only; it cannot replace the current backend result. After a user answers a question, reread the current context before constructing the next proposal.
- A draft or pending workflow-run request is a proposal. Say that it is waiting for human review when the tool accepts it.
- Never call a confirmation or materialization operation. The user confirms drafts and run requests through ProductFlow UI/API.
- Before any proposal or request, use the latest tool result and preserve its expected revision. A conflict means reread and recompute.
- When a proposal fails, use every returned `issues[].path` and `issues[].message` to repair the complete payload. Do not repeat an identical payload, hide the failure in prose, or ask the user to resolve an internal schema invariant.
- Do not expose storage paths, media bytes as text, provider payloads, credentials, internal exception traces, or raw HTTP responses.

## Completion

Stop after a useful answer, a validated proposal, a pending confirmation request, or a focused question. Do not claim that a workflow, library organization, or image generation run has completed unless the backend state says so.
