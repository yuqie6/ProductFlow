/* global EventTarget, performance, setTimeout, URL, location, fetch, TextEncoder, TextDecoder, AbortController, MessageEvent, clearTimeout */
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import process from "node:process";
import { log } from "node:console";
import { chromium } from "@playwright/test";
import { createServer } from "vite";

let input = "";
for await (const chunk of process.stdin) input += chunk;
const fixture = JSON.parse(input);
let disconnectEnabled = false;
let truncatedResponse;
const streamCursors = [];
const cacheDir = await mkdtemp(path.join(tmpdir(), "pf-browser-gap-"));
const server = await createServer({
  configFile: false,
  cacheDir,
  plugins: [{
    name: "agent-sse-disconnect-probe",
    configureServer(vite) {
      vite.middlewares.use(async (request, response, next) => {
        const url = new URL(request.url, "http://probe.local");
        if (url.pathname === "/__disconnect_stream") {
          if (!truncatedResponse) { response.writeHead(409).end(); return; }
          truncatedResponse.destroy();
          truncatedResponse = undefined;
          response.writeHead(204).end();
          return;
        }
        if (!disconnectEnabled || url.pathname !== fixture.cases[0].url) { next(); return; }
        streamCursors.push(Number(url.searchParams.get("after")));
        if (streamCursors.length !== 1) { next(); return; }
        const controller = new AbortController();
        response.on("close", () => controller.abort());
        try {
          const upstream = await fetch(fixture.api + request.url, {
            headers: { Cookie: request.headers.cookie ?? "" }, signal: controller.signal,
          });
          assert.equal(upstream.status, 200);
          const reader = upstream.body.getReader();
          const decoder = new TextDecoder();
          let buffered = "";
          try {
            while (!buffered.includes("\n\n")) {
              const chunk = await reader.read();
              assert.equal(chunk.done, false);
              buffered += decoder.decode(chunk.value, { stream: true });
            }
          } finally { await reader.cancel(); }
          const frame = buffered.slice(0, buffered.indexOf("\n\n") + 2);
          assert.match(frame, /id: 1\n/);
          response.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" });
          truncatedResponse = response;
          response.write(frame);
        } catch (error) { response.destroy(error); }
      });
    },
  }],
  server: { host: "127.0.0.1", port: 0, proxy: { "/api": fixture.api } },
  logLevel: "error",
});
let browser;
try {
  await server.listen();
  const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies(fixture.cookies.map((cookie) => ({ ...cookie, url: origin })));
  const page = await context.newPage();
  await page.route("**/__gap_probe", (route) => route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Gap probe</title>" }));
  await page.goto(`${origin}/__gap_probe`);
  for (const scenario of fixture.cases) {
    const result = await page.evaluate(async ({ count, runId, turnId, url }) => {
      const { subscribeToConversationEvents } = await import("/src/pages/workbench/agent/conversation/runtime.ts");
      class Source extends EventTarget {
        closed = false;
        close() { this.closed = true; }
      }
      const source = new Source();
      const received = [];
      const cursors = [];
      let bytes = 0;
      let deltas = 0;
      let stop;
      let timer;
      const started = performance.now();
      try {
        await new Promise((resolve, reject) => {
          timer = setTimeout(() => reject(new Error("gap repair exceeded 5s")), 5000);
          stop = subscribeToConversationEvents({
            url,
            scope: { run_id: runId, turn_id: turnId },
            createEventSource: () => source,
            fetchEventPage: async (pageURL, signal) => {
              cursors.push(Number(new URL(pageURL, location.origin).searchParams.get("after")));
              const response = await fetch(pageURL, { credentials: "include", signal });
              if (!response.ok) throw new Error(`event page status ${response.status}`);
              const text = await response.text();
              bytes += new TextEncoder().encode(text).length;
              return JSON.parse(text);
            },
            onProtocolError: reject,
            onEvent: (event) => {
              received.push(event.sequence);
              if (event.kind === "item.delta" && event.payload.delta === "x") deltas++;
              if (event.sequence === count) resolve();
            },
          });
          source.dispatchEvent(new MessageEvent("turn.completed", { data: JSON.stringify({
            schema_version: 1, run_id: runId, turn_id: turnId, sequence: count,
            created_at: new Date().toISOString(), kind: "turn.completed", payload: { reason: "completed" },
          }) }));
        });
        return { ms: performance.now() - started, received, cursors, bytes, deltas, closed: source.closed };
      } finally {
        clearTimeout(timer);
        stop?.();
      }
    }, scenario);
    assert.deepEqual(result.received, Array.from({ length: scenario.count }, (_, index) => index + 1));
    assert.equal(result.deltas, scenario.count - 2);
    assert.equal(result.closed, true);
    assert.deepEqual(result.cursors, Array.from({ length: Math.ceil(scenario.count / 250) }, (_, index) => index * 250));
    assert.ok(result.ms < 5000);
    log(JSON.stringify({ count: scenario.count, pages: result.cursors.length, bytes: result.bytes, repair_ms: result.ms, ordered_exactly_once: true, text_deltas: result.deltas }));
  }
  disconnectEnabled = true;
  const reconnect = await page.evaluate(async ({ runId, turnId, url }) => {
    const { subscribeToConversationEvents } = await import("/src/pages/workbench/agent/conversation/runtime.ts");
    const received = [];
    const states = [];
    const streamErrors = [];
    let started;
    let stop;
    let timer;
    try {
      await new Promise((resolve, reject) => {
        timer = setTimeout(() => reject(new Error("SSE reconnect exceeded 5s")), 5000);
        stop = subscribeToConversationEvents({
          url, scope: { run_id: runId, turn_id: turnId },
          onProtocolError: reject,
          onConnectionState: (state) => states.push(state),
          onStreamError: (error) => { if (error) streamErrors.push(error); },
          onEvent: (event) => {
            received.push(event.sequence);
            if (event.sequence === 1) {
              started = performance.now();
              void fetch("/__disconnect_stream").then((response) => {
                if (response.status !== 204) reject(new Error(`disconnect probe ${response.status}`));
              }).catch(reject);
            }
            if (event.sequence === 3) resolve();
          },
        });
      });
      return { received, states, streamErrors, reconnect_ms: performance.now() - started };
    } finally { clearTimeout(timer); stop?.(); }
  }, fixture.cases[0]);
  assert.deepEqual(streamCursors, [0, 1]);
  assert.deepEqual(reconnect.received, [1, 2, 3]);
  assert.deepEqual(reconnect.states, ["connecting", "open", "reconnecting", "connecting", "open", "closed"]);
  assert.deepEqual(reconnect.streamErrors, []);
  assert.ok(reconnect.reconnect_ms < 5000);
  log(JSON.stringify({ native_sse_disconnect: true, ...reconnect, request_cursors: streamCursors }));
} finally {
  truncatedResponse?.destroy();
  await browser?.close();
  await server.close();
  await rm(cacheDir, { recursive: true, force: true });
}
