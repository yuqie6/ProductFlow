import type { ImageSessionStatus } from "../../lib/types";

export interface ImageSessionEventOptions {
  onError?: (error?: Error) => void;
  onOpen?: () => void;
  createEventSource?: (url: string, init?: EventSourceInit) => ImageSessionEventSourceLike;
}

export interface ImageSessionEventSourceLike {
  addEventListener(type: string, listener: EventListener): void;
  removeEventListener?(type: string, listener: EventListener): void;
  close(): void;
}

/** 订阅文/图生图会话状态。连接错误交给调用方显示，轮询只作显式降级。 */
export function subscribeImageSessionEvents(
  url: string,
  onStatus: (status: ImageSessionStatus) => void,
  options: ImageSessionEventOptions = {},
): () => void {
  if (typeof EventSource === "undefined" && !options.createEventSource) {
    options.onError?.(new Error("当前浏览器不支持生图事件流"));
    return () => undefined;
  }
  const factory = options.createEventSource ?? ((nextURL, init) => new EventSource(nextURL, init));
  const source = factory(url, { withCredentials: true });
  let terminal = false;
  const handleOpen = () => options.onOpen?.();
  const handleError = () => {
    if (!terminal) options.onError?.(new Error("生图事件流连接失败"));
  };
  const handle = (event: Event) => {
    const data = (event as Event & { data?: unknown }).data;
    if (typeof data !== "string") {
      options.onError?.(new Error("生图事件缺少 data"));
      return;
    }
    try {
      const parsed = parseImageSessionStatusEvent(data);
      onStatus(parsed);
      if (!parsed.has_active_generation_task) {
        terminal = true;
        source.close();
      }
    } catch (error) {
      options.onError?.(error instanceof Error ? error : new Error("生图事件无效"));
    }
  };
  source.addEventListener("open", handleOpen);
  source.addEventListener("error", handleError);
  source.addEventListener("session.status", handle);
  return () => {
    source.removeEventListener?.("open", handleOpen);
    source.removeEventListener?.("error", handleError);
    source.removeEventListener?.("session.status", handle);
    source.close();
  };
}

function parseImageSessionStatusEvent(raw: string): ImageSessionStatus {
  let value: unknown;
  try {
    value = JSON.parse(raw) as unknown;
  } catch {
    throw new Error("生图事件不是有效 JSON");
  }
  if (!isRecord(value) || typeof value.id !== "string" || !value.id) {
    throw new Error("生图事件 id 无效");
  }
  if (typeof value.has_active_generation_task !== "boolean" || !Array.isArray(value.generation_tasks)) {
    throw new Error("生图事件 status 无效");
  }
  if (typeof value.title !== "string" || typeof value.rounds_count !== "number" || typeof value.updated_at !== "string") {
    throw new Error("生图事件投影无效");
  }
  return value as unknown as ImageSessionStatus;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
