---
name: workflow-draft
description: Create or revise a schema-v2 ProductFlow WorkflowDraft for review.
---

# Workflow Draft

Use for requests to design or adjust a workflow.

Read the current product/workflow context and any explicitly selected reference assets first. Preserve confirmed choices and use the current revision returned by ProductFlow. Build the complete versioned draft and submit it through `propose_workflow_draft`.

If the backend rejects the proposal, use only the bounded validation error to repair the payload. A successful tool result means the proposal is reviewable, not materialized.

Never update the online workflow directly, call a confirmation route, start a WorkflowRun, or treat assistant prose as a saved draft.
