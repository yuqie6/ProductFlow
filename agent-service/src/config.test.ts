import { afterEach, describe, expect, it, vi } from "vitest";
import { loadConfig } from "./config.js";

afterEach(() => vi.unstubAllEnvs());

describe("evolution trace configuration", () => {
  it("defaults to disabled without requiring or resolving a storage root", () => {
    vi.stubEnv("AGENT_SERVICE_INTERNAL_TOKEN", "a".repeat(32));
    vi.stubEnv("AGENT_EVOLUTION_TRACES", "");
    vi.stubEnv("STORAGE_ROOT", "");
    expect(loadConfig().evolutionTraceRoot).toBeUndefined();
  });

  it("requires an explicit switch and absolute storage root", () => {
    vi.stubEnv("AGENT_SERVICE_INTERNAL_TOKEN", "a".repeat(32));
    vi.stubEnv("AGENT_EVOLUTION_TRACES", "true");
    expect(() => loadConfig()).toThrow(/must be 0 or 1/);
    vi.stubEnv("AGENT_EVOLUTION_TRACES", "1");
    vi.stubEnv("STORAGE_ROOT", "./relative");
    expect(() => loadConfig()).toThrow(/absolute STORAGE_ROOT/);
    vi.stubEnv("STORAGE_ROOT", "/tmp/productflow-config-test");
    expect(loadConfig().evolutionTraceRoot).toBe("/tmp/productflow-config-test/agent-evolution-traces");
  });
});
