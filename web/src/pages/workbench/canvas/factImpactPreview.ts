import type { FactImpactNode, FactsImpactPreviewResponse } from "../../../lib/types";

export function defaultSelectedImpactNodeIds(preview: FactsImpactPreviewResponse): string[] {
  if (preview.default_update_node_ids?.length) {
    return [...preview.default_update_node_ids];
  }
  return preview.nodes.filter((node) => node.default_selected).map((node) => node.node_id);
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
  return preview.changed_fact_keys.length > 0 && preview.nodes.length > 0;
}
