/**
 * 来自环境变量的 Agent-service 配置。这里的供应商密钥只给 Pi 模型运行时用；
 * 商品、图和任务的业务权威仍是 ProductFlow。
 */

import { resolve } from "node:path";

export interface Config {
  listenAddress: string;
  dataRoot: string;
  productFlowBaseURL: string;
  internalToken: string;
  requestTimeoutMS: number;
  providerRequestTimeoutMS: number;
  eventPollIntervalMS: number;
  heartbeatIntervalMS: number;
  maxBodyBytes: number;
  maxIterations: number;
  modelContextWindow: number;
  autoCompactTokenLimit: number;
  maxConcurrentTurns: number;
  providerAPIKey: string;
  providerBaseURL: string | null;
  providerModel: string | null;
  providerReasoningEffort: string | null;
  providerReasoningSummary: string | null;
  providerTextVerbosity: string | null;
  providerServiceTier: string | null;
}

function env(key: string, fallback = ""): string {
  return process.env[key]?.trim() || fallback;
}

function positiveInt(key: string, fallback: number): number {
  const raw = env(key);
  if (!raw) return fallback;
  if (!/^\d+$/u.test(raw)) throw new Error(`${key} must be a positive integer`);
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(`${key} must be a positive integer`);
  return value;
}

function durationMS(key: string, fallback: number): number {
  const raw = env(key);
  if (!raw) return fallback;
  const match = /^(\d+(?:\.\d+)?)(ms|s|m)$/u.exec(raw);
  if (!match) throw new Error(`${key} must use ms, s, or m`);
  const amount = Number(match[1]);
  const multiplier = match[2] === "ms" ? 1 : match[2] === "s" ? 1000 : 60_000;
  const value = Math.round(amount * multiplier);
  if (!Number.isFinite(value) || value <= 0) throw new Error(`${key} must be positive`);
  return value;
}

export function loadConfig(): Config {
  const productFlowBaseURL = env("PRODUCTFLOW_INTERNAL_BASE_URL", "http://127.0.0.1:29282").replace(/\/$/u, "");
  if (!/^https?:\/\/[^/]+$/u.test(productFlowBaseURL)) {
    throw new Error("PRODUCTFLOW_INTERNAL_BASE_URL must be an absolute HTTP(S) URL without a path");
  }
  const internalToken = env("AGENT_SERVICE_INTERNAL_TOKEN");
  if (internalToken.length < 32) throw new Error("AGENT_SERVICE_INTERNAL_TOKEN must contain at least 32 characters");
  const modelContextWindow = positiveInt("AGENT_MODEL_CONTEXT_WINDOW", 128_000);
  const autoCompactTokenLimit = positiveInt("AGENT_AUTO_COMPACT_TOKEN_LIMIT", 96_000);
  if (autoCompactTokenLimit >= modelContextWindow) {
    throw new Error("AGENT_AUTO_COMPACT_TOKEN_LIMIT must be below AGENT_MODEL_CONTEXT_WINDOW");
  }
  const eventPollIntervalMS = durationMS("AGENT_EVENT_POLL_INTERVAL", 100);
  const heartbeatIntervalMS = durationMS("AGENT_HEARTBEAT_INTERVAL", 15_000);
  if (heartbeatIntervalMS < eventPollIntervalMS) throw new Error("AGENT_HEARTBEAT_INTERVAL must not be shorter than poll interval");
  const dataRoot = resolve(env("AGENT_DATA_ROOT", "./data"));
  return {
    listenAddress: env("AGENT_LISTEN_ADDRESS", "127.0.0.1:29284"),
    dataRoot,
    productFlowBaseURL,
    internalToken,
    requestTimeoutMS: durationMS("PRODUCTFLOW_REQUEST_TIMEOUT", 30_000),
    providerRequestTimeoutMS: durationMS("AGENT_PROVIDER_REQUEST_TIMEOUT", 300_000),
    eventPollIntervalMS,
    heartbeatIntervalMS,
    maxBodyBytes: positiveInt("AGENT_MAX_BODY_BYTES", 96 << 20),
    maxIterations: positiveInt("AGENT_MAX_ITERATIONS", 40),
    modelContextWindow,
    autoCompactTokenLimit,
    maxConcurrentTurns: positiveInt("AGENT_MAX_CONCURRENT_TURNS", 3),
    providerAPIKey: env("AGENT_PROVIDER_API_KEY"),
    providerBaseURL: env("AGENT_PROVIDER_BASE_URL") || null,
    providerModel: env("AGENT_PROVIDER_MODEL") || null,
    providerReasoningEffort: env("AGENT_PROVIDER_REASONING_EFFORT") || null,
    providerReasoningSummary: env("AGENT_PROVIDER_REASONING_SUMMARY") || null,
    providerTextVerbosity: env("AGENT_PROVIDER_TEXT_VERBOSITY") || null,
    providerServiceTier: env("AGENT_PROVIDER_SERVICE_TIER") || null,
  };
}
