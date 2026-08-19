---
name: workflow-run-request
description: Prepare a bounded request to run an existing ProductFlow workflow without bypassing confirmation.
---

# Workflow Run Request

Use when the user asks to run or retry an existing workflow.

Read the current workflow and revision. For a product-scoped conversation use `request_workflow_run_v1`; for a global conversation inspect the explicit product and workflow first, then use the global form. Preserve the expected revision and provide a source run ID only for an explicit retry.

The tool creates a pending request. It must not start, cancel, retry, or materialize a WorkflowRun on its own. Tell the user that confirmation is required and rely on the ProductFlow status for later execution results.
