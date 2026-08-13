export interface WorkflowCanvasViewport {
  x: number;
  y: number;
  zoom: number;
  surface_width?: number;
  surface_height?: number;
}

export interface WorkflowCanvasStateV1 {
  schema_version: 1;
  open_folder_id: string | null;
  global_viewport: WorkflowCanvasViewport | null;
  folder_viewports: Record<string, WorkflowCanvasViewport>;
}

const STORAGE_PREFIX = "productflow.workflowV2.canvasState.v1";

export function workflowCanvasStateStorageKey(workflowId: string): string {
  return `${STORAGE_PREFIX}:${workflowId}`;
}

export function emptyWorkflowCanvasState(): WorkflowCanvasStateV1 {
  return {
    schema_version: 1,
    open_folder_id: null,
    global_viewport: null,
    folder_viewports: {},
  };
}

export function parseWorkflowCanvasState(raw: string | null): WorkflowCanvasStateV1 {
  if (!raw) {
    return emptyWorkflowCanvasState();
  }
  try {
    const candidate = JSON.parse(raw) as Record<string, unknown>;
    if (candidate.schema_version !== 1) {
      return emptyWorkflowCanvasState();
    }
    const folderViewports: Record<string, WorkflowCanvasViewport> = {};
    if (isRecord(candidate.folder_viewports)) {
      for (const [folderId, viewport] of Object.entries(candidate.folder_viewports)) {
        const parsed = parseViewport(viewport);
        if (folderId && parsed) {
          folderViewports[folderId] = parsed;
        }
      }
    }
    return {
      schema_version: 1,
      open_folder_id: typeof candidate.open_folder_id === "string" && candidate.open_folder_id
        ? candidate.open_folder_id
        : null,
      global_viewport: parseViewport(candidate.global_viewport),
      folder_viewports: folderViewports,
    };
  } catch {
    return emptyWorkflowCanvasState();
  }
}

export function reconcileWorkflowCanvasState(
  state: WorkflowCanvasStateV1,
  folderIds: Iterable<string>,
): WorkflowCanvasStateV1 {
  const existing = new Set(folderIds);
  return {
    schema_version: 1,
    open_folder_id: state.open_folder_id && existing.has(state.open_folder_id) ? state.open_folder_id : null,
    global_viewport: state.global_viewport,
    folder_viewports: Object.fromEntries(
      Object.entries(state.folder_viewports).filter(([folderId]) => existing.has(folderId)),
    ),
  };
}

export function isWorkflowCanvasViewportCompatible(
  viewport: WorkflowCanvasViewport | null,
  surfaceWidth: number,
): viewport is WorkflowCanvasViewport {
  if (!viewport || !Number.isFinite(surfaceWidth) || surfaceWidth <= 0) {
    return false;
  }
  if (viewport.surface_width === undefined) {
    return surfaceWidth >= 1024;
  }
  const savedWideLayout = viewport.surface_width >= 1024;
  const currentWideLayout = surfaceWidth >= 1024;
  const widthRatio = Math.max(viewport.surface_width, surfaceWidth)
    / Math.min(viewport.surface_width, surfaceWidth);
  return savedWideLayout === currentWideLayout && widthRatio <= 1.4;
}

function parseViewport(value: unknown): WorkflowCanvasViewport | null {
  if (!isRecord(value)) {
    return null;
  }
  const { x, y, zoom } = value;
  if (![x, y, zoom].every((number) => typeof number === "number" && Number.isFinite(number))) {
    return null;
  }
  if ((zoom as number) < 0.05 || (zoom as number) > 4) {
    return null;
  }
  const surfaceWidth = parseSurfaceDimension(value.surface_width);
  const surfaceHeight = parseSurfaceDimension(value.surface_height);
  return {
    x: x as number,
    y: y as number,
    zoom: zoom as number,
    ...(surfaceWidth === null ? {} : { surface_width: surfaceWidth }),
    ...(surfaceHeight === null ? {} : { surface_height: surfaceHeight }),
  };
}

function parseSurfaceDimension(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) && value > 0 && value <= 100_000
    ? value
    : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
