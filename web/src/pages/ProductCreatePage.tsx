import { useMemo, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, ChevronRight, Eye, ImagePlus, LayoutTemplate, Loader2, Tag, X } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { Drawer } from "vaul";

import { ImageDropZone } from "../components/ImageDropZone";
import { api, ApiError } from "../lib/api";
import { localizeCanvasTemplateSummary } from "../lib/canvasTemplateLocalization";
import { useI18n } from "../lib/preferences";
import type { Locale, TranslationKey } from "../lib/i18n";
import type { CanvasTemplateSummary, WorkflowNodeType } from "../lib/types";

interface PreviewNode {
  id: string;
  title: string;
  subtitle: string;
  x: number;
  y: number;
  width?: number;
  tone?: "input" | "copy" | "image" | "output" | "blank";
}

interface PreviewPortUsage {
  inputs: Set<string>;
  outputs: Set<string>;
}

interface PreviewEdge {
  from: string;
  to: string;
}

interface CanvasPlanOption {
  key: string;
  label: string;
  shortLabel: string;
  description: string;
  badge: string;
  stage: string;
  outputCount: number;
  referenceCount: number;
  previewNodes: PreviewNode[];
  previewEdges: PreviewEdge[];
}

const PREVIEW_MIN_WIDTH = 920;
const PREVIEW_NODE_WIDTH = 248;
const NODE_HEIGHT = 92;
const PRODUCT_CREATE_FORM_ID = "product-create-form";

const NODE_TYPE_LABEL_KEYS: Record<WorkflowNodeType, TranslationKey> = {
  product_context: "create.productContext",
  reference_image: "create.referenceImage",
  copy_generation: "create.copy",
  image_generation: "create.imageGeneration",
};

const stageLabelKeys: Record<string, TranslationKey> = {
  blank: "create.stage.blank",
  listing: "create.stage.listing",
  detail: "create.stage.detail",
  content: "create.stage.content",
  gallery: "create.stage.gallery",
  campaign: "create.stage.campaign",
};

const stageOrder = ["blank", "listing", "detail", "gallery", "content", "campaign"];

const toneClasses: Record<NonNullable<PreviewNode["tone"]>, string> = {
  input: "border-sky-100 bg-sky-50/90 text-sky-900 dark:border-sky-400/35 dark:bg-sky-500/12 dark:text-sky-100",
  copy: "border-violet-100 bg-violet-50/90 text-violet-900 dark:border-violet-400/40 dark:bg-violet-500/16 dark:text-violet-100",
  image: "border-emerald-100 bg-emerald-50/90 text-emerald-900 dark:border-emerald-400/35 dark:bg-emerald-500/12 dark:text-emerald-100",
  output: "border-amber-100 bg-amber-50/90 text-amber-900 dark:border-amber-400/35 dark:bg-amber-500/12 dark:text-amber-100",
  blank: "border-dashed border-zinc-300 bg-white/80 text-zinc-500 dark:border-slate-600 dark:bg-[#151f33]/80 dark:text-slate-300",
};

function nodeTone(nodeType: WorkflowNodeType, outputNodeKeys: Set<string>, nodeKey: string): PreviewNode["tone"] {
  if (nodeType === "product_context") {
    return "input";
  }
  if (nodeType === "copy_generation") {
    return "copy";
  }
  if (nodeType === "image_generation") {
    return "image";
  }
  return outputNodeKeys.has(nodeKey) ? "output" : "input";
}

function nodeSubtitle(
  node: CanvasTemplateSummary["preview_nodes"][number],
  outputNodeKeys: Set<string>,
  t: ReturnType<typeof useI18n>["t"],
): string {
  if (outputNodeKeys.has(node.key)) {
    return t("create.outputSlot");
  }
  if (node.node_type === "image_generation" && node.size) {
    return node.size.replace("x", " x ");
  }
  return t(NODE_TYPE_LABEL_KEYS[node.node_type]);
}

function canvasTemplateToPlan(template: CanvasTemplateSummary, t: ReturnType<typeof useI18n>["t"]): CanvasPlanOption {
  const outputNodeKeys = new Set(template.output_slots.map((slot) => slot.node_key));
  return {
    key: template.key,
    label: template.title,
    shortLabel: template.output_slots.map((slot) => slot.label).join(" / ") || template.scenario.title,
    description: template.description,
    badge: template.scenario.title || t("create.template"),
    stage: template.scenario.ecommerce_stage,
    outputCount: template.output_slots.length,
    referenceCount: template.reference_input_hints.length,
    previewNodes: template.preview_nodes.map((node) => ({
      id: node.key,
      title: node.title,
      subtitle: nodeSubtitle(node, outputNodeKeys, t),
      x: node.position_x,
      y: node.position_y,
      tone: nodeTone(node.node_type, outputNodeKeys, node.key),
    })),
    previewEdges: template.preview_edges.map((edge) => ({
      from: edge.source_node_key,
      to: edge.target_node_key,
    })),
  };
}

function sortPlans(plans: CanvasPlanOption[], locale: Locale): CanvasPlanOption[] {
  return [...plans].sort((left, right) => {
    const leftIndex = stageOrder.indexOf(left.stage);
    const rightIndex = stageOrder.indexOf(right.stage);
    const normalizedLeft = leftIndex === -1 ? stageOrder.length : leftIndex;
    const normalizedRight = rightIndex === -1 ? stageOrder.length : rightIndex;
    return normalizedLeft - normalizedRight || left.label.localeCompare(right.label, locale);
  });
}

function previewWidth(plan: CanvasPlanOption): number {
  if (!plan.previewNodes.length) {
    return PREVIEW_MIN_WIDTH;
  }
  return Math.max(
    PREVIEW_MIN_WIDTH,
    Math.max(...plan.previewNodes.map((node) => node.x + (node.width ?? PREVIEW_NODE_WIDTH))) + 96,
  );
}

function groupedPlans(plans: CanvasPlanOption[], t: ReturnType<typeof useI18n>["t"]) {
  const groups = new Map<string, CanvasPlanOption[]>();
  for (const plan of plans) {
    const items = groups.get(plan.stage) ?? [];
    items.push(plan);
    groups.set(plan.stage, items);
  }
  return stageOrder
    .filter((stage) => groups.has(stage))
    .map((stage) => ({ stage, label: stageLabelKeys[stage] ? t(stageLabelKeys[stage]) : stage, plans: groups.get(stage) ?? [] }));
}

export function ProductCreatePage() {
  const { locale, t } = useI18n();
  const navigate = useNavigate();
  const mobileTemplateButtonRef = useRef<HTMLButtonElement | null>(null);
  const mobilePreviewButtonRef = useRef<HTMLButtonElement | null>(null);
  const [name, setName] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [canvasTemplateKey, setCanvasTemplateKey] = useState<string>("");
  const [error, setError] = useState("");
  const [mobileTemplateSheetOpen, setMobileTemplateSheetOpen] = useState(false);
  const [mobilePreviewSheetOpen, setMobilePreviewSheetOpen] = useState(false);

  const templatesQuery = useQuery({
    queryKey: ["canvas-templates"],
    queryFn: () => api.listCanvasTemplates(),
  });

  const canvasPlanOptions = useMemo(() => {
    const blankCanvasPlan: CanvasPlanOption = {
      key: "",
      label: t("create.blankCanvas"),
      shortLabel: t("create.freeLayout"),
      description: t("create.blankDescription"),
      badge: t("create.basic"),
      stage: "blank",
      outputCount: 0,
      referenceCount: 0,
      previewNodes: [
        { id: "product", title: t("create.productContext"), subtitle: t("create.productInfoNode"), x: 48, y: 112, tone: "input" },
        { id: "blank", title: t("create.freeLayout"), subtitle: t("create.addNode"), x: 368, y: 112, tone: "blank" },
      ],
      previewEdges: [{ from: "product", to: "blank" }],
    };
    const fullCanvasTemplates =
      templatesQuery.data?.items
        .filter((template) => template.kind === "full_canvas")
        .map((template) => localizeCanvasTemplateSummary(template, locale))
        .map((template) => canvasTemplateToPlan(template, t)) ?? [];
    return [blankCanvasPlan, ...sortPlans(fullCanvasTemplates, locale)];
  }, [locale, t, templatesQuery.data]);

  const selectedPlan =
    canvasPlanOptions.find((option) => option.key === canvasTemplateKey) ?? canvasPlanOptions[0];

  const planGroups = useMemo(() => groupedPlans(canvasPlanOptions, t), [canvasPlanOptions, t]);

  const previewLabel = useMemo(() => {
    if (!file) {
      return t("create.uploadIdle");
    }
    return file.name;
  }, [file, t]);

  const createProductMutation = useMutation({
    mutationFn: () => {
      if (!file) {
        throw new Error(t("create.requiredImage"));
      }
      return api.createProduct({
        name,
        file,
        canvas_template_key: selectedPlan.key,
        template_language: selectedPlan.key ? locale : undefined,
      });
    },
    onSuccess: (product) => {
      navigate(`/products/${product.id}`);
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError) {
        setError(mutationError.detail);
        return;
      }
      setError(mutationError instanceof Error ? mutationError.message : t("create.failed"));
    },
  });

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    createProductMutation.mutate();
  };

  const handleImageFiles = (files: File[]) => {
    setFile(files[0] ?? null);
    setError("");
  };

  const templatePanelContent = (
    <>
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-zinc-950 dark:text-white">{t("create.templateTitle")}</h2>
          <p className="mt-1 text-sm text-zinc-500 dark:text-slate-400">{t("create.templateDescription")}</p>
        </div>
        {templatesQuery.isLoading ? <Loader2 size={16} className="animate-spin text-zinc-400" /> : null}
      </div>

      {templatesQuery.isError ? (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
          {t("create.templateLoadFailed")}
        </div>
      ) : null}

      <div className="mt-4 space-y-5 pr-1 lg:max-h-[610px] lg:overflow-y-auto">
        {planGroups.map((group) => (
          <div key={group.stage}>
            <div className="mb-2 flex items-center justify-between">
              <h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-zinc-500 dark:text-slate-400">{group.label}</h3>
              <span className="text-[11px] text-zinc-400 dark:text-slate-500">{group.plans.length}</span>
            </div>
            <div className="space-y-2">
              {group.plans.map((option) => {
                const selected = selectedPlan.key === option.key;
                return (
                  <button
                    key={option.key || "blank"}
                    type="button"
                    onClick={() => {
                      setCanvasTemplateKey(option.key);
                      setMobileTemplateSheetOpen(false);
                    }}
                    className={`w-full rounded-lg border p-3 text-left transition-colors ${
                      selected
                        ? "border-blue-500 bg-blue-50/50 shadow-[0_0_0_1px_rgb(59_130_246)] dark:border-violet-400 dark:bg-violet-500/18 dark:shadow-[0_0_0_1px_rgba(167,139,250,0.65)]"
                        : "border-zinc-200 bg-white hover:border-zinc-300 hover:bg-zinc-50 dark:border-slate-700/80 dark:bg-[#151f33] dark:hover:border-violet-400/45 dark:hover:bg-violet-500/12"
                    }`}
                  >
                    <div className="flex min-w-0 items-start justify-between gap-3">
                      <div className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-semibold text-zinc-950 dark:text-white">
                          {option.label}
                        </span>
                        <p className="mt-1 line-clamp-2 text-xs leading-5 text-zinc-500 dark:text-slate-400">
                          {option.description}
                        </p>
                      </div>
                      {selected ? <Check size={14} className="mt-0.5 shrink-0 text-blue-600 dark:text-violet-200" /> : null}
                    </div>
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      <TemplateChip>{option.shortLabel}</TemplateChip>
                      {option.outputCount ? <TemplateChip>{t("create.outputCount", { count: option.outputCount })}</TemplateChip> : null}
                      {option.referenceCount ? <TemplateChip>{t("create.referenceCount", { count: option.referenceCount })}</TemplateChip> : null}
                    </div>
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </>
  );

  const previewPanelContent = (
    <>
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-lg font-semibold text-zinc-950 dark:text-white">{selectedPlan.label}</h2>
            <span className="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] font-medium text-zinc-500 dark:border dark:border-slate-700 dark:bg-[#151f33] dark:text-slate-300">
              {selectedPlan.badge}
            </span>
          </div>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-zinc-500 dark:text-slate-400">{selectedPlan.description}</p>
        </div>
        <div className="flex items-center gap-2 text-xs text-zinc-500 dark:text-slate-300">
          <span className="rounded-md border border-zinc-200 bg-zinc-50 px-2 py-1 dark:border-slate-700 dark:bg-[#151f33]">
            {t("create.nodeCount", { count: selectedPlan.previewNodes.length })}
          </span>
          <span className="rounded-md border border-zinc-200 bg-zinc-50 px-2 py-1 dark:border-slate-700 dark:bg-[#151f33]">
            {t("create.edgeCount", { count: selectedPlan.previewEdges.length })}
          </span>
        </div>
      </div>
      <WorkflowPreview plan={selectedPlan} />
    </>
  );

  return (
    <div className="min-h-screen bg-zinc-100 px-4 pb-[calc(6.25rem+env(safe-area-inset-bottom))] pt-4 text-zinc-900 dark:bg-[#060a12] dark:text-slate-100 sm:px-6 lg:px-8 lg:pb-8">
      <main className="mx-auto max-w-[1480px]">
        <div className="mb-5 flex items-start justify-between gap-4 border-b border-zinc-200 pb-4 dark:border-slate-800">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-md bg-blue-50 text-blue-600 dark:border dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
              <Tag size={21} />
            </div>
            <div>
              <h1 className="text-xl font-semibold text-zinc-950 dark:text-white">{t("create.title")}</h1>
              <p className="mt-1 text-sm text-zinc-500 dark:text-slate-400">{t("create.description")}</p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => navigate("/products")}
            aria-label={t("create.close")}
            className="flex h-10 w-10 items-center justify-center rounded-full bg-zinc-200/70 text-zinc-500 transition-colors hover:bg-zinc-300 hover:text-zinc-900 dark:border dark:border-slate-700/80 dark:bg-[#151f33] dark:text-slate-300 dark:hover:bg-[#1a2740] dark:hover:text-white"
          >
            <X size={18} />
          </button>
        </div>

        <form id={PRODUCT_CREATE_FORM_ID} onSubmit={handleSubmit} className="grid gap-5 lg:grid-cols-[360px_minmax(0,1fr)]">
          <section className="rounded-lg border border-zinc-200 bg-white p-5 shadow-sm dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_16px_48px_rgba(0,0,0,0.22)]">
            <h2 className="text-base font-semibold text-zinc-950 dark:text-white">{t("create.productInfo")}</h2>

            <div className="mt-5">
              <label className="mb-2 block text-sm font-medium text-zinc-700 dark:text-slate-300">
                {t("create.mainImage")} <span className="text-red-500">*</span>
              </label>
              <ImageDropZone
                ariaLabel={t("create.uploadAria")}
                className="flex aspect-[1.55] cursor-pointer flex-col items-center justify-center rounded-md border border-dashed border-zinc-300 bg-zinc-50/40 p-7 text-zinc-500 transition-colors hover:border-blue-300 hover:bg-blue-50/40 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-400 dark:hover:border-violet-400/55 dark:hover:bg-violet-500/10"
                onFiles={handleImageFiles}
              >
                {({ isDragging }) => (
                  <>
                    <ImagePlus size={34} className="mb-3 text-zinc-400 dark:text-slate-500" />
                    <p className="text-sm font-medium text-zinc-700 dark:text-slate-200">{isDragging ? t("create.uploadDrop") : previewLabel}</p>
                    <p className="mt-2 text-xs text-zinc-500 dark:text-slate-400">{t("create.uploadHint")}</p>
                  </>
                )}
              </ImageDropZone>
            </div>

            <div className="mt-6">
              <label className="mb-2 block text-sm font-medium text-zinc-700 dark:text-slate-300">
                {t("create.productName")} <span className="text-red-500">*</span>
              </label>
              <input
                required
                type="text"
                maxLength={60}
                value={name}
                onChange={(event) => setName(event.target.value)}
                className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2.5 text-sm transition-shadow placeholder:text-zinc-400 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-100 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/20"
                placeholder={t("create.namePlaceholder")}
              />
              <div className="mt-1 text-right text-xs text-zinc-400 dark:text-slate-500">{name.length} / 60</div>
            </div>

            <div className="mt-6 rounded-2xl border border-zinc-200 bg-zinc-50/70 p-3 dark:border-slate-700 dark:bg-[#0b1220] lg:hidden">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-zinc-500 dark:text-slate-400">{t("create.templateTitle")}</div>
                  <div className="mt-1 truncate text-sm font-semibold text-zinc-950 dark:text-white">{selectedPlan.label}</div>
                  <div className="mt-1 flex flex-wrap gap-1.5">
                    <TemplateChip>{selectedPlan.shortLabel}</TemplateChip>
                    <TemplateChip>{t("create.nodeCount", { count: selectedPlan.previewNodes.length })}</TemplateChip>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => setMobileTemplateSheetOpen(true)}
                  className="inline-flex min-h-11 shrink-0 items-center rounded-xl border border-zinc-200 bg-white px-3 text-xs font-semibold text-zinc-700 shadow-sm transition-colors hover:border-blue-200 hover:text-blue-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-200 dark:hover:border-violet-400/60 dark:hover:text-violet-100 dark:focus-visible:ring-violet-400"
                >
                  {t("create.changeTemplate")}
                </button>
              </div>
            </div>

            {error ? <div className="mt-4 text-sm text-red-600">{error}</div> : null}

            <div className="mt-6 hidden gap-3 lg:flex">
              <button
                type="button"
                onClick={() => navigate("/products")}
                className="flex-1 rounded-md border border-zinc-200 px-4 py-2.5 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:bg-zinc-50 dark:border-slate-700 dark:text-slate-300 dark:hover:border-slate-500 dark:hover:bg-white/10 dark:hover:text-white"
              >
                {t("create.cancel")}
              </button>
              <button
                type="submit"
                disabled={createProductMutation.isPending}
                className="flex flex-1 items-center justify-center rounded-md bg-blue-600 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-700 disabled:opacity-50 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-1 dark:ring-violet-300/35"
              >
                {createProductMutation.isPending ? <Loader2 size={15} className="mr-2 animate-spin" /> : null}
                {t("create.submit")}
              </button>
            </div>
          </section>

          <section className="hidden min-h-[720px] gap-5 lg:grid xl:grid-cols-[320px_minmax(0,1fr)]">
            <div className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_16px_48px_rgba(0,0,0,0.22)]">
              {templatePanelContent}
            </div>

            <div className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_16px_48px_rgba(0,0,0,0.22)]">
              {previewPanelContent}
            </div>
          </section>
        </form>
      </main>

      <div className="fixed inset-x-0 z-40 px-3 lg:hidden" style={{ bottom: "calc(0.75rem + env(safe-area-inset-bottom))" }}>
        <div className="mx-auto flex max-w-2xl items-center gap-2 rounded-2xl border border-slate-200 bg-white p-2 shadow-[0_-6px_18px_rgba(15,23,42,0.12)] dark:border-slate-700 dark:bg-slate-950 dark:shadow-[0_-12px_28px_rgba(0,0,0,0.30)]">
          <button
            ref={mobileTemplateButtonRef}
            type="button"
            onClick={() => setMobileTemplateSheetOpen(true)}
            className="inline-flex min-h-11 min-w-0 flex-1 items-center rounded-xl border border-slate-200 bg-white px-3 text-left text-xs font-semibold text-slate-700 shadow-sm transition-colors active:scale-[0.98] hover:border-indigo-200 hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-200 dark:hover:border-violet-400/60 dark:hover:text-violet-100 dark:focus-visible:ring-violet-400"
            aria-label={t("create.openTemplateSheet")}
          >
            <LayoutTemplate size={16} className="mr-2 shrink-0 text-indigo-600 dark:text-violet-200" />
            <span className="min-w-0 flex-1 truncate">{selectedPlan.shortLabel}</span>
            <ChevronRight size={16} className="ml-2 shrink-0 text-slate-400" />
          </button>
          <button
            ref={mobilePreviewButtonRef}
            type="button"
            onClick={() => setMobilePreviewSheetOpen(true)}
            className="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 text-slate-700 shadow-sm transition-colors active:scale-[0.98] hover:border-indigo-200 hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-200 dark:hover:border-violet-400/60 dark:hover:text-violet-100 dark:focus-visible:ring-violet-400"
            aria-label={t("create.openPreviewSheet")}
            title={t("create.openPreviewSheet")}
          >
            <Eye size={17} />
          </button>
          <button
            type="submit"
            form={PRODUCT_CREATE_FORM_ID}
            disabled={createProductMutation.isPending}
            className="inline-flex min-h-11 shrink-0 items-center justify-center rounded-xl bg-indigo-600 px-3 text-sm font-semibold text-white shadow-md shadow-indigo-600/16 transition-colors active:scale-[0.98] hover:bg-indigo-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 disabled:opacity-60 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-1 dark:ring-violet-300/35"
          >
            {createProductMutation.isPending ? <Loader2 size={15} className="mr-1.5 animate-spin" /> : null}
            {t("create.submitShort")}
          </button>
        </div>
      </div>

      <Drawer.Root
        direction="bottom"
        handleOnly
        open={mobileTemplateSheetOpen}
        onOpenChange={(open) => {
          setMobileTemplateSheetOpen(open);
          if (!open) {
            mobileTemplateButtonRef.current?.focus();
          }
        }}
      >
        <Drawer.Portal>
          <Drawer.Overlay className="fixed inset-0 z-[70] bg-slate-950/42 lg:hidden" />
          <Drawer.Content className="fixed inset-x-0 bottom-0 z-[71] flex max-h-[80dvh] flex-col overflow-hidden rounded-t-[1.5rem] border-t border-slate-200 bg-white shadow-[0_-12px_34px_rgba(15,23,42,0.16)] outline-none dark:border-slate-700 dark:bg-[#0f1726] dark:shadow-[0_-18px_42px_rgba(0,0,0,0.34)] lg:hidden">
            <Drawer.Title className="sr-only">{t("create.mobileTemplateSheet")}</Drawer.Title>
            <Drawer.Description className="sr-only">{t("create.templateDescription")}</Drawer.Description>
            <Drawer.Handle className="mx-auto mt-2 flex h-7 w-24 items-center justify-center rounded-full text-slate-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:text-slate-500 dark:focus-visible:ring-violet-400">
              <span className="h-1.5 w-12 rounded-full bg-slate-300 dark:bg-slate-600" />
            </Drawer.Handle>
            <div data-vaul-no-drag className="min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain px-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] pt-2 [-webkit-overflow-scrolling:touch]">
              {templatePanelContent}
            </div>
          </Drawer.Content>
        </Drawer.Portal>
      </Drawer.Root>

      <Drawer.Root
        direction="bottom"
        handleOnly
        open={mobilePreviewSheetOpen}
        onOpenChange={(open) => {
          setMobilePreviewSheetOpen(open);
          if (!open) {
            mobilePreviewButtonRef.current?.focus();
          }
        }}
      >
        <Drawer.Portal>
          <Drawer.Overlay className="fixed inset-0 z-[70] bg-slate-950/42 lg:hidden" />
          <Drawer.Content className="fixed inset-x-0 bottom-0 z-[71] flex max-h-[80dvh] flex-col overflow-hidden rounded-t-[1.5rem] border-t border-slate-200 bg-white shadow-[0_-12px_34px_rgba(15,23,42,0.16)] outline-none dark:border-slate-700 dark:bg-[#0f1726] dark:shadow-[0_-18px_42px_rgba(0,0,0,0.34)] lg:hidden">
            <Drawer.Title className="sr-only">{t("create.mobilePreviewSheet")}</Drawer.Title>
            <Drawer.Description className="sr-only">{selectedPlan.description}</Drawer.Description>
            <Drawer.Handle className="mx-auto mt-2 flex h-7 w-24 items-center justify-center rounded-full text-slate-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:text-slate-500 dark:focus-visible:ring-violet-400">
              <span className="h-1.5 w-12 rounded-full bg-slate-300 dark:bg-slate-600" />
            </Drawer.Handle>
            <div data-vaul-no-drag className="min-h-0 flex-1 touch-pan-y overflow-y-auto overscroll-contain px-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] pt-2 [-webkit-overflow-scrolling:touch]">
              {previewPanelContent}
            </div>
          </Drawer.Content>
        </Drawer.Portal>
      </Drawer.Root>
    </div>
  );
}

function TemplateChip({ children }: { children: ReactNode }) {
  return (
    <span className="rounded border border-zinc-200 bg-zinc-50 px-1.5 py-0.5 text-[10px] font-medium text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
      {children}
    </span>
  );
}

function WorkflowPreview({ plan }: { plan: CanvasPlanOption }) {
  const nodeById = new Map(plan.previewNodes.map((node) => [node.id, node]));
  const width = previewWidth(plan);
  const portUsage = plan.previewEdges.reduce<PreviewPortUsage>(
    (usage, edge) => {
      usage.outputs.add(edge.from);
      usage.inputs.add(edge.to);
      return usage;
    },
    { inputs: new Set<string>(), outputs: new Set<string>() },
  );
  return (
    <div className="relative h-[560px] overflow-x-auto overflow-y-hidden rounded-md border border-zinc-100 bg-zinc-50 dark:border-slate-700/80 dark:bg-[#0b1220]">
      <div
        className="relative h-full bg-[radial-gradient(circle_at_1px_1px,rgb(212_212_216)_1px,transparent_0)] bg-[length:16px_16px] dark:bg-[radial-gradient(circle_at_1px_1px,rgba(148,163,184,0.2)_1px,transparent_0)]"
        style={{ width }}
      >
        <svg className="pointer-events-none absolute inset-0 z-0 h-full w-full" viewBox={`0 0 ${width} 560`} aria-hidden="true">
          {plan.previewEdges.map((edge) => {
            const from = nodeById.get(edge.from);
            const to = nodeById.get(edge.to);
            if (!from || !to) {
              return null;
            }
            const fromWidth = from.width ?? PREVIEW_NODE_WIDTH;
            const startX = from.x + fromWidth;
            const startY = from.y + NODE_HEIGHT / 2;
            const endX = to.x;
            const endY = to.y + NODE_HEIGHT / 2;
            const midX = startX + Math.max((endX - startX) / 2, 36);
            return (
              <path
                key={`${edge.from}-${edge.to}`}
                d={`M ${startX} ${startY} C ${midX} ${startY}, ${midX} ${endY}, ${endX} ${endY}`}
                fill="none"
                className="stroke-indigo-600 dark:stroke-violet-400"
                strokeLinecap="round"
                strokeOpacity="0.75"
                strokeWidth="1.8"
              />
            );
          })}
        </svg>
        {plan.previewNodes.map((node) => (
          <PreviewNodeCard key={node.id} node={node} portUsage={portUsage} />
        ))}
      </div>
    </div>
  );
}

function PreviewNodeCard({ node, portUsage }: { node: PreviewNode; portUsage: PreviewPortUsage }) {
  const { t } = useI18n();
  const width = node.width ?? PREVIEW_NODE_WIDTH;
  const status = node.tone === "copy" || node.tone === "image" ? t("create.pending") : t("create.available");
  const showInputPort = portUsage.inputs.has(node.id);
  const showOutputPort = portUsage.outputs.has(node.id);
  return (
    <div
      className={`absolute z-10 rounded-lg border px-3 py-3 shadow-sm backdrop-blur dark:shadow-slate-950/25 ${toneClasses[node.tone ?? "input"]}`}
      style={{ left: node.x, top: node.y, width, height: NODE_HEIGHT }}
    >
      {showInputPort ? (
        <span
          aria-hidden="true"
          className="absolute left-[-5px] top-1/2 h-2.5 w-2.5 -translate-y-1/2 rounded-full border border-slate-300 bg-white shadow-sm dark:border-slate-500 dark:bg-[#0b1220]"
        />
      ) : null}
      {showOutputPort ? (
        <span
          aria-hidden="true"
          className="absolute right-[-6px] top-1/2 h-3 w-3 -translate-y-1/2 rounded-full border-2 border-indigo-500 bg-white shadow-sm dark:border-violet-400 dark:bg-[#0b1220]"
        />
      ) : null}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{node.title}</div>
          <div className="mt-1 truncate text-xs opacity-70">{node.subtitle}</div>
        </div>
        <span className="shrink-0 rounded-full bg-white/70 px-2 py-0.5 text-[10px] text-zinc-500 dark:bg-[#0b1220]/80 dark:text-slate-300">{status}</span>
      </div>
      <div className="mt-3 flex gap-1.5">
        <span className="h-1.5 w-7 rounded-full bg-current opacity-20" />
        <span className="h-1.5 w-4 rounded-full bg-current opacity-20" />
      </div>
    </div>
  );
}
