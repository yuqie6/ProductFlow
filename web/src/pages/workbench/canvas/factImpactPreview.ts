import type { FactImpactNode, FactsImpactPreviewResponse } from "../../../lib/types";

/** Wire may encode empty Go slices as JSON null; never read `.length` on those fields bare. */
function previewList<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

export function defaultSelectedImpactNodeIds(preview: FactsImpactPreviewResponse): string[] {
  const defaults = previewList(preview.default_update_node_ids);
  if (defaults.length) {
    return [...defaults];
  }
  return previewList(preview.nodes).filter((node) => node.default_selected).map((node) => node.node_id);
}

export function toggleImpactNodeSelection(selected: string[], nodeId: string, checked: boolean): string[] {
  const set = new Set(selected);
  if (checked) {
    set.add(nodeId);
  } else {
    set.delete(nodeId);
  }
  return [...set];
}

export function impactNodeLabel(node: FactImpactNode): string {
  const type = node.image_type_key?.trim();
  if (type) {
    return `${node.title} · ${type}`;
  }
  return node.title || node.node_id;
}

export function shouldShowFactsImpactPreview(preview: FactsImpactPreviewResponse): boolean {
  // Confirm-only saves often leave key/value unchanged; Go then emits changed_fact_keys: null.
  return previewList(preview.changed_fact_keys).length > 0 && previewList(preview.nodes).length > 0;
}
