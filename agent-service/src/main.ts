import { loadConfig } from "./config.js";
import { PiRuntimeManager } from "./pi-runtime.js";
import { ProductFlowClient } from "./productflow.js";
import { loadSkillCatalog } from "./skills.js";
import { createHTTPServer } from "./server.js";
import { TurnStore } from "./store.js";

async function main(): Promise<void> {
  const config = loadConfig();
  const store = new TurnStore(config.dataRoot);
  await store.init();
  const skills = await loadSkillCatalog();
  const productFlow = new ProductFlowClient(config.productFlowBaseURL, config.internalToken, config.requestTimeoutMS);
  const manager = new PiRuntimeManager(config, store, productFlow, skills);
  store.setEventPublisher((scope, event) => manager.publishDurableEvent(scope, event));
  const recovery = await manager.recoverAfterRestart();
  if (
    recovery.queued_turns > 0 ||
    recovery.deferred_turns > 0 ||
    recovery.waiting_input_turns > 0 ||
    recovery.restored_terminal_turns > 0 ||
    recovery.unknown_turns > 0
  ) {
    process.stdout.write(`Recovered Agent runtime state: ${JSON.stringify(recovery)}\n`);
  }
  const server = createHTTPServer(manager, config);
  const { host, port } = parseListenAddress(config.listenAddress);

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, () => {
      server.off("error", reject);
      resolve();
    });
  });
  process.stdout.write(`ProductFlow Pi Agent listening on ${host}:${port}\n`);

  let closing = false;
  const close = async (signal: string) => {
    if (closing) return;
    closing = true;
    process.stdout.write(`Received ${signal}; stopping ProductFlow Pi Agent\n`);
    await manager.close();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  };
  process.once("SIGTERM", () => void close("SIGTERM").finally(() => process.exit(0)));
  process.once("SIGINT", () => void close("SIGINT").finally(() => process.exit(0)));
}

function parseListenAddress(value: string): { host: string; port: number } {
  const match = /^(.*):(\d+)$/u.exec(value.trim());
  if (!match) throw new Error("AGENT_LISTEN_ADDRESS must use host:port");
  const port = Number(match[2]);
  if (!Number.isInteger(port) || port < 1 || port > 65_535) throw new Error("AGENT_LISTEN_ADDRESS port is invalid");
  return { host: match[1] || "127.0.0.1", port };
}

void main().catch((error: unknown) => {
  process.stderr.write(`${error instanceof Error ? error.stack ?? error.message : String(error)}\n`);
  process.exitCode = 1;
});
