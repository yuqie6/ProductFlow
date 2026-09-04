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

export async function submitAfterSuccessfulFlush(
  flush: () => Promise<unknown>,
  submit: () => Promise<unknown>,
): Promise<boolean> {
  try {
    await flush();
  } catch {
    return false;
  }
  await submit();
  return true;
}
