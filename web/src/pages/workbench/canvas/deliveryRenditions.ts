import type { WorkflowDeliverySpec } from "../../../lib/types";

export { parseDeliveryPresetCatalog, parseWorkflowDeliverySpec } from "../../../lib/deliveryPresets";

export function replaceDeliverySpec(
  config: Record<string, unknown>,
  deliverySpec: WorkflowDeliverySpec,
): Record<string, unknown> {
  return { ...config, delivery_spec: deliverySpec };
}

export function deliverySpecLabel(spec: WorkflowDeliverySpec): string {
  return `${spec.width} x ${spec.height} ${spec.format.toUpperCase()}`;
}

export function deliverySpecKey(spec: WorkflowDeliverySpec): string {
  return JSON.stringify({
    width: spec.width,
    height: spec.height,
    format: spec.format,
    max_byte_size: spec.max_byte_size ?? null,
    fit: spec.fit,
    background_color: spec.background_color ?? null,
    crop_anchor: spec.crop_anchor ?? null,
  });
}
