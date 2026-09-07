/** 受控二维排版（IQ-CF-06 / CF-B4）浏览器侧：同输入 Document 层框预览。 */

export const LAYOUT_SCHEMA_VERSION = 1 as const;

export type LayoutAlign = "left" | "center" | "right";
export type LayoutVAlign = "top" | "middle" | "bottom";
export type LayoutLayerType = "image" | "text" | "shape";

export type LayoutInsets = {
  top: number;
  right: number;
  bottom: number;
  left: number;
};

export type LayoutLayer = {
  id: string;
  type: LayoutLayerType;
  x: number;
  y: number;
  width: number;
  height: number;
  z_index: number;
  asset_id?: string;
  text?: string;
  font_id?: string;
  font_size?: number;
  color?: string;
  align?: LayoutAlign;
  valign?: LayoutVAlign;
  shape?: "rect";
  fill?: string;
};

export type LayoutDocument = {
  schema_version: number;
  width: number;
  height: number;
  background?: string;
  safe_area: LayoutInsets;
  subject_asset_id: string;
  layers: LayoutLayer[];
};

export type LayoutFrame = {
  id: string;
  type: LayoutLayerType;
  x: number;
  y: number;
  width: number;
  height: number;
  z_index: number;
};

export type LayoutPlanLayer = LayoutFrame & {
  asset_id?: string;
  outside_safe?: boolean;
};

export type LayoutPlan = {
  schema_version: number;
  width: number;
  height: number;
  safe_area: LayoutInsets;
  layers: LayoutPlanLayer[];
};

/** 与 Go sortedLayers 一致：z_index 升序，同级按 id。 */
export function sortedLayers(layers: LayoutLayer[]): LayoutLayer[] {
  return [...layers].sort((a, b) => {
    if (a.z_index !== b.z_index) return a.z_index - b.z_index;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

/** 预览层外接框；与导出 LayoutPlan 框对齐（备忘上限 ≤1px）。 */
export function previewFrames(doc: LayoutDocument): LayoutFrame[] {
  return sortedLayers(doc.layers).map((layer) => ({
    id: layer.id,
    type: layer.type,
    x: layer.x,
    y: layer.y,
    width: layer.width,
    height: layer.height,
    z_index: layer.z_index,
  }));
}

export function compareFrames(
  preview: LayoutFrame[],
  plan: LayoutPlan,
  tolerancePx = 1,
): string | null {
  if (preview.length !== plan.layers.length) {
    return `层数不一致 preview=${preview.length} plan=${plan.layers.length}`;
  }
  for (let i = 0; i < preview.length; i++) {
    const p = preview[i];
    const l = plan.layers[i];
    if (p.id !== l.id || p.type !== l.type) {
      return `层 ${i} id/type 不一致`;
    }
    if (
      Math.abs(p.x - l.x) > tolerancePx ||
      Math.abs(p.y - l.y) > tolerancePx ||
      Math.abs(p.width - l.width) > tolerancePx ||
      Math.abs(p.height - l.height) > tolerancePx
    ) {
      return `层 ${p.id} 框差超限`;
    }
  }
  return null;
}

/** 在 canvas 上按 Document 画框预览（非权威像素；权威导出在 Go Compose）。 */
export function drawPreviewOutline(
  ctx: CanvasRenderingContext2D,
  doc: LayoutDocument,
): void {
  ctx.clearRect(0, 0, doc.width, doc.height);
  if (doc.background) {
    ctx.fillStyle = doc.background;
    ctx.fillRect(0, 0, doc.width, doc.height);
  }
  const sa = doc.safe_area;
  ctx.strokeStyle = "#00aa66";
  ctx.strokeRect(
    sa.left,
    sa.top,
    doc.width - sa.left - sa.right,
    doc.height - sa.top - sa.bottom,
  );
  for (const layer of sortedLayers(doc.layers)) {
    ctx.strokeStyle = layer.type === "text" ? "#2244cc" : "#333333";
    ctx.strokeRect(layer.x, layer.y, layer.width, layer.height);
    if (layer.type === "text" && layer.text) {
      ctx.fillStyle = layer.color || "#111111";
      ctx.font = `${layer.font_size || 16}px sans-serif`;
      ctx.textBaseline = "top";
      ctx.fillText(layer.text, layer.x, layer.y, layer.width);
    }
  }
}
