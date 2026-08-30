let inflight = false;

export async function withGraphRunSubmit<T>(run: () => Promise<T>): Promise<T | undefined> {
  if (inflight) return undefined;
  inflight = true;
  try {
    return await run();
  } finally {
    inflight = false;
  }
}
