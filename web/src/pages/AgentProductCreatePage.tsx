import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, CircleAlert, Loader2, RotateCw, Settings2, Sparkles, TriangleAlert, X } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { api, ApiError } from "../lib/api";
import type { TranslateFunction } from "../lib/preferences";
import { useI18n } from "../lib/preferences";
import type {
  AgentProductImageTypeKey,
  AgentProductWorkspaceSnapshot,
} from "../lib/types";
import { agentProductWorkbenchPath } from "./workbench/agent/productWorkbenchRoute";
import { AgentProductCreateForm } from "./product-create/AgentProductCreateForm";
import {
  isAmbiguousFinalizeError,
  parsePendingDraft,
  resolveWorkspaceRestorationId,
  submitAgentProductIntake,
  type PendingDraftState,
} from "./product-create/intakeSubmission";
import {
  aspectRatioForSelection,
  buildAgentProductSelection,
  toggleAgentImageType,
  updateAgentImageTypeAspectRatio,
  updateAgentImageTypeQuantity,
  validateAgentProductWorkspaceInput,
  type AgentImageTypeSelectionDraft,
  type AgentProductCreateValidationIssue,
} from "./product-create/imageTypeSelection";
import {
  buildCreateGenerationSpec,
  defaultCreateOutputDraft,
  isCreateBriefReady,
  isCreateOutputReady,
  type CreateOutputDraft,
} from "./product-create/createIntake";

const PENDING_DRAFT_STORAGE_KEY = "productflow.agent-create.pending-draft.v1";
const INTAKE_STORAGE_KEY_PREFIX = "productflow.agent-create.intake.v1:";

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
  const pendingDraft = parsePendingDraft(raw);
  if (raw && !pendingDraft) removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
  return pendingDraft;
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
  const [brief, setBrief] = useState("");
  const [outputDraft, setOutputDraft] = useState<CreateOutputDraft>(defaultCreateOutputDraft);
  const [localWorkspace, setLocalWorkspace] = useState<AgentProductWorkspaceSnapshot | null>(null);
  const [selections, setSelections] = useState<AgentImageTypeSelectionDraft[]>([]);
  const [referenceFiles, setReferenceFiles] = useState<File[]>([]);
  const [error, setError] = useState("");
  const [reconciliationRequired, setReconciliationRequired] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const draftIdempotencyKeyRef = useRef(pendingDraft?.idempotencyKey ?? createIdempotencyKey());
  const intakeIdempotencyRef = useRef<IntakeIdempotencyState | null>(null);
  const submissionWorkspaceRef = useRef<AgentProductWorkspaceSnapshot | null>(null);
  const navigationScheduledRef = useRef(false);
  const navigationTimerRef = useRef<number | null>(null);

  const workspaceId = resolveWorkspaceRestorationId(searchParams.get("workspace"), pendingDraft);
  const agentSessionId = searchParams.get("agent_session_id")?.trim() || pendingDraft?.agentSessionId || null;
  const routeAgentTaskId = searchParams.get("agent_task_id")?.trim() || pendingDraft?.agentTaskId || null;
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
  const agentTaskId = routeAgentTaskId || workspace?.task_id || null;

  const optionsQuery = useQuery({
    queryKey: ["agent-product-workspace-options"],
    queryFn: api.getAgentProductWorkspaceOptions,
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
      navigate(
        agentProductWorkbenchPath(
          workspace.product.id,
          workspace.conversation.session_id,
          workspace.task_id || agentTaskId,
        ),
        { replace: true },
      );
    }, delay);
  }, [agentTaskId, navigate, workspace]);

  useEffect(
    () => () => {
      if (navigationTimerRef.current !== null) window.clearTimeout(navigationTimerRef.current);
    },
    [],
  );

  const retainWorkspace = (nextWorkspace: AgentProductWorkspaceSnapshot) => {
    submissionWorkspaceRef.current = nextWorkspace;
    const conversationId = nextWorkspace.conversation.id;
    const retainedPending = {
      name: nextWorkspace.product.name,
      idempotencyKey: draftIdempotencyKeyRef.current,
      conversationId,
      ...(nextWorkspace.conversation.session_id
        ? { agentSessionId: nextWorkspace.conversation.session_id }
        : {}),
      ...(nextWorkspace.task_id ? { agentTaskId: nextWorkspace.task_id } : {}),
    } satisfies PendingDraftState;
    writeSessionValue(PENDING_DRAFT_STORAGE_KEY, JSON.stringify(retainedPending));
    setLocalWorkspace(nextWorkspace);
    queryClient.setQueryData(["agent-product-workspace", conversationId], nextWorkspace);
    void queryClient.invalidateQueries({ queryKey: ["products"] });
    const params = new URLSearchParams({ workspace: conversationId });
    if (nextWorkspace.conversation.session_id) {
      params.set("agent_session_id", nextWorkspace.conversation.session_id);
    }
    if (nextWorkspace.task_id) {
      params.set("agent_task_id", nextWorkspace.task_id);
    }
    setSearchParams(params, { replace: true });
  };

  const finalizeIntake = async (targetWorkspace: AgentProductWorkspaceSnapshot) => {
    const targetConversationId = targetWorkspace.conversation.id;
    const idempotencyState =
      intakeIdempotencyRef.current?.conversationId === targetConversationId
        ? intakeIdempotencyRef.current
        : {
          conversationId: targetConversationId,
          idempotencyKey: readOrCreateIntakeIdempotencyKey(targetConversationId),
        };
    intakeIdempotencyRef.current = idempotencyState;
    return api.finalizeAgentProductWorkspaceIntake({
      conversation_id: targetConversationId,
      selection: buildAgentProductSelection(selections),
      images: referenceFiles,
      idempotency_key: idempotencyState.idempotencyKey,
      task_id: agentTaskId,
    });
  };

  const applyReconciledWorkspace = (reconciledWorkspace: AgentProductWorkspaceSnapshot) => {
    setLocalWorkspace(reconciledWorkspace);
    queryClient.setQueryData(
      ["agent-product-workspace", reconciledWorkspace.conversation.id],
      reconciledWorkspace,
    );
    if (reconciledWorkspace.intake_finalized) {
      removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
      removeSessionValue(intakeStorageKey(reconciledWorkspace.conversation.id));
    }
  };

  const reconcileWorkspace = async (targetWorkspace: AgentProductWorkspaceSnapshot) => {
    const reconciledWorkspace = await api.getAgentProductWorkspace(targetWorkspace.conversation.id);
    applyReconciledWorkspace(reconciledWorkspace);
    return reconciledWorkspace;
  };

  const submitMutation = useMutation({
    mutationFn: async () => {
      const trimmedName = name.trim();
      submissionWorkspaceRef.current = workspace ?? null;
      const pending = {
        name: trimmedName,
        idempotencyKey: draftIdempotencyKeyRef.current,
        ...(workspace ? { conversationId: workspace.conversation.id } : {}),
        ...(agentSessionId ? { agentSessionId } : {}),
        ...(agentTaskId ? { agentTaskId } : {}),
      } satisfies PendingDraftState;
      if (!workspace) writeSessionValue(PENDING_DRAFT_STORAGE_KEY, JSON.stringify(pending));
      return submitAgentProductIntake({
        workspace: workspace ?? null,
        createWorkspace: () =>
          api.createAgentProductDraftWorkspace({
            name: trimmedName,
            idempotency_key: pending.idempotencyKey,
            agent_session_id: pending.agentSessionId,
          }),
        retainWorkspace,
        finalizeWorkspace: finalizeIntake,
      });
    },
    onSuccess: (finalizedWorkspace) => {
      setError("");
      setLocalWorkspace(finalizedWorkspace);
      queryClient.setQueryData(
        ["agent-product-workspace", finalizedWorkspace.conversation.id],
        finalizedWorkspace,
      );
      removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
      removeSessionValue(intakeStorageKey(finalizedWorkspace.conversation.id));
      submissionWorkspaceRef.current = finalizedWorkspace;
      void queryClient.invalidateQueries({ queryKey: ["products"] });
      void queryClient.invalidateQueries({
        queryKey: ["agent-workbench", finalizedWorkspace.product.id],
      });
    },
    onError: async (mutationError) => {
      const originalError = errorDetail(mutationError, t("agentCreate.error.failed"));
      const retainedWorkspace = submissionWorkspaceRef.current ?? workspace ?? localWorkspace;
      if (!retainedWorkspace || !isAmbiguousFinalizeError(mutationError)) {
        setError(originalError);
        return;
      }

      try {
        const reconciledWorkspace = await reconcileWorkspace(retainedWorkspace);
        setReconciliationRequired(false);
        if (!reconciledWorkspace.intake_finalized) setError(originalError);
      } catch {
        setReconciliationRequired(true);
        setError(t("agentCreate.error.reconcileFailed"));
      }
    },
  });

  const startAgentMutation = useMutation({
    mutationFn: async () => {
      const trimmedName = name.trim();
      const pending = {
        name: trimmedName,
        idempotencyKey: draftIdempotencyKeyRef.current,
        ...(workspace ? { conversationId: workspace.conversation.id } : {}),
        ...(agentSessionId ? { agentSessionId } : {}),
        ...(agentTaskId ? { agentTaskId } : {}),
      } satisfies PendingDraftState;
      if (workspace) {
        return workspace;
      }
      writeSessionValue(PENDING_DRAFT_STORAGE_KEY, JSON.stringify(pending));
      return api.createAgentProductDraftWorkspace({
        name: trimmedName,
        idempotency_key: pending.idempotencyKey,
        agent_session_id: pending.agentSessionId,
      });
    },
    onSuccess: (createdWorkspace) => {
      setError("");
      removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
      void queryClient.invalidateQueries({ queryKey: ["products"] });
      void queryClient.invalidateQueries({
        queryKey: ["agent-workbench", createdWorkspace.product.id],
      });
      navigationScheduledRef.current = true;
      navigate(
        agentProductWorkbenchPath(
          createdWorkspace.product.id,
          createdWorkspace.conversation.session_id,
          createdWorkspace.task_id || agentTaskId,
        ),
        { replace: true },
      );
    },
    onError: (mutationError) => {
      setError(errorDetail(mutationError, t("agentCreate.error.failed")));
    },
  });

  const reconciliationMutation = useMutation({
    mutationFn: async () => {
      if (!workspace) throw new Error(t("agentCreate.error.reconcileFailed"));
      return reconcileWorkspace(workspace);
    },
    onSuccess: (reconciledWorkspace) => {
      setReconciliationRequired(false);
      if (!reconciledWorkspace.intake_finalized) setError("");
    },
    onError: () => {
      setReconciliationRequired(true);
      setError(t("agentCreate.error.reconcileFailed"));
    },
  });

  useEffect(() => {
    if (!workspace || workspace.intake_finalized || navigationScheduledRef.current) {
      return;
    }
    if (
      submitMutation.isPending ||
      startAgentMutation.isPending ||
      reconciliationMutation.isPending
    ) {
      return;
    }
    navigationScheduledRef.current = true;
    navigate(
      agentProductWorkbenchPath(
        workspace.product.id,
        workspace.conversation.session_id,
        workspace.task_id || agentTaskId,
      ),
      { replace: true },
    );
  }, [
    agentTaskId,
    navigate,
    reconciliationMutation.isPending,
    startAgentMutation.isPending,
    submitMutation.isPending,
    workspace,
  ]);

  const rotateIntakeIdempotencyKey = () => {
    if (!conversationId) return;
    const idempotencyKey = createIdempotencyKey();
    intakeIdempotencyRef.current = { conversationId, idempotencyKey };
    writeSessionValue(intakeStorageKey(conversationId), idempotencyKey);
  };

  const handleNameChange = (nextName: string) => {
    if (workspace) return;
    setName(nextName);
    draftIdempotencyKeyRef.current = createIdempotencyKey();
    removeSessionValue(PENDING_DRAFT_STORAGE_KEY);
    setError("");
  };

  const directCreateMutation = useMutation({
    mutationFn: async () => {
      const trimmedName = name.trim();
      return api.createProductDirect({
        name: trimmedName,
        images: [...referenceFiles],
        imageTypes: selections.map((item) => ({
          key: item.key,
          quantity: item.quantity,
          aspect_ratio: aspectRatioForSelection(item),
        })),
        sourceNote: brief.trim(),
        generationSpec: buildCreateGenerationSpec(outputDraft) ?? undefined,
      });
    },
    onSuccess: (result) => {
      queryClient.setQueryData(["workflow-graph", result.product.id], result.graph);
      queryClient.setQueryData(["product", result.product.id], result.product);
      void queryClient.invalidateQueries({ queryKey: ["products"] });
      navigate(`/products/${result.product.id}`);
    },
    onError: (mutationError) => {
      setError(errorDetail(mutationError, t("agentCreate.error.failed")));
    },
  });

  const handleDirectCreate = () => {
    if (workspace || submitMutation.isPending || startAgentMutation.isPending || directCreateMutation.isPending) return;
    const issue = validateAgentProductWorkspaceInput({
      name,
      selections,
      referenceImageCount: referenceFiles.length,
      limits: options?.limits ?? null,
    });
    if (issue) {
      setError(validationMessage(t, issue));
      return;
    }
    if (!isCreateBriefReady(brief)) {
      setError(t("agentCreate.error.briefRequired"));
      return;
    }
    if (!isCreateOutputReady(outputDraft)) {
      setError(t("agentCreate.error.outputInvalid"));
      return;
    }
    setError("");
    directCreateMutation.mutate();
  };

  const handleSubmit = () => {
    if (reconciliationRequired) {
      if (!reconciliationMutation.isPending) reconciliationMutation.mutate();
      return;
    }
    if (submitMutation.isPending || startAgentMutation.isPending || workspace?.intake_finalized) return;
    const trimmedName = (workspace?.product.name ?? name).trim();
    if (!trimmedName) {
      setError(t("agentCreate.error.nameRequired"));
      return;
    }
    if (!options) {
      if (optionsQuery.isLoading) return;
      setError("");
      startAgentMutation.mutate();
      return;
    }
    const intakeIssue = validateAgentProductWorkspaceInput({
      name: trimmedName,
      selections,
      referenceImageCount: referenceFiles.length,
      limits: options.limits,
    });
    setError("");
    if (intakeIssue === null) {
      submitMutation.mutate();
      return;
    }
    startAgentMutation.mutate();
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

  const handleAspectRatioChange = (key: AgentProductImageTypeKey, aspectRatio: string) => {
    setSelections((current) => updateAgentImageTypeAspectRatio(current, key, aspectRatio));
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

  const liveIssue = options
    ? validateAgentProductWorkspaceInput({
      name: workspace?.product.name ?? name,
      selections,
      referenceImageCount: Math.max(referenceFiles.length, options.limits.min_reference_images),
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
  const isSubmitting =
    submitMutation.isPending ||
    startAgentMutation.isPending ||
    reconciliationMutation.isPending ||
    directCreateMutation.isPending;
  const restoredUnfinalizedWorkspace = Boolean(
    workspaceId && workspace && !workspace.intake_finalized && !localWorkspace,
  );

  return (
    <div
      className={`relative flex h-dvh min-h-[560px] flex-col overflow-hidden bg-surface-base text-text-primary transition-opacity duration-200 motion-reduce:transition-none ${leaving ? "opacity-0" : "opacity-100"
        }`}
    >
      <header className="relative z-20 flex h-14 shrink-0 items-center justify-between border-b border-border-l1 bg-surface-raised/85 px-4 backdrop-blur-md sm:px-6">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-accent text-surface-raised shadow-sm">
            <Bot size={17} aria-hidden="true" />
          </div>
          <div className="min-w-0 truncate text-sm font-semibold">
            ProductFlow <span className="font-normal text-text-muted">/</span>{" "}
            <span className="font-normal text-text-secondary">{t("agentCreate.title")}</span>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            title={t("agentCreate.settings")}
            aria-label={t("agentCreate.settings")}
            onClick={() => navigate("/settings?section=agent")}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Settings2 size={17} />
          </button>
          <button
            type="button"
            title={t("agentCreate.close")}
            aria-label={t("agentCreate.close")}
            onClick={() => navigate("/products")}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-text-secondary transition-colors hover:bg-surface-subtle hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <X size={18} />
          </button>
        </div>
      </header>

      <main className="relative z-10 min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {restoring ? (
          <div className="flex min-h-full flex-col items-center justify-center gap-3 px-5">
            <span className="flex h-12 w-12 items-center justify-center rounded-2xl border border-border-l2 bg-surface-raised shadow-sm">
              <Loader2
                size={20}
                className="animate-spin text-accent motion-reduce:animate-none"
              />
            </span>
            <p className="text-sm text-text-secondary">{t("agentCreate.restoring")}</p>
          </div>
        ) : null}

        {restoreError ? (
          <div className="mx-auto flex min-h-full w-full max-w-md flex-col items-center justify-center px-5 text-center">
            <span className="flex h-12 w-12 items-center justify-center rounded-2xl border border-state-error/30 bg-state-error/10 text-state-error">
              <CircleAlert size={20} aria-hidden="true" />
            </span>
            <p role="alert" className="mt-4 text-sm leading-6 text-text-secondary">
              {errorDetail(restoreError, t("agentCreate.error.failed"))}
            </p>
            <button
              type="button"
              onClick={() => void workspaceQuery.refetch()}
              className="mt-4 inline-flex h-10 items-center gap-2 rounded-lg border border-border-l3 bg-surface-raised px-3 text-sm font-semibold text-text-secondary shadow-sm transition-colors hover:border-accent/50 hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <RotateCw size={15} />
              {t("agentCreate.retryOptions")}
            </button>
          </div>
        ) : null}

        {!restoring && !restoreError && !workspace?.intake_finalized ? (
          <div className="mx-auto w-full max-w-[920px] px-4 py-7 sm:px-6 sm:py-10">
            <div className="mb-6 flex items-start gap-3.5 sm:mb-8 sm:items-center">
              <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-accent text-surface-raised shadow-sm">
                <Sparkles size={20} aria-hidden="true" />
              </div>
              <div className="min-w-0">
                <h1 className="text-xl font-semibold leading-7 tracking-tight text-text-primary sm:text-[26px] sm:leading-8">
                  {t("agentCreate.title")}
                </h1>
                <p className="mt-1 text-sm leading-6 text-text-secondary">
                  {t("agentCreate.description")}
                </p>
              </div>
            </div>
            {restoredUnfinalizedWorkspace ? (
              <div className="mb-5 flex items-start gap-2.5 rounded-xl border border-state-warning/30 bg-state-warning/10 px-4 py-3 text-sm leading-5 text-text-primary">
                <TriangleAlert
                  size={16}
                  className="mt-0.5 shrink-0 text-state-warning"
                  aria-hidden="true"
                />
                <span>{t("agentCreate.recoveryNotice")}</span>
              </div>
            ) : null}
            <AgentProductCreateForm
              productName={workspace?.product.name ?? name}
              isProductNameReadOnly={Boolean(workspace)}
              options={options}
              selections={selections}
              referenceFiles={referenceFiles}
              isOptionsLoading={optionsQuery.isLoading}
              isOptionsError={optionsQuery.isError}
              isSubmitting={isSubmitting}
              editingLocked={reconciliationRequired}
              primaryActionLabel={
                reconciliationRequired ? t("agentCreate.recheckStatus") : undefined
              }
              error={liveError}
              onProductNameChange={handleNameChange}
              onToggleImageType={handleToggleImageType}
              onQuantityChange={handleQuantityChange}
              onAspectRatioChange={handleAspectRatioChange}
              onAddReferenceFiles={handleAddReferenceFiles}
              onRemoveReferenceFile={handleRemoveReferenceFile}
              brief={brief}
              outputDraft={outputDraft}
              onBriefChange={(value) => {
                setBrief(value);
                setError("");
              }}
              onOutputChange={(value) => {
                setOutputDraft(value);
                setError("");
              }}
              onRetryOptions={() => void optionsQuery.refetch()}
              onSubmit={handleSubmit}
              onDirectCreate={workspace ? undefined : handleDirectCreate}
              isDirectCreating={directCreateMutation.isPending}
            />
          </div>
        ) : null}
      </main>
    </div>
  );
}
