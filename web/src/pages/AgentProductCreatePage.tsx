import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, Bot, Loader2, RotateCw, Settings2, X } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { api, ApiError } from "../lib/api";
import type { TranslateFunction } from "../lib/preferences";
import { useI18n } from "../lib/preferences";
import type {
  AgentProductImageTypeKey,
  AgentProductWorkspaceSnapshot,
} from "../lib/types";
import { AgentProductCreateForm } from "./product-create/AgentProductCreateForm";
import {
  buildAgentProductSelection,
  toggleAgentImageType,
  updateAgentImageTypeQuantity,
  validateAgentProductWorkspaceInput,
  type AgentImageTypeSelectionDraft,
  type AgentProductCreateValidationIssue,
} from "./product-create/imageTypeSelection";

const PENDING_DRAFT_STORAGE_KEY = "productflow.agent-create.pending-draft.v1";
const INTAKE_STORAGE_KEY_PREFIX = "productflow.agent-create.intake.v1:";

interface PendingDraftState {
  name: string;
  idempotencyKey: string;
}

interface IntakeIdempotencyState {
  conversationId: string;
  idempotencyKey: string;
}

function createIdempotencyKey(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function readSessionValue(key: string): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeSessionValue(key: string, value: string): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.setItem(key, value);
  } catch {
    // Idempotency still holds for the current mounted page when storage is unavailable.
  }
}

function removeSessionValue(key: string): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.removeItem(key);
  } catch {
    // Nothing else can be done when browser storage is unavailable.
  }
}

function readPendingDraft(): PendingDraftState | null {
  const raw = readSessionValue(PENDING_DRAFT_STORAGE_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<PendingDraftState>;
    if (typeof parsed.name === "string" && typeof parsed.idempotencyKey === "string") {
      return { name: parsed.name, idempotencyKey: parsed.idempotencyKey };
    }
  } catch {
    removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
  }
  return null;
}

function intakeStorageKey(conversationId: string): string {
  return `${INTAKE_STORAGE_KEY_PREFIX}${conversationId}`;
}

function readOrCreateIntakeIdempotencyKey(conversationId: string): string {
  const existing = readSessionValue(intakeStorageKey(conversationId));
  if (existing) return existing;
  const created = createIdempotencyKey();
  writeSessionValue(intakeStorageKey(conversationId), created);
  return created;
}

function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function validationMessage(t: TranslateFunction, issue: AgentProductCreateValidationIssue): string {
  switch (issue.code) {
    case "options_unavailable":
      return t("agentCreate.error.optionsUnavailable");
    case "name_required":
      return t("agentCreate.error.nameRequired");
    case "image_type_required":
      return t("agentCreate.error.imageTypeRequired", { minimum: issue.minimum });
    case "duplicate_image_type":
      return t("agentCreate.error.duplicateImageType");
    case "quantity_out_of_range":
      return t("agentCreate.error.quantityRange", {
        minimum: issue.minimum,
        maximum: issue.maximum,
      });
    case "total_images_exceeded":
      return t("agentCreate.error.totalExceeded", { total: issue.total, maximum: issue.maximum });
    case "reference_count_out_of_range":
      return t("agentCreate.error.referenceRange", {
        minimum: issue.minimum,
        maximum: issue.maximum,
      });
  }
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.detail;
  return error instanceof Error ? error.message : fallback;
}

export function AgentProductCreatePage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [pendingDraft] = useState(readPendingDraft);
  const [name, setName] = useState(pendingDraft?.name ?? "");
  const [localWorkspace, setLocalWorkspace] = useState<AgentProductWorkspaceSnapshot | null>(null);
  const [selections, setSelections] = useState<AgentImageTypeSelectionDraft[]>([]);
  const [referenceFiles, setReferenceFiles] = useState<File[]>([]);
  const [error, setError] = useState("");
  const [leaving, setLeaving] = useState(false);
  const draftIdempotencyKeyRef = useRef(pendingDraft?.idempotencyKey ?? createIdempotencyKey());
  const intakeIdempotencyRef = useRef<IntakeIdempotencyState | null>(null);
  const navigationScheduledRef = useRef(false);
  const navigationTimerRef = useRef<number | null>(null);

  const workspaceId = searchParams.get("workspace")?.trim() ?? "";
  const workspaceQuery = useQuery({
    queryKey: ["agent-product-workspace", workspaceId],
    queryFn: () => api.getAgentProductWorkspace(workspaceId),
    enabled: Boolean(workspaceId),
    retry: false,
  });
  const workspaceFromMutation =
    localWorkspace?.conversation.id === workspaceId || !workspaceId ? localWorkspace : null;
  const workspace = workspaceQuery.data ?? workspaceFromMutation;
  const conversationId = workspace?.conversation.id ?? "";

  const optionsQuery = useQuery({
    queryKey: ["agent-product-workspace-options"],
    queryFn: api.getAgentProductWorkspaceOptions,
    enabled: Boolean(workspace && !workspace.intake_finalized),
  });
  const options = optionsQuery.data ?? null;

  useEffect(() => {
    if (!workspace) return;
    setName(workspace.product.name);
    if (workspace.intake_finalized || !conversationId) return;
    intakeIdempotencyRef.current = {
      conversationId,
      idempotencyKey: readOrCreateIntakeIdempotencyKey(conversationId),
    };
  }, [conversationId, workspace]);

  useEffect(() => {
    if (!workspace?.intake_finalized || navigationScheduledRef.current) return;
    navigationScheduledRef.current = true;
    setLeaving(true);
    const delay = prefersReducedMotion() ? 0 : 180;
    navigationTimerRef.current = window.setTimeout(() => {
      navigate(`/products/${workspace.product.id}`, { replace: true });
    }, delay);
  }, [navigate, workspace]);

  useEffect(
    () => () => {
      if (navigationTimerRef.current !== null) window.clearTimeout(navigationTimerRef.current);
    },
    [],
  );

  const createDraftMutation = useMutation({
    mutationFn: api.createAgentProductDraftWorkspace,
    onSuccess: (createdWorkspace) => {
      removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
      setError("");
      setLocalWorkspace(createdWorkspace);
      queryClient.setQueryData(
        ["agent-product-workspace", createdWorkspace.conversation.id],
        createdWorkspace,
      );
      void queryClient.invalidateQueries({ queryKey: ["products"] });
      setSearchParams({ workspace: createdWorkspace.conversation.id }, { replace: true });
    },
    onError: (mutationError) => {
      setError(errorDetail(mutationError, t("agentCreate.error.failed")));
    },
  });

  const finalizeIntakeMutation = useMutation({
    mutationFn: api.finalizeAgentProductWorkspaceIntake,
    onSuccess: (finalizedWorkspace) => {
      setError("");
      setLocalWorkspace(finalizedWorkspace);
      queryClient.setQueryData(
        ["agent-product-workspace", finalizedWorkspace.conversation.id],
        finalizedWorkspace,
      );
      removeSessionValue(intakeStorageKey(finalizedWorkspace.conversation.id));
      void queryClient.invalidateQueries({ queryKey: ["products"] });
      void queryClient.invalidateQueries({
        queryKey: ["agent-workbench", finalizedWorkspace.product.id],
      });
    },
    onError: (mutationError) => {
      setError(errorDetail(mutationError, t("agentCreate.error.failed")));
    },
  });

  const rotateIntakeIdempotencyKey = () => {
    if (!conversationId) return;
    const idempotencyKey = createIdempotencyKey();
    intakeIdempotencyRef.current = { conversationId, idempotencyKey };
    writeSessionValue(intakeStorageKey(conversationId), idempotencyKey);
  };

  const handleNameChange = (nextName: string) => {
    setName(nextName);
    draftIdempotencyKeyRef.current = createIdempotencyKey();
    removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
    setError("");
  };

  const handleCreateDraft = () => {
    if (createDraftMutation.isPending) return;
    const trimmedName = name.trim();
    if (!trimmedName) {
      setError(t("agentCreate.error.nameRequired"));
      return;
    }
    const pending = {
      name: trimmedName,
      idempotencyKey: draftIdempotencyKeyRef.current,
    } satisfies PendingDraftState;
    writeSessionValue(PENDING_DRAFT_STORAGE_KEY, JSON.stringify(pending));
    setError("");
    createDraftMutation.mutate({
      name: trimmedName,
      idempotency_key: pending.idempotencyKey,
    });
  };

  const handleFinalizeIntake = () => {
    if (!workspace || finalizeIntakeMutation.isPending) return;
    const issue = validateAgentProductWorkspaceInput({
      name: workspace.product.name,
      selections,
      referenceImageCount: referenceFiles.length,
      limits: options?.limits ?? null,
    });
    if (issue) {
      setError(validationMessage(t, issue));
      return;
    }
    const idempotencyState =
      intakeIdempotencyRef.current?.conversationId === conversationId
        ? intakeIdempotencyRef.current
        : {
            conversationId,
            idempotencyKey: readOrCreateIntakeIdempotencyKey(conversationId),
          };
    intakeIdempotencyRef.current = idempotencyState;
    setError("");
    finalizeIntakeMutation.mutate({
      conversation_id: conversationId,
      selection: buildAgentProductSelection(selections),
      images: referenceFiles,
      idempotency_key: idempotencyState.idempotencyKey,
    });
  };

  const handleToggleImageType = (key: AgentProductImageTypeKey, selected: boolean) => {
    if (!options) return;
    setSelections((current) =>
      toggleAgentImageType(current, key, selected, options.limits.default_images_per_type),
    );
    rotateIntakeIdempotencyKey();
    setError("");
  };

  const handleQuantityChange = (key: AgentProductImageTypeKey, quantity: number) => {
    setSelections((current) => updateAgentImageTypeQuantity(current, key, quantity));
    rotateIntakeIdempotencyKey();
    setError("");
  };

  const handleAddReferenceFiles = (files: File[]) => {
    if (!options) {
      setError(t("agentCreate.error.optionsUnavailable"));
      return;
    }
    if (
      files.some(
        (file) => !options.limits.allowed_image_mime_types.some((mimeType) => mimeType === file.type),
      )
    ) {
      setError(t("agentCreate.error.unsupportedImage"));
      return;
    }
    if (referenceFiles.length + files.length > options.limits.max_reference_images) {
      setError(
        t("agentCreate.error.tooManyReferences", { maximum: options.limits.max_reference_images }),
      );
      return;
    }
    setReferenceFiles((current) => [...current, ...files]);
    rotateIntakeIdempotencyKey();
    setError("");
  };

  const handleRemoveReferenceFile = (index: number) => {
    setReferenceFiles((current) => current.filter((_, currentIndex) => currentIndex !== index));
    rotateIntakeIdempotencyKey();
    setError("");
  };

  const liveIssue =
    workspace && options
      ? validateAgentProductWorkspaceInput({
          name: workspace.product.name,
          selections,
          referenceImageCount: Math.max(
            referenceFiles.length,
            options.limits.min_reference_images,
          ),
          limits: options.limits,
        })
      : null;
  const liveError =
    error ||
    (liveIssue?.code === "quantity_out_of_range" || liveIssue?.code === "total_images_exceeded"
      ? validationMessage(t, liveIssue)
      : "");
  const restoring = Boolean(workspaceId && !workspace && workspaceQuery.isLoading);
  const restoreError = workspaceId && !workspace ? workspaceQuery.error : null;
  const phase = restoring || restoreError ? "restoring" : workspace ? "intake" : "identity";

  return (
    <div
      data-agent-create-phase={phase}
      className={`flex h-dvh min-h-[560px] flex-col overflow-hidden bg-white text-zinc-950 transition-opacity duration-200 motion-reduce:transition-none dark:bg-[#070a0f] dark:text-slate-100 ${
        leaving ? "opacity-0" : "opacity-100"
      }`}
    >
      <header className="z-10 flex h-14 shrink-0 items-center justify-between border-b border-zinc-200 bg-white px-4 dark:border-slate-800 dark:!bg-[#070a0f] sm:px-6">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-zinc-950 text-white dark:bg-cyan-400 dark:text-[#071018]">
            <Bot size={17} aria-hidden="true" />
          </div>
          <div className="min-w-0 truncate text-sm font-semibold">
            ProductFlow <span className="font-normal text-zinc-400 dark:text-slate-500">/</span>{" "}
            <span className="font-normal text-zinc-600 dark:text-slate-300">{t("agentCreate.title")}</span>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            title={t("agentCreate.settings")}
            aria-label={t("agentCreate.settings")}
            onClick={() => navigate("/settings?section=agent")}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <Settings2 size={17} />
          </button>
          <button
            type="button"
            title={t("agentCreate.close")}
            aria-label={t("agentCreate.close")}
            onClick={() => navigate("/products")}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <X size={18} />
          </button>
        </div>
      </header>

      <main className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {restoring ? (
          <div className="flex min-h-full items-center justify-center px-5 text-sm text-zinc-500 dark:text-slate-400">
            <Loader2 size={18} className="mr-2 animate-spin motion-reduce:animate-none" />
            {t("agentCreate.restoring")}
          </div>
        ) : null}

        {restoreError ? (
          <div className="mx-auto flex min-h-full w-full max-w-md flex-col items-center justify-center px-5 text-center">
            <p role="alert" className="text-sm leading-6 text-red-700 dark:text-red-200">
              {errorDetail(restoreError, t("agentCreate.error.failed"))}
            </p>
            <button
              type="button"
              onClick={() => void workspaceQuery.refetch()}
              className="mt-4 inline-flex h-10 items-center gap-2 rounded-md border border-zinc-300 px-3 text-sm font-semibold text-zinc-700 hover:border-zinc-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:border-slate-700 dark:text-slate-200"
            >
              <RotateCw size={15} />
              {t("agentCreate.retryOptions")}
            </button>
          </div>
        ) : null}

        {!workspaceId && !workspace ? (
          <div className="mx-auto flex min-h-full w-full max-w-[780px] flex-col items-center justify-center px-4 py-10 sm:px-6">
            <div className="flex h-11 w-11 items-center justify-center rounded-md border border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-slate-700 dark:!bg-[#0d1117] dark:text-slate-200">
              <Bot size={21} aria-hidden="true" />
            </div>
            <h1 className="mt-5 text-center text-2xl font-semibold tracking-normal text-zinc-950 dark:text-white sm:text-3xl">
              {t("agentCreate.title")}
            </h1>
            <form
              noValidate
              onSubmit={(event) => {
                event.preventDefault();
                handleCreateDraft();
              }}
              className="mt-8 w-full"
            >
              <div className="flex min-h-14 items-center gap-2 rounded-lg border border-zinc-300 bg-white p-2 pl-4 shadow-sm transition-[border-color,box-shadow] focus-within:border-zinc-500 focus-within:shadow-md dark:border-slate-700 dark:!bg-[#0d1117] dark:focus-within:border-slate-500">
                <label htmlFor="agent-product-name" className="sr-only">
                  {t("agentCreate.productName")}
                </label>
                <input
                  id="agent-product-name"
                  autoFocus
                  autoComplete="off"
                  value={name}
                  disabled={createDraftMutation.isPending}
                  onChange={(event) => handleNameChange(event.target.value)}
                  placeholder={t("agentCreate.namePlaceholder")}
                  className="min-w-0 flex-1 border-0 bg-transparent px-0 py-2 text-base outline-none placeholder:text-zinc-400 dark:!bg-transparent dark:placeholder:text-slate-500"
                />
                <button
                  type="submit"
                  title={t("agentCreate.continue")}
                  aria-label={t("agentCreate.continue")}
                  disabled={createDraftMutation.isPending || !name.trim()}
                  className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-zinc-950 text-white transition-colors hover:bg-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-35 dark:bg-cyan-400 dark:text-[#071018] dark:hover:bg-cyan-300"
                >
                  {createDraftMutation.isPending ? (
                    <Loader2 size={17} className="animate-spin motion-reduce:animate-none" />
                  ) : (
                    <ArrowUp size={18} />
                  )}
                </button>
              </div>
              {error ? (
                <p role="alert" className="mt-3 px-1 text-sm leading-5 text-red-700 dark:text-red-200">
                  {error}
                </p>
              ) : null}
            </form>
          </div>
        ) : null}

        {workspace && !workspace.intake_finalized ? (
          <div className="agent-create-message-in mx-auto w-full max-w-[880px] px-4 py-7 sm:px-6 sm:py-10">
            <div className="flex justify-end">
              <div className="max-w-[min(34rem,88%)] rounded-lg bg-blue-50 px-3.5 py-2.5 text-sm leading-5 text-zinc-900 dark:bg-cyan-400/10 dark:text-slate-100">
                {workspace.product.name}
              </div>
            </div>

            <div className="mt-7">
              <div className="flex items-center gap-2 text-xs font-semibold text-zinc-500 dark:text-slate-400">
                <Bot size={15} aria-hidden="true" />
                Agent
              </div>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-zinc-700 dark:text-slate-300">
                {t("agentCreate.description")}
              </p>
              <AgentProductCreateForm
                options={options}
                selections={selections}
                referenceFiles={referenceFiles}
                isOptionsLoading={optionsQuery.isLoading}
                isOptionsError={optionsQuery.isError}
                isSubmitting={finalizeIntakeMutation.isPending}
                error={liveError}
                onToggleImageType={handleToggleImageType}
                onQuantityChange={handleQuantityChange}
                onAddReferenceFiles={handleAddReferenceFiles}
                onRemoveReferenceFile={handleRemoveReferenceFile}
                onRetryOptions={() => void optionsQuery.refetch()}
                onSubmit={handleFinalizeIntake}
              />
            </div>
          </div>
        ) : null}
      </main>
    </div>
  );
}
