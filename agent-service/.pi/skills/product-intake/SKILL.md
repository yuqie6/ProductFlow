---
name: product-intake
description: Collect missing product facts and reference-image requirements for a ProductFlow live graph.
---

# Product Intake

Use when the user is creating or completing a product workspace.

Read product context before asking questions. Treat uploaded reference asset IDs and user choices as authoritative. Ask only for missing facts that change the planned output. Inspect explicitly selected images when visual evidence is needed.

If intake is empty and the live graph is birth (`product_source` only, or `birth_expandable` will become true after persist): persist intake from this conversation. Use the attached or listed product asset IDs plus the image types the user named. Call `finalize_product_intake_v1`, then reread `get_product_workflow_context_v1`. That tool writes intake and expands the photography/infographic template (one group + prompt + N image nodes per generating type). Do not `propose_graph_change_set_v1` a first complete topology. Do not send the user to a create form.

If intake is already present and the graph is still only `product_source` (`birth_expandable` is true): call `finalize_product_intake_v1` again with the same selection so the template expands. Do not invent `add_node` or `connect`.

When intake is present and the graph is already expanded: edit with Graph Command names from the apply/propose tool schema. One reversible change uses `apply_graph_change_set_v1`. A multi-node overlay uses `propose_graph_change_set_v1`. Do not submit a second complete topology on an already expanded graph.

Do not infer brand, material, dimensions, color, or product features from a filename. Do not edit storage, confirm a proposal, or start a run.
