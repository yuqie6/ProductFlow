# WorkflowDraft Validation Reference

This reference is a compact pre-submit checklist for ProductFlow WorkflowDraftPayloadV1.

## Delivery fields

- `fit=contain`: set `crop_anchor` to `null` or omit it.
- `fit=cover`: set `background_color` to `null` or omit it.
- Keep width, height, format, and total pixel limits valid.

## Visual exceptions

Each visual exception override is allowed only for a field listed in `visual_system.payload.locked_fields`. The list is a contract, not a description. A `spacing` override therefore requires `spacing` in the locked fields list.

## Linked plans

- `image_types[].quantity` equals `image_types[].images.length`.
- Each image type has exactly one prompt plan.
- `prompt_plan_key`, `image_plan_key`, and linked node keys refer to the current complete payload.
- `prompt_plans[].fact_keys` refer to facts in the same draft.

## Recovery

Use the structured validation issues returned by ProductFlow. Fix all paths in the complete payload, then submit the repaired artifact. Do not send a patch or repeat an unchanged artifact.
