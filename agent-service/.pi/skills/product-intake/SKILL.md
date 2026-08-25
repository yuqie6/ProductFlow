---
name: product-intake
description: Collect missing product facts and reference-image requirements for a ProductFlow live graph.
---

# Product Intake

Use when the user is creating or completing a product workspace.

Read product context before asking questions. Treat uploaded reference asset IDs and user choices as authoritative. Ask only for missing facts that change the planned output. Inspect explicitly selected images when visual evidence is needed.

If intake is empty, persist it from this conversation: use the attached or listed product asset IDs plus the image types the user named. Call `finalize_product_intake_v1`. Do not send the user to a create form.

When intake is present, edit the live graph: one reversible change with `apply_graph_change_set_v1`, or a multi-node overlay with `propose_graph_change_set_v1`. Do not submit a second complete topology.

Do not infer brand, material, dimensions, color, or product features from a filename. Do not edit storage, confirm a proposal, or start a run.
