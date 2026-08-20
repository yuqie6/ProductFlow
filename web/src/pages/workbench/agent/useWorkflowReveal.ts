import { useEffect, useMemo, useRef, useState } from "react";

import { api } from "../../../lib/api";
import type { WorkflowMaterializationResult } from "../../../lib/types";
import {
  abortableDelay,
  applyWorkflowRevealEvent,
  collectWorkflowRevealEvents,
  emptyWorkflowRevealVisibility,
  shouldAnimateWorkflowReveal,
  workflowRevealEventDelay,
  type WorkflowRevealStreamReader,
} from "./workflowReveal";
import type { WorkflowRevealVisibility } from "../canvas/V2WorkflowCanvas";

export type WorkflowRevealStatus = "idle" | "loading" | "revealing" | "completed" | "fallback";

interface UseWorkflowRevealInput {
  materialization: WorkflowMaterializationResult | null;
  enabled?: boolean;
  reducedMotion?: boolean;
  read?: WorkflowRevealStreamReader;
  onFallback: () => void | Promise<void>;
}

interface WorkflowRevealState {
  materializationId: string | null;
  status: WorkflowRevealStatus;
  visibility: WorkflowRevealVisibility | null;
  sequence: number;
  error: string | null;
}

const idleState: WorkflowRevealState = {
  materializationId: null,
  status: "idle",
  visibility: null,
  sequence: 0,
  error: null,
};

export function useWorkflowReveal({
  materialization,
  enabled = true,
  reducedMotion,
  read,
  onFallback,
}: UseWorkflowRevealInput): WorkflowRevealState {
  const systemReducedMotion = usePrefersReducedMotion();
  const shouldReduceMotion = reducedMotion ?? systemReducedMotion;
  const [state, setState] = useState<WorkflowRevealState>(idleState);
  const fallbackRef = useRef(onFallback);
  fallbackRef.current = onFallback;
  const initialVisibility = useMemo(emptyWorkflowRevealVisibility, [materialization?.id]);
  const shouldAnimate = shouldAnimateWorkflowReveal({
    enabled: Boolean(enabled && materialization),
    created: Boolean(materialization?.created),
    reducedMotion: shouldReduceMotion,
  });

  useEffect(() => {
    if (!enabled || !materialization) {
      setState(idleState);
      return;
    }
    if (!materialization.created || shouldReduceMotion) {
      setState({
        materializationId: materialization.id,
        status: "completed",
        visibility: null,
        sequence: 0,
        error: null,
      });
      return;
    }

    const controller = new AbortController();
    const streamReader: WorkflowRevealStreamReader = read ?? ((after, onChunk, signal) =>
      api.streamWorkflowRevealEvents(materialization.id, { after, onChunk, signal }));
    setState({
      materializationId: materialization.id,
      status: "loading",
      visibility: initialVisibility,
      sequence: 0,
      error: null,
    });

    void collectWorkflowRevealEvents({
      materializationId: materialization.id,
      workflow: materialization.workflow,
      read: streamReader,
      signal: controller.signal,
    }).then(async (events) => {
      if (controller.signal.aborted) return;
      setState((current) => ({ ...current, status: "revealing" }));
      let visibility = initialVisibility;
      for (const event of events) {
        await abortableDelay(workflowRevealEventDelay(event.kind), controller.signal);
        if (controller.signal.aborted) return;
        visibility = applyWorkflowRevealEvent(visibility, event);
        setState({
          materializationId: materialization.id,
          status: event.kind === "completed" ? "completed" : "revealing",
          visibility: event.kind === "completed" ? null : visibility,
          sequence: event.sequence,
          error: null,
        });
      }
    }).catch(async (error: unknown) => {
      if (controller.signal.aborted || isAbortError(error)) return;
      try {
        await fallbackRef.current();
      } catch {
        // The persisted materialization response still carries a complete workflow.
      } finally {
        if (!controller.signal.aborted) {
          setState({
            materializationId: materialization.id,
            status: "fallback",
            visibility: null,
            sequence: 0,
            error: error instanceof Error ? error.message : "工作流 reveal 失败",
          });
        }
      }
    });

    return () => controller.abort();
  }, [enabled, initialVisibility, materialization, read, shouldReduceMotion]);

  if (shouldAnimate && state.materializationId !== materialization?.id) {
    return {
      materializationId: materialization?.id ?? null,
      status: "loading",
      visibility: initialVisibility,
      sequence: 0,
      error: null,
    };
  }
  return state;
}

function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() =>
    typeof window !== "undefined" && typeof window.matchMedia === "function"
      ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
      : false,
  );

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReduced(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return reduced;
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}
