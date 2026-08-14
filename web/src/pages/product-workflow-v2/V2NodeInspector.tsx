import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  CircleDot,
  FileText,
  Image as ImageIcon,
  Link2,
  Loader2,
  Package,
  Play,
  Save,
  XCircle,
} from "lucide-react";
import { useState } from "react";

import { SelectField } from "../../components/SelectField";
import { ApiError, api } from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type { DownloadableImage } from "../../lib/image-downloads";
import { useI18n } from "../../lib/preferences";
import type {
  CanonicalProductDetail,
  ProductWorkflowV2,
  UpdateWorkflowNodeV2Input,
  WorkflowDeliverySpec,
  WorkflowDraftProductFact,
  WorkflowGenerationSpec,
  WorkflowImagePromptPayloadV1,
  WorkflowNodeDetailV2,
  WorkflowNodeRunV2,
  WorkflowNodeTypeV2,
  WorkflowNodeV2,
} from "../../lib/types";
import { TextArea } from "../product-detail/TextArea";
import { statusClass } from "../product-detail/utils";
import { DeliveryRenditionPanel } from "./DeliveryRenditionPanel";

interface V2NodeInspectorProps {
  product: CanonicalProductDetail;
  workflow: ProductWorkflowV2;
  node: WorkflowNodeV2 | null;
  facts: WorkflowDraftProductFact[];
  onBindReference: (node: WorkflowNodeV2) => void;
  onPreviewImage: (image: DownloadableImage) => void;
  onWorkflowChanged: () => Promise<unknown>;
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

export function V2NodeInspector({
  product,
  workflow,
  node,
  facts,
  onBindReference,
  onPreviewImage,
  onWorkflowChanged,
}: V2NodeInspectorProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
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

  const refreshNodeViews = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["v2-workflow-node-detail", product.id, workflow.id, node?.id] }),
      queryClient.invalidateQueries({ queryKey: runsQueryKey }),
      onWorkflowChanged(),
    ]);
  };
  const updateMutation = useMutation({
    mutationFn: (input: UpdateWorkflowNodeV2Input) =>
      api.updateWorkflowNodeV2(product.id, workflow.id, node!.id, input),
    onSuccess: refreshNodeViews,
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        void refreshNodeViews();
      }
    },
  });
  const runMutation = useMutation({
    mutationFn: () => api.runWorkflowNodeV2(node!.id),
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
    return <PanelState icon={<CircleDot size={20} />} text={t("agentWorkbench.nodeEditor.select")} />;
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
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-indigo-100 bg-indigo-50 text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
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
              {updateMutation.isPending ? (
                <span className="inline-flex items-center rounded-full border border-blue-200 bg-blue-50 px-2 py-0.5 text-[10px] font-medium text-blue-700 dark:border-blue-400/30 dark:bg-blue-500/10 dark:text-blue-200">
                  <Loader2 size={10} className="mr-1 animate-spin" />{t("detail.inspector.saving")}
                </span>
              ) : updateMutation.isSuccess ? (
                <span className="inline-flex items-center rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700 dark:border-emerald-400/30 dark:bg-emerald-500/10 dark:text-emerald-200">
                  <CheckCircle2 size={10} className="mr-1" />{t("agentWorkbench.nodeEditor.saved")}
                </span>
              ) : null}
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

      {mutationError ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          <AlertCircle size={13} className="mr-1.5 inline" />
          {errorDetail(mutationError, t("agentWorkbench.nodeEditor.loadFailed"))}
        </div>
      ) : null}
      {node.failure_reason ? (
        <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-400/30 dark:bg-red-500/10 dark:text-red-200">
          {node.failure_reason}
        </div>
      ) : null}

      {node.node_type === "product_context" ? (
        <ProductContextEditor product={product} detail={detail} facts={facts} />
      ) : node.node_type === "reference_image" ? (
        <ReferenceNodeEditor
          key={`${node.id}:${detail.workflow_edit_version}`}
          detail={detail}
          busy={updateMutation.isPending}
          onBind={() => onBindReference(node)}
          onSave={(input) => updateMutation.mutate(input)}
        />
      ) : node.node_type === "prompt_generation" && detail.prompt_artifact ? (
        <PromptNodeEditor
          key={`${node.id}:${detail.prompt_artifact.version_id}`}
          detail={detail}
          busy={updateMutation.isPending}
          onSave={(input) => updateMutation.mutate(input)}
        />
      ) : node.node_type === "image_generation" ? (
        <>
          <ImageNodeEditor
            key={`${node.id}:${detail.workflow_edit_version}`}
            detail={detail}
            busy={updateMutation.isPending}
            onSave={(input) => updateMutation.mutate(input)}
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
  detail,
  facts,
}: {
  product: CanonicalProductDetail;
  detail: WorkflowNodeDetailV2;
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
        <ReadOnlyRow label={t("agentWorkbench.nodeEditor.nodeName")} value={product.name} />
        <ReadOnlyRow label={t("detail.inspector.category")} value={product.category || t("agentWorkbench.nodeEditor.noValue")} />
        <ReadOnlyRow label={t("detail.inspector.price")} value={product.price || t("agentWorkbench.nodeEditor.noValue")} />
        <ReadOnlyRow label={t("agentWorkbench.nodeEditor.sourceDraft")} value={detail.source_draft_revision_id} mono />
        <ReadOnlyRow label={t("agentWorkbench.nodeEditor.visualSystem")} value={detail.visual_system_version_id} mono />
      </dl>
      <div className="mt-5 border-t border-zinc-200 pt-4 dark:border-slate-700">
        <SectionTitle title={t("agentWorkbench.nodeEditor.productFacts")} />
        {facts.length ? (
          <div className="mt-2 divide-y divide-zinc-100 dark:divide-slate-800">
            {facts.map((fact) => (
              <div key={fact.key} className="py-2.5">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-xs font-semibold text-zinc-800 dark:text-slate-100">{fact.key}</span>
                  <span className="shrink-0 rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-500 dark:bg-slate-800 dark:text-slate-300">
                    {factSourceLabel(fact.source_type, t)}
                  </span>
                </div>
                <div className="mt-1 break-words text-xs leading-5 text-zinc-600 dark:text-slate-300">{formatFactValue(fact.value)}</div>
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
  busy,
  onBind,
  onSave,
}: {
  detail: WorkflowNodeDetailV2;
  busy: boolean;
  onBind: () => void;
  onSave: (input: UpdateWorkflowNodeV2Input) => void;
}) {
  const { t } = useI18n();
  const node = detail.node;
  const [title, setTitle] = useState(node.title);
  const [role, setRole] = useState(textConfig(node.config_json.role));
  const [label, setLabel] = useState(textConfig(node.config_json.label));
  return (
    <EditorForm onSubmit={() => onSave({
      node_type: "reference_image",
      expected_edit_version: detail.workflow_edit_version,
      title,
      role,
      label,
    })} busy={busy}>
      <TextInput label={t("agentWorkbench.nodeEditor.nodeName")} value={title} onChange={setTitle} />
      <TextInput label={t("agentWorkbench.nodeEditor.referenceRole")} value={role} onChange={setRole} />
      <TextInput label={t("agentWorkbench.nodeEditor.referenceLabel")} value={label} onChange={setLabel} />
      <button
        type="button"
        onClick={onBind}
        className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-xl border border-indigo-200 bg-indigo-50 px-3 text-xs font-semibold text-indigo-700 hover:border-indigo-300 hover:bg-indigo-100 dark:border-violet-400/35 dark:bg-violet-500/10 dark:text-violet-100 dark:hover:bg-violet-500/15"
      >
        <Link2 size={14} />{t("agentWorkbench.nodeEditor.bindReference")}
      </button>
    </EditorForm>
  );
}

function PromptNodeEditor({
  detail,
  busy,
  onSave,
}: {
  detail: WorkflowNodeDetailV2;
  busy: boolean;
  onSave: (input: UpdateWorkflowNodeV2Input) => void;
}) {
  const { t } = useI18n();
  const artifact = detail.prompt_artifact!;
  const [title, setTitle] = useState(detail.node.title);
  const [payload, setPayload] = useState<WorkflowImagePromptPayloadV1>(artifact.payload);
  const patchPayload = (patch: Partial<WorkflowImagePromptPayloadV1>) => setPayload((current) => ({ ...current, ...patch }));

  return (
    <EditorForm onSubmit={() => onSave({
      node_type: "prompt_generation",
      expected_edit_version: detail.workflow_edit_version,
      expected_prompt_artifact_version_id: artifact.version_id,
      title,
      prompt_payload: payload,
    })} busy={busy}>
      <div className="flex items-center justify-between gap-2">
        <SectionTitle title={t("workflowConfirmation.section.prompts")} />
        <span className="rounded-full bg-indigo-50 px-2 py-1 text-[10px] font-semibold text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200">
          {t("agentWorkbench.nodeEditor.promptVersion", { version: artifact.version })}
        </span>
      </div>
      <TextInput label={t("agentWorkbench.nodeEditor.nodeName")} value={title} onChange={setTitle} />
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
            <div className="mb-2 text-xs font-semibold text-zinc-800 dark:text-slate-100">{image.image_plan_key}</div>
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
    </EditorForm>
  );
}

function ImageNodeEditor({
  detail,
  busy,
  onSave,
}: {
  detail: WorkflowNodeDetailV2;
  busy: boolean;
  onSave: (input: UpdateWorkflowNodeV2Input) => void;
}) {
  const { t } = useI18n();
  const parsedGeneration = parseGenerationSpec(detail.node.config_json.generation_spec);
  const parsedDelivery = parseDeliverySpec(detail.node.config_json.delivery_spec);
  const [title, setTitle] = useState(detail.node.title);
  const [variation, setVariation] = useState(textConfig(detail.node.config_json.variation_instruction));
  const [generation, setGeneration] = useState<WorkflowGenerationSpec | null>(parsedGeneration);
  const [delivery, setDelivery] = useState<WorkflowDeliverySpec | null>(parsedDelivery);

  if (!generation) {
    return <PanelState icon={<AlertCircle size={20} />} text={t("agentWorkbench.nodeEditor.loadFailed")} />;
  }

  return (
    <EditorForm onSubmit={() => onSave({
      node_type: "image_generation",
      expected_edit_version: detail.workflow_edit_version,
      title,
      variation_instruction: nullableText(variation),
      generation_spec: generation,
      delivery_spec: delivery,
    })} busy={busy}>
      <TextInput label={t("agentWorkbench.nodeEditor.nodeName")} value={title} onChange={setTitle} />
      <TextArea label={t("workflowConfirmation.variation")} value={variation} onChange={setVariation} minRows={3} />
      <FieldGroup title={t("agentWorkbench.nodeEditor.generationSettings")}>
        <TextInput label={t("agentWorkbench.nodeEditor.aspectRatio")} value={generation.aspect_ratio} onChange={(aspect_ratio) => setGeneration({ ...generation, aspect_ratio })} />
        <div className="grid gap-2 sm:grid-cols-2">
          <SelectInput label={t("agentWorkbench.nodeEditor.resolution")} value={generation.resolution_tier} options={["standard", "high", "ultra"]} onChange={(resolution_tier) => setGeneration({ ...generation, resolution_tier: resolution_tier as WorkflowGenerationSpec["resolution_tier"] })} />
          <SelectInput label={t("agentWorkbench.nodeEditor.quality")} value={generation.quality_intent} options={["draft", "standard", "high"]} onChange={(quality_intent) => setGeneration({ ...generation, quality_intent: quality_intent as WorkflowGenerationSpec["quality_intent"] })} />
          <SelectInput label={t("agentWorkbench.nodeEditor.referenceFidelity")} value={generation.reference_fidelity} options={["low", "medium", "high"]} onChange={(reference_fidelity) => setGeneration({ ...generation, reference_fidelity: reference_fidelity as WorkflowGenerationSpec["reference_fidelity"] })} />
          <SelectInput label={t("agentWorkbench.nodeEditor.backgroundIntent")} value={generation.background_intent} options={["auto", "opaque", "transparent"]} onChange={(background_intent) => setGeneration({ ...generation, background_intent: background_intent as WorkflowGenerationSpec["background_intent"] })} />
        </div>
        <SelectInput label={t("agentWorkbench.nodeEditor.textPolicy")} value={generation.text_policy} options={["none", "allow", "required"]} onChange={(text_policy) => setGeneration({ ...generation, text_policy: text_policy as WorkflowGenerationSpec["text_policy"], text_language: text_policy === "none" ? null : generation.text_language })} />
        {generation.text_policy !== "none" ? (
          <TextInput label={t("agentWorkbench.nodeEditor.textLanguage")} value={generation.text_language ?? ""} onChange={(text_language) => setGeneration({ ...generation, text_language: nullableText(text_language) })} />
        ) : null}
      </FieldGroup>

      <FieldGroup title={t("workflowConfirmation.deliverySpec")}>
        <CheckboxField
          label={t("agentWorkbench.nodeEditor.deliveryEnabled")}
          checked={delivery !== null}
          onChange={(enabled) => setDelivery(enabled ? defaultDeliverySpec() : null)}
        />
        {delivery ? (
          <>
            <div className="grid grid-cols-2 gap-2">
              <NumberInput label={t("agentWorkbench.nodeEditor.width")} value={delivery.width} min={1} onChange={(width) => setDelivery({ ...delivery, width })} />
              <NumberInput label={t("agentWorkbench.nodeEditor.height")} value={delivery.height} min={1} onChange={(height) => setDelivery({ ...delivery, height })} />
              <SelectInput label={t("agentWorkbench.nodeEditor.format")} value={delivery.format} options={["png", "jpeg", "webp"]} onChange={(format) => setDelivery({ ...delivery, format: format as WorkflowDeliverySpec["format"] })} />
              <SelectInput label={t("agentWorkbench.nodeEditor.fit")} value={delivery.fit} options={["contain", "cover"]} onChange={(fit) => setDelivery({ ...delivery, fit: fit as WorkflowDeliverySpec["fit"], crop_anchor: fit === "contain" ? null : delivery.crop_anchor })} />
            </div>
            <OptionalNumberInput label={t("agentWorkbench.nodeEditor.maxBytes")} value={delivery.max_byte_size ?? null} min={1} onChange={(max_byte_size) => setDelivery({ ...delivery, max_byte_size })} />
            <TextInput label={t("agentWorkbench.nodeEditor.backgroundColor")} value={delivery.background_color ?? ""} onChange={(background_color) => setDelivery({ ...delivery, background_color: nullableText(background_color) })} />
            {delivery.fit === "cover" ? (
              <SelectInput label={t("agentWorkbench.nodeEditor.cropAnchor")} value={delivery.crop_anchor ?? "center"} options={["center", "top", "bottom", "left", "right"]} onChange={(crop_anchor) => setDelivery({ ...delivery, crop_anchor: crop_anchor as NonNullable<WorkflowDeliverySpec["crop_anchor"]> })} />
            ) : null}
          </>
        ) : null}
      </FieldGroup>
    </EditorForm>
  );
}

function EditorForm({
  children,
  busy,
  onSubmit,
}: {
  children: React.ReactNode;
  busy: boolean;
  onSubmit: () => void;
}) {
  const { t } = useI18n();
  return (
    <form
      className="config-bubble space-y-4 rounded-2xl p-4 shadow-sm"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      {children}
      <button type="submit" disabled={busy} className="btn-primary-spring inline-flex h-10 w-full items-center justify-center rounded-xl px-3 text-xs font-semibold">
        {busy ? <Loader2 size={14} className="mr-1.5 animate-spin" /> : <Save size={14} className="mr-1.5" />}
        {t("detail.save")}
      </button>
    </form>
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

function TextInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} className="input-premium h-10 w-full px-3 text-xs outline-none" />
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

function OptionalNumberInput({ label, value, min, onChange }: { label: string; value: number | null; min?: number; onChange: (value: number | null) => void }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold text-zinc-500 dark:text-slate-400">{label}</span>
      <input type="number" value={value ?? ""} min={min} onChange={(event) => onChange(event.target.value ? Number(event.target.value) : null)} className="input-premium h-10 w-full px-3 text-xs outline-none" />
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
  if (type === "reference_image") return <Link2 size={16} />;
  if (type === "prompt_generation") return <FileText size={16} />;
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

function parseGenerationSpec(value: unknown): WorkflowGenerationSpec | null {
  if (!isRecord(value)) return null;
  const candidate = value as Partial<WorkflowGenerationSpec>;
  if (
    typeof candidate.aspect_ratio !== "string"
    || !["standard", "high", "ultra"].includes(candidate.resolution_tier ?? "")
    || !["draft", "standard", "high"].includes(candidate.quality_intent ?? "")
    || !["low", "medium", "high"].includes(candidate.reference_fidelity ?? "")
    || !["auto", "opaque", "transparent"].includes(candidate.background_intent ?? "")
    || !["none", "allow", "required"].includes(candidate.text_policy ?? "")
  ) return null;
  return candidate as WorkflowGenerationSpec;
}

function parseDeliverySpec(value: unknown): WorkflowDeliverySpec | null {
  if (value == null) return null;
  if (!isRecord(value)) return null;
  const candidate = value as Partial<WorkflowDeliverySpec>;
  if (
    typeof candidate.width !== "number"
    || typeof candidate.height !== "number"
    || !["png", "jpeg", "webp"].includes(candidate.format ?? "")
    || !["contain", "cover"].includes(candidate.fit ?? "")
  ) return null;
  return candidate as WorkflowDeliverySpec;
}

function defaultDeliverySpec(): WorkflowDeliverySpec {
  return {
    width: 1200,
    height: 1200,
    format: "png",
    max_byte_size: null,
    fit: "contain",
    background_color: null,
    crop_anchor: null,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function textConfig(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function nullableText(value: string): string | null {
  const normalized = value.trim();
  return normalized || null;
}

function splitLines(value: string): string[] {
  return value.split("\n").map((item) => item.trim()).filter(Boolean);
}

function formatFactValue(value: WorkflowDraftProductFact["value"]): string {
  if (typeof value === "string") return value;
  if (value === null) return "null";
  return JSON.stringify(value);
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.detail;
  if (error instanceof Error) return error.message;
  return fallback;
}
