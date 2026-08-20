---
name: workflow-draft
description: Create or revise a schema-v2 ProductFlow WorkflowDraft for review.
---

# Workflow Draft

Use for requests to design or adjust a schema-v2 ProductFlow WorkflowDraft.

## Required sequence

1. Load `productflow-core`, then load this skill with `load_productflow_skill`.
2. Call `get_product_workflow_context_v1` and read the returned `draft_guidance`, current WorkflowDraft revision, intake, facts, reference bindings, and any recipe or legacy seed.
3. Inspect only explicitly selected or necessary verified image assets. Ask one focused `ask_user` question when a missing fact or choice changes the result. After the answer, read the current context again.
4. Build one complete `WorkflowDraftPayloadV1` from current facts. Preserve confirmed intake image types and quantities unless the user explicitly confirms a change.
5. Run the pre-submit checklist below in your own reasoning, then call `propose_workflow_draft` once with the complete payload.

## Cross-field preflight

- `delivery_spec.fit=contain` requires `crop_anchor` to be `null` or omitted.
- `delivery_spec.fit=cover` requires `background_color` to be `null` or omitted.
- Every `visual_exceptions[].overrides[].field` must appear in `visual_system.payload.locked_fields`. If an exception overrides `spacing`, include `spacing` in `locked_fields`.
- Every image type `quantity` must equal its `images` length. Image plan keys and prompt plan keys must remain unique and linked one-to-one.
- `prompt_plans[].fact_keys` must refer to facts in the same draft. Reference bindings must use real, verified asset IDs from current ProductFlow context.
- The payload must contain the complete current artifact. Do not submit a patch, copied recipe, legacy archive payload, or prose-only plan.

## Validation recovery

ProductFlow may return a structured `workflow_draft_validation_failed` error with up to eight `issues`, each containing a `path` and `message`. Repair all listed paths in the complete payload before retrying. An error on a cross-field path is an instruction to fix the relationship, not a request for the user to restate the design. Do not retry the same payload.

An accepted result with `pending_confirmation=true` means the proposal is reviewable. It does not materialize or confirm the workflow.

Never update the online workflow directly, call a confirmation route, start a WorkflowRun, or treat assistant prose as a saved draft.
