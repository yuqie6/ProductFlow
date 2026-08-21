import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Link2,
  Loader2,
  Plus,
  Play,
  Save,
  Search,
  Trash2,
  Undo2,
  XCircle,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { CompactInput, CompactNumberInput, CompactSelect } from "../../../components/CompactFormFields";
import { ImageAspectRatioPicker } from "../../../components/ImageAspectRatioPicker";
import { ImageGenerationSettingsTabs, type ImageGenerationSettingsTab } from "../../../components/ImageGenerationSettingsTabs";
import { api, ApiError } from "../../../lib/api";
import type { DownloadableImage } from "../../../lib/image-downloads";
import { sanitizeFilenamePart } from "../../../lib/image-downloads";
import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type {
  CanonicalProductDetail,
  GraphConfigStatus,
  GraphEdgeRole,
  GraphProductFactSet,
  GraphNode,
  GraphNodeType,
  GraphProjection,
  ProductFactsResponse,
  WorkflowDeliverySpec,
  WorkflowGenerationSpec,
  WorkflowNodeStatus,
} from "../../../lib/types";
import { IMAGE_PREVIEW_SURFACE_CLASS_NAME } from "../chrome/constants";
import { DownloadLink } from "../chrome/ImageDownloadComponents";
import { SaveStatusBadge, type SaveStatus } from "../chrome/SaveStatusBadge";
import { TextArea } from "../chrome/TextArea";
import { statusClass } from "../chrome/utils";
import { workflowNodeKindTheme } from "../chrome/WorkflowNodeCard";
import { DeliveryRenditionPanel } from "./DeliveryRenditionPanel";
import { graphNodeTitleKey } from "./graphLayout";
import {
  defaultDeliverySpec,
  graphBriefConfig,
  graphBriefDraft,
  graphImageAssetConfig,
  graphImageAssetDraft,
  graphImageGenerationConfig,
  graphImageGenerationDraft,
  graphPromptConfig,
  graphPromptDraft,
  graphProductSourceConfig,
  graphProductSourceDraft,
  normalizeProductFactsDraft,
  productFactsDraft,
  productFactsPayload,
  validateProductFactsDraft,
  graphTitleDraft,
  graphVisualConfig,
  graphVisualDraft,
  normalizeGraphImageGenerationDraft,
  validateGraphImageGenerationDraft,
  validateGraphTitle,
  type GraphBriefDraft,
  type GraphImageAssetDraft,
  type GraphImageGenerationDraft,
  type GraphPromptDraft,
  type GraphProductSourceDraft,
  type ProductFactsDraft,
  type ProductFactRowDraft,
  type GraphTitleDraft,
  type GraphVisualDraft,
} from "./graphNodeEditorDrafts";
import { useNodeDraftAutosave, type NodeDraftAutosave } from "./useNodeDraftAutosave";

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

export function GraphNodeInspector({
  graph,
  node,
  busy,
  onCommit,
  onBind,
  onJump,
  onPreviewImage,
}: {
  graph: GraphProjection;
  node: GraphNode | null;
  product?: CanonicalProductDetail | null;
  busy: boolean;
  onCommit: (input: {
    title: string;
    config: Record<string, unknown>;
    boundAssetId: string | null;
  }) => Promise<GraphProjection | void> | GraphProjection | void;
  onBind?: () => void;
  onJump?: (nodeId: string) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [saveState, setSaveState] = useState<{ status: SaveStatus; error: string | null }>({
    status: "idle",
    error: null,
  });
  const runsQueryKey = ["graph-runs", graph.product_id, graph.id] as const;
  const runsQuery = useQuery({
    queryKey: runsQueryKey,
    queryFn: () => api.listGraphRuns(graph.product_id, graph.id),
    enabled: Boolean(node && (node.node_type === "prompt_generation" || node.node_type === "image_generation")),
    refetchInterval: (query) => query.state.data?.items.some((run) => run.status === "running") ? 1200 : false,
  });
  const activeRun = node
    ? runsQuery.data?.items.find((run) => run.status === "running" && run.node_runs.some((item) => (
      item.node_id === node.id && ACTIVE_RUN_STATUSES.has(item.status)
    ))) ?? null
    : null;
  const nodeStatus: WorkflowNodeStatus = activeRun
    ? (activeRun.node_runs.find((item) => item.node_id === node?.id)?.status ?? "running")
    : "idle";
  const runMutation = useMutation({
    mutationFn: (input: { scope: "node" | "to_node"; node_id: string }) => api.submitGraphRun(graph.product_id, graph.id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: runsQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workflow-graph", graph.product_id] });
    },
  });
  const cancelMutation = useMutation({
    mutationFn: (runId: string) => api.cancelGraphRun(graph.product_id, graph.id, runId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: runsQueryKey }),
  });

  useEffect(() => {
    setSaveState({ status: "idle", error: null });
  }, [node?.id]);

  const persist = useCallback(async (input: {
    title: string;
    config: Record<string, unknown>;
    boundAssetId: string | null;
  }) => {
    const next = await onCommit(input);
    return { edit_version: next?.revision ?? graph.revision };
  }, [graph.revision, onCommit]);

  if (!node) {
    return <GraphInspectorDashboard graph={graph} />;
  }

  const theme = workflowNodeKindTheme(node.node_type);
  const Icon = theme.icon;
  const image = nodePreview(node);
  const missingPrompt = node.node_type === "image_generation"
    && !node.incoming.some((edge) => edge.role === "prompt");
  const canRun = node.node_type === "prompt_generation" || node.node_type === "image_generation";
  const mutationError = runMutation.error ?? cancelMutation.error;
  const incoming = node.incoming.map((edge) => ({
    edge,
    related: graph.nodes.find((item) => item.id === edge.node_id) ?? null,
  }));
  const outgoing = node.outgoing.map((edge) => ({
    edge,
    related: graph.nodes.find((item) => item.id === edge.node_id) ?? null,
  }));

  return (
    <div className="space-y-3 pb-4" data-graph-node-inspector>
      <section className="config-bubble rounded-2xl p-4 shadow-sm">
        <div className="flex items-start gap-3">
          <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border shadow-sm ${theme.iconBox}`}>
            <Icon size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <h3 className="truncate text-base font-semibold text-zinc-950 dark:text-white">{node.title}</h3>
            <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${theme.badge}`}>
                {t(graphNodeTitleKey(node.node_type))}
              </span>
              <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(nodeStatus)}`}>
                {ACTIVE_RUN_STATUSES.has(nodeStatus) ? <Loader2 size={10} className="mr-1 animate-spin" /> : null}
                {t(`detail.nodeStatus.${nodeStatus}`)}
              </span>
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${
                node.config_status === "ready"
                  ? "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300"
                  : "border-slate-200 bg-slate-100 text-slate-700 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200"
              }`}>
                {t(configStatusKey(node.config_status))}
              </span>
              <SaveStatusBadge status={saveState.status} />
            </div>
          </div>
        </div>

        {node.unused && node.bound_asset_id ? (
          <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
            {t("graph.inspector.unused")}
          </div>
        ) : null}
        {node.node_type === "image_asset" && !node.bound_asset_id ? (
          <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
            {t("graph.inspector.unbound")}
          </div>
        ) : null}
        {missingPrompt ? (
          <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
            {t("graph.inspector.missingPrompt")}
          </div>
        ) : null}
        {activeRun ? (
          <div className="mt-3 flex items-start gap-2 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
            <Loader2 size={14} className="mt-0.5 shrink-0 animate-spin" />
            <div className="font-semibold">{t(`detail.nodeStatus.${nodeStatus}`)}</div>
          </div>
        ) : null}

        {canRun || activeRun ? (
          <div className="mt-4 space-y-2">
            {canRun ? (
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => runMutation.mutate({ scope: "node", node_id: node.id })}
                  disabled={Boolean(activeRun) || runMutation.isPending || busy || missingPrompt}
                  className="inline-flex h-10 items-center justify-center rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
                >
                  {runMutation.isPending && runMutation.variables?.scope === "node" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Play size={14} className="mr-1.5" />}
                  {t("graph.runs.scope.node")}
                </button>
                <button
                  type="button"
                  onClick={() => runMutation.mutate({ scope: "to_node", node_id: node.id })}
                  disabled={Boolean(activeRun) || runMutation.isPending || busy || missingPrompt}
                  className="inline-flex h-10 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  {runMutation.isPending && runMutation.variables?.scope === "to_node" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Play size={14} className="mr-1.5" />}
                  {t("graph.runs.scope.toNode")}
                </button>
              </div>
            ) : null}
            {activeRun ? (
              <button
                type="button"
                onClick={() => cancelMutation.mutate(activeRun.id)}
                disabled={cancelMutation.isPending}
                className="inline-flex h-10 w-full items-center justify-center rounded-xl border border-red-200 bg-white px-3 text-xs font-semibold text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-900/50 dark:bg-transparent dark:text-red-300 dark:hover:bg-red-950/30"
              >
                {cancelMutation.isPending ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <XCircle size={14} className="mr-1.5" />}
                {t("detail.cancel")}
              </button>
            ) : null}
          </div>
        ) : null}
      </section>

      {saveState.error || mutationError ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {saveState.error ?? t("workflowV2.error.structure")}
        </div>
      ) : null}

      {node.node_type === "product_source" ? (
        <ProductSourceEditor
          key={node.id}
          node={node}
          graphProductId={graph.product_id}
          busy={busy}
          graphRevision={graph.revision}
          onSave={persist}
          onSaveStateChange={(status, error) => setSaveState({ status, error })}
        />
      ) : node.node_type === "image_asset" ? (
        <ImageAssetEditor
          key={node.id}
          node={node}
          image={image}
          busy={busy}
          graphRevision={graph.revision}
          onBind={onBind}
          onSave={persist}
          onSaveStateChange={(status, error) => setSaveState({ status, error })}
          onPreviewImage={onPreviewImage}
        />
      ) : node.node_type === "creative_brief" ? (
        <BriefEditor
          key={node.id}
          node={node}
          busy={busy}
          graphRevision={graph.revision}
          onSave={persist}
          onSaveStateChange={(status, error) => setSaveState({ status, error })}
        />
      ) : node.node_type === "visual_system" ? (
        <VisualEditor
          key={node.id}
          node={node}
          busy={busy}
          graphRevision={graph.revision}
          onSave={persist}
          onSaveStateChange={(status, error) => setSaveState({ status, error })}
        />
      ) : node.node_type === "prompt_generation" ? (
        <PromptEditor
          key={node.id}
          node={node}
          busy={busy}
          graphRevision={graph.revision}
          onSave={persist}
          onSaveStateChange={(status, error) => setSaveState({ status, error })}
        />
      ) : (
        <ImageGenerationEditor
          key={node.id}
          node={node}
          image={image}
          busy={busy}
          graphRevision={graph.revision}
          onSave={persist}
          onSaveStateChange={(status, error) => setSaveState({ status, error })}
          onPreviewImage={onPreviewImage}
        />
      )}

      {node.node_type === "image_generation" && onPreviewImage ? (
        <DeliveryRenditionPanel
          productId={graph.product_id}
          sourceAssetId={node.preview_asset_id}
          deliverySpec={node.config.delivery_spec}
          onPreviewImage={onPreviewImage}
        />
      ) : null}

      <EdgeList heading={t("graph.inspector.inputs")} empty={t("graph.inspector.inputsEmpty")} items={incoming} onJump={onJump} />
      <EdgeList heading={t("graph.inspector.outputs")} empty={t("graph.inspector.outputsEmpty")} items={outgoing} onJump={onJump} />
    </div>
  );
}

function GraphInspectorDashboard({ graph }: { graph: GraphProjection }) {
  const { t } = useI18n();
  const counts = useMemo(() => ({
    product_source: graph.nodes.filter((node) => node.node_type === "product_source").length,
    image_asset: graph.nodes.filter((node) => node.node_type === "image_asset").length,
    creative_brief: graph.nodes.filter((node) => node.node_type === "creative_brief").length,
    visual_system: graph.nodes.filter((node) => node.node_type === "visual_system").length,
    prompt_generation: graph.nodes.filter((node) => node.node_type === "prompt_generation").length,
    image_generation: graph.nodes.filter((node) => node.node_type === "image_generation").length,
  }), [graph.nodes]);
  return (
    <div className="space-y-3 p-3.5 pb-6" data-graph-node-inspector>
      <section className="config-bubble rounded-2xl p-4 shadow-sm">
        <h3 className="text-sm font-semibold text-zinc-950 dark:text-white">{graph.title}</h3>
        <p className="mt-1 text-[11px] text-zinc-500 dark:text-slate-400">
          {t("graph.inspector.graphRevision", { revision: graph.revision })}
        </p>
        <p className="mt-3 text-xs leading-5 text-zinc-600 dark:text-slate-300">{t("graph.inspector.selectHint")}</p>
        <dl className="mt-4 grid grid-cols-2 gap-2 text-xs">
          {(Object.keys(counts) as GraphNodeType[]).map((type) => (
            <div key={type} className="rounded-xl border border-zinc-200 bg-zinc-50 px-3 py-2 dark:border-slate-700 dark:bg-[#0b1220]">
              <dt className="text-[10px] text-zinc-500 dark:text-slate-400">{t(graphNodeTitleKey(type))}</dt>
              <dd className="mt-0.5 text-sm font-semibold text-zinc-900 dark:text-slate-100">{counts[type]}</dd>
            </div>
          ))}
        </dl>
      </section>
    </div>
  );
}

function ProductSourceEditor({
  node,
  graphProductId,
  busy,
  graphRevision,
  onSave,
  onSaveStateChange,
}: {
  node: GraphNode;
  graphProductId: string;
  busy: boolean;
  graphRevision: number;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const sourceDraft = graphProductSourceDraft(node);
  const sourceProductId = sourceDraft.source_product_id;
  const [search, setSearch] = useState("");
  const [factsForm, setFactsForm] = useState<ProductFactsDraft | null>(null);
  const [factsSaveState, setFactsSaveState] = useState<{ status: SaveStatus; error: string | null }>({
    status: "idle",
    error: null,
  });
  const productsQuery = useQuery({
    queryKey: ["product-source-picker", search],
    queryFn: () => api.listProducts({ q: search, page: 1, page_size: 20 }),
    staleTime: 10_000,
  });
  const factsQuery = useQuery<ProductFactsResponse>({
    queryKey: ["product-facts", sourceProductId],
    queryFn: () => api.getProductFacts(sourceProductId as string),
    enabled: Boolean(sourceProductId),
  });
  const editor = useNodeDraftAutosave<GraphTitleDraft>({
    serverValue: graphTitleDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => ({ title: draft.title.trim() }),
    validate: (draft) => validateGraphTitle(draft.title, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({ title: draft.title, config: node.config, boundAssetId: node.bound_asset_id }),
    onStateChange: onSaveStateChange,
  });

  useEffect(() => {
    setSearch("");
    setFactsForm(null);
    setFactsSaveState({ status: "idle", error: null });
  }, [sourceProductId]);

  useEffect(() => {
    if (!sourceProductId) {
      setFactsForm(null);
      return;
    }
    if (factsQuery.data) {
      setFactsForm(productFactsDraft(factsQuery.data.product, factsQuery.data.fact_set));
    }
  }, [factsQuery.data, sourceProductId]);

  const saveBinding = useCallback(async (nextProductId: string | null, productName?: string) => {
    const nextTitle = node.title === t("graph.node.productSource") && productName ? productName : node.title;
    const nextDraft: GraphProductSourceDraft = {
      title: nextTitle,
      source_product_id: nextProductId,
      fact_set_version_id: nextProductId === sourceProductId ? sourceDraft.fact_set_version_id : null,
    };
    try {
      await onSave({
        title: nextTitle,
        config: graphProductSourceConfig(node, nextDraft),
        boundAssetId: node.bound_asset_id,
      });
      onSaveStateChange("saved", null);
    } catch (error) {
      onSaveStateChange("failed", errorMessage(error, t("workflowV2.error.structure")));
    }
  }, [node, onSave, onSaveStateChange, sourceDraft.fact_set_version_id, sourceProductId, t]);

  const saveFacts = async () => {
    if (!sourceProductId || !factsForm || factsQuery.isPending) return;
    const validationError = validateProductFactsDraft(factsForm);
    if (validationError) {
      setFactsSaveState({ status: "failed", error: productFactsValidationMessage(validationError, t) });
      return;
    }
    const normalized = normalizeProductFactsDraft(factsForm);
    setFactsSaveState({ status: "saving", error: null });
    try {
      const response = await api.updateProductFacts(sourceProductId, {
        expected_fact_version: factsQuery.data?.fact_set?.version ?? node.product_fact_set?.version ?? null,
        name: normalized.name,
        category: normalized.category || null,
        price: normalized.price || null,
        source_note: normalized.source_note || null,
        facts: productFactsPayload(normalized),
      });
      queryClient.setQueryData(["product-facts", sourceProductId], response);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["product-facts", sourceProductId] }),
        queryClient.invalidateQueries({ queryKey: ["product", sourceProductId] }),
        queryClient.invalidateQueries({ queryKey: ["products"] }),
        queryClient.invalidateQueries({ queryKey: ["workflow-graph", graphProductId] }),
      ]);
      setFactsForm(productFactsDraft(response.product, response.fact_set));
      setFactsSaveState({ status: "saved", error: null });
    } catch (error) {
      setFactsSaveState({
        status: "failed",
        error: errorMessage(
          error,
          error instanceof ApiError && error.status === 409
            ? t("graph.inspector.productFactsConflict")
            : t("graph.inspector.productFactsSaveFailed"),
        ),
      });
    }
  };

  const sourceProduct = factsQuery.data?.product ?? node.source_product ?? null;
  const factSet: GraphProductFactSet | null = factsQuery.data
    ? factsQuery.data.fact_set
    : node.product_fact_set ?? null;
  const pickerItems = productsQuery.data?.items ?? [];

  return (
    <AutosaveForm editor={editor} busy={busy}>
      <div className="flex items-center justify-between gap-3">
        <SectionTitle title={t("graph.inspector.productInfo")} />
        <SaveStatusBadge status={factsSaveState.status} />
      </div>
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} onChange={(title) => editor.update({ title })} />
      <p className="text-[11px] leading-5 text-zinc-500 dark:text-slate-400">{t("graph.inspector.productSourceHint")}</p>
      {!sourceProductId ? (
        <div className="rounded-xl border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-800 dark:border-amber-400/30 dark:bg-amber-500/10 dark:text-amber-100">
          {t("graph.inspector.productSourceUnbound")}
        </div>
      ) : null}

      <div className="space-y-2">
        <label className="block">
          <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{t("graph.inspector.productSourceSearch")}</span>
          <span className="relative block">
            <Search size={14} className="pointer-events-none absolute left-3 top-3 text-zinc-400" aria-hidden="true" />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("graph.inspector.productSourceSearchPlaceholder")}
              className="input-premium h-10 w-full pl-9 pr-3 text-xs outline-none"
            />
          </span>
        </label>
        <div className="max-h-44 space-y-1 overflow-y-auto rounded-xl border border-zinc-200 p-1 dark:border-slate-700">
          {productsQuery.isPending ? <p className="px-2 py-2 text-xs text-zinc-500 dark:text-slate-400">{t("app.loading")}</p> : null}
          {!productsQuery.isPending && pickerItems.length === 0 ? <p className="px-2 py-2 text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.productSourceNoProducts")}</p> : null}
          {pickerItems.map((item) => (
            <button
              key={item.id}
              type="button"
              disabled={busy || item.id === sourceProductId}
              onClick={() => void saveBinding(item.id, item.name)}
              className="flex w-full min-w-0 items-start justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-xs hover:bg-zinc-50 disabled:cursor-default disabled:opacity-60 dark:hover:bg-slate-800"
            >
              <span className="min-w-0">
                <span className="block truncate font-semibold text-zinc-900 dark:text-slate-100">{item.name}</span>
                <span className="mt-0.5 block truncate text-[10px] text-zinc-500 dark:text-slate-400">{item.category || t("agentWorkbench.nodeEditor.noValue")}</span>
              </span>
              {item.id === sourceProductId ? <span className="shrink-0 text-[10px] text-emerald-600">{t("graph.inspector.productSourceSelected")}</span> : null}
            </button>
          ))}
        </div>
        {sourceProductId ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => void saveBinding(null)}
            className="inline-flex h-9 w-full items-center justify-center rounded-xl border border-slate-200 px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            {t("graph.inspector.productSourceClear")}
          </button>
        ) : null}
      </div>

      {factsQuery.error ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {errorMessage(factsQuery.error, t("graph.inspector.productFactsLoadFailed"))}
        </div>
      ) : null}
      {sourceProductId && factsQuery.isPending ? <p className="text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.productFactsLoading")}</p> : null}
      {sourceProductId && factsForm ? (
        <div className="space-y-3 border-t border-zinc-200 pt-4 dark:border-slate-700">
          <div className="flex items-center justify-between gap-2">
            <SectionTitle title={t("graph.inspector.productFacts")} />
            {factSet ? <span className="text-[10px] text-zinc-500 dark:text-slate-400">{t("graph.inspector.productFactsVersion", { version: factSet.version })}</span> : null}
          </div>
          <TextInput label={t("detail.inspector.productName")} value={factsForm.name} maxLength={255} onChange={(name) => setFactsForm({ ...factsForm, name })} />
          <TextInput label={t("detail.inspector.category")} value={factsForm.category} maxLength={255} onChange={(category) => setFactsForm({ ...factsForm, category })} />
          <TextInput label={t("detail.inspector.price")} value={factsForm.price} maxLength={120} onChange={(price) => setFactsForm({ ...factsForm, price })} />
          <TextArea label={t("detail.inspector.productDescription")} value={factsForm.source_note} onChange={(source_note) => setFactsForm({ ...factsForm, source_note })} minRows={2} maxRows={8} />
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{t("graph.inspector.productFacts")}</span>
              <button
                type="button"
                onClick={() => setFactsForm({
                  ...factsForm,
                  facts: [...factsForm.facts, {
                    id: nextFactRowId(factsForm.facts),
                    key: "",
                    value: "",
                    original: { key: "", value: "" },
                  }],
                })}
                className="inline-flex h-8 items-center gap-1 rounded-lg border border-slate-200 px-2 text-[10px] font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              >
                <Plus size={13} aria-hidden="true" />
                {t("graph.inspector.productFactAdd")}
              </button>
            </div>
            {factsForm.facts.map((fact, index) => (
              <div key={fact.id} className="grid grid-cols-[minmax(0,0.85fr)_minmax(0,1fr)_32px] gap-1.5">
                <input
                  value={fact.key}
                  aria-label={`${t("graph.inspector.productFactKey")} ${index + 1}`}
                  onChange={(event) => updateFactRow(setFactsForm, factsForm, fact.id, { key: event.target.value })}
                  placeholder={t("graph.inspector.productFactKey")}
                  className="input-premium h-9 min-w-0 px-2 text-xs outline-none"
                />
                <input
                  value={fact.value}
                  aria-label={`${t("graph.inspector.productFactValue")} ${index + 1}`}
                  onChange={(event) => updateFactRow(setFactsForm, factsForm, fact.id, { value: event.target.value })}
                  placeholder={t("graph.inspector.productFactValue")}
                  className="input-premium h-9 min-w-0 px-2 text-xs outline-none"
                />
                <button
                  type="button"
                  aria-label={t("graph.inspector.productFactRemove")}
                  title={t("graph.inspector.productFactRemove")}
                  onClick={() => setFactsForm({ ...factsForm, facts: factsForm.facts.filter((item) => item.id !== fact.id) })}
                  className="inline-flex h-9 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:border-red-200 hover:text-red-600 dark:border-slate-700 dark:text-slate-400"
                >
                  <Trash2 size={14} aria-hidden="true" />
                </button>
              </div>
            ))}
          </div>
          {validateProductFactsDraft(factsForm) ? (
            <p role="alert" className="text-[11px] leading-5 text-red-600 dark:text-red-300">{productFactsValidationMessage(validateProductFactsDraft(factsForm), t)}</p>
          ) : null}
          {factsSaveState.error ? <p role="alert" className="text-[11px] leading-5 text-red-600 dark:text-red-300">{factsSaveState.error}</p> : null}
          <button
            type="button"
            onClick={() => void saveFacts()}
            disabled={busy || factsSaveState.status === "saving" || Boolean(validateProductFactsDraft(factsForm))}
            className="inline-flex h-10 w-full items-center justify-center gap-1.5 rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
          >
            {factsSaveState.status === "saving" ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
            {factsSaveState.status === "saving" ? t("graph.inspector.productFactsSaving") : t("graph.inspector.productFactsSave")}
          </button>
        </div>
      ) : null}
      {sourceProduct && !factsForm && !factsQuery.isPending ? (
        <ReadOnlyRow label={t("graph.inspector.productSourceSelected")} value={sourceProduct.name} />
      ) : null}
    </AutosaveForm>
  );
}

function ImageAssetEditor({
  node,
  image,
  busy,
  graphRevision,
  onBind,
  onSave,
  onSaveStateChange,
  onPreviewImage,
}: {
  node: GraphNode;
  image: DownloadableImage | null;
  busy: boolean;
  graphRevision: number;
  onBind?: () => void;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const editor = useNodeDraftAutosave<GraphImageAssetDraft>({
    serverValue: graphImageAssetDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => ({ title: draft.title.trim(), role: draft.role.trim(), label: draft.label.trim() }),
    validate: (draft) => validateGraphTitle(draft.title, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: graphImageAssetConfig(node, draft),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  return (
    <AutosaveForm editor={editor} busy={busy}>
      {image && onPreviewImage ? <NodeImagePreview image={image} onPreview={onPreviewImage} /> : null}
      {!image ? <p className="text-xs text-zinc-500 dark:text-slate-400">{t("graph.inspector.noPreview")}</p> : null}
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} onChange={(title) => editor.update({ ...editor.draft, title })} />
      <TextInput label={t("graph.inspector.assetRole")} value={editor.draft.role} maxLength={120} onChange={(role) => editor.update({ ...editor.draft, role })} />
      <TextInput label={t("graph.inspector.assetLabel")} value={editor.draft.label} maxLength={255} onChange={(label) => editor.update({ ...editor.draft, label })} />
      {onBind ? (
        <button
          type="button"
          onClick={() => void onBind()}
          disabled={busy}
          className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
        >
          <Link2 size={14} />
          {node.bound_asset_id ? t("graph.inspector.rebind") : t("graph.inspector.bind")}
        </button>
      ) : null}
      {node.bound_asset_id ? (
        <button
          type="button"
          disabled={busy}
          onClick={() => void onSave({
            title: editor.draft.title.trim() || node.title,
            config: graphImageAssetConfig(node, editor.draft),
            boundAssetId: null,
          })}
          className="inline-flex h-10 w-full items-center justify-center rounded-xl border border-slate-200 px-3 text-xs font-semibold text-slate-700 dark:border-slate-700 dark:text-slate-200"
        >
          {t("graph.inspector.unbind")}
        </button>
      ) : null}
    </AutosaveForm>
  );
}

function BriefEditor({
  node,
  busy,
  graphRevision,
  onSave,
  onSaveStateChange,
}: {
  node: GraphNode;
  busy: boolean;
  graphRevision: number;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const editor = useNodeDraftAutosave<GraphBriefDraft>({
    serverValue: graphBriefDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => ({
      ...draft,
      title: draft.title.trim(),
      goal: draft.goal.trim(),
    }),
    validate: (draft) => validateGraphTitle(draft.title, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: graphBriefConfig(node, draft),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  return (
    <AutosaveForm editor={editor} busy={busy}>
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} onChange={(title) => editor.update({ ...editor.draft, title })} />
      <TextArea label={t("workflowConfirmation.designGoal")} value={editor.draft.goal} onChange={(goal) => editor.update({ ...editor.draft, goal })} minRows={3} />
      <LineListField label={t("graph.inspector.designGoals")} value={editor.draft.design_goals} onChange={(design_goals) => editor.update({ ...editor.draft, design_goals })} />
      <LineListField label={t("graph.inspector.requiredCopy")} value={editor.draft.required_copy} onChange={(required_copy) => editor.update({ ...editor.draft, required_copy })} />
      <LineListField label={t("workflowConfirmation.creativeBoundary")} value={editor.draft.prohibitions} onChange={(prohibitions) => editor.update({ ...editor.draft, prohibitions })} />
    </AutosaveForm>
  );
}

function VisualEditor({
  node,
  busy,
  graphRevision,
  onSave,
  onSaveStateChange,
}: {
  node: GraphNode;
  busy: boolean;
  graphRevision: number;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const editor = useNodeDraftAutosave<GraphVisualDraft>({
    serverValue: graphVisualDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => ({
      title: draft.title.trim(),
      visual_system_version_id: draft.visual_system_version_id,
      style: draft.style,
      prohibitions: draft.prohibitions,
      background: draft.background.trim(),
    }),
    validate: (draft) => validateGraphTitle(draft.title, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: graphVisualConfig(node, draft),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  return (
    <AutosaveForm editor={editor} busy={busy}>
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} onChange={(title) => editor.update({ ...editor.draft, title })} />
      {editor.draft.visual_system_version_id ? (
        <ReadOnlyRow label={t("graph.inspector.visualVersion")} value={editor.draft.visual_system_version_id} mono />
      ) : null}
      <p className="text-[11px] leading-5 text-zinc-500 dark:text-slate-400">{t("graph.inspector.visualVersionHint")}</p>
      <LineListField label={t("graph.inspector.visualStyle")} value={editor.draft.style} onChange={(style) => editor.update({ ...editor.draft, style })} />
      <TextInput
        label={t("graph.inspector.visualBackground")}
        value={editor.draft.background}
        maxLength={32}
        onChange={(background) => editor.update({ ...editor.draft, background })}
      />
      <LineListField label={t("workflowConfirmation.creativeBoundary")} value={editor.draft.prohibitions} onChange={(prohibitions) => editor.update({ ...editor.draft, prohibitions })} />
    </AutosaveForm>
  );
}

function PromptEditor({
  node,
  busy,
  graphRevision,
  onSave,
  onSaveStateChange,
}: {
  node: GraphNode;
  busy: boolean;
  graphRevision: number;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
}) {
  const { t } = useI18n();
  const editor = useNodeDraftAutosave<GraphPromptDraft>({
    serverValue: graphPromptDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: (draft) => ({ ...draft, title: draft.title.trim() }),
    validate: (draft) => validateGraphTitle(draft.title, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: graphPromptConfig(node, draft),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  const imageType = typeof node.config.image_type_key === "string" ? node.config.image_type_key : "";
  return (
    <AutosaveForm editor={editor} busy={busy}>
      <SectionTitle title={t("graph.inspector.promptSection")} />
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} onChange={(title) => editor.update({ ...editor.draft, title })} />
      {imageType ? <ReadOnlyRow label={t("graph.inspector.imageType")} value={imageType} mono /> : null}
      <TextArea label={t("workflowConfirmation.designGoal")} value={editor.draft.design_goal} onChange={(design_goal) => editor.update({ ...editor.draft, design_goal })} minRows={3} />
      <LineListField label={t("workflowConfirmation.sharedRules")} value={editor.draft.shared_rules} onChange={(shared_rules) => editor.update({ ...editor.draft, shared_rules })} />
      <LineListField label={t("workflowConfirmation.creativeBoundary")} value={editor.draft.creative_boundary} onChange={(creative_boundary) => editor.update({ ...editor.draft, creative_boundary })} />

      <FieldGroup title={t("workflowConfirmation.productFidelity")}>
        <div className="grid gap-2 sm:grid-cols-2">
          <CheckboxField label={t("agentWorkbench.nodeEditor.complexStructure")} checked={editor.draft.complex_structure} onChange={(complex_structure) => editor.update({ ...editor.draft, complex_structure })} />
          <CheckboxField label={t("agentWorkbench.nodeEditor.productPresent")} checked={editor.draft.product_present} onChange={(product_present) => editor.update({ ...editor.draft, product_present })} />
        </div>
        <CompactSelect
          label={t("agentWorkbench.nodeEditor.pictureInPicture")}
          value={editor.draft.picture_in_picture}
          options={selectOptions(["none", "allowed", "required"], t)}
          onChange={(picture_in_picture) => editor.update({
            ...editor.draft,
            picture_in_picture: picture_in_picture as GraphPromptDraft["picture_in_picture"],
          })}
        />
        <LineListField label={t("agentWorkbench.nodeEditor.requirements")} value={editor.draft.requirements} onChange={(requirements) => editor.update({ ...editor.draft, requirements })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.composition")}>
        <TextInput label={t("agentWorkbench.nodeEditor.viewpoint")} value={editor.draft.viewpoint} onChange={(viewpoint) => editor.update({ ...editor.draft, viewpoint })} />
        <CompactNumberInput
          label={t("agentWorkbench.nodeEditor.productShare")}
          value={editor.draft.product_share_percent}
          min={1}
          max={100}
          onChange={(product_share_percent) => product_share_percent !== null && editor.update({ ...editor.draft, product_share_percent })}
        />
        <TextArea label={t("agentWorkbench.nodeEditor.layout")} value={editor.draft.layout} onChange={(layout) => editor.update({ ...editor.draft, layout })} />
        <LineListField label={t("agentWorkbench.nodeEditor.copyRegions")} value={editor.draft.copy_regions} onChange={(copy_regions) => editor.update({ ...editor.draft, copy_regions })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.content")}>
        <LineListField label={t("agentWorkbench.nodeEditor.focus")} value={editor.draft.focus} onChange={(focus) => editor.update({ ...editor.draft, focus })} />
        <LineListField label={t("agentWorkbench.nodeEditor.sellingPoints")} value={editor.draft.selling_points} onChange={(selling_points) => editor.update({ ...editor.draft, selling_points })} />
        <TextArea label={t("agentWorkbench.nodeEditor.background")} value={editor.draft.background} onChange={(background) => editor.update({ ...editor.draft, background })} />
        <LineListField label={t("agentWorkbench.nodeEditor.decorations")} value={editor.draft.decorations} onChange={(decorations) => editor.update({ ...editor.draft, decorations })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.textContent")}>
        <TextInput label={t("agentWorkbench.nodeEditor.headline")} value={editor.draft.headline} onChange={(headline) => editor.update({ ...editor.draft, headline })} />
        <TextInput label={t("agentWorkbench.nodeEditor.subtitle")} value={editor.draft.subtitle} onChange={(subtitle) => editor.update({ ...editor.draft, subtitle })} />
        <TextArea label={t("agentWorkbench.nodeEditor.body")} value={editor.draft.body} onChange={(body) => editor.update({ ...editor.draft, body })} />
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.atmosphere")}>
        <LineListField label={t("agentWorkbench.nodeEditor.keywords")} value={editor.draft.keywords} onChange={(keywords) => editor.update({ ...editor.draft, keywords })} />
        <TextArea label={t("agentWorkbench.nodeEditor.lighting")} value={editor.draft.lighting} onChange={(lighting) => editor.update({ ...editor.draft, lighting })} />
      </FieldGroup>
    </AutosaveForm>
  );
}

function ImageGenerationEditor({
  node,
  image,
  busy,
  graphRevision,
  onSave,
  onSaveStateChange,
  onPreviewImage,
}: {
  node: GraphNode;
  image: DownloadableImage | null;
  busy: boolean;
  graphRevision: number;
  onSave: (input: { title: string; config: Record<string, unknown>; boundAssetId: string | null }) => Promise<{ edit_version: number }>;
  onSaveStateChange: (status: SaveStatus, error: string | null) => void;
  onPreviewImage?: (image: DownloadableImage) => void;
}) {
  const { t } = useI18n();
  const [settingsTab, setSettingsTab] = useState<ImageGenerationSettingsTab>("basic");
  const editor = useNodeDraftAutosave<GraphImageGenerationDraft>({
    serverValue: graphImageGenerationDraft(node),
    serverEditVersion: graphRevision,
    disabled: busy,
    normalize: normalizeGraphImageGenerationDraft,
    validate: (draft) => validateGraphImageGenerationDraft(draft, t("agentWorkbench.nodeEditor.invalidDraft")),
    save: (draft) => onSave({
      title: draft.title,
      config: graphImageGenerationConfig(node, draft),
      boundAssetId: node.bound_asset_id,
    }),
    onStateChange: onSaveStateChange,
  });
  const { generation, delivery } = editor.draft;
  const patchGeneration = (patch: Partial<WorkflowGenerationSpec>) => editor.update({
    ...editor.draft,
    generation: { ...generation, ...patch },
  });
  const patchDelivery = (patch: Partial<WorkflowDeliverySpec>) => {
    if (delivery) editor.update({ ...editor.draft, delivery: { ...delivery, ...patch } });
  };
  const imageType = typeof node.config.image_type_key === "string" ? node.config.image_type_key : "";
  return (
    <AutosaveForm editor={editor} busy={busy}>
      {image && onPreviewImage ? <NodeImagePreview image={image} onPreview={onPreviewImage} /> : null}
      <TextInput label={t("graph.inspector.titleField")} value={editor.draft.title} maxLength={255} onChange={(title) => editor.update({ ...editor.draft, title })} />
      {imageType ? <ReadOnlyRow label={t("graph.inspector.imageType")} value={imageType} mono /> : null}
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
                    {delivery.fit === "contain" ? (
                      <CompactInput label={t("agentWorkbench.nodeEditor.backgroundColor")} value={delivery.background_color ?? ""} maxLength={7} placeholder="#FFFFFF" onChange={(background_color) => patchDelivery({ background_color: background_color.trim() || null })} />
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
    </AutosaveForm>
  );
}

function AutosaveForm<T>({
  children,
  busy,
  editor,
}: {
  children: ReactNode;
  busy: boolean;
  editor: NodeDraftAutosave<T>;
}) {
  const { t } = useI18n();
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
          <button type="button" onClick={editor.discard} disabled={busy} className="inline-flex h-10 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200">
            <Undo2 size={14} className="mr-1.5" />
            {t("settings.discard")}
          </button>
          <button type="submit" disabled={busy} className="inline-flex h-10 items-center justify-center rounded-xl bg-slate-900 px-3 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900">
            {busy || editor.status === "saving" ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Save size={14} className="mr-1.5" />}
            {t("detail.save")}
          </button>
        </div>
      ) : null}
    </form>
  );
}

function EdgeList({
  heading,
  empty,
  items,
  onJump,
}: {
  heading: string;
  empty: string;
  items: Array<{ edge: GraphNode["incoming"][number]; related: GraphNode | null }>;
  onJump?: (nodeId: string) => void;
}) {
  const { t } = useI18n();
  return (
    <section className="config-bubble rounded-2xl p-4 shadow-sm">
      <h4 className="text-xs font-semibold text-zinc-800 dark:text-slate-100">{heading}</h4>
      {items.length === 0 ? <p className="mt-2 text-xs text-zinc-500 dark:text-slate-400">{empty}</p> : null}
      <ul className="mt-2 space-y-1">
        {items.map(({ edge, related }) => (
          <li key={edge.id}>
            <button
              type="button"
              disabled={!related || !onJump}
              onClick={() => related && onJump?.(related.id)}
              className="flex w-full min-w-0 items-center justify-between gap-2 rounded-xl px-2.5 py-2 text-left hover:bg-zinc-50 disabled:text-zinc-400 dark:hover:bg-slate-900/60"
            >
              <span className="min-w-0 truncate text-xs font-medium text-zinc-800 dark:text-slate-100">
                {related?.title ?? edge.node_id}
              </span>
              <span className="shrink-0 rounded-full bg-zinc-100 px-2 py-0.5 text-[10px] font-medium text-zinc-500 dark:bg-slate-800 dark:text-slate-300">
                {t(edgeRoleKey(edge.role))}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
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

function FieldGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="space-y-3 border-t border-zinc-200 pt-4 dark:border-slate-700">
      <legend className="mb-1 text-xs font-semibold text-zinc-800 dark:text-slate-100">{title}</legend>
      {children}
    </fieldset>
  );
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

function CheckboxField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="flex min-h-10 items-center gap-2 rounded-xl border border-zinc-200 bg-zinc-50 px-3 text-xs font-medium text-zinc-700 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-200">
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} className="h-4 w-4 accent-slate-800" />
      <span>{label}</span>
    </label>
  );
}

function LineListField({ label, value, onChange }: { label: string; value: string[]; onChange: (value: string[]) => void }) {
  return <TextArea label={label} value={value.join("\n")} onChange={(next) => onChange(next.split("\n").map((item) => item.trim()).filter(Boolean))} minRows={2} />;
}

function updateFactRow(
  setFactsForm: (next: ProductFactsDraft) => void,
  form: ProductFactsDraft,
  id: string,
  patch: Partial<ProductFactRowDraft>,
) {
  setFactsForm({
    ...form,
    facts: form.facts.map((fact) => fact.id === id ? { ...fact, ...patch } : fact),
  });
}

function nextFactRowId(rows: ProductFactRowDraft[]): string {
  const used = new Set(rows.map((row) => row.id));
  let index = rows.length;
  while (used.has(`fact-${index}`)) index += 1;
  return `fact-${index}`;
}

function productFactsValidationMessage(
  error: ReturnType<typeof validateProductFactsDraft>,
  t: ReturnType<typeof useI18n>["t"],
): string {
  switch (error) {
    case "empty_key":
      return t("graph.inspector.productFactsEmptyKey");
    case "duplicate_key":
      return t("graph.inspector.productFactsDuplicateKey");
    case "empty_value":
      return t("graph.inspector.productFactsEmptyValue");
    default:
      return "";
  }
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.detail) return error.detail;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

function ReadOnlyRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[112px_minmax(0,1fr)] gap-3 border-b border-zinc-100 py-2 last:border-0 dark:border-slate-800">
      <dt className="text-zinc-500 dark:text-slate-400">{label}</dt>
      <dd className={`min-w-0 break-all text-zinc-800 dark:text-slate-100 ${mono ? "font-mono text-[10px]" : "font-medium"}`}>{value}</dd>
    </div>
  );
}

function selectOptionLabel(option: string, t: ReturnType<typeof useI18n>["t"]): string {
  if (option === "png" || option === "jpeg" || option === "webp") return option.toUpperCase();
  const key = SELECT_OPTION_LABEL_KEYS[option as keyof typeof SELECT_OPTION_LABEL_KEYS];
  return key ? t(key) : option;
}

function selectOptions(options: readonly string[], t: ReturnType<typeof useI18n>["t"]) {
  return options.map((option) => ({ value: option, label: selectOptionLabel(option, t) }));
}

function edgeRoleKey(role: GraphEdgeRole): TranslationKey {
  const keys = {
    facts: "graph.inspector.role.facts",
    reference: "graph.inspector.role.reference",
    brief: "graph.inspector.role.brief",
    visual_guidance: "graph.inspector.role.visual_guidance",
    prompt: "graph.inspector.role.prompt",
  } as const;
  return keys[role];
}

function configStatusKey(status: GraphConfigStatus): TranslationKey {
  const keys = {
    incomplete: "graph.inspector.config.incomplete",
    ready: "graph.inspector.config.ready",
    stale: "graph.inspector.config.stale",
  } as const;
  return keys[status];
}

function nodePreview(node: GraphNode): DownloadableImage | null {
  if (!node.preview_asset_id && !node.bound_asset_id) return null;
  const assetId = node.preview_asset_id ?? node.bound_asset_id;
  if (!assetId) return null;
  return {
    previewUrl: api.getProductImageAssetMediaUrl(assetId, "thumbnail"),
    downloadUrl: api.getProductImageAssetMediaUrl(assetId),
    filename: `${sanitizeFilenamePart(node.title, "workflow-image")}.png`,
    alt: node.title,
  };
}
