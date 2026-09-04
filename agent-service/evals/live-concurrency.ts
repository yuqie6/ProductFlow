/** Live eval job-pool concurrency, capped at production `maxConcurrentTurns`. */

/** Matches `AGENT_MAX_CONCURRENT_TURNS` in `src/config.ts`. */
export const PRODUCTION_MAX_CONCURRENT_TURNS_DEFAULT = 3;

export function resolveLiveEvalConcurrency(
  requested: number | undefined,
  env: NodeJS.ProcessEnv = process.env,
): { concurrency: number; productionMaxConcurrentTurns: number } {
  const productionMaxConcurrentTurns = productionMaxConcurrentTurnsFrom(env);
  const requestedConcurrency = positiveInteger(
    requested ?? PRODUCTION_MAX_CONCURRENT_TURNS_DEFAULT,
    "concurrency",
  );
  return {
    concurrency: Math.min(requestedConcurrency, productionMaxConcurrentTurns),
    productionMaxConcurrentTurns,
  };
}

function productionMaxConcurrentTurnsFrom(env: NodeJS.ProcessEnv): number {
  const raw = env.AGENT_MAX_CONCURRENT_TURNS?.trim();
  if (!raw) return PRODUCTION_MAX_CONCURRENT_TURNS_DEFAULT;
  if (!/^\d+$/u.test(raw)) throw new Error("AGENT_MAX_CONCURRENT_TURNS must be a positive integer");
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error("AGENT_MAX_CONCURRENT_TURNS must be a positive integer");
  }
  return value;
}

function positiveInteger(value: number, name: string): number {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error(`${name} must be a positive integer`);
  return value;
}
