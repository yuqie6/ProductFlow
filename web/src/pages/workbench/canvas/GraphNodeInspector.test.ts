import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type {
  DeliveryPresetCatalog,
  GraphCatalogConfigField,
  GraphNode,
  GraphNodeCatalog,
  GraphProjection,
  WorkflowDeliverySpec,
} from "../../../lib/types";
import { applyDeliveryPresetToCatalogDraft, GraphNodeInspector } from "./GraphNodeInspector";

function node(partial: Partial<GraphNode> & Pick<GraphNode, "id" | "node_type">): GraphNode {
  return {
    title: partial.title ?? partial.id,
    position_x: 0,
    position_y: 0,
    config: {},
    bound_asset_id: null,
    group_id: null,
    preview_asset_id: null,
    config_status: "incomplete",
    unused: false,
    incoming: [],
    outgoing: [],
    ...partial,
  };
}

function field(
  key: string,
  control: GraphCatalogConfigField["control"],
  extra: Partial<GraphCatalogConfigField> = {},
): GraphCatalogConfigField {
  return {
    key,
    value_kind: extra.value_kind ?? (control === "string_list" ? "string_list" : "string"),
    required: false,
    control,
    ...extra,
  };
}

const catalog: GraphNodeCatalog = {
  version: 3,
  nodes: [
    {
      node_type: "product_source", output_data_type: "product_facts", kind: "source", accepts: [], config_fields: [
        field("source_product_id", "hidden", { value_kind: "string_or_null" }),
      ]
    },
    {
      node_type: "image_asset", output_data_type: "image_asset", kind: "source", accepts: [], config_fields: [
        field("role", "text", { label_key: "graph.inspector.assetRole", value_kind: "string_or_null" }),
        field("label", "text", { label_key: "graph.inspector.assetLabel", value_kind: "string_or_null" }),
      ]
    },
    {
      node_type: "creative_brief", output_data_type: "creative_brief", kind: "document", accepts: [], config_fields: [
        field("goal", "textarea", { label_key: "workflowConfirmation.designGoal" }),
        field("design_goals", "string_list", { label_key: "graph.inspector.designGoals" }),
      ]
    },
    {
      node_type: "visual_system", output_data_type: "visual_system", kind: "document", accepts: [], config_fields: [
        field("visual_system_version_id", "hidden", { value_kind: "string_or_null" }),
        field("visual_overlay", "group", {
          value_kind: "object_or_null",
          hint_key: "graph.inspector.visualVersionHint",
          fields: [
            field("style", "string_list", { label_key: "graph.inspector.visualStyle" }),
            field("colors", "visual_background", { label_key: "graph.inspector.visualBackground", value_kind: "object" }),
          ],
        }),
      ]
    },
    { node_type: "image_prompt", output_data_type: "prompt", kind: "document", accepts: [], config_fields: [] },
    {
      node_type: "image_generation", output_data_type: "image_asset", kind: "effect", accepts: [
        { data_type: "prompt", role: "prompt", max_count: 1, required_to_run: true },
        { data_type: "image_asset", role: "reference", max_count: null, required_to_run: false },
        { data_type: "visual_system", role: "visual_guidance", max_count: 1, required_to_run: false },
      ], config_fields: [
        field("variation_instruction", "textarea", {
          label_key: "workflowConfirmation.variation",
          value_kind: "string_or_null",
          max_length: 4000,
        }),
        field("generation_spec", "group", {
          value_kind: "object",
          label_key: "agentWorkbench.nodeEditor.generationSettings",
          fields: [
            field("aspect_ratio", "aspect_ratio", { label_key: "agentWorkbench.nodeEditor.aspectRatio", panel: "basic" }),
          ],
        }),
        field("visual_overlay", "group", {
          value_kind: "object_or_null",
          fields: [
            field("style", "string_list", { label_key: "graph.inspector.visualStyle" }),
            field("colors", "visual_background", { label_key: "graph.inspector.visualBackground", value_kind: "object_list" }),
          ],
        }),
      ]
    },
  ],
};

const graph: GraphProjection = {
  id: "g1",
  product_id: "p1",
  title: "夏季主图",
  schema_version: 3,
  revision: 4,
  last_operation_group_id: null,
  can_undo: false,
  can_redo: false,
  nodes: [
    node({ id: "source", node_type: "product_source", title: "商品资料", config_status: "ready" }),
    node({
      id: "brief",
      node_type: "creative_brief",
      title: "创作要求",
      config: { goal: "突出瓶身", design_goals: ["主图清晰"] },
    }),
    node({
      id: "asset",
      node_type: "image_asset",
      title: "参考图 1",
      bound_asset_id: "asset-1",
      unused: true,
    }),
    node({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
      incoming: [{
        id: "edge-prompt",
        node_id: "brief",
        data_type: "creative_brief",
        role: "brief",
        order: 0,
      }],
      config: {
        generation_spec: {
          aspect_ratio: "4:5",
          resolution_tier: "high",
          quality_intent: "high",
          reference_fidelity: "high",
          background_intent: "auto",
          text_policy: "none",
        },
      },
    }),
  ],
  edges: [],
  groups: [],
};

const deliveryPresetCatalog: DeliveryPresetCatalog = {
  supports_custom: true,
  items: [
    preset("taobao_tmall_hero", "淘宝/天猫首屏", "3:4", "hero", 1200, 1600),
    preset("jd_hero", "京东主图", "1:1", "hero", 1200, 1200),
    preset("amazon_hero", "Amazon 主图", "1:1", "hero", 1200, 1200),
    preset("detail_portrait", "详情竖图", "3:4", "detail", 1200, 1600),
    preset("scene_landscape", "场景横图", "4:3", "scene", 1600, 1200),
  ],
};

function renderInspector(
  selected: GraphNode | null,
  client?: QueryClient,
  nextCatalog: GraphNodeCatalog | null = catalog,
  options: { busy?: boolean; catalogError?: string | null; preview?: boolean; localEdit?: boolean; pinAsset?: boolean } = {},
): string {
  const queryClient = client ?? new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderToStaticMarkup(createElement(
    QueryClientProvider,
    { client: queryClient },
    createElement(GraphNodeInspector, {
      graph,
      node: selected,
      catalog: nextCatalog,
      busy: options.busy ?? false,
      catalogError: options.catalogError,
      onCommit: async () => graph,
      onOpenAdd: () => undefined,
      onOpenLibrary: () => undefined,
      onPreviewImage: options.preview ? () => undefined : undefined,
      onOpenLocalEdit: options.localEdit ? () => undefined : undefined,
      onPinAsset: options.pinAsset ? () => undefined : undefined,
    }),
  ));
}

describe("GraphNodeInspector", () => {
  it("shows next actions instead of a revision dump when nothing is selected", () => {
    const markup = renderInspector(null);
    expect(markup).toContain("夏季主图");
    expect(markup).toContain("选一个节点继续改");
    expect(markup).toContain("添加节点");
    expect(markup).toContain("打开图库");
    expect(markup).toContain("运行整张图");
    expect(markup).not.toContain("版本 4");
    expect(markup).not.toContain("generation_spec");
    expect(markup).not.toContain("保存配置");
  });

  it("renders generation settings from catalog fields instead of a raw config textarea", () => {
    const markup = renderInspector(graph.nodes.find((item) => item.id === "image") ?? null);
    expect(markup).toContain("画面比例");
    expect(markup).toContain("4:5");
    expect(markup).toContain("还缺提示词，先连上再运行");
    expect(markup).not.toContain("先连上参考图");
    expect(markup).not.toContain("generation_spec");
    expect(markup).toContain("运行该节点");
    expect(markup).toContain("运行到这里");
    expect(markup).not.toContain('data-image-fidelity-panel="true"');
  });

  it("lets an image node run without a reference edge", () => {
    const selectedImage = node({
      id: "image",
      node_type: "image_generation",
      title: "主图 1",
      incoming: [{
        id: "edge-prompt",
        node_id: "prompt",
        data_type: "prompt",
        role: "prompt",
        order: 0,
      }],
      config: {
        generation_spec: {
          aspect_ratio: "1:1",
          text_policy: "none",
        },
      },
    });
    const markup = renderInspector(selectedImage);
    expect(markup).not.toContain("先连上参考图");
    expect(markup).not.toContain("先连上提示词节点");
    expect(markup).toContain("运行该节点");
  });

  it("offers complete, rewrite, and replace when the document is authored", () => {
    const selected = node({
      id: "prompt",
      node_type: "image_prompt",
      title: "提示词",
      document_origin: "authored",
      config: {
        prompt: { composition: { layout: "左侧留白" } },
      },
    });
    const markup = renderInspector(selected);
    expect(markup).toContain("补全文稿");
    expect(markup).toContain("改写文稿");
    expect(markup).toContain("重新生成");
    expect(markup).toContain("data-graph-inspector-complete");
    expect(markup).toContain("data-graph-inspector-rewrite");
  });

  it("shows candidate review in the inspector without replacing the editor document", () => {
    const selected = node({
      id: "prompt",
      node_type: "image_prompt",
      title: "提示词",
      document_origin: "authored",
      pending_candidate_artifact_id: "candidate-1",
      config: { prompt: { design_goal: "人工目标" } },
    });
    const markup = renderInspector(selected);
    expect(markup).toContain("data-graph-document-candidate");
    expect(markup).toContain("正在读取建议");
  });

  it("exposes local edit for the current generation asset and keeps its node target", () => {
    const selectedImage = node({
      ...(graph.nodes.find((item) => item.id === "image") ?? {}),
      id: "image",
      node_type: "image_generation",
      preview_asset_id: "asset-current",
    });
    const markup = renderInspector(selectedImage, undefined, catalog, { preview: true, localEdit: true });

    expect(markup).toContain('data-graph-node-local-edit');
    expect(markup).toContain("局部编辑");
    expect(markup).toContain('data-image-fidelity-panel="true"');
    expect(markup).toContain("人工保真检查");
    expect(markup).toMatch(/data-fidelity-submit[^>]*disabled=""/);
  });

  it("shows pin-as-image-asset when the generation node has a current output", () => {
    const selectedImage = node({
      ...(graph.nodes.find((item) => item.id === "image") ?? {}),
      id: "image",
      node_type: "image_generation",
      preview_asset_id: "asset-current",
    });
    const withPin = renderInspector(selectedImage, undefined, catalog, { pinAsset: true });
    const withoutPin = renderInspector(selectedImage);
    expect(withPin).toContain("data-graph-pin-asset");
    expect(withPin).toContain("固定为图片素材");
    expect(withoutPin).not.toContain("data-graph-pin-asset");
  });

  it("renders localized delivery presets without catalog governance metadata", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["delivery-presets"], deliveryPresetCatalog);
    const selectedImage = node({
      ...(graph.nodes.find((item) => item.id === "image") ?? {}),
      id: "image",
      node_type: "image_generation",
      config: {
        ...(graph.nodes.find((item) => item.id === "image")?.config ?? {}),
        delivery_spec: deliveryPresetCatalog.items[0]?.delivery_spec,
      },
    });

    const markup = renderInspector(
      selectedImage,
      client,
      catalog,
      { preview: true },
    );

    expect(markup).toContain("平台预设");
    expect(markup).toContain("淘宝/天猫首屏");
    expect(markup).toContain("京东主图");
    expect(markup).toContain("封面主图");
    expect(markup).not.toContain("支持自定义");
    expect(markup).not.toContain("2026-08-24");
    expect(markup).not.toContain("docs/ARCHITECTURE.md");
    expect(markup).not.toContain("模板仅提供便捷默认值");
    expect((markup.match(/data-delivery-preset-key=/g) ?? []).length).toBe(5);
  });

  it("shows recoverable loading and error states for the preset catalog", async () => {
    const loadingMarkup = renderInspector(
      graph.nodes.find((item) => item.id === "image") ?? null,
      new QueryClient({ defaultOptions: { queries: { retry: false } } }),
      catalog,
      { preview: true },
    );
    expect(loadingMarkup).toContain("正在加载平台预设");

    const failedClient = new QueryClient({ defaultOptions: { queries: { retry: false, retryOnMount: false } } });
    await failedClient.fetchQuery({
      queryKey: ["delivery-presets"],
      queryFn: async () => {
        throw new Error("目录失效");
      },
      retry: false,
    }).catch(() => undefined);
    const errorMarkup = renderInspector(graph.nodes.find((item) => item.id === "image") ?? null, failedClient, catalog, { preview: true });
    expect(errorMarkup).toContain("目录失效");
    expect(errorMarkup).toContain("重试加载");
  });

  it("applies a preset to only the delivery draft field", () => {
    const deliverySpec: WorkflowDeliverySpec = {
      width: 1200,
      height: 1600,
      format: "png",
      max_byte_size: null,
      fit: "contain",
      background_color: null,
      crop_anchor: null,
    };
    const draft = {
      title: "未保存标题",
      config: {
        generation_spec: { aspect_ratio: "4:5" },
        provider: "keep-provider-out-of-this-action",
        visual_overlay: { style: ["clean"] },
      },
    };

    expect(applyDeliveryPresetToCatalogDraft(draft, deliverySpec)).toEqual({
      title: "未保存标题",
      config: {
        ...draft.config,
        delivery_spec: deliverySpec,
      },
    });
    expect(applyDeliveryPresetToCatalogDraft(draft, deliverySpec).config.generation_spec).toBe(draft.config.generation_spec);
    expect(applyDeliveryPresetToCatalogDraft(draft, deliverySpec).config.provider).toBe("keep-provider-out-of-this-action");
  });

  it("renders an image node inline visual overlay from catalog fields", () => {
    const markup = renderInspector(node({
      id: "image-inline-overlay",
      node_type: "image_generation",
      title: "主图 1",
      config: {
        visual_overlay: {
          style: ["暖色"],
          colors: [{ role: "background", value: "#FFFFFF" }],
        },
      },
    }));
    expect(markup).toContain("风格关键词");
    expect(markup).toContain("暖色");
    expect(markup).toContain("背景色");
  });

  it("disables catalog controls while the graph is busy", () => {
    const markup = renderInspector(graph.nodes.find((item) => item.id === "image") ?? null, undefined, catalog, { busy: true });
    expect(markup).toContain("差异指令");
    expect(markup).toContain('disabled=""');
    expect(markup).toContain('maxLength="4000"');
  });

  it("shows a recoverable catalog error with a retry action", () => {
    const markup = renderInspector(
      graph.nodes.find((item) => item.id === "image") ?? null,
      undefined,
      null,
      { catalogError: "配置暂时加载失败。" },
    );
    expect(markup).toContain('role="alert"');
    expect(markup).toContain("配置暂时加载失败。");
    expect(markup).toContain("重新加载");
  });

  it("renders a newly catalogued field without a typed inspector draft", () => {
    const nextCatalog: GraphNodeCatalog = {
      ...catalog,
      nodes: catalog.nodes.map((item) => item.node_type === "creative_brief" ? {
        ...item,
        config_fields: [
          ...(item.config_fields ?? []),
          field("campaign_line", "text", { label_key: "graph.inspector.requiredCopy" }),
        ],
      } : item),
    };
    const markup = renderInspector(node({
      id: "brief-extra",
      node_type: "creative_brief",
      title: "创作要求",
      config: { campaign_line: "主标题留下" },
    }), undefined, nextCatalog);
    expect(markup).toContain("必要文案");
    expect(markup).toContain("主标题留下");
  });

  it("renders creative brief fields and bound-but-unused state", () => {
    const brief = renderInspector(graph.nodes.find((item) => item.id === "brief") ?? null);
    expect(brief).toContain("设计目标");
    expect(brief).toContain("突出瓶身");
    expect(brief).not.toContain("JSON");

    const asset = renderInspector(graph.nodes.find((item) => item.id === "asset") ?? null);
    expect(asset).toContain("已选图，还没被用到");
    expect(asset).not.toContain("还没选图");
  });

  it("marks a product source without a binding as incomplete instead of using the workbench product", () => {
    const markup = renderInspector(graph.nodes.find((item) => item.id === "source") ?? null);
    expect(markup).toContain("尚未绑定商品资料");
    expect(markup).toContain("搜索已有商品");
    expect(markup).not.toContain("这里只展示商品信息");
  });

  it("runs visual system and creative brief nodes without treating them as sources", () => {
    const visual = renderInspector(node({
      id: "visual-run",
      node_type: "visual_system",
      title: "视觉规范",
      config_status: "incomplete",
    }));
    expect(visual).toContain("运行该节点");

    const brief = renderInspector(node({
      id: "brief-run",
      node_type: "creative_brief",
      title: "创作要求",
      config_status: "incomplete",
    }));
    expect(brief).toContain("运行该节点");

    const source = renderInspector(graph.nodes.find((item) => item.id === "source") ?? null);
    expect(source).not.toContain("运行该节点");
  });

  it("keeps inspector runs available while another graph run is queued", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["graph-runs", "p1", "g1"], {
      items: [{
        id: "run-live",
        graph_id: "g1",
        status: "queued",
        scope: "graph",
        requested_node_id: null,
        graph_revision: 4,
        failure_reason: null,
        is_retryable: false,
        started_at: "2026-08-24T00:00:00Z",
        finished_at: null,
        node_runs: [],
      }],
    });

    const markup = renderInspector(graph.nodes.find((item) => item.id === "brief") ?? null, client);

    expect(markup).toContain('data-graph-inspector-run-node="true"');
    expect(markup).not.toMatch(/data-graph-inspector-run-node="true"[^>]*disabled=""/);
    expect(markup).not.toMatch(/data-graph-inspector-run-to-node="true"[^>]*disabled=""/);
  });

  it("lets a visual system be edited as overlay fields instead of a version UUID", () => {
    const markup = renderInspector(node({
      id: "visual",
      node_type: "visual_system",
      title: "视觉规范",
      config: { visual_overlay: { style: ["干净白底"] } },
    }));
    expect(markup).toContain("风格关键词");
    expect(markup).toContain("干净白底");
    expect(markup).toContain("背景色");
    expect(markup).not.toContain("visual_system_version_id");
  });

  it("shows incoming sources as title and role on the first screen, not compiled ids", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["graph-runs", "p1", "g1"], {
      items: [{
        id: "run-1",
        graph_id: "g1",
        status: "succeeded",
        scope: "node",
        requested_node_id: "image",
        graph_revision: 4,
        failure_reason: null,
        is_retryable: false,
        started_at: "2026-08-21T00:00:00Z",
        finished_at: "2026-08-21T00:00:08Z",
        node_runs: [{
          id: "nr-1",
          node_id: "image",
          status: "succeeded",
          sort_order: 0,
          node_title: "主图 1",
          input_trace: [{
            edge_id: "edge-prompt",
            source_node_id: "brief",
            source_title: "创作要求",
            role: "brief",
            order: 0,
            artifact_id: "artifact-brief",
            artifact_type: "creative_brief",
            version_id: "version-brief",
          }],
          compiled_context: {
            incoming_edge_ids: ["edge-1"],
            fact_count: 2,
            reference_asset_ids: ["asset-a"],
            input_digest: "deadbeef",
          },
          output: null,
          failure_reason: null,
          attempt_count: 1,
          started_at: "2026-08-21T00:00:00Z",
          finished_at: "2026-08-21T00:00:08Z",
        }],
      }],
    });
    const listRun = client.getQueryData<{ items: Array<Record<string, unknown>> }>(["graph-runs", "p1", "g1"]);
    client.setQueryData(["graph-run", "p1", "g1", "run-1"], listRun?.items[0]);
    const markup = renderInspector(graph.nodes.find((item) => item.id === "image") ?? null, client);
    const firstScreen = markup.split("data-graph-technical-details")[0];
    expect(firstScreen).toContain("来自");
    expect(firstScreen).toContain("data-graph-runtime-input-used");
    expect(firstScreen).toContain("创作要求");
    expect(firstScreen).not.toContain("artifact-brief");
    expect(firstScreen).not.toContain("version-brief");
    expect(firstScreen).not.toContain("creative_brief");
    expect(firstScreen).not.toContain("asset-a");
    expect(firstScreen).not.toContain("deadbeef");
    expect(markup).toContain("data-graph-technical-details");
    expect(markup).toContain("artifact-brief");
    expect(markup).toContain("version-brief");
    expect(markup).toContain("产物类型");
    expect(markup).not.toContain("creative_brief");
    expect(markup).toContain("deadbeef");
  });

  it("shows the last generated prompt on a prompt node", () => {
    const markup = renderInspector(node({
      id: "prompt",
      node_type: "image_prompt",
      title: "首屏海报图提示词",
      config_status: "ready",
      current_artifact_payload: {
        design_goal: "为筋膜枪生成首屏海报",
        content: { background: "干净背景" },
      },
    }));
    expect(markup).toContain("上次写出");
    expect(markup).toContain("为筋膜枪生成首屏海报");
    expect(markup).toContain("干净背景");
  });

  it("shows the last failure on the open inspector", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["graph-runs", "p1", "g1"], {
      items: [{
        id: "run-9",
        graph_id: "g1",
        status: "failed",
        scope: "node",
        requested_node_id: "image",
        graph_revision: 4,
        failure_reason: "上游失败",
        is_retryable: true,
        started_at: "2026-08-21T00:00:00Z",
        finished_at: "2026-08-21T00:00:02Z",
        node_runs: [{
          id: "nr-9",
          node_id: "image",
          status: "failed",
          sort_order: 0,
          compiled_context: null,
          output: null,
          failure_reason: "模型超时",
          attempt_count: 1,
          started_at: "2026-08-21T00:00:00Z",
          finished_at: "2026-08-21T00:00:02Z",
        }],
      }],
    });
    const markup = renderInspector(graph.nodes.find((item) => item.id === "image") ?? null, client);
    expect(markup).toContain("模型超时");
    expect(markup).toContain("上次失败");
    expect(markup).toContain("重试");
  });

  it("shows requested vs measured output and uses the node spec for preview ratio", () => {
    const markup = renderInspector(node({
      id: "hero-measured",
      node_type: "image_generation",
      title: "首屏海报图 1",
      preview_asset_id: "asset-hero",
      config: {
        generation_spec: {
          aspect_ratio: "3:4",
          resolution_tier: "high",
          quality_intent: "high",
          reference_fidelity: "high",
          background_intent: "auto",
          text_policy: "required",
          text_language: "zh-CN",
        },
      },
      current_artifact_payload: {
        measured_output: {
          mime_type: "image/png",
          provider_status: "completed",
          requested_aspect_ratio: "3:4",
          measured_width: 1024,
          measured_height: 1536,
          requested_quality: "high",
          effective_parameters: {
            action: "generate",
            quality: "high",
            notes: [{ kind: "fallback", message: "供应商不支持部分参数，已按基础参数完成。" }],
          },
        },
      },
    }), undefined, catalog, { preview: true });
    expect(markup).toContain("出图结果");
    expect(markup).toContain("要求比例");
    expect(markup).toContain("3:4");
    expect(markup).toContain("1024×1536");
    expect(markup).toContain("生成");
    expect(markup).toContain("高质量");
    expect(markup).not.toContain(">generate<");
    expect(markup).toContain("供应商回退");
    expect(markup).toContain("data-preview-aspect=\"3:4\"");
    expect(markup).not.toContain("aspect-[4/3]");
    expect(markup).toContain("data-aspect-matched=\"true\"");
  });

  it("keeps a mismatched generation and shows the measured ratio instead of failing silently", () => {
    const markup = renderInspector(node({
      id: "hero-mismatch",
      node_type: "image_generation",
      title: "首屏海报图 1",
      preview_asset_id: "asset-hero",
      config: {
        generation_spec: {
          aspect_ratio: "3:4",
          resolution_tier: "high",
          quality_intent: "high",
          reference_fidelity: "high",
          background_intent: "auto",
          text_policy: "none",
        },
      },
      current_artifact_payload: {
        measured_output: {
          requested_aspect_ratio: "3:4",
          measured_width: 1448,
          measured_height: 1086,
          aspect_matched: false,
          aspect_mismatch: "供应商没有按 3:4 出图（实际 1448×1086）",
          effective_parameters: { action: "generate" },
        },
      },
    }), undefined, catalog, { preview: true });
    expect(markup).toContain("data-aspect-matched=\"false\"");
    expect(markup).toContain("未按 3:4 出图，实际 1448×1086");
    expect(markup).toContain("data-preview-aspect=\"3:4\"");
  });
});

function preset(
  key: string,
  title: string,
  aspectRatio: string,
  applicableImageType: string,
  width: number,
  height: number,
) {
  return {
    key,
    title,
    aspect_ratio: aspectRatio,
    applicable_image_type: applicableImageType,
    reviewed_at: "2026-08-24",
    source: "docs/ARCHITECTURE.md §7",
    disclaimer: "模板仅提供便捷默认值，不构成平台审核或合规保证。",
    delivery_spec: {
      width,
      height,
      format: "png" as const,
      max_byte_size: null,
      fit: "contain" as const,
      background_color: null,
      crop_anchor: null,
    },
  };
}
