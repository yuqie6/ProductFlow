import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Braces,
  Eye,
  Image as ImageIcon,
  ImagePlus,
  Link2,
  Loader2,
  Package,
  Play,
  Save,
  Undo2,
  XCircle,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { CompactInput, CompactNumberInput, CompactSelect } from "../../../components/CompactFormFields";
import { ImageAspectRatioPicker } from "../../../components/ImageAspectRatioPicker";
import { ImageGenerationSettingsTabs, type ImageGenerationSettingsTab } from "../../../components/ImageGenerationSettingsTabs";
import { PromptPreviewDialog, type PromptPreview } from "../../../components/PromptPreviewDialog";
import { SelectField } from "../../../components/SelectField";
import { ApiError, api } from "../../../lib/api";
import { formatDateTime } from "../../../lib/format";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { useI18n } from "../../../lib/preferences";
import type {
  CanonicalProductDetail,
  ProductWorkflowV2,
  UpdateWorkflowNodeV2Input,
  WorkflowCanvasMutationResult,
  WorkflowDeliverySpec,
  WorkflowDraftProductFact,
  WorkflowGenerationSpec,
  WorkflowImagePromptPayloadV1,
  WorkflowNodeDetailV2,
  WorkflowNodeRunV2,
  WorkflowNodeTypeV2,
  WorkflowNodeV2,
} from "../../../lib/types";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "../chrome/constants";
import { DownloadLink } from "../chrome/ImageDownloadComponents";
import { SaveStatusBadge, type SaveStatus } from "../chrome/SaveStatusBadge";
import { TextArea } from "../chrome/TextArea";
import { statusClass } from "../chrome/utils";
import { DeliveryRenditionPanel } from "./DeliveryRenditionPanel";
import { parseWorkflowDeliverySpec } from "./deliveryRenditions";
import { parseWorkflowGenerationSpec } from "./generationSpec";
import { workflowNodeDownloadableImage } from "./nodeImages";
import {
  defaultDeliverySpec,
  formatFactValue,
  humanizeFactKey,
  imageEditorDraft,
  normalizeImageDraft,
  normalizeReferenceDraft,
  referenceEditorDraft,
  validateImageDraft,
  validateReferenceDraft,
  type ImageEditorDraft,
  type ReferenceEditorDraft,
} from "./nodeEditorDrafts";
import { useV2NodeDraftAutosave, type V2NodeDraftAutosave } from "./useV2NodeDraftAutosave";
import { V2CanvasDashboard } from "./V2CanvasDashboard";

export type V2NodeInspectorFlush = () => Promise<number | null>;

interface V2NodeInspectorProps {
  product: CanonicalProductDetail;
  workflow: ProductWorkflowV2;
  node: WorkflowNodeV2 | null;
  facts: WorkflowDraftProductFact[];
  onBindReference: (node: WorkflowNodeV2) => void | Promise<void>;
  onPreviewImage: (image: DownloadableImage) => void;
  onWorkflowChanged: () => Promise<unknown>;
  onFlushRegistration?: (flush: V2NodeInspectorFlush | null) => void;
  onOpenAddPanel?: () => void;
  onOpenLibraryPanel?: () => void;
  onRunWorkflow?: () => void;
  runWorkflowBusy?: boolean;
}

interface EditorSaveState {
  status: SaveStatus;
  error: string | null;
}

const ACTIVE_RUN_STATUSES = new Set(["queued", "running"]);
const SELECT_OPTION_LABEL_KEYS = {
  none: "agentWorkbench.nodeEditor.option.none",
  allowed: "agentWorkbench.nodeEditor.option.allowed",
  required: "agentWorkbench.nodeEditor.option.required",
  standard: "agentWorkbench.nodeEditor.option.standard",
  high: "agentWorkbench.nodeEditor.option.high",
  ultra: "agentWorkbench.nodeEditor.option.ultra",
  draft: "agentWorkbench.nodeEditor.option.draft",
  low: "agentWorkbench.nodeEditor.option.low",
  medium: "agentWorkbench.nodeEditor.option.medium",
  auto: "agentWorkbench.nodeEditor.option.auto",
  opaque: "agentWorkbench.nodeEditor.option.opaque",
  transparent: "agentWorkbench.nodeEditor.option.transparent",
  allow: "agentWorkbench.nodeEditor.option.allow",
  contain: "agentWorkbench.nodeEditor.option.contain",
  cover: "agentWorkbench.nodeEditor.option.cover",
  center: "agentWorkbench.nodeEditor.option.center",
  top: "agentWorkbench.nodeEditor.option.top",
  bottom: "agentWorkbench.nodeEditor.option.bottom",
  left: "agentWorkbench.nodeEditor.option.left",
  right: "agentWorkbench.nodeEditor.option.right",
} as const;
const FACT_STATUS_LABEL_KEYS = {
  observed: "workflowConfirmation.factStatus.observed",
  user_declared: "workflowConfirmation.factStatus.userDeclared",
  confirmed: "workflowConfirmation.factStatus.confirmed",
  conflicted: "workflowConfirmation.factStatus.conflicted",
} as const;

export function V2NodeInspector({
  product,
  workflow,
  node,
  facts,
  onBindReference,
  onPreviewImage,
  onWorkflowChanged,
  onFlushRegistration,
  onOpenAddPanel,
  onOpenLibraryPanel,
  onRunWorkflow,
  runWorkflowBusy,
}: V2NodeInspectorProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [editorSaveState, setEditorSaveState] = useState<EditorSaveState>({ status: "idle", error: null });
  const editorFlushRef = useRef<V2NodeInspectorFlush>(async () => null);
  const registerEditorFlush = useCallback((flush: V2NodeInspectorFlush | null) => {
    editorFlushRef.current = flush ?? (async () => null);
    onFlushRegistration?.(flush);
  }, [onFlushRegistration]);
  const updateEditorSaveState = useCallback((status: SaveStatus, error: string | null) => {
    setEditorSaveState((current) => current.status === status && current.error === error
      ? current
      : { status, error });
  }, []);

  useEffect(() => {
    setEditorSaveState({ status: "idle", error: null });
  }, [node?.id]);
  const detailQuery = useQuery({
    queryKey: ["v2-workflow-node-detail", product.id, workflow.id, node?.id],
    queryFn: () => api.getWorkflowNodeDetailV2(product.id, workflow.id, node!.id),
    enabled: Boolean(node),
  });
  const runsQueryKey = ["v2-workflow-node-runs", node?.id] as const;
  const runsQuery = useQuery({
    queryKey: runsQueryKey,
    queryFn: () => api.listWorkflowNodeRunsV2(node!.id),
    enabled: Boolean(node),
    refetchInterval: (query) => query.state.data?.items.some((run) => ACTIVE_RUN_STATUSES.has(run.status))
      ? 1_200
      : false,
  });
  const activeRun = runsQuery.data?.items.find((run) => ACTIVE_RUN_STATUSES.has(run.status)) ?? null;

  const refreshNodeViews = useCallback(async () => {
    await Promise.all([
      detailQuery.refetch(),
      queryClient.invalidateQueries({ queryKey: runsQueryKey }),
      onWorkflowChanged(),
    ]);
  }, [detailQuery.refetch, onWorkflowChanged, queryClient, runsQueryKey]);
  const updateMutation = useMutation({
    mutationFn: (input: UpdateWorkflowNodeV2Input) => api.updateWorkflowNodeV2(
      product.id,
      workflow.id,
      node!.id,
      input,
    ),
    onSuccess: refreshNodeViews,
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        // A concurrent edit won on the server: re-pull authoritative state so
        // the inspector converges instead of showing a stale edit.
        await refreshNodeViews();
      }
    },
  });
  const runMutation = useMutation({
    mutationFn: async () => {
      await editorFlushRef.current();
      return api.runWorkflowNodeV2(node!.id);
    },
    onSuccess: async (result) => {
      queryClient.setQueryData<{ items: WorkflowNodeRunV2[] }>(runsQueryKey, (current) => ({
        items: [result.node_run, ...(current?.items ?? []).filter((item) => item.id !== result.node_run.id)],
      }));
      await refreshNodeViews();
    },
  });
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelWorkflowNodeRunV2(runId),
    onSuccess: refreshNodeViews,
  });

  if (!node) {
    return (
      <V2CanvasDashboard
        product={product}
        workflow={workflow}
        facts={facts}
        onOpenAddPanel={onOpenAddPanel}
        onOpenLibraryPanel={onOpenLibraryPanel}
        onRunWorkflow={onRunWorkflow}
        runWorkflowBusy={runWorkflowBusy}
      />
    );
  }
  if (detailQuery.isLoading) {
    return <PanelState icon={<Loader2 size={20} className="animate-spin" />} text={t("app.loading")} />;
  }
  if (detailQuery.isError || !detailQuery.data) {
    return (
      <PanelState
        icon={<AlertCircle size={20} />}
        text={errorDetail(detailQuery.error, t("agentWorkbench.nodeEditor.loadFailed"))}
        action={t("agentWorkbench.retry")}
        onAction={() => void detailQuery.refetch()}
      />
    );
  }

  const detail = detailQuery.data;
  const canRun = node.node_type === "prompt_generation" || node.node_type === "image_generation";
  const mutationError = updateMutation.error ?? runMutation.error ?? cancelMutation.error;

  return (
    <div className="space-y-3 pb-4" data-v2-node-inspector>
      <section className="config-bubble rounded-2xl p-4 shadow-sm">
        <div className="flex items-start gap-3">
          <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border shadow-sm ${
            node.node_type === "product_context"
              ? "border-purple-200/80 bg-purple-50 text-purple-700 dark:border-purple-500/30 dark:bg-purple-950/60 dark:text-purple-300"
              : node.node_type === "reference_image"
                ? "border-indigo-200/80 bg-indigo-50 text-indigo-700 dark:border-indigo-500/30 dark:bg-indigo-950/60 dark:text-indigo-300"
                : node.node_type === "prompt_generation"
                  ? "border-amber-200/80 bg-amber-50 text-amber-700 dark:border-amber-500/30 dark:bg-amber-950/60 dark:text-amber-300"
                  : "border-cyan-200/80 bg-cyan-50 text-cyan-700 dark:border-cyan-500/30 dark:bg-cyan-950/60 dark:text-cyan-300"
          }`}>
            <NodeTypeIcon type={node.node_type} />
          </span>
          <div className="min-w-0 flex-1">
            <h3 className="truncate text-base font-semibold text-zinc-950 dark:text-white">{node.title}</h3>
            <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
              <span className="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-medium text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
                {nodeTypeLabel(node.node_type, t)}
              </span>
              <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(node.status)}`}>
                {ACTIVE_RUN_STATUSES.has(node.status) ? <Loader2 size={10} className="mr-1 animate-spin" /> : null}
                {t(`detail.nodeStatus.${node.status}`)}
              </span>
              {node.node_type !== "product_context" ? <SaveStatusBadge status={editorSaveState.status} /> : null}
            </div>
          </div>
        </div>

        {activeRun ? (
          <div className="mt-3 flex items-start gap-2 rounded-xl border border-indigo-100 bg-indigo-50 px-3 py-2.5 text-xs text-indigo-700 dark:border-violet-400/30 dark:bg-violet-500/10 dark:text-violet-100">
            <Loader2 size={14} className="mt-0.5 shrink-0 animate-spin" />
            <div className="min-w-0 flex-1">
              <div className="font-semibold">{t(`detail.nodeStatus.${activeRun.status}`)}</div>
              <div className="mt-0.5 text-[10px] opacity-75">{formatDateTime(activeRun.started_at, t.locale)}</div>
            </div>
          </div>
        ) : null}

        {canRun || activeRun ? (
          <div className={`mt-4 grid gap-2 ${activeRun ? "grid-cols-2" : "grid-cols-1"}`}>
            {canRun ? (
              <button
                type="button"
                onClick={() => runMutation.mutate()}
                disabled={Boolean(activeRun) || runMutation.isPending || updateMutation.isPending}
                className="btn-primary-spring inline-flex h-10 items-center justify-center rounded-xl px-3 text-xs font-semibold"
              >
                {runMutation.isPending ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Play size={14} className="mr-1.5" />}
                {t("detail.run")}
              </button>
            ) : null}
            {activeRun ? (
              <button
                type="button"
                onClick={() => cancelMutation.mutate(activeRun.id)}
                disabled={cancelMutation.isPending}
                className="btn-danger-spring inline-flex h-10 items-center justify-center rounded-xl px-3 text-xs font-semibold"
              >
                {cancelMutation.isPending ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <XCircle size={14} className="mr-1.5" />}
                {t("detail.cancel")}
              </button>
            ) : null}
          </div>
        ) : null}
      </section>

      {editorSaveState.error || mutationError ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {editorSaveState.error ?? errorDetail(mutationError, t("agentWorkbench.nodeEditor.loadFailed"))}
        </div>
      ) : null}
      {node.failure_reason ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {node.failure_reason}
        </div>
      ) : null}

      {node.node_type === "product_context" ? (
        <ProductContextEditor product={product} facts={facts} />
      ) : node.node_type === "reference_image" ? (
        <ReferenceNodeEditor
          key={node.id}
          detail={detail}
          node={node}
          workflowEditVersion={workflow.edit_version}
          busy={updateMutation.isPending}
          onBind={() => onBindReference(node)}
          onSave={(input) => updateMutation.mutateAsync(input)}
          onFlushRegistration={registerEditorFlush}
          onSaveStateChange={updateEditorSaveState}
          onPreviewImage={onPreviewImage}
        />
      ) : node.node_type === "prompt_generation" && detail.prompt_artifact ? (
        <PromptNodeEditor
          key={`${node.id}:${detail.prompt_artifact.version_id}`}
          detail={detail}
          workflowEditVersion={workflow.edit_version}
          busy={updateMutation.isPending}
          onSave={(input) => updateMutation.mutateAsync(input)}
          onFlushRegistration={registerEditorFlush}
          onSaveStateChange={updateEditorSaveState}
        />
      ) : node.node_type === "image_generation" ? (
        <>
          <ImageNodeEditor
            key={node.id}
            detail={detail}
            node={node}
            workflowEditVersion={workflow.edit_version}
            busy={updateMutation.isPending}
            onSave={(input) => updateMutation.mutateAsync(input)}
            onFlushRegistration={registerEditorFlush}
            onSaveStateChange={updateEditorSaveState}
            onPreviewImage={onPreviewImage}
          />
          <section className="config-bubble overflow-hidden rounded-2xl shadow-sm">
            <SectionHeading title={t("workflowV2.sidebar.artifacts")} />
            <DeliveryRenditionPanel productId={product.id} node={node} onPreviewImage={onPreviewImage} />
          </section>
        </>
      ) : (
        <PanelState text={t("agentWorkbench.nodeEditor.loadFailed")} />
      )}
    </div>
  );
}

function ProductContextEditor({
  product,
  facts,
}: {
  product: CanonicalProductDetail;
  facts: WorkflowDraftProductFact[];
}) {
  const { t } = useI18n();
  return (
    <section className="config-bubble rounded-2xl p-4 shadow-sm">
      <div className="flex items-center justify-between gap-3">
        <SectionTitle title={t("agentWorkbench.nodeEditor.productLineage")} />
        <span className="rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-[10px] font-medium text-zinc-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
          {t("agentWorkbench.nodeEditor.readOnly")}
        </span>
      </div>
      <dl className="mt-3 grid gap-2 text-xs">
        <ReadOnlyRow label={t("detail.inspector.productName")} value={product.name} />
        <ReadOnlyRow label={t("detail.inspector.category")} value={product.category || t("agentWorkbench.nodeEditor.noValue")} />
        <ReadOnlyRow label={t("detail.inspector.price")} value={product.price || t("agentWorkbench.nodeEditor.noValue")} />
      </dl>
      <div className="mt-5 border-t border-zinc-200 pt-4 dark:border-slate-700">
        <SectionTitle title={t("agentWorkbench.nodeEditor.productFacts")} />
        {facts.length ? (
          <div className="mt-2 divide-y divide-zinc-100 dark:divide-slate-800">
            {facts.map((fact) => (
              <div key={fact.key} className="py-2.5">
                <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                  <span className="min-w-0 flex-1 text-xs font-semibold text-zinc-800 dark:text-slate-100">{humanizeFactKey(fact.key)}</span>
                  <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${fact.status === "conflicted" ? "bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-200" : "bg-zinc-100 text-zinc-500 dark:bg-slate-800 dark:text-slate-300"}`}>
                    {t(FACT_STATUS_LABEL_KEYS[fact.status])}
                  </span>
                  <span className="shrink-0 rounded bg-blue-50 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-cyan-400/10 dark:text-cyan-200">
                    {factSourceLabel(fact.source_type, t)}
                  </span>
                </div>
                <div className="mt-1 break-words text-xs leading-5 text-zinc-600 dark:text-slate-300">
                  {formatFactValue(fact.value, t("agentWorkbench.nodeEditor.noValue"), {
                    true: t("agentWorkbench.nodeEditor.factTrue"),
                    false: t("agentWorkbench.nodeEditor.factFalse"),
                  })}
                </div>
                {fact.requires_confirmation ? (
                  <div className="mt-1 text-[11px] font-medium text-amber-700 dark:text-amber-300">
                    {t("workflowConfirmation.requiresConfirmation")}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        ) : (
          <p className="mt-2 text-xs text-zinc-500 dark:text-slate-400">{t("agentWorkbench.nodeEditor.noFacts")}</p>
        )}
      </div>
    </section>
  );
}

function ReferenceNodeEditor({
  detail,
  node,
  workflowEditVersion,
  busy,
  onBind,
  onSave,
  onFlushRegistration,
  onSaveStateChange,
  onPreviewImage,
}: {
  detail: WorkflowNodeDetailV2;
  node: WorkflowNodeV2;
  workflowEditVersion: number;
  busy: boolean;
  onBind: () => void | Promise<void>;
  onSave: (input: UpdateWorkflowNodeV2Input) => Promise<WorkflowCanvasMutationResult>;
  onFlushRegistration: (flush: V2NodeInspectorFlush | null) => void;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
  onPreviewImage: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const editor = useV2NodeDraftAutosave<ReferenceEditorDraft>({
    serverValue: referenceEditorDraft(node),
    serverEditVersion: Math.max(detail.workflow_edit_version, workflowEditVersion),
    disabled: busy,
    normalize: normalizeReferenceDraft,
    validate: (draft) => validateReferenceDraft(draft, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft, expectedEditVersion) => onSave({
      node_type: "reference_image",
      expected_edit_version: expectedEditVersion,
      title: draft.title,
      role: draft.role,
      label: draft.label,
    }),
    onStateChange: onSaveStateChange,
  });
  const image = workflowNodeDownloadableImage(node);
  return (
    <AutosaveEditorForm editor={editor} busy={busy} onFlushRegistration={onFlushRegistration}>
      {image ? <NodeImagePreview image={image} onPreview={onPreviewImage} /> : null}
      <TextInput
        label={t("agentWorkbench.nodeEditor.nodeName")}
        value={editor.draft.title}
        maxLength={255}
        onChange={(title) => editor.update({ ...editor.draft, title })}
      />
      <TextInput
        label={t("agentWorkbench.nodeEditor.referenceRole")}
        value={editor.draft.role}
        maxLength={120}
        onChange={(role) => editor.update({ ...editor.draft, role })}
      />
      <TextInput
        label={t("agentWorkbench.nodeEditor.referenceLabel")}
        value={editor.draft.label}
        maxLength={255}
        onChange={(label) => editor.update({ ...editor.draft, label })}
      />
      <button
        type="button"
        onClick={() => void onBind()}
        className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl border border-indigo-200 bg-indigo-50 px-3 text-xs font-semibold text-indigo-700 hover:border-indigo-300 hover:bg-indigo-100 dark:border-violet-400/35 dark:bg-violet-500/10 dark:text-violet-100 dark:hover:bg-violet-500/15"
      >
        <Link2 size={14} />{t("agentWorkbench.nodeEditor.bindReference")}
      </button>
    </AutosaveEditorForm>
  );
}

function PromptNodeEditor({
  detail,
  workflowEditVersion,
  busy,
  onSave,
  onFlushRegistration,
  onSaveStateChange,
}: {
  detail: WorkflowNodeDetailV2;
  workflowEditVersion: number;
  busy: boolean;
  onSave: (input: UpdateWorkflowNodeV2Input) => Promise<WorkflowCanvasMutationResult>;
  onFlushRegistration: (flush: V2NodeInspectorFlush | null) => void;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const artifact = detail.prompt_artifact!;
  const [title, setTitle] = useState(detail.node.title);
  const [payload, setPayload] = useState<WorkflowImagePromptPayloadV1>(artifact.payload);
  const [saveState, setSaveState] = useState<EditorSaveState>({ status: "idle", error: null });
  const [promptPreview, setPromptPreview] = useState<PromptPreview | null>(null);
  const patchPayload = (patch: Partial<WorkflowImagePromptPayloadV1>) => setPayload((current) => ({ ...current, ...patch }));
  const dirty = title !== detail.node.title || !sameJson(payload, artifact.payload);
  const submit = async () => {
    if (!dirty || busy) {
      return;
    }
    const normalizedTitle = title.trim();
    if (!normalizedTitle || title.length > 255) {
      const error = new Error(t("agentWorkbench.nodeEditor.invalidDraft"));
      setSaveState({ status: "failed", error: error.message });
      throw error;
    }
    setTitle(normalizedTitle);
    setSaveState({ status: "saving", error: null });
    try {
      await onSave({
        node_type: "prompt_generation",
        expected_edit_version: Math.max(detail.workflow_edit_version, workflowEditVersion),
        expected_prompt_artifact_version_id: artifact.version_id,
        title: normalizedTitle,
        prompt_payload: payload,
      });
      setSaveState({ status: "saved", error: null });
    } catch (error) {
      const message = errorDetail(error, t("agentWorkbench.nodeEditor.loadFailed"));
      setSaveState({ status: "failed", error: message });
      throw error;
    }
  };
  const discard = () => {
    setTitle(detail.node.title);
    setPayload(artifact.payload);
    setSaveState({ status: "saved", error: null });
  };

  useEffect(() => {
    if (dirty && saveState.status === "saved") {
      setSaveState({ status: "idle", error: null });
    }
  }, [dirty, saveState.status]);

  useEffect(() => {
    onSaveStateChange(saveState.status, saveState.error);
  }, [onSaveStateChange, saveState.error, saveState.status]);

  useEffect(() => {
    const flush: V2NodeInspectorFlush = async () => {
      if (dirty) {
        const error = new Error(t("agentWorkbench.nodeEditor.unsavedPrompt"));
        setSaveState({ status: "failed", error: error.message });
        throw error;
      }
      return Math.max(detail.workflow_edit_version, workflowEditVersion);
    };
    onFlushRegistration(flush);
    return () => onFlushRegistration(null);
  }, [detail.workflow_edit_version, dirty, onFlushRegistration, t, workflowEditVersion]);

  return (
    <PromptEditorForm busy={busy} dirty={dirty} onSubmit={submit} onDiscard={discard}>
      <div className="flex items-center justify-between gap-2">
        <SectionTitle title={t("workflowConfirmation.section.prompts")} />
        <span className="rounded-full bg-indigo-50 px-2 py-1 text-[10px] font-semibold text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200">
          {t("agentWorkbench.nodeEditor.promptVersion", { version: artifact.version })}
        </span>
      </div>
      <TextInput label={t("agentWorkbench.nodeEditor.nodeName")} value={title} maxLength={255} onChange={setTitle} />
      <button
        type="button"
        onClick={() => setPromptPreview({
          title: title || detail.node.title,
          text: formatPromptArtifactPreview(payload, t),
          meta: t("agentWorkbench.nodeEditor.promptVersion", { version: artifact.version }),
        })}
        className="inline-flex h-9 w-full items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-xs font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-950 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-violet-400/45 dark:hover:bg-violet-500/12 dark:hover:text-white"
      >
        <Eye size={13} className="mr-1.5" />
        {t("agentWorkbench.nodeEditor.previewPrompt")}
      </button>
      <TextArea label={t("workflowConfirmation.designGoal")} value={payload.design_goal} onChange={(design_goal) => patchPayload({ design_goal })} minRows={3} />
      <LineListField label={t("workflowConfirmation.sharedRules")} value={payload.shared_rules} onChange={(shared_rules) => patchPayload({ shared_rules })} />
      <LineListField label={t("workflowConfirmation.creativeBoundary")} value={payload.creative_boundary} onChange={(creative_boundary) => patchPayload({ creative_boundary })} />

      <FieldGroup title={t("workflowConfirmation.productFidelity")}>
        <div className="grid gap-2 sm:grid-cols-2">
          <CheckboxField label={t("agentWorkbench.nodeEditor.complexStructure")} checked={payload.product_fidelity.complex_structure} onChange={(complex_structure) => patchPayload({ product_fidelity: { ...payload.product_fidelity, complex_structure } })} />
          <CheckboxField label={t("agentWorkbench.nodeEditor.productPresent")} checked={payload.product_fidelity.product_present} onChange={(product_present) => patchPayload({ product_fidelity: { ...payload.product_fidelity, product_present } })} />
        </div>
        <SelectInput label={t("agentWorkbench.nodeEditor.pictureInPicture")} value={payload.product_fidelity.picture_in_picture} options={["none", "allowed", "required"]} onChange={(picture_in_picture) => patchPayload({ product_fidelity: { ...payload.product_fidelity, picture_in_picture: picture_in_picture as "none" | "allowed" | "required" } })} />
        <LineListField label={t("agentWorkbench.nodeEditor.requirements")} value={payload.product_fidelity.requirements} onChange={(requirements) => patchPayload({ product_fidelity: { ...payload.product_fidelity, requirements } })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.composition")}>
        <TextInput label={t("agentWorkbench.nodeEditor.viewpoint")} value={payload.composition.viewpoint} onChange={(viewpoint) => patchPayload({ composition: { ...payload.composition, viewpoint } })} />
        <NumberInput label={t("agentWorkbench.nodeEditor.productShare")} value={payload.composition.product_share_percent} min={1} max={100} onChange={(product_share_percent) => patchPayload({ composition: { ...payload.composition, product_share_percent } })} />
        <TextArea label={t("agentWorkbench.nodeEditor.layout")} value={payload.composition.layout} onChange={(layout) => patchPayload({ composition: { ...payload.composition, layout } })} />
        <LineListField label={t("agentWorkbench.nodeEditor.copyRegions")} value={payload.composition.copy_regions} onChange={(copy_regions) => patchPayload({ composition: { ...payload.composition, copy_regions } })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.content")}>
        <LineListField label={t("agentWorkbench.nodeEditor.focus")} value={payload.content.focus} onChange={(focus) => patchPayload({ content: { ...payload.content, focus } })} />
        <LineListField label={t("agentWorkbench.nodeEditor.sellingPoints")} value={payload.content.selling_points} onChange={(selling_points) => patchPayload({ content: { ...payload.content, selling_points } })} />
        <TextArea label={t("agentWorkbench.nodeEditor.background")} value={payload.content.background} onChange={(background) => patchPayload({ content: { ...payload.content, background } })} />
        <LineListField label={t("agentWorkbench.nodeEditor.decorations")} value={payload.content.decorations} onChange={(decorations) => patchPayload({ content: { ...payload.content, decorations } })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.textContent")}>
        <TextInput label={t("agentWorkbench.nodeEditor.headline")} value={payload.text.headline ?? ""} onChange={(headline) => patchPayload({ text: { ...payload.text, headline: nullableText(headline) } })} />
        <TextInput label={t("agentWorkbench.nodeEditor.subtitle")} value={payload.text.subtitle ?? ""} onChange={(subtitle) => patchPayload({ text: { ...payload.text, subtitle: nullableText(subtitle) } })} />
        <TextArea label={t("agentWorkbench.nodeEditor.body")} value={payload.text.body ?? ""} onChange={(body) => patchPayload({ text: { ...payload.text, body: nullableText(body) } })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.atmosphere")}>
        <LineListField label={t("agentWorkbench.nodeEditor.keywords")} value={payload.atmosphere.keywords} onChange={(keywords) => patchPayload({ atmosphere: { ...payload.atmosphere, keywords } })} />
        <TextArea label={t("agentWorkbench.nodeEditor.lighting")} value={payload.atmosphere.lighting} onChange={(lighting) => patchPayload({ atmosphere: { ...payload.atmosphere, lighting } })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.perImageInstructions")}>
        {payload.images.map((image, index) => (
          <div key={image.image_plan_key} className="border-t border-zinc-200 pt-3 first:border-t-0 first:pt-0 dark:border-slate-700">
            <div className="mb-2 text-xs font-semibold text-zinc-800 dark:text-slate-100">
              {t("agentWorkbench.nodeEditor.perImageNumber", { number: index + 1 })}
            </div>
            <TextArea label={t("agentWorkbench.nodeEditor.instruction")} value={image.instruction} onChange={(instruction) => setPayload((current) => ({ ...current, images: current.images.map((item, itemIndex) => itemIndex === index ? { ...item, instruction } : item) }))} minRows={3} />
            <div className="mt-2">
              <TextInput label={t("agentWorkbench.nodeEditor.viewpoint")} value={image.viewpoint ?? ""} onChange={(viewpoint) => setPayload((current) => ({ ...current, images: current.images.map((item, itemIndex) => itemIndex === index ? { ...item, viewpoint: nullableText(viewpoint) } : item) }))} />
            </div>
            <div className="mt-2">
              <LineListField label={t("agentWorkbench.nodeEditor.compositionAdjustments")} value={image.composition_adjustments} onChange={(composition_adjustments) => setPayload((current) => ({ ...current, images: current.images.map((item, itemIndex) => itemIndex === index ? { ...item, composition_adjustments } : item) }))} />
            </div>
            <div className="mt-2">
              <TextArea label={t("agentWorkbench.nodeEditor.lighting")} value={image.lighting ?? ""} onChange={(lighting) => setPayload((current) => ({ ...current, images: current.images.map((item, itemIndex) => itemIndex === index ? { ...item, lighting: nullableText(lighting) } : item) }))} />
            </div>
          </div>
        ))}
      </FieldGroup>

      <div className="rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2.5 text-[11px] leading-5 text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-400">
        {t("agentWorkbench.nodeEditor.immutableLineage")}
      </div>
      {promptPreview ? <PromptPreviewDialog preview={promptPreview} onClose={() => setPromptPreview(null)} /> : null}
    </PromptEditorForm>
  );
}

function ImageNodeEditor({
  detail,
  node,
  workflowEditVersion,
  busy,
  onSave,
  onFlushRegistration,
  onSaveStateChange,
  onPreviewImage,
}: {
  detail: WorkflowNodeDetailV2;
  node: WorkflowNodeV2;
  workflowEditVersion: number;
  busy: boolean;
  onSave: (input: UpdateWorkflowNodeV2Input) => Promise<WorkflowCanvasMutationResult>;
  onFlushRegistration: (flush: V2NodeInspectorFlush | null) => void;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
  onPreviewImage: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const serverDraft = imageEditorDraft(node);
  if (!serverDraft) {
    return <PanelState icon={<AlertCircle size={20} />} text={t("agentWorkbench.nodeEditor.loadFailed")} />;
  }
  return (
    <ImageNodeEditorFields
      detail={detail}
      node={node}
      serverDraft={serverDraft}
      workflowEditVersion={workflowEditVersion}
      busy={busy}
      onSave={onSave}
      onFlushRegistration={onFlushRegistration}
      onSaveStateChange={onSaveStateChange}
      onPreviewImage={onPreviewImage}
    />
  );
}

function ImageNodeEditorFields({
  detail,
  node,
  serverDraft,
  workflowEditVersion,
  busy,
  onSave,
  onFlushRegistration,
  onSaveStateChange,
  onPreviewImage,
}: {
  detail: WorkflowNodeDetailV2;
  node: WorkflowNodeV2;
  serverDraft: ImageEditorDraft;
  workflowEditVersion: number;
  busy: boolean;
  onSave: (input: UpdateWorkflowNodeV2Input) => Promise<WorkflowCanvasMutationResult>;
  onFlushRegistration: (flush: V2NodeInspectorFlush | null) => void;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
  onPreviewImage: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const [settingsTab, setSettingsTab] = useState<ImageGenerationSettingsTab>("basic");
  const editor = useV2NodeDraftAutosave<ImageEditorDraft>({
    serverValue: serverDraft,
    serverEditVersion: Math.max(detail.workflow_edit_version, workflowEditVersion),
    disabled: busy,
    normalize: normalizeImageDraft,
    validate: (draft) => validateImageDraft(draft, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft, expectedEditVersion) => onSave({
      node_type: "image_generation",
      expected_edit_version: expectedEditVersion,
      title: draft.title,
      variation_instruction: nullableText(draft.variation),
      generation_spec: parseWorkflowGenerationSpec(draft.generation)!,
      delivery_spec: draft.delivery ? parseWorkflowDeliverySpec(draft.delivery) : null,
    }),
    onStateChange: onSaveStateChange,
  });
  const { generation, delivery } = editor.draft;
  const image = workflowNodeDownloadableImage(node);
  const patchGeneration = (patch: Partial<WorkflowGenerationSpec>) => editor.update({
    ...editor.draft,
    generation: { ...generation, ...patch },
  });
  const patchDelivery = (patch: Partial<WorkflowDeliverySpec>) => {
    if (delivery) {
      editor.update({ ...editor.draft, delivery: { ...delivery, ...patch } });
    }
  };

  return (
    <AutosaveEditorForm editor={editor} busy={busy} onFlushRegistration={onFlushRegistration}>
      {image ? <NodeImagePreview image={image} onPreview={onPreviewImage} /> : null}
      <TextInput
        label={t("agentWorkbench.nodeEditor.nodeName")}
        value={editor.draft.title}
        maxLength={255}
        onChange={(title) => editor.update({ ...editor.draft, title })}
      />
      <TextArea
        label={t("workflowConfirmation.variation")}
        value={editor.draft.variation}
        onChange={(variation) => editor.update({ ...editor.draft, variation })}
        minRows={3}
        maxRows={12}
      />
      <FieldGroup title={t("agentWorkbench.nodeEditor.generationSettings")}>
        <ImageGenerationSettingsTabs
          value={settingsTab}
          onChange={setSettingsTab}
          basic={(
            <div className="space-y-4">
              <div>
                <div className="mb-2 text-[11px] font-semibold text-slate-600 dark:text-slate-300">
                  {t("agentWorkbench.nodeEditor.aspectRatio")}
                </div>
                <ImageAspectRatioPicker
                  value={generation.aspect_ratio}
                  onChange={(aspect_ratio) => patchGeneration({ aspect_ratio })}
                />
              </div>
              <div className="grid grid-cols-2 gap-2">
                <CompactSelect
                  label={t("agentWorkbench.nodeEditor.resolution")}
                  value={generation.resolution_tier}
                  options={selectOptions(["standard", "high", "ultra"], t)}
                  onChange={(resolution_tier) => patchGeneration({ resolution_tier: resolution_tier as WorkflowGenerationSpec["resolution_tier"] })}
                />
                <CompactSelect
                  label={t("agentWorkbench.nodeEditor.quality")}
                  value={generation.quality_intent}
                  options={selectOptions(["draft", "standard", "high"], t)}
                  onChange={(quality_intent) => patchGeneration({ quality_intent: quality_intent as WorkflowGenerationSpec["quality_intent"] })}
                />
              </div>
            </div>
          )}
          advanced={(
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-2">
                <CompactSelect
                  label={t("agentWorkbench.nodeEditor.referenceFidelity")}
                  value={generation.reference_fidelity}
                  options={selectOptions(["low", "medium", "high"], t)}
                  onChange={(reference_fidelity) => patchGeneration({ reference_fidelity: reference_fidelity as WorkflowGenerationSpec["reference_fidelity"] })}
                />
                <CompactSelect
                  label={t("agentWorkbench.nodeEditor.backgroundIntent")}
                  value={generation.background_intent}
                  options={selectOptions(["auto", "opaque", "transparent"], t)}
                  onChange={(background_intent) => patchGeneration({ background_intent: background_intent as WorkflowGenerationSpec["background_intent"] })}
                />
              </div>
              <CompactSelect
                label={t("agentWorkbench.nodeEditor.textPolicy")}
                value={generation.text_policy}
                options={selectOptions(["none", "allow", "required"], t)}
                onChange={(textPolicy) => patchGeneration({
                  text_policy: textPolicy as WorkflowGenerationSpec["text_policy"],
                  text_language: textPolicy === "none" ? null : generation.text_language,
                })}
              />
              {generation.text_policy !== "none" ? (
                <CompactInput
                  label={t("agentWorkbench.nodeEditor.textLanguage")}
                  value={generation.text_language ?? ""}
                  maxLength={80}
                  onChange={(text_language) => patchGeneration({ text_language: text_language || null })}
                />
              ) : null}

              <fieldset className="space-y-3 border-t border-zinc-200 pt-4 dark:border-slate-700">
                <legend className="mb-1 text-xs font-semibold text-zinc-800 dark:text-slate-100">
                  {t("workflowConfirmation.deliverySpec")}
                </legend>
                <CheckboxField
                  label={t("agentWorkbench.nodeEditor.deliveryEnabled")}
                  checked={delivery !== null}
                  onChange={(enabled) => editor.update({
                    ...editor.draft,
                    delivery: enabled ? defaultDeliverySpec() : null,
                  })}
                />
                {delivery ? (
                  <>
                    <div className="grid grid-cols-2 gap-2">
                      <CompactNumberInput label={t("agentWorkbench.nodeEditor.width")} value={delivery.width} min={1} max={16384} onChange={(width) => width !== null && patchDelivery({ width })} />
                      <CompactNumberInput label={t("agentWorkbench.nodeEditor.height")} value={delivery.height} min={1} max={16384} onChange={(height) => height !== null && patchDelivery({ height })} />
                      <CompactSelect label={t("agentWorkbench.nodeEditor.format")} value={delivery.format} options={selectOptions(["png", "jpeg", "webp"], t)} onChange={(format) => patchDelivery({ format: format as WorkflowDeliverySpec["format"] })} />
                      <CompactSelect
                        label={t("agentWorkbench.nodeEditor.fit")}
                        value={delivery.fit}
                        options={selectOptions(["contain", "cover"], t)}
                        onChange={(fitValue) => {
                          const fit = fitValue as WorkflowDeliverySpec["fit"];
                          patchDelivery({
                            fit,
                            crop_anchor: fit === "contain" ? null : delivery.crop_anchor,
                            background_color: fit === "cover" ? null : delivery.background_color,
                          });
                        }}
                      />
                    </div>
                    <CompactNumberInput label={t("agentWorkbench.nodeEditor.maxBytes")} value={delivery.max_byte_size ?? null} min={1} optional onChange={(max_byte_size) => patchDelivery({ max_byte_size })} />
                    {delivery.fit === "contain" ? (
                      <CompactInput label={t("agentWorkbench.nodeEditor.backgroundColor")} value={delivery.background_color ?? ""} maxLength={7} placeholder="#FFFFFF" onChange={(background_color) => patchDelivery({ background_color: nullableText(background_color) })} />
                    ) : (
                      <CompactSelect label={t("agentWorkbench.nodeEditor.cropAnchor")} value={delivery.crop_anchor ?? "center"} options={selectOptions(["center", "top", "bottom", "left", "right"], t)} onChange={(crop_anchor) => patchDelivery({ crop_anchor: crop_anchor as NonNullable<WorkflowDeliverySpec["crop_anchor"]> })} />
                    )}
                  </>
                ) : null}
              </fieldset>
            </div>
          )}
        />
      </FieldGroup>
    </AutosaveEditorForm>
  );
}

function AutosaveEditorForm<T>({
  children,
  busy,
  editor,
  onFlushRegistration,
}: {
  children: React.ReactNode;
  busy: boolean;
  editor: V2NodeDraftAutosave<T>;
  onFlushRegistration: (flush: V2NodeInspectorFlush | null) => void;
}) {
  const { t } = useI18n();
  useEffect(() => {
    onFlushRegistration(editor.flush);
    return () => onFlushRegistration(null);
  }, [editor.flush, onFlushRegistration]);
  return (
    <form
      className="config-bubble space-y-4 rounded-2xl p-4 shadow-sm"
      onSubmit={(event) => {
        event.preventDefault();
        void editor.flush(true).catch(() => undefined);
      }}
    >
      {children}
      {editor.dirty || editor.status === "failed" ? (
        <div className="grid grid-cols-2 gap-2 border-t border-zinc-200 pt-4 dark:border-slate-700">
          <button
            type="button"
            onClick={editor.discard}
            disabled={busy}
            className="btn-secondary-spring inline-flex h-10 items-center justify-center rounded-xl px-3 text-xs font-semibold"
          >
            <Undo2 size={14} className="mr-1.5" />
            {t("settings.discard")}
          </button>
          <button type="submit" disabled={busy} className="btn-primary-spring inline-flex h-10 items-center justify-center rounded-xl px-3 text-xs font-semibold">
            {busy || editor.status === "saving" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Save size={14} className="mr-1.5" />}
            {t("detail.save")}
          </button>
        </div>
      ) : null}
    </form>
  );
}

function PromptEditorForm({
  children,
  busy,
  dirty,
  onSubmit,
  onDiscard,
}: {
  children: React.ReactNode;
  busy: boolean;
  dirty: boolean;
  onSubmit: () => Promise<unknown>;
  onDiscard: () => void;
}) {
  const { t } = useI18n();
  return (
    <form
      className="config-bubble space-y-4 rounded-2xl p-4 shadow-sm"
      onSubmit={(event) => {
        event.preventDefault();
        void onSubmit().catch(() => undefined);
      }}
    >
      {children}
      <div className="grid grid-cols-2 gap-2 border-t border-zinc-200 pt-4 dark:border-slate-700">
        <button
          type="button"
          onClick={onDiscard}
          disabled={busy || !dirty}
          className="btn-secondary-spring inline-flex h-10 items-center justify-center rounded-xl px-3 text-xs font-semibold disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Undo2 size={14} className="mr-1.5" />
          {t("settings.discard")}
        </button>
        <button
          type="submit"
          disabled={busy || !dirty}
          className="btn-primary-spring inline-flex h-10 items-center justify-center rounded-xl px-3 text-xs font-semibold disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Save size={14} className="mr-1.5" />}
          {t("agentWorkbench.nodeEditor.savePromptVersion")}
        </button>
      </div>
    </form>
  );
}

function NodeImagePreview({ image, onPreview }: { image: DownloadableImage; onPreview: (image: DownloadableImage) => void }) {
  const { t } = useI18n();
  return (
    <div className="relative overflow-hidden rounded-xl border border-zinc-200 dark:border-slate-700">
      <button
        type="button"
        onClick={() => onPreview(image)}
        className={`block aspect-[4/3] w-full ${IMAGE_PREVIEW_SURFACE_CLASS_NAME}`}
        aria-label={t("detail.previewImage", { alt: image.alt })}
      >
        <img src={image.previewUrl} alt={image.alt} className="h-full w-full object-contain" />
      </button>
      <DownloadLink image={image} variant="overlay" />
    </div>
  );
}

function FieldGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset className="space-y-3 border-t border-zinc-200 pt-4 dark:border-slate-700">
      <legend className="mb-1 text-xs font-semibold text-zinc-800 dark:text-slate-100">{title}</legend>
      {children}
    </fieldset>
  );
}

function SectionHeading({ title }: { title: string }) {
  return <div className="border-b border-zinc-200 px-4 py-3 text-xs font-semibold text-zinc-800 dark:border-slate-700 dark:text-slate-100">{title}</div>;
}

function SectionTitle({ title }: { title: string }) {
  return <h4 className="text-xs font-semibold text-zinc-800 dark:text-slate-100">{title}</h4>;
}

function TextInput({
  label,
  value,
  maxLength,
  onChange,
}: {
  label: string;
  value: string;
  maxLength?: number;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{label}</span>
      <input value={value} maxLength={maxLength} onChange={(event) => onChange(event.target.value)} className="input-premium h-10 w-full px-3 text-xs outline-none" />
    </label>
  );
}

function NumberInput({ label, value, min, max, onChange }: { label: string; value: number; min?: number; max?: number; onChange: (value: number) => void }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{label}</span>
      <input type="number" value={value} min={min} max={max} onChange={(event) => onChange(Number(event.target.value))} className="input-premium h-10 w-full px-3 text-xs outline-none" />
    </label>
  );
}

function SelectInput({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange: (value: string) => void }) {
  const { t } = useI18n();
  return (
    <div className="block">
      <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{label}</span>
      <SelectField
        value={value}
        options={options.map((option) => ({
          value: option,
          label: selectOptionLabel(option, t),
        }))}
        onChange={onChange}
        ariaLabel={label}
        visualSize="md"
      />
    </div>
  );
}

function selectOptionLabel(option: string, t: ReturnType<typeof useI18n>["t"]): string {
  if (option === "png" || option === "jpeg" || option === "webp") {
    return option.toUpperCase();
  }
  const key = SELECT_OPTION_LABEL_KEYS[option as keyof typeof SELECT_OPTION_LABEL_KEYS];
  return key ? t(key) : option;
}

function selectOptions(options: readonly string[], t: ReturnType<typeof useI18n>["t"]) {
  return options.map((option) => ({ value: option, label: selectOptionLabel(option, t) }));
}

function CheckboxField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="flex min-h-10 items-center gap-2 rounded-xl border border-zinc-200 bg-zinc-50 px-3 text-xs font-medium text-zinc-700 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-200">
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} className="h-4 w-4 accent-indigo-600" />
      <span>{label}</span>
    </label>
  );
}

function LineListField({ label, value, onChange }: { label: string; value: string[]; onChange: (value: string[]) => void }) {
  return <TextArea label={label} value={value.join("\n")} onChange={(next) => onChange(splitLines(next))} minRows={2} />;
}

function ReadOnlyRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[112px_minmax(0,1fr)] gap-3 border-b border-zinc-100 py-2 last:border-0 dark:border-slate-800">
      <dt className="text-zinc-500 dark:text-slate-400">{label}</dt>
      <dd className={`min-w-0 break-all text-zinc-800 dark:text-slate-100 ${mono ? "font-mono text-[10px]" : "font-medium"}`}>{value}</dd>
    </div>
  );
}

function PanelState({ icon, text, action, onAction }: { icon?: React.ReactNode; text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex min-h-[260px] flex-col items-center justify-center gap-2 px-6 text-center text-xs text-zinc-500 dark:text-slate-400">
      {icon ? <span className="text-zinc-400 dark:text-slate-500">{icon}</span> : null}
      <span className="max-w-[260px] leading-5">{text}</span>
      {action && onAction ? <button type="button" onClick={onAction} className="mt-1 font-semibold text-indigo-700 hover:underline dark:text-violet-300">{action}</button> : null}
    </div>
  );
}

function NodeTypeIcon({ type }: { type: WorkflowNodeTypeV2 }) {
  if (type === "product_context") return <Package size={16} />;
  if (type === "reference_image") return <ImagePlus size={16} />;
  if (type === "prompt_generation") return <Braces size={16} />;
  return <ImageIcon size={16} />;
}

function nodeTypeLabel(type: WorkflowNodeTypeV2, t: ReturnType<typeof useI18n>["t"]): string {
  const keys = {
    product_context: "workflowV2.node.productContext",
    reference_image: "workflowV2.node.referenceImage",
    prompt_generation: "workflowV2.node.promptGeneration",
    image_generation: "workflowV2.node.imageGeneration",
  } as const;
  return t(keys[type]);
}

function factSourceLabel(source: WorkflowDraftProductFact["source_type"], t: ReturnType<typeof useI18n>["t"]): string {
  const keys = {
    user: "workflowConfirmation.factSource.user",
    image_observation: "workflowConfirmation.factSource.imageObservation",
    agent_inference: "workflowConfirmation.factSource.agentInference",
    legacy_product: "workflowConfirmation.factSource.legacyProduct",
  } as const;
  return t(keys[source]);
}

function sameJson(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function nullableText(value: string): string | null {
  const normalized = value.trim();
  return normalized || null;
}

function splitLines(value: string): string[] {
  return value.split("\n").map((item) => item.trim()).filter(Boolean);
}

function formatPromptArtifactPreview(
  payload: WorkflowImagePromptPayloadV1,
  t: ReturnType<typeof useI18n>["t"],
): string {
  const lines: string[] = [];
  const section = (title: string, values: string[]) => {
    const visible = values.map((value) => value.trim()).filter(Boolean);
    if (!visible.length) return;
    if (lines.length) lines.push("");
    lines.push(`## ${title}`, ...visible);
  };
  const bullets = (values: string[]) => values.map((value) => `- ${value}`);

  section(t("workflowConfirmation.designGoal"), [payload.design_goal]);
  section(t("workflowConfirmation.sharedRules"), bullets(payload.shared_rules));
  section(t("workflowConfirmation.productFidelity"), [
    ...bullets(payload.product_fidelity.requirements),
    `${t("agentWorkbench.nodeEditor.productPresent")}: ${payload.product_fidelity.product_present ? t("agentWorkbench.nodeEditor.factTrue") : t("agentWorkbench.nodeEditor.factFalse")}`,
  ]);
  section(t("workflowConfirmation.creativeBoundary"), bullets(payload.creative_boundary));
  section(t("workflowConfirmation.composition"), [
    `${t("agentWorkbench.nodeEditor.viewpoint")}: ${payload.composition.viewpoint}`,
    `${t("agentWorkbench.nodeEditor.productShare")}: ${payload.composition.product_share_percent}%`,
    payload.composition.layout,
    ...bullets(payload.composition.copy_regions),
  ]);
  section(t("workflowConfirmation.content"), [
    ...bullets(payload.content.focus),
    ...bullets(payload.content.selling_points),
    payload.content.background,
    ...bullets(payload.content.decorations),
  ]);
  section(t("workflowConfirmation.textContent"), [
    payload.text.headline ? `${t("agentWorkbench.nodeEditor.headline")}: ${payload.text.headline}` : "",
    payload.text.subtitle ? `${t("agentWorkbench.nodeEditor.subtitle")}: ${payload.text.subtitle}` : "",
    payload.text.body ? `${t("agentWorkbench.nodeEditor.body")}: ${payload.text.body}` : "",
  ]);
  section(t("workflowConfirmation.atmosphere"), [
    ...bullets(payload.atmosphere.keywords),
    payload.atmosphere.lighting,
  ]);
  payload.images.forEach((image, index) => {
    section(t("agentWorkbench.nodeEditor.perImageNumber", { number: index + 1 }), [
      image.instruction,
      image.viewpoint ? `${t("agentWorkbench.nodeEditor.viewpoint")}: ${image.viewpoint}` : "",
      ...bullets(image.composition_adjustments),
      image.lighting ? `${t("agentWorkbench.nodeEditor.lighting")}: ${image.lighting}` : "",
    ]);
  });
  return lines.join("\n");
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.detail;
  if (error instanceof Error) return error.message;
  return fallback;
}
