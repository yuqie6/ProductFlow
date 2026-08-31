import {
  type JsonObject,
  type RuntimeContext,
  type Scope,
  type StartTurnInput,
  type ToolStepDetails,
  type ToolStepKind,
  toolKind,
} from "./contracts.js";
import { compactAskUserSummary } from "./question-resume.js";

const TOOL_STEP_SUMMARIES: Record<ToolStepKind, string> = {
  load_skill: "加载版本化 ProductFlow Skill 指令",
  inject_context: "注入本轮 ProductFlow 上下文",
  ask_question: "等待用户回答结构化问题",
  inspect_image: "检查选中的商品图片",
  propose_draft: "提交完整 ProductFlow 草案",
  inspect_context: "读取 ProductFlow 当前上下文",
  read_history: "读取有界历史信息",
  organize_assets: "整理 ProductFlow 素材",
  request_workflow_run: "请求执行工作流并等待确认",
  create_product: "创建商品工作区",
  apply_graph: "立即写入 live graph ChangeSet",
  propose_graph: "提交未应用的图提案",
  focus_canvas: "聚焦 live graph 画布选区",
  expand_intake: "写入商品 intake 并展开出生图",
  discard_proposal: "丢弃未应用的图提案",
  cancel_run: "取消进行中的工作流运行",
};

export function toolStepSummary(name: string): string {
  return TOOL_STEP_SUMMARIES[toolKind(name)];
}

export function buildContextStepDetails(
  scope: Scope,
  runtimeContext: RuntimeContext,
  pageContext: StartTurnInput["page_context"],
  selectedAssetCount: number,
  skillCatalogHash: string,
  contextBytes: number,
): ToolStepDetails {
  return {
    phase: "context_injection",
    context_sections: ["productflow_contract", "skill_catalog", "runtime_context", "page_context", "selected_assets"],
    runtime_context_keys: Object.keys(runtimeContext).sort().slice(0, 32),
    contract_fields: ["scope_type", "product_id", "current_draft_version", "skill_catalog_hash"],
    ...(pageContext ? { page_route: pageContext.route, page_type: pageContext.page_type } : {}),
    selected_asset_count: selectedAssetCount,
    ...(pageContext ? { visible_asset_count: pageContext.visible_asset_ids.length } : {}),
    context_bytes: contextBytes,
    input_summary: `注入 ${scope.scope_type} contract、Skill catalog ${skillCatalogHash.slice(0, 12)} 和当前页面摘要。`,
  };
}

export function toolStepDetailsForStart(name: string, args: unknown): ToolStepDetails {
  const argumentsObject = isRecord(args) ? args : {};
  switch (toolKind(name)) {
    case "load_skill":
      return {
        phase: "skill_load",
        skill_name: safeDetailString(argumentsObject.skill_name, 64),
        resource_path: safeDetailString(argumentsObject.resource_path, 256),
        input_summary: "读取一个精确匹配的版本化 Skill 或静态参考。",
      };
    case "ask_question": {
      const options = Array.isArray(argumentsObject.options)
        ? argumentsObject.options.flatMap((option) => {
          if (!isRecord(option)) return [];
          const label = safeDetailString(option.label, 80);
          return label ? [label] : [];
        })
        : [];
      return {
        phase: "question",
        question_header: safeDetailString(argumentsObject.header, 32),
        question_text: safeDetailString(argumentsObject.question, 2000, true),
        ...(options.length ? { option_labels: options.slice(0, 5) } : {}),
        input_summary: "向用户提出一个会影响结果的结构化选择。",
      };
    }
    case "inspect_context":
      return { phase: "tool_result", input_summary: "读取当前 ProductFlow 商品、事实、草案或运行上下文。" };
    case "inspect_image":
      return { phase: "tool_result", input_summary: "读取明确选中的已核验图片信息。" };
    case "propose_draft":
      return { phase: "tool_result", input_summary: "提交完整草案，由 ProductFlow Schema 和业务规则校验。" };
    case "read_history":
      return { phase: "tool_result", input_summary: "读取有界的历史摘要或归档信息。" };
    case "organize_assets":
      return { phase: "tool_result", input_summary: "准备或执行受限的素材整理操作。" };
    case "request_workflow_run":
      return { phase: "tool_result", input_summary: "准备工作流执行请求，等待用户确认。" };
    case "create_product":
      return { phase: "tool_result", input_summary: "创建商品工作区。" };
    case "apply_graph":
      return { phase: "tool_result", input_summary: "立即把 Graph Command 写入 live graph。" };
    case "propose_graph":
      return { phase: "tool_result", input_summary: "提交未应用的图提案，等待确认。" };
    case "focus_canvas":
      return { phase: "tool_result", input_summary: "请求画布聚焦到指定节点、边或分组。" };
    case "expand_intake":
      return { phase: "tool_result", input_summary: "写入商品 intake 并展开摄影/信息图模板。" };
    case "discard_proposal":
      return { phase: "tool_result", input_summary: "丢弃当前未应用的图提案。" };
    case "cancel_run":
      return { phase: "tool_result", input_summary: "取消仍在运行的工作流。" };
    case "inject_context":
      return { phase: "context_injection" };
    default:
      return { phase: "tool_result", input_summary: `执行 ProductFlow 工具 ${name}。` };
  }
}

export function toolStepDetailsForResult(name: string, result: unknown, isError: boolean): ToolStepDetails | undefined {
  const resultObject = isRecord(result) ? result : {};
  const resultDetails = isRecord(resultObject.details) ? resultObject.details : {};
  const kind = toolKind(name);
  if (isError) {
    return {
      phase: kind === "ask_question" ? "question" : kind === "load_skill" ? "skill_load" : "tool_result",
      output_summary: "工具调用失败，详情见错误信息。",
    };
  }
  const journalMeta = projectJournalMeta(resultDetails);
  switch (kind) {
    case "load_skill": {
      const instructionExcerpt = safeDetailString(resultDetails.instruction_excerpt, 12_000, true);
      return {
        ...journalMeta,
        phase: "skill_load",
        ...(safeDetailString(resultDetails.skill_name, 64) ? { skill_name: safeDetailString(resultDetails.skill_name, 64) } : {}),
        ...(safeDetailString(resultDetails.resource_path, 256)
          ? { resource_path: safeDetailString(resultDetails.resource_path, 256) }
          : {}),
        ...(instructionExcerpt ? { instruction_excerpt: instructionExcerpt } : {}),
        ...(typeof resultDetails.instruction_truncated === "boolean"
          ? { instruction_truncated: resultDetails.instruction_truncated }
          : {}),
        output_summary: "已加载版本化 Skill 指令；完整内容已提供给模型。",
      };
    }
    case "ask_question":
      return {
        ...journalMeta,
        phase: "question",
        ...(safeDetailString(resultDetails.question_id, 120)
          ? { question_id: safeDetailString(resultDetails.question_id, 120) }
          : {}),
        output_summary: compactAskUserSummary(result, []),
      };
    case "inspect_context": {
      const includesNodeCatalog = name === "get_product_workflow_context_v1" || name === "inspect_global_workflow_context_v1";
      return {
        ...journalMeta,
        phase: "tool_result",
        ...(includesNodeCatalog
          ? { context_sections: ["product_facts", "intake", "live_graph", "verified_reference_assets", "node_catalog"] }
          : {}),
        output_summary: includesNodeCatalog
          ? "已读取当前商品事实、intake、live graph、参考资产和 Node Catalog config_fields；Inspector 与节点配置写入以此为唯一来源。"
          : "已读取有界 ProductFlow 上下文。",
      };
    }
    case "inspect_image":
      return { ...journalMeta, phase: "tool_result", output_summary: "已读取选中图片的有界检查结果。" };
    case "propose_draft":
      return { ...journalMeta, phase: "tool_result", output_summary: "后端已接受完整草案，当前等待用户确认。" };
    case "read_history":
      return { ...journalMeta, phase: "tool_result", output_summary: "已读取有界历史摘要。" };
    case "organize_assets":
      return { ...journalMeta, phase: "tool_result", output_summary: "素材操作已返回 ProductFlow 结果。" };
    case "request_workflow_run":
      return { ...journalMeta, phase: "tool_result", output_summary: "执行请求已准备，当前等待用户确认。" };
    case "create_product":
      return { ...journalMeta, phase: "tool_result", output_summary: "商品工作区创建结果已返回。" };
    case "apply_graph":
      return { ...journalMeta, phase: "tool_result", output_summary: "Graph Command 已写入 live graph。" };
    case "propose_graph":
      return { ...journalMeta, phase: "tool_result", output_summary: "图提案已作为未应用幽灵预览提交。" };
    case "focus_canvas":
      return { ...journalMeta, phase: "tool_result", output_summary: "画布聚焦请求已记录。" };
    case "expand_intake":
      return { ...journalMeta, phase: "tool_result", output_summary: "商品 intake 已写入，出生图已展开。" };
    case "discard_proposal":
      return { ...journalMeta, phase: "tool_result", output_summary: "未应用的图提案已丢弃。" };
    case "cancel_run":
      return { ...journalMeta, phase: "tool_result", output_summary: "工作流取消请求已提交。" };
    case "inject_context":
      return { ...journalMeta, phase: "context_injection", output_summary: "上下文已注入模型会话。" };
  }
}

function projectJournalMeta(details: Record<string, unknown>): ToolStepDetails {
  const projected: ToolStepDetails = {};
  if (typeof details.truncated === "boolean") projected.truncated = details.truncated;
  if (typeof details.pending_confirmation === "boolean") projected.pending_confirmation = details.pending_confirmation;
  if (typeof details.reconciled === "boolean") projected.reconciled = details.reconciled;
  if (details.response_format === "concise" || details.response_format === "detailed") projected.response_format = details.response_format;
  copyJournalInteger(projected, details, "item_count", 128);
  copyJournalInteger(projected, details, "node_count", 10_000);
  copyJournalInteger(projected, details, "group_count", 10_000);
  copyJournalInteger(projected, details, "asset_count", 100);
  copyJournalInteger(projected, details, "expected_workflow_revision", 1_000_000, 1);
  for (const [source, target, maximum] of [
    ["workflow_id", "workflow_id", 64],
    ["workflow_title", "workflow_title", 240],
    ["summary", "summary", 240],
    ["run_id", "run_id", 64],
    ["proposal_id", "proposal_id", 64],
    ["product_id", "product_id", 64],
    ["request_id", "request_id", 64],
    ["artifact_name", "artifact_name", 120],
  ] as const) {
    const value = safeDetailString(details[source], maximum);
    if (value) projected[target] = value;
  }
  if (typeof details.product_workspace_created === "boolean") projected.product_workspace_created = details.product_workspace_created;
  const operations = journalStringList(details.operation_summaries, 16, 160, false);
  if (operations) projected.operation_summaries = operations;
  const nodes = journalStringList(details.affected_node_ids, 20, 80, true);
  if (nodes) projected.affected_node_ids = nodes;
  const edges = journalStringList(details.affected_edge_ids, 20, 80, true);
  if (edges) projected.affected_edge_ids = edges;
  const groups = journalStringList(details.affected_group_ids, 20, 80, true);
  if (groups) projected.affected_group_ids = groups;
  return projected;
}

function copyJournalInteger(
  target: ToolStepDetails,
  details: Record<string, unknown>,
  key: "item_count" | "node_count" | "group_count" | "asset_count" | "expected_workflow_revision",
  maximum: number,
  minimum = 0,
): void {
  const value = details[key];
  if (typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum) target[key] = value;
}

function journalStringList(value: unknown, maxItems: number, maxLength: number, unique: boolean): string[] | undefined {
  if (!Array.isArray(value) || value.length === 0) return undefined;
  const items = value.flatMap((item) => {
    if (typeof item !== "string") return [];
    const trimmed = item.trim();
    if (!trimmed || trimmed.length > maxLength || /[\r\n]/u.test(trimmed)) return [];
    return [trimmed];
  }).slice(0, maxItems);
  const result = unique ? [...new Set(items)] : items;
  return result.length ? result : undefined;
}

export function resultMetaFromToolResult(result: unknown): JsonObject | undefined {
  if (!isRecord(result) || !isRecord(result.details)) return undefined;
  const projected = projectJournalMeta(result.details);
  return Object.keys(projected).length ? projected as JsonObject : undefined;
}

export function mergeToolStepDetails(
  started: ToolStepDetails | undefined,
  result: ToolStepDetails | undefined,
  failure: ToolStepDetails | undefined,
): ToolStepDetails | undefined {
  if (!started && !result && !failure) return undefined;
  const phase = started?.phase ?? result?.phase ?? failure?.phase;
  return { ...started, ...result, ...failure, ...(phase ? { phase } : {}) };
}

function safeDetailString(value: unknown, maximum: number, allowNewline = false): string | undefined {
  if (typeof value !== "string" || !value.trim() || value.length > maximum || (!allowNewline && /[\r\n]/u.test(value))) return undefined;
  return value;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
