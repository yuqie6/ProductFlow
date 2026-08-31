/**
 * ProductFlow 与本 Pi 进程之间的内部 HTTP 适配。
 *
 * 写操作需要共享内部 token。只有 `/healthz` 不鉴权。
 * conversation 与 task 路径共用同一套 Turn 运行时；查找依据是 ProductFlow contract，不是本地文件身份。
 */

import { timingSafeEqual } from "node:crypto";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { byteLength, PageContext, StartTurnInput, TurnAnswer, validatePageContext } from "./contracts.js";
import { Config } from "./config.js";
import { ProductFlowError } from "./contracts.js";
import { renderMetrics } from "./metrics.js";
import { PiRuntimeManager, RuntimeLookup } from "./pi-runtime.js";
import { RuntimeError } from "./store.js";

const MAX_INPUT_TEXT_CHARS = 20_000;
const MAX_INPUT_ASSETS = 6;

export function createHTTPServer(manager: PiRuntimeManager, config: Config): Server {
  return createServer((request, response) => {
    void handleRequest(manager, config, request, response).catch((error: unknown) => {
      if (!response.headersSent) writeError(response, error);
      else response.destroy();
    });
  });
}

async function handleRequest(
  manager: PiRuntimeManager,
  config: Config,
  request: IncomingMessage,
  response: ServerResponse,
): Promise<void> {
  const url = new URL(request.url ?? "/", `http://${request.headers.host ?? "localhost"}`);
  if (url.pathname === "/healthz" && request.method === "GET") {
    writeJSON(response, 200, manager.health());
    return;
  }
  if (url.pathname === "/metrics" && request.method === "GET") {
    if (!config.internalToken) {
      writeJSON(response, 404, { error: { code: "not_found", message: "Agent route does not exist" } });
      return;
    }
    if (!authorized(request, config.internalToken)) {
      writeJSON(response, 401, { error: { code: "unauthorized", message: "invalid internal Agent token" } });
      return;
    }
    const health = manager.health();
    response.statusCode = 200;
    response.setHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8");
    response.end(renderMetrics({
      activeTurns: health.active_turns,
      queuedTurns: health.queued_turns,
      backgroundResumable: false,
    }));
    return;
  }
  if (!authorized(request, config.internalToken)) {
    writeJSON(response, 401, { error: { code: "unauthorized", message: "invalid internal Agent token" } });
    return;
  }
  const route = parseRoute(url.pathname);
  if (!route) {
    writeJSON(response, 404, { error: { code: "not_found", message: "Agent route does not exist" } });
    return;
  }
  const lookup: RuntimeLookup = route.kind === "conversation" ? { conversationID: route.scopeID } : { taskID: route.scopeID };
  if (route.action === "start" && request.method === "POST") {
    const body = parseStartInput(await readJSON(request, config.maxBodyBytes));
    const state = await manager.start({ lookup, input: body.input, turnID: body.turnID });
    response.setHeader("Location", `${url.pathname}/${encodeURIComponent(state.turn_id)}`);
    writeJSON(response, 202, state);
    return;
  }
  if (!route.turnID) {
    writeJSON(response, 405, { error: { code: "method_not_allowed", message: "method not allowed" } });
    return;
  }
  if (route.action === "get" && request.method === "GET") {
    writeJSON(response, 200, await manager.get(lookup, route.turnID));
    return;
  }
  if (route.action === "cancel" && request.method === "POST") {
    writeJSON(response, 200, await manager.cancel(lookup, route.turnID));
    return;
  }
  if (route.action === "resume" && request.method === "POST") {
    writeJSON(response, 200, await manager.resume(lookup, route.turnID));
    return;
  }
  if (route.action === "answer" && request.method === "POST" && route.questionID) {
    const body = parseAnswer(await readJSON(request, config.maxBodyBytes));
    writeJSON(response, 200, await manager.answerQuestion(lookup, route.turnID, route.questionID, body));
    return;
  }
  writeJSON(response, 405, { error: { code: "method_not_allowed", message: "method not allowed" } });
}

/** `/internal/v1/{conversations|tasks}/:id/...`，按 ProductFlow scope 查找。 */
function parseRoute(pathname: string): Route | null {
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length < 5 || segments[0] !== "internal" || segments[1] !== "v1") return null;
  const kind = segments[2];
  if (kind !== "conversations" && kind !== "tasks") return null;
  if (segments[4] !== "turns") return null;
  const scopeID = decodeSegment(segments[3]);
  if (!scopeID) return null;
  if (segments.length === 5) return { kind: kind === "conversations" ? "conversation" : "task", scopeID, action: "start" };
  const turnID = decodeSegment(segments[5]);
  if (!turnID) return null;
  if (segments.length === 6) return { kind: kind === "conversations" ? "conversation" : "task", scopeID, turnID, action: "get" };
  if (segments.length === 7 && ["cancel", "resume"].includes(segments[6])) {
    return { kind: kind === "conversations" ? "conversation" : "task", scopeID, turnID, action: segments[6] as RouteAction };
  }
  if (segments.length === 9 && segments[6] === "questions" && segments[8] === "answer") {
    const questionID = decodeSegment(segments[7]);
    if (!questionID) return null;
    return { kind: kind === "conversations" ? "conversation" : "task", scopeID, turnID, questionID, action: "answer" };
  }
  return null;
}

type RouteAction = "start" | "get" | "cancel" | "resume" | "answer";
interface Route {
  kind: "conversation" | "task";
  scopeID: string;
  turnID?: string;
  questionID?: string;
  action: RouteAction;
}

function parseStartInput(value: unknown): { input: StartTurnInput; turnID?: string } {
  const body = object(value, "request body");
  rejectUnknown(body, ["input_text", "asset_ids", "idempotency_key", "page_context", "turn_id"]);
  const inputText = stringValue(body.input_text, "input_text").trim();
  const idempotencyKey = stringValue(body.idempotency_key, "idempotency_key").trim();
  if (!inputText || inputText.length > MAX_INPUT_TEXT_CHARS) throw new RuntimeError(400, "invalid_argument", "input_text is required and must be at most 20000 characters");
  if (!idempotencyKey || byteLength(idempotencyKey) > 200) throw new RuntimeError(400, "invalid_argument", "idempotency_key is required and must be at most 200 bytes");
  const turnID = body.turn_id === undefined ? undefined : stringValue(body.turn_id, "turn_id").trim();
  if (turnID !== undefined && (!turnID || byteLength(turnID) > 120)) {
    throw new RuntimeError(400, "invalid_argument", "turn_id is invalid");
  }
  const rawAssetIDs = body.asset_ids === undefined ? [] : arrayValue(body.asset_ids, "asset_ids");
  if (rawAssetIDs.length > MAX_INPUT_ASSETS) throw new RuntimeError(400, "invalid_argument", `asset_ids cannot contain more than ${MAX_INPUT_ASSETS} values`);
  const assetIDs = rawAssetIDs.map((value, index) => {
    const assetID = stringValue(value, `asset_ids[${index}]`).trim();
    if (!assetID || byteLength(assetID) > 64) throw new RuntimeError(400, "invalid_argument", "asset_ids contain an invalid ID");
    return assetID;
  });
  if (new Set(assetIDs).size !== assetIDs.length) throw new RuntimeError(400, "invalid_argument", "asset_ids cannot contain duplicates");
  const pageContext = body.page_context === undefined || body.page_context === null ? null : parsePageContext(body.page_context);
  return {
    input: { input_text: inputText, asset_ids: assetIDs, idempotency_key: idempotencyKey, page_context: pageContext },
    ...(turnID === undefined ? {} : { turnID }),
  };
}

function parsePageContext(value: unknown): PageContext {
  const body = object(value, "page_context");
  rejectUnknown(body, [
    "snapshot_id",
    "route",
    "page_type",
    "product_id",
    "workflow_id",
    "selected_asset_ids",
    "visible_asset_ids",
    "filters",
    "workflow_revision",
    "library_revision",
    "digest",
    "captured_at",
  ]);
  const result: PageContext = {
    snapshot_id: stringValue(body.snapshot_id, "page_context.snapshot_id"),
    route: stringValue(body.route, "page_context.route"),
    page_type: stringValue(body.page_type, "page_context.page_type"),
    product_id: nullableString(body.product_id, "page_context.product_id"),
    workflow_id: nullableString(body.workflow_id, "page_context.workflow_id"),
    selected_asset_ids: stringArray(body.selected_asset_ids, "page_context.selected_asset_ids"),
    visible_asset_ids: stringArray(body.visible_asset_ids, "page_context.visible_asset_ids"),
    filters: stringMap(body.filters, "page_context.filters"),
    workflow_revision: nullableInteger(body.workflow_revision, "page_context.workflow_revision"),
    library_revision: nullableInteger(body.library_revision, "page_context.library_revision"),
    digest: stringValue(body.digest, "page_context.digest"),
    captured_at: stringValue(body.captured_at, "page_context.captured_at"),
  };
  if (new Set(result.selected_asset_ids).size !== result.selected_asset_ids.length || new Set(result.visible_asset_ids).size !== result.visible_asset_ids.length) {
    throw new RuntimeError(400, "invalid_argument", "page_context asset IDs cannot contain duplicates");
  }
  try {
    validatePageContext(result);
  } catch (error) {
    throw new RuntimeError(400, "invalid_argument", error instanceof Error ? error.message : "invalid page_context");
  }
  return result;
}

function parseAnswer(value: unknown): TurnAnswer {
  const body = object(value, "request body");
  rejectUnknown(body, ["answer"]);
  const answer = object(body.answer, "answer");
  rejectUnknown(answer, ["option", "text", "skip"]);
  if (answer.skip === true) {
    if (answer.option !== undefined || answer.text !== undefined) {
      throw new RuntimeError(400, "invalid_argument", "skip cannot be combined with option or text");
    }
    return { skip: true };
  }
  if (answer.skip !== undefined && answer.skip !== false) {
    throw new RuntimeError(400, "invalid_argument", "answer.skip must be true when provided");
  }
  const hasOption = answer.option !== undefined;
  const text = answer.text === undefined ? "" : stringValue(answer.text, "answer.text").trim();
  if (hasOption === Boolean(text)) throw new RuntimeError(400, "invalid_argument", "answer must contain exactly one of option, text, or skip");
  if (hasOption) {
    if (!Number.isSafeInteger(answer.option) || Number(answer.option) < 0) throw new RuntimeError(400, "invalid_argument", "answer.option must be a non-negative integer");
    return { option: Number(answer.option) };
  }
  if (byteLength(text) > 4000) throw new RuntimeError(400, "invalid_argument", "answer.text must be at most 4000 bytes");
  return { text };
}

async function readJSON(request: IncomingMessage, maxBytes: number): Promise<unknown> {
  const chunks: Buffer[] = [];
  let total = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    total += buffer.byteLength;
    if (total > maxBytes) throw new RuntimeError(413, "body_too_large", "request body exceeds the limit");
    chunks.push(buffer);
  }
  if (chunks.length === 0) throw new RuntimeError(400, "invalid_json", "request body is required");
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    throw new RuntimeError(400, "invalid_json", "request body is not valid JSON");
  }
}

function authorized(request: IncomingMessage, expected: string): boolean {
  const actual = request.headers.authorization ?? "";
  const prefix = "Bearer ";
  if (!actual.startsWith(prefix)) return false;
  const actualBytes = Buffer.from(actual.slice(prefix.length));
  const expectedBytes = Buffer.from(expected);
  return actualBytes.length === expectedBytes.length && timingSafeEqual(actualBytes, expectedBytes);
}

function object(value: unknown, name: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new RuntimeError(400, "invalid_argument", `${name} must be an object`);
  return value as Record<string, unknown>;
}

function rejectUnknown(value: Record<string, unknown>, allowed: string[]): void {
  const unknown = Object.keys(value).filter((key) => !allowed.includes(key));
  if (unknown.length > 0) throw new RuntimeError(400, "invalid_argument", `unknown request field: ${unknown[0]}`);
}

function stringValue(value: unknown, name: string): string {
  if (typeof value !== "string") throw new RuntimeError(400, "invalid_argument", `${name} must be a string`);
  return value;
}

function arrayValue(value: unknown, name: string): unknown[] {
  if (!Array.isArray(value)) throw new RuntimeError(400, "invalid_argument", `${name} must be an array`);
  return value;
}

function stringArray(value: unknown, name: string): string[] {
  return arrayValue(value, name).map((item, index) => stringValue(item, `${name}[${index}]`));
}

function nullableString(value: unknown, name: string): string | null {
  return value === null ? null : stringValue(value, name);
}

function nullableInteger(value: unknown, name: string): number | null {
  if (value === null) return null;
  if (!Number.isSafeInteger(value)) throw new RuntimeError(400, "invalid_argument", `${name} must be an integer or null`);
  return Number(value);
}

function stringMap(value: unknown, name: string): Record<string, string> {
  const record = object(value, name);
  return Object.fromEntries(Object.entries(record).map(([key, item]) => [key, stringValue(item, `${name}.${key}`)]));
}

function decodeSegment(value: string): string | null {
  try {
    return decodeURIComponent(value);
  } catch {
    return null;
  }
}

function writeJSON(response: ServerResponse, status: number, value: unknown): void {
  const body = JSON.stringify(value);
  response.statusCode = status;
  response.setHeader("Content-Type", "application/json; charset=utf-8");
  response.setHeader("Content-Length", Buffer.byteLength(body));
  response.end(body);
}

function writeError(response: ServerResponse, error: unknown): void {
  const mapped = mapError(error);
  writeJSON(response, mapped.status, { error: { code: mapped.code, message: mapped.message } });
}

function mapError(error: unknown): { status: number; code: string; message: string } {
  if (error instanceof RuntimeError) return { status: error.status, code: error.code, message: error.message };
  if (error instanceof ProductFlowError) return { status: error.status, code: `productflow_${error.code}`, message: error.message };
  if (error instanceof Error) return { status: 500, code: "internal_error", message: "Agent runtime failed" };
  return { status: 500, code: "internal_error", message: "Agent runtime failed" };
}
