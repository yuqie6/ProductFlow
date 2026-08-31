/**
 * 进程入口：先恢复本地 Turn 文件，再开始监听。
 *
 * 恢复必须在 bind 之前，避免重启后对着未恢复的存储接下新 Turn。
 * 本地恢复仍不能证明 ProductFlow 侧的工具副作用。
 */

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
  let closeAfterLockLoss: ((error: Error) => void) | undefined;
  let startupLockLoss: Error | undefined;
  const processLock = await store.acquireProcessLock((error) => {
    if (closeAfterLockLoss) closeAfterLockLoss(error);
    else startupLockLoss = error;
  });
  let manager: PiRuntimeManager | undefined;
  let server: ReturnType<typeof createHTTPServer> | undefined;
  try {
    const skills = await loadSkillCatalog();
    const productFlow = new ProductFlowClient(config.productFlowBaseURL, config.internalToken, config.requestTimeoutMS);
    const runtimeManager = new PiRuntimeManager(config, store, productFlow, skills, await store.publisherID());
    manager = runtimeManager;
    store.setEventPublisher((scope, event) => runtimeManager.publishDurableEvent(scope, event));
    // 先恢复本地文件再接流量，避免 queued Turn 丢失。
    const recovery = await runtimeManager.recoverAfterRestart();
    if (
      recovery.replayed_handoffs > 0 ||
      recovery.queued_turns > 0 ||
      recovery.deferred_turns > 0 ||
      recovery.waiting_input_turns > 0 ||
      recovery.restored_terminal_turns > 0 ||
      recovery.unknown_turns > 0
    ) {
      process.stdout.write(`Recovered Agent runtime state: ${JSON.stringify(recovery)}\n`);
    }
    const httpServer = createHTTPServer(runtimeManager, config);
    server = httpServer;
    const { host, port } = parseListenAddress(config.listenAddress);
    if (startupLockLoss) throw startupLockLoss;

    await new Promise<void>((resolve, reject) => {
      httpServer.once("error", reject);
      httpServer.listen(port, host, () => {
        httpServer.off("error", reject);
        resolve();
      });
    });
    process.stdout.write(`ProductFlow Pi Agent listening on ${host}:${port}\n`);

    let closePromise: Promise<void> | undefined;
    let failureExit = false;
    const close = (signal: string, failed = false): Promise<void> => {
      failureExit ||= failed;
      closePromise ??= (async () => {
        process.stdout.write(`Received ${signal}; stopping ProductFlow Pi Agent\n`);
        await runtimeManager.close();
        httpServer.closeAllConnections();
        await new Promise<void>((resolve) => httpServer.close(() => resolve()));
        await processLock.release();
      })();
      return closePromise;
    };
    closeAfterLockLoss = (error) => {
      process.stderr.write(`${error.message}\n`);
      void close("LOCK_LOST", true).finally(() => {
        if (failureExit) process.exitCode = 1;
      });
    };
    if (startupLockLoss) closeAfterLockLoss(startupLockLoss);
    process.once("SIGTERM", () => void close("SIGTERM").finally(() => {
      process.exitCode = failureExit ? 1 : 0;
    }));
    process.once("SIGINT", () => void close("SIGINT").finally(() => {
      process.exitCode = failureExit ? 1 : 0;
    }));
  } catch (error) {
    await manager?.close();
    if (server?.listening) {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server?.close(() => resolve()));
    }
    await processLock.release();
    throw error;
  }
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
