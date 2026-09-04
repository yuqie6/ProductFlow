/**
 * 按 `edit_version` 做 inspector 乐观自动保存。
 *
 * 409 表示 live 图已变，草稿不会强行合并。运行前 flush，让 Graph Command 看到最新配置。
 * 草稿基线是开始编辑时的图 revision：兄弟节点配置变更不改写该基线，也不在客户端判冲突。
 */

import { useCallback, useEffect, useRef, useState } from "react";

import type { SaveStatus } from "../chrome/SaveStatusBadge";

interface VersionedSaveResult {
  edit_version: number;
}

interface UseNodeDraftAutosaveOptions<T> {
  serverValue: T;
  serverEditVersion: number;
  disabled?: boolean;
  debounceMs?: number;
  normalize?: (draft: T) => T;
  validate: (draft: T) => string | null;
  versionConflictMessage: string;
  save: (draft: T, expectedEditVersion: number) => Promise<VersionedSaveResult>;
  onStateChange?: (status: SaveStatus, error: string | null) => void;
}

export interface NodeDraftAutosave<T> {
  draft: T;
  dirty: boolean;
  status: SaveStatus;
  error: string | null;
  update: (draft: T) => void;
  discard: () => void;
  flush: (forceRetry?: boolean) => Promise<number>;
}

export interface NodeDraftSession<T> {
  draft: T;
  baseline: T;
  editVersion: number;
  lastServerSignature: string;
  sequence: number;
  blocked: { sequence: number; error: Error } | null;
  savePromise: Promise<number> | null;
}

export function createNodeDraftSession<T>(serverValue: T, serverEditVersion: number): NodeDraftSession<T> {
  return {
    draft: serverValue,
    baseline: serverValue,
    editVersion: serverEditVersion,
    lastServerSignature: stableJson(serverValue),
    sequence: 0,
    blocked: null,
    savePromise: null,
  };
}

export function isVersionConflictStatus(error: unknown): boolean {
  return Boolean(
    error
    && typeof error === "object"
    && "status" in error
    && (error as { status: unknown }).status === 409,
  );
}

export function nodeDraftSaveError(error: unknown, versionConflictMessage: string): Error {
  if (isVersionConflictStatus(error)) {
    return new Error(versionConflictMessage);
  }
  return normalizeError(error);
}

export function applyServerToNodeDraft<T>(
  session: NodeDraftSession<T>,
  serverValue: T,
  serverEditVersion: number,
  versionConflictMessage: string,
): { conflict: boolean; adopted: boolean } {
  const nextSignature = stableJson(serverValue);
  const versionAdvanced = serverEditVersion > session.editVersion;
  const serverUnchanged = nextSignature === session.lastServerSignature;
  if (!versionAdvanced && serverUnchanged) {
    return { conflict: false, adopted: false };
  }

  const hadLocalChanges = !sameJson(session.draft, session.baseline);
  const thisNodeUnchanged = serverUnchanged || sameJson(serverValue, session.baseline);
  session.lastServerSignature = nextSignature;

  if (thisNodeUnchanged) {
    return { conflict: false, adopted: false };
  }

  if (!hadLocalChanges || sameJson(session.draft, serverValue)) {
    session.editVersion = Math.max(serverEditVersion, session.editVersion);
    session.baseline = serverValue;
    session.draft = serverValue;
    session.blocked = null;
    return { conflict: false, adopted: true };
  }

  session.blocked = { sequence: session.sequence, error: new Error(versionConflictMessage) };
  return { conflict: true, adopted: false };
}

export function updateNodeDraft<T>(session: NodeDraftSession<T>, nextDraft: T): void {
  session.sequence += 1;
  session.blocked = null;
  session.draft = nextDraft;
}

export function discardNodeDraft<T>(session: NodeDraftSession<T>): void {
  session.sequence += 1;
  session.blocked = null;
  session.draft = session.baseline;
}

export async function flushNodeDraft<T>(
  session: NodeDraftSession<T>,
  deps: {
    save: (draft: T, expectedEditVersion: number) => Promise<VersionedSaveResult>;
    normalize: (draft: T) => T;
    validate: (draft: T) => string | null;
    versionConflictMessage: string;
    forceRetry?: boolean;
    onChange?: () => void;
  },
): Promise<number> {
  let allowBlockedRetry = deps.forceRetry === true;
  while (!sameJson(session.draft, session.baseline)) {
    const blocked = session.blocked;
    if (blocked && blocked.sequence === session.sequence && !allowBlockedRetry) {
      throw blocked.error;
    }
    allowBlockedRetry = false;

    if (session.savePromise) {
      await session.savePromise;
      continue;
    }

    const snapshot = deps.normalize(session.draft);
    const snapshotSequence = session.sequence;
    if (!sameJson(snapshot, session.draft)) {
      session.draft = snapshot;
      deps.onChange?.();
    }
    const validationError = deps.validate(snapshot);
    if (validationError) {
      const nextError = new Error(validationError);
      session.blocked = { sequence: snapshotSequence, error: nextError };
      deps.onChange?.();
      throw nextError;
    }

    const expectedEditVersion = session.editVersion;
    const pending = deps.save(snapshot, expectedEditVersion).then((result) => {
      session.editVersion = result.edit_version;
      session.baseline = snapshot;
      session.lastServerSignature = stableJson(snapshot);
      session.blocked = null;
      return result.edit_version;
    });
    session.savePromise = pending;
    deps.onChange?.();
    try {
      await pending;
    } catch (cause) {
      const nextError = nodeDraftSaveError(cause, deps.versionConflictMessage);
      session.blocked = { sequence: snapshotSequence, error: nextError };
      deps.onChange?.();
      throw nextError;
    } finally {
      if (session.savePromise === pending) {
        session.savePromise = null;
      }
    }
  }
  return session.editVersion;
}

export function useNodeDraftAutosave<T>({
  serverValue,
  serverEditVersion,
  disabled = false,
  debounceMs = 700,
  normalize = (value) => value,
  validate,
  versionConflictMessage,
  save,
  onStateChange,
}: UseNodeDraftAutosaveOptions<T>): NodeDraftAutosave<T> {
  const sessionRef = useRef<NodeDraftSession<T> | null>(null);
  if (sessionRef.current === null) {
    sessionRef.current = createNodeDraftSession(serverValue, serverEditVersion);
  }
  const session = sessionRef.current;
  const [draft, setDraft] = useState(session.draft);
  const [status, setStatus] = useState<SaveStatus>("idle");
  const [error, setError] = useState<string | null>(null);
  const saveRef = useRef(save);
  const normalizeRef = useRef(normalize);
  const validateRef = useRef(validate);
  const onStateChangeRef = useRef(onStateChange);
  const conflictMessageRef = useRef(versionConflictMessage);
  const mountedRef = useRef(true);

  saveRef.current = save;
  normalizeRef.current = normalize;
  validateRef.current = validate;
  onStateChangeRef.current = onStateChange;
  conflictMessageRef.current = versionConflictMessage;

  const syncView = useCallback((nextStatus?: SaveStatus, nextError?: string | null) => {
    if (!mountedRef.current) return;
    setDraft(session.draft);
    if (nextStatus !== undefined) {
      setStatus(nextStatus);
    }
    if (nextError !== undefined) {
      setError(nextError);
    }
  }, [session]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    onStateChangeRef.current?.(status, error);
  }, [error, status]);

  useEffect(() => {
    const result = applyServerToNodeDraft(session, serverValue, serverEditVersion, conflictMessageRef.current);
    if (result.conflict) {
      syncView("failed", session.blocked?.error.message ?? versionConflictMessage);
      return;
    }
    if (result.adopted) {
      syncView(session.blocked ? undefined : "saved", session.blocked ? undefined : null);
    }
  }, [serverEditVersion, serverValue, session, syncView, versionConflictMessage]);

  const update = useCallback((nextDraft: T) => {
    updateNodeDraft(session, nextDraft);
    syncView(sameJson(nextDraft, session.baseline) ? "saved" : "saving", null);
  }, [session, syncView]);

  const discard = useCallback(() => {
    discardNodeDraft(session);
    syncView("saved", null);
  }, [session, syncView]);

  const flush = useCallback(async (forceRetry = false): Promise<number> => {
    try {
      if (mountedRef.current && !sameJson(session.draft, session.baseline)) {
        setError(null);
        setStatus("saving");
      }
      const version = await flushNodeDraft(session, {
        save: (snapshot, expectedEditVersion) => saveRef.current(snapshot, expectedEditVersion),
        normalize: (value) => normalizeRef.current(value),
        validate: (value) => validateRef.current(value),
        versionConflictMessage: conflictMessageRef.current,
        forceRetry,
        onChange: () => {
          if (!mountedRef.current) return;
          setDraft(session.draft);
        },
      });
      if (mountedRef.current) {
        setDraft(session.draft);
        if (sameJson(session.draft, session.baseline)) {
          setError(null);
          setStatus("saved");
        } else {
          setStatus("saving");
        }
      }
      return version;
    } catch (cause) {
      const nextError = session.blocked?.error ?? nodeDraftSaveError(cause, conflictMessageRef.current);
      if (mountedRef.current) {
        setDraft(session.draft);
        setError(nextError.message);
        setStatus("failed");
      }
      throw nextError;
    }
  }, [session]);

  const dirty = !sameJson(draft, session.baseline);
  useEffect(() => {
    const blocked = session.blocked;
    if (
      disabled
      || !dirty
      || (blocked && blocked.sequence === session.sequence)
    ) {
      return;
    }
    const timer = window.setTimeout(() => {
      void flush().catch(() => undefined);
    }, debounceMs);
    return () => window.clearTimeout(timer);
  }, [debounceMs, disabled, dirty, draft, flush, session]);

  return { draft, dirty, status, error, update, discard, flush };
}

function stableJson(value: unknown): string {
  return JSON.stringify(value);
}

function sameJson(left: unknown, right: unknown): boolean {
  return stableJson(left) === stableJson(right);
}

function normalizeError(error: unknown): Error {
  if (error instanceof Error) {
    return error;
  }
  return new Error(String(error));
}
