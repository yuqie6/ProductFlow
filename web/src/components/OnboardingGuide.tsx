import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowRight,
  CheckCircle2,
  ListChecks,
  PlayCircle,
  RotateCcw,
  X,
} from "lucide-react";
import { useLocation, useNavigate } from "react-router-dom";

import {
  DEFAULT_ONBOARDING_STATE,
  ONBOARDING_CHANGE_EVENT,
  ONBOARDING_STEPS,
  ONBOARDING_STORAGE_KEY,
  getStepById,
  getStepIndex,
  pageLabel,
  resolveOnboardingPath,
} from "../lib/onboarding";
import type { OnboardingPage, OnboardingState } from "../lib/onboarding";

function nowIso() {
  return new Date().toISOString();
}

function normalizeState(value: unknown): OnboardingState {
  if (!value || typeof value !== "object") {
    return { ...DEFAULT_ONBOARDING_STATE };
  }
  const candidate = value as Partial<OnboardingState>;
  const status = candidate.status;
  const stepId = candidate.stepId;
  const validStatus =
    status === "idle" ||
    status === "active" ||
    status === "completed" ||
    status === "skipped";
  const validStepId =
    typeof stepId === "string" &&
    ONBOARDING_STEPS.some((step) => step.id === stepId);

  return {
    status: validStatus ? status : DEFAULT_ONBOARDING_STATE.status,
    stepId: validStepId ? stepId : DEFAULT_ONBOARDING_STATE.stepId,
    updatedAt: typeof candidate.updatedAt === "string" ? candidate.updatedAt : "",
  };
}

function readState(): OnboardingState {
  if (typeof window === "undefined") {
    return { ...DEFAULT_ONBOARDING_STATE };
  }
  const raw = window.localStorage.getItem(ONBOARDING_STORAGE_KEY);
  if (!raw) {
    return { ...DEFAULT_ONBOARDING_STATE };
  }
  try {
    return normalizeState(JSON.parse(raw));
  } catch {
    return { ...DEFAULT_ONBOARDING_STATE };
  }
}

function writeState(nextState: OnboardingState) {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(ONBOARDING_STORAGE_KEY, JSON.stringify(nextState));
  window.dispatchEvent(new Event(ONBOARDING_CHANGE_EVENT));
}

function productIdFromPath(pathname: string): string | undefined {
  const match = /^\/products\/([^/]+)(?:\/|$)/.exec(pathname);
  if (!match || match[1] === "new") {
    return undefined;
  }
  return match[1];
}

export function useOnboarding() {
  const [state, setState] = useState<OnboardingState>(() => readState());

  useEffect(() => {
    const update = () => setState(readState());
    window.addEventListener("storage", update);
    window.addEventListener(ONBOARDING_CHANGE_EVENT, update);
    return () => {
      window.removeEventListener("storage", update);
      window.removeEventListener(ONBOARDING_CHANGE_EVENT, update);
    };
  }, []);

  const persist = useCallback((nextState: OnboardingState) => {
    writeState(nextState);
    setState(nextState);
  }, []);

  const start = useCallback((stepId = ONBOARDING_STEPS[0].id) => {
    persist({ status: "active", stepId, updatedAt: nowIso() });
  }, [persist]);

  const reset = useCallback(() => {
    persist({ status: "active", stepId: ONBOARDING_STEPS[0].id, updatedAt: nowIso() });
  }, [persist]);

  const skip = useCallback(() => {
    persist({ status: "skipped", stepId: state.stepId, updatedAt: nowIso() });
  }, [persist, state.stepId]);

  const complete = useCallback(() => {
    persist({ status: "completed", stepId: ONBOARDING_STEPS.at(-1)?.id ?? state.stepId, updatedAt: nowIso() });
  }, [persist, state.stepId]);

  const goToStep = useCallback((stepId: string) => {
    persist({ status: "active", stepId: getStepById(stepId).id, updatedAt: nowIso() });
  }, [persist]);

  const advance = useCallback(() => {
    const currentIndex = getStepIndex(state.stepId);
    const nextStep = ONBOARDING_STEPS[currentIndex + 1];
    if (!nextStep) {
      persist({ status: "completed", stepId: state.stepId, updatedAt: nowIso() });
      return;
    }
    persist({ status: "active", stepId: nextStep.id, updatedAt: nowIso() });
  }, [persist, state.stepId]);

  const step = useMemo(() => getStepById(state.stepId), [state.stepId]);
  const stepIndex = getStepIndex(state.stepId);

  return {
    state,
    step,
    stepIndex,
    totalSteps: ONBOARDING_STEPS.length,
    active: state.status === "active",
    start,
    reset,
    skip,
    complete,
    goToStep,
    advance,
  };
}

export function OnboardingNavButton() {
  const onboarding = useOnboarding();
  const navigate = useNavigate();
  const location = useLocation();
  const currentProductId = productIdFromPath(location.pathname);

  const handleClick = () => {
    if (!onboarding.active) {
      onboarding.start();
      navigate(resolveOnboardingPath(ONBOARDING_STEPS[0], currentProductId));
      return;
    }
    navigate(resolveOnboardingPath(onboarding.step, currentProductId));
  };

  const label = onboarding.active
    ? `继续引导 ${onboarding.stepIndex + 1}/${onboarding.totalSteps}`
    : onboarding.state.status === "completed"
      ? "重看引导"
      : "开始引导";

  return (
    <button
      type="button"
      onClick={handleClick}
      className="inline-flex items-center rounded-full border border-zinc-300 bg-white px-3.5 py-2 text-sm font-semibold text-zinc-700 shadow-sm transition-colors hover:border-zinc-400 hover:bg-zinc-50 hover:text-zinc-950"
    >
      <PlayCircle size={16} className="mr-1.5" />
      {label}
    </button>
  );
}

interface OnboardingGuideCardProps {
  page: OnboardingPage;
  productId?: string;
  className?: string;
}

export function OnboardingGuideCard({ page, productId, className = "" }: OnboardingGuideCardProps) {
  const onboarding = useOnboarding();
  const navigate = useNavigate();
  const step = onboarding.step;
  const isCurrentPage = step.page === page;
  const isLastStep = onboarding.stepIndex === onboarding.totalSteps - 1;
  const progressPercent = Math.round(((onboarding.stepIndex + 1) / onboarding.totalSteps) * 100);

  if (!onboarding.active) {
    return null;
  }

  const handlePrimaryAction = () => {
    if (!isCurrentPage) {
      navigate(resolveOnboardingPath(step, productId));
      return;
    }
    if (step.id === "create-product-entry") {
      onboarding.advance();
      navigate("/products/new");
      return;
    }
    if (step.id === "workbench-inspect-iterate") {
      onboarding.advance();
      navigate(resolveOnboardingPath(ONBOARDING_STEPS[onboarding.stepIndex + 1], productId));
      return;
    }
    if (isLastStep) {
      onboarding.complete();
      return;
    }
    onboarding.advance();
  };

  return (
    <section className={`rounded-2xl border border-blue-200 bg-blue-50/80 p-4 shadow-sm ${className}`}>
      <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="mb-2 inline-flex items-center rounded-full border border-blue-200 bg-white/80 px-2.5 py-1 text-[11px] font-semibold text-blue-700">
            <ListChecks size={13} className="mr-1.5" /> 产品内引导 · {onboarding.stepIndex + 1}/{onboarding.totalSteps}
          </div>
          <h2 className="text-base font-semibold text-zinc-950">{step.title}</h2>
          <p className="mt-1 text-sm leading-6 text-zinc-600">{step.goal}</p>
        </div>
        <div className="min-w-[140px] text-right">
          <div className="text-xs font-medium text-blue-700">{progressPercent}%</div>
          <div className="mt-2 h-2 overflow-hidden rounded-full bg-white">
            <div className="h-full rounded-full bg-blue-600" style={{ width: `${progressPercent}%` }} />
          </div>
        </div>
      </div>

      {isCurrentPage ? (
        <div className="rounded-xl border border-blue-100 bg-white/80 p-3">
          <ol className="space-y-2 text-sm leading-6 text-zinc-700">
            {step.instructions.map((instruction, index) => (
              <li key={instruction} className="flex gap-2">
                <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-blue-600 text-[11px] font-semibold text-white">
                  {index + 1}
                </span>
                <span>{instruction}</span>
              </li>
            ))}
          </ol>
          <div className="mt-3 rounded-lg bg-blue-50 px-3 py-2 text-xs leading-5 text-blue-800">
            <span className="font-semibold">应该看到：</span>
            {step.expected}
          </div>
        </div>
      ) : (
        <div className="rounded-xl border border-blue-100 bg-white/80 px-3 py-3 text-sm leading-6 text-zinc-700">
          当前引导步骤在 <span className="font-semibold text-zinc-950">{pageLabel(step.page)}</span>。
          你可以先跳过去，也可以跳过/重置引导。
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={handlePrimaryAction}
            className="inline-flex items-center rounded-md bg-zinc-900 px-3.5 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800"
          >
            {isLastStep && isCurrentPage ? <CheckCircle2 size={15} className="mr-1.5" /> : null}
            {isCurrentPage ? step.ctaLabel : `前往${pageLabel(step.page)}`}
            {!isLastStep || !isCurrentPage ? <ArrowRight size={15} className="ml-1.5" /> : null}
          </button>
          <button
            type="button"
            onClick={onboarding.complete}
            className="inline-flex items-center rounded-md border border-blue-200 bg-white/80 px-3 py-2 text-sm font-medium text-blue-700 transition-colors hover:border-blue-300 hover:text-blue-900"
          >
            <CheckCircle2 size={15} className="mr-1.5" /> 标记完成
          </button>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={onboarding.skip}
            className="inline-flex items-center rounded-md px-2.5 py-2 text-sm font-medium text-zinc-500 transition-colors hover:bg-white/70 hover:text-zinc-900"
          >
            <X size={14} className="mr-1" /> 跳过
          </button>
          <button
            type="button"
            onClick={onboarding.reset}
            className="inline-flex items-center rounded-md px-2.5 py-2 text-sm font-medium text-zinc-500 transition-colors hover:bg-white/70 hover:text-zinc-900"
          >
            <RotateCcw size={14} className="mr-1" /> 重置
          </button>
        </div>
      </div>
    </section>
  );
}
