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

export function isWorkflowCanvasViewportScopeActive(
  activeGroupId: string | null | undefined,
  updateGroupId: string | null | undefined,
): boolean {
  return (activeGroupId ?? null) === (updateGroupId ?? null);
}

export function workflowCanvasFitMinZoom(surfaceWidth: number): number {
  if (surfaceWidth < 480) return 0.24;
  if (surfaceWidth < 720) return 0.32;
  return 0.55;
}

const VIEWPORT_STORAGE_PREFIX = "productflow.workflowV3.canvasState.v1:";

export function workflowCanvasViewportStorageKey(
  workflowId: string,
  groupId?: string | null,
): string {
  if (groupId) return `${VIEWPORT_STORAGE_PREFIX}${workflowId}:group:${groupId}`;
  return `${VIEWPORT_STORAGE_PREFIX}${workflowId}`;
}

function isViewportRecord(value: unknown): value is WorkflowCanvasViewport {
  if (!value || typeof value !== "object") return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.x === "number"
    && typeof record.y === "number"
    && typeof record.zoom === "number"
    && typeof record.surface_width === "number"
    && typeof record.surface_height === "number"
  );
}

export function parseStoredWorkflowCanvasViewport(raw: string | null): WorkflowCanvasViewport | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return isViewportRecord(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

export function readStoredWorkflowCanvasViewport(
  workflowId: string,
  groupId?: string | null,
): WorkflowCanvasViewport | null {
  if (typeof window === "undefined" || !workflowId) return null;
  try {
    return parseStoredWorkflowCanvasViewport(
      window.localStorage.getItem(workflowCanvasViewportStorageKey(workflowId, groupId)),
    );
  } catch {
    return null;
  }
}

export function writeStoredWorkflowCanvasViewport(
  workflowId: string,
  viewport: WorkflowCanvasViewport,
  groupId?: string | null,
): void {
  if (typeof window === "undefined" || !workflowId) return;
  try {
    window.localStorage.setItem(
      workflowCanvasViewportStorageKey(workflowId, groupId),
      JSON.stringify(viewport),
    );
  } catch {
    // Private mode and quota errors stay client-local; the live viewport still works.
  }
}
