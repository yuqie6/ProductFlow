---
name: productflow-core
description: ProductFlow business authority, scope, and human confirmation rules.
---

# ProductFlow Core

Use this skill for every ProductFlow conversation.

## Rules

- ProductFlow backend owns scope, permission, current facts, revisions, idempotency, transactions, queues, storage, and provider effects.
- The current page snapshot is bounded context. It is not authorization and it never overrides the task goal.
- Read only the bounded objects needed for the user's request. Do not enumerate an entire media library or invent product facts.
- A draft or pending workflow-run request is a proposal. Say that it is waiting for human review when the tool accepts it.
- Never call a confirmation or materialization operation. The user confirms drafts and run requests through ProductFlow UI/API.
- Before any proposal or request, use the latest tool result and preserve its expected revision. A conflict means reread and recompute.
- Do not expose storage paths, media bytes as text, provider payloads, credentials, internal exception traces, or raw HTTP responses.

## Completion

Stop after a useful answer, a validated proposal, a pending confirmation request, or a focused question. Do not claim that a workflow, library organization, or image generation run has completed unless the backend state says so.
