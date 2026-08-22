---
name: product-intake
description: Collect missing product facts and reference-image requirements for a ProductFlow workflow draft.
---

# Product Intake

Use when the user is creating or completing a product workspace.

Read product context before asking questions. Treat uploaded reference asset IDs and user choices as authoritative. Ask only for missing facts that change the planned output. Inspect explicitly selected images when visual evidence is needed.

If intake is empty, persist it from this conversation: use the attached or listed product asset IDs plus the image types the user named. Call `finalize_product_intake_v1`. Do not send the user to a create form.

When the required facts are present, call `propose_workflow_draft` with a complete schema-valid payload. Include the current draft version and real asset IDs returned by ProductFlow. The tool only creates a reviewable proposal; the user must confirm it before materialization.

Do not infer brand, material, dimensions, color, or product features from a filename. Do not edit storage, materialize a workflow, or start a run.
