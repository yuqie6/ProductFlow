/**
 * 按 `edit_version` 做 inspector 乐观自动保存。
 *
 * 409 表示 live 图已变，草稿不会强行合并。运行前 flush，让 Graph Command 看到最新配置。
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

interface BlockedSave {
  sequence: number;
  error: Error;
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
  const [draft, setDraft] = useState(serverValue);
  const [status, setStatus] = useState<SaveStatus>("idle");
  const [error, setError] = useState<string | null>(null);
  const draftRef = useRef(serverValue);
  const baselineRef = useRef(serverValue);
  const editVersionRef = useRef(serverEditVersion);
  const sequenceRef = useRef(0);
  const blockedRef = useRef<BlockedSave | null>(null);
  const savePromiseRef = useRef<Promise<number> | null>(null);
  const saveRef = useRef(save);
  const normalizeRef = useRef(normalize);
  const validateRef = useRef(validate);
  const onStateChangeRef = useRef(onStateChange);
  const lastServerSignatureRef = useRef(stableJson(serverValue));
  const mountedRef = useRef(true);

  saveRef.current = save;
  normalizeRef.current = normalize;
  validateRef.current = validate;
  onStateChangeRef.current = onStateChange;

  useEffect(() => {
    // 开发环境下 React StrictMode 会重放 effect，每次 setup 都要恢复 mounted 状态
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    onStateChangeRef.current?.(status, error);
  }, [error, status]);

  useEffect(() => {
    const nextSignature = stableJson(serverValue);
    const versionAdvanced = serverEditVersion > editVersionRef.current;
    if (!versionAdvanced && nextSignature === lastServerSignatureRef.current) {
      return;
    }
    if (serverEditVersion >= editVersionRef.current) {
      editVersionRef.current = serverEditVersion;
    }
    lastServerSignatureRef.current = nextSignature;
    const hadLocalChanges = !sameJson(draftRef.current, baselineRef.current);
    baselineRef.current = serverValue;
    if (versionAdvanced && hadLocalChanges && !sameJson(draftRef.current, serverValue)) {
      const conflict = new Error(versionConflictMessage);
      blockedRef.current = { sequence: sequenceRef.current, error: conflict };
      setError(conflict.message);
      setStatus("failed");
      return;
    }
    if (!hadLocalChanges || sameJson(draftRef.current, serverValue)) {
      draftRef.current = serverValue;
      setDraft(serverValue);
      if (!blockedRef.current) {
        setStatus("saved");
        setError(null);
      }
    }
  }, [serverEditVersion, serverValue, versionConflictMessage]);

  const update = useCallback((nextDraft: T) => {
    sequenceRef.current += 1;
    blockedRef.current = null;
    draftRef.current = nextDraft;
    setDraft(nextDraft);
    setError(null);
    setStatus(sameJson(nextDraft, baselineRef.current) ? "saved" : "saving");
  }, []);

  const discard = useCallback(() => {
    sequenceRef.current += 1;
    blockedRef.current = null;
    const baseline = baselineRef.current;
    draftRef.current = baseline;
    setDraft(baseline);
    setError(null);
    setStatus("saved");
  }, []);

  const flush = useCallback(async (forceRetry = false): Promise<number> => {
    let allowBlockedRetry = forceRetry;
    while (!sameJson(draftRef.current, baselineRef.current)) {
      const blocked = blockedRef.current;
      if (blocked && blocked.sequence === sequenceRef.current && !allowBlockedRetry) {
        throw blocked.error;
      }
      allowBlockedRetry = false;

      if (savePromiseRef.current) {
        await savePromiseRef.current;
        continue;
      }

      const snapshot = normalizeRef.current(draftRef.current);
      const snapshotSequence = sequenceRef.current;
      if (!sameJson(snapshot, draftRef.current)) {
        draftRef.current = snapshot;
        if (mountedRef.current) {
          setDraft(snapshot);
        }
      }
      const validationError = validateRef.current(snapshot);
      if (validationError) {
        const nextError = new Error(validationError);
        blockedRef.current = { sequence: snapshotSequence, error: nextError };
        setError(validationError);
        setStatus("failed");
        throw nextError;
      }

      setError(null);
      setStatus("saving");
      const pending = saveRef.current(snapshot, editVersionRef.current).then((result) => {
        editVersionRef.current = result.edit_version;
        baselineRef.current = snapshot;
        lastServerSignatureRef.current = stableJson(snapshot);
        blockedRef.current = null;
        if (mountedRef.current) {
          if (sameJson(draftRef.current, snapshot)) {
            setError(null);
            setStatus("saved");
          } else {
            setStatus("saving");
          }
        }
        return result.edit_version;
      });
      savePromiseRef.current = pending;
      try {
        await pending;
      } catch (cause) {
        const nextError = normalizeError(cause);
        blockedRef.current = { sequence: snapshotSequence, error: nextError };
        if (mountedRef.current) {
          setError(nextError.message);
          setStatus("failed");
        }
        throw nextError;
      } finally {
        if (savePromiseRef.current === pending) {
          savePromiseRef.current = null;
        }
      }
    }
    return editVersionRef.current;
  }, []);

  const dirty = !sameJson(draft, baselineRef.current);
  useEffect(() => {
    const blocked = blockedRef.current;
    if (
      disabled
      || !dirty
      || (blocked && blocked.sequence === sequenceRef.current)
    ) {
      return;
    }
    const timer = window.setTimeout(() => {
      void flush().catch(() => undefined);
    }, debounceMs);
    return () => window.clearTimeout(timer);
  }, [debounceMs, disabled, dirty, draft, flush]);

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
