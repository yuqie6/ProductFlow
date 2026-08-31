export const GRAPH_PORT_MAX_VISUAL_SCALE = 1.4;

export function graphPortVisualScale(zoom: number): number {
  if (!Number.isFinite(zoom) || zoom <= 0) {
    return 1;
  }
  return Math.min(GRAPH_PORT_MAX_VISUAL_SCALE, Math.max(1, 1 / zoom));
}

export function graphEdgeEmphasis(input: {
  edgeSelected: boolean;
  sourceSelected: boolean;
  targetSelected: boolean;
}): "active" | "receded" {
  if (input.edgeSelected || input.sourceSelected || input.targetSelected) {
    return "active";
  }
  return "receded";
}

export function graphEdgeDeleteClassName(selected: boolean, hovered: boolean): string {
  return selected || hovered
    ? "!pointer-events-auto scale-100 opacity-100"
    : "!pointer-events-none scale-75 opacity-0";
}
