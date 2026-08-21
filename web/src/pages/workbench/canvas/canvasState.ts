export interface WorkflowCanvasViewport {
  x: number;
  y: number;
  zoom: number;
  surface_width: number;
  surface_height: number;
}

export function isWorkflowCanvasViewportCompatible(
  viewport: WorkflowCanvasViewport | null,
  surfaceWidth: number,
): viewport is WorkflowCanvasViewport {
  if (!viewport || !Number.isFinite(surfaceWidth) || surfaceWidth <= 0) {
    return false;
  }
  const savedWideLayout = viewport.surface_width >= 1024;
  const currentWideLayout = surfaceWidth >= 1024;
  const widthRatio = Math.max(viewport.surface_width, surfaceWidth)
    / Math.min(viewport.surface_width, surfaceWidth);
  return savedWideLayout === currentWideLayout && widthRatio <= 1.4;
}

export function workflowCanvasFitMinZoom(surfaceWidth: number): number {
  if (surfaceWidth < 480) return 0.24;
  if (surfaceWidth < 720) return 0.32;
  return 0.55;
}
