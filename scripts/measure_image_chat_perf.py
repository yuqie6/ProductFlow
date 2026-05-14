#!/usr/bin/env python3
"""Measure ImageChatPage refresh and session switching performance.

This intentionally uses only stdlib + websocket-client, which is already
available in the local Python environment, so the measurement loop does not add
frontend dependencies.
"""

from __future__ import annotations

import argparse
import json
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import websocket


MAIN_IMAGE_READY_JS = r"""
(() => {
  const imgs = Array.from(document.querySelectorAll('section img[src*="/api/image-session-assets/"]'));
  const candidates = imgs
    .map((img) => {
      const rect = img.getBoundingClientRect();
      return {
        src: img.currentSrc || img.src,
        complete: img.complete,
        naturalWidth: img.naturalWidth,
        naturalHeight: img.naturalHeight,
        width: rect.width,
        height: rect.height,
        area: rect.width * rect.height,
      };
    })
    .filter((item) => item.complete && item.naturalWidth > 0 && item.width >= 120 && item.height >= 120)
    .sort((a, b) => b.area - a.area);
  return candidates[0] || null;
})()
"""

SESSION_BUTTONS_JS = r"""
(() => Array.from(document.querySelectorAll('aside button'))
  .map((button, index) => {
    const text = (button.textContent || '').replace(/\s+/g, ' ').trim();
    const rect = button.getBoundingClientRect();
    return { index, text, x: rect.left + rect.width / 2, y: rect.top + rect.height / 2, width: rect.width, height: rect.height };
  })
  .filter((item) => item.width > 120 && item.height > 40 && /轮|round/i.test(item.text))
)()
"""

PERFORMANCE_JS = r"""
(() => {
  const nav = performance.getEntriesByType('navigation')[0];
  return {
    domContentLoaded: nav ? nav.domContentLoadedEventEnd : null,
    load: nav ? nav.loadEventEnd : null,
    transferSize: performance.getEntriesByType('resource').reduce((sum, entry) => sum + (entry.transferSize || 0), 0),
  };
})()
"""


@dataclass
class NetworkRequest:
  url: str
  resource_type: str
  start_ms: float
  end_ms: float | None = None
  encoded_bytes: int = 0
  failed: bool = False

  @property
  def duration_ms(self) -> float | None:
    if self.end_ms is None:
      return None
    return self.end_ms - self.start_ms


@dataclass
class CdpClient:
  ws_url: str
  ws: websocket.WebSocket = field(init=False)
  next_id: int = 1
  pending: dict[int, dict[str, Any]] = field(default_factory=dict)
  events: list[dict[str, Any]] = field(default_factory=list)
  requests: dict[str, NetworkRequest] = field(default_factory=dict)

  def __post_init__(self) -> None:
    self.ws = websocket.create_connection(self.ws_url, timeout=10)

  def close(self) -> None:
    self.ws.close()

  def send(self, method: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
    message_id = self.next_id
    self.next_id += 1
    self.ws.send(json.dumps({"id": message_id, "method": method, "params": params or {}}))
    while True:
      message = json.loads(self.ws.recv())
      if message.get("id") == message_id:
        if "error" in message:
          raise RuntimeError(f"{method} failed: {message['error']}")
        return message.get("result", {})
      self._handle_event(message)

  def drain(self, timeout_ms: int = 100) -> None:
    original_timeout = self.ws.gettimeout()
    self.ws.settimeout(timeout_ms / 1000)
    try:
      while True:
        try:
          message = json.loads(self.ws.recv())
        except TimeoutError:
          return
        except websocket.WebSocketTimeoutException:
          return
        self._handle_event(message)
    finally:
      self.ws.settimeout(original_timeout)

  def _handle_event(self, message: dict[str, Any]) -> None:
    method = message.get("method")
    params = message.get("params", {})
    if not method:
      return
    self.events.append(message)
    now_ms = time.perf_counter() * 1000
    if method == "Network.requestWillBeSent":
      self.requests[params["requestId"]] = NetworkRequest(
        url=params.get("request", {}).get("url", ""),
        resource_type=params.get("type", ""),
        start_ms=now_ms,
      )
    elif method == "Network.loadingFinished":
      request = self.requests.get(params.get("requestId"))
      if request:
        request.end_ms = now_ms
        request.encoded_bytes = int(params.get("encodedDataLength") or 0)
    elif method == "Network.loadingFailed":
      request = self.requests.get(params.get("requestId"))
      if request:
        request.end_ms = now_ms
        request.failed = True

  def evaluate(self, expression: str, await_promise: bool = False) -> Any:
    result = self.send(
      "Runtime.evaluate",
      {
        "expression": expression,
        "returnByValue": True,
        "awaitPromise": await_promise,
      },
    )
    remote = result.get("result", {})
    if "value" in remote:
      return remote["value"]
    return None


def wait_for(predicate, timeout_s: float, interval_s: float = 0.05) -> tuple[Any, float]:
  start = time.perf_counter()
  last_value = None
  while time.perf_counter() - start < timeout_s:
    last_value = predicate()
    if last_value:
      return last_value, (time.perf_counter() - start) * 1000
    time.sleep(interval_s)
  return last_value, (time.perf_counter() - start) * 1000


def launch_chrome(width: int, height: int) -> tuple[subprocess.Popen[str], str, Path]:
  user_data_dir = Path(tempfile.mkdtemp(prefix="productflow-perf-chrome-"))
  with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
  chrome = subprocess.Popen(
    [
      "google-chrome",
      "--headless=new",
      "--disable-gpu",
      "--no-sandbox",
      f"--remote-debugging-port={port}",
      "--remote-allow-origins=*",
      f"--user-data-dir={user_data_dir}",
      f"--window-size={width},{height}",
      "about:blank",
    ],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
    text=True,
  )
  list_url = f"http://127.0.0.1:{port}/json/list"
  for _ in range(100):
    try:
      with urllib.request.urlopen(list_url, timeout=0.2) as response:
        targets = json.loads(response.read().decode("utf-8"))
      page = next((target for target in targets if target.get("type") == "page"), targets[0])
      return chrome, page["webSocketDebuggerUrl"], user_data_dir
    except Exception:
      time.sleep(0.05)
  chrome.terminate()
  raise RuntimeError("Chrome did not expose DevTools endpoint")


def summarise_requests(requests: list[NetworkRequest]) -> dict[str, Any]:
  api = [r for r in requests if "/api/" in r.url and "image-session-assets" not in r.url]
  images = [r for r in requests if "/api/image-session-assets/" in r.url]
  js = [r for r in requests if r.resource_type == "Script" or r.url.endswith(".js")]
  css = [r for r in requests if r.resource_type == "Stylesheet" or r.url.endswith(".css")]

  def total_bytes(items: list[NetworkRequest]) -> int:
    return sum(r.encoded_bytes for r in items)

  def slowest(items: list[NetworkRequest], n: int = 5) -> list[dict[str, Any]]:
    rows = []
    for request in sorted(items, key=lambda item: item.duration_ms or 0, reverse=True)[:n]:
      rows.append(
        {
          "ms": round(request.duration_ms or 0, 1),
          "bytes": request.encoded_bytes,
          "type": request.resource_type,
          "url": request.url.replace("http://127.0.0.1:29283", "").replace("http://127.0.0.1:29282", ""),
        }
      )
    return rows

  return {
    "counts": {
      "all": len(requests),
      "api": len(api),
      "images": len(images),
      "js": len(js),
      "css": len(css),
    },
    "bytes": {
      "all": total_bytes(requests),
      "api": total_bytes(api),
      "images": total_bytes(images),
      "js": total_bytes(js),
      "css": total_bytes(css),
    },
    "slowest": slowest(requests),
  }


def run(args: argparse.Namespace) -> dict[str, Any]:
  chrome, ws_url, user_data_dir = launch_chrome(args.width, args.height)
  client = CdpClient(ws_url)
  try:
    client.send("Page.enable")
    client.send("Runtime.enable")
    client.send("Network.enable")
    client.send("Emulation.setDeviceMetricsOverride", {"width": args.width, "height": args.height, "deviceScaleFactor": 1, "mobile": args.width < 900})

    nav_start = time.perf_counter()
    client.send("Page.navigate", {"url": args.url})
    wait_for(lambda: client.evaluate("document.readyState === 'complete'"), timeout_s=10)
    client.drain(250)
    main_image, main_image_ms = wait_for(lambda: client.evaluate(MAIN_IMAGE_READY_JS), timeout_s=args.timeout)
    client.drain(500)
    perf = client.evaluate(PERFORMANCE_JS)
    refresh_wall_ms = (time.perf_counter() - nav_start) * 1000
    refresh_requests = list(client.requests.values())

    session_buttons = client.evaluate(SESSION_BUTTONS_JS) or []
    switch_results = []
    for button in session_buttons[1 : args.switches + 1]:
      start_requests = len(client.requests)
      switch_start = time.perf_counter()
      client.send(
        "Input.dispatchMouseEvent",
        {"type": "mousePressed", "x": button["x"], "y": button["y"], "button": "left", "clickCount": 1},
      )
      client.send(
        "Input.dispatchMouseEvent",
        {"type": "mouseReleased", "x": button["x"], "y": button["y"], "button": "left", "clickCount": 1},
      )
      previous_src = main_image.get("src") if isinstance(main_image, dict) else None
      switched_image, image_wait_ms = wait_for(
        lambda: (state if (state := client.evaluate(MAIN_IMAGE_READY_JS)) and state.get("src") != previous_src else None),
        timeout_s=args.timeout,
      )
      image_ready_ms = (time.perf_counter() - switch_start) * 1000
      client.drain(300)
      switch_requests = list(client.requests.values())[start_requests:]
      switch_results.append(
        {
          "buttonText": button["text"][:80],
          "clickToImageMs": round(image_ready_ms, 1),
          "imageWaitMs": round(image_wait_ms, 1),
          "image": switched_image,
          "network": summarise_requests(switch_requests),
        }
      )
      main_image = switched_image or main_image

    requests = list(client.requests.values())
    return {
      "viewport": {"width": args.width, "height": args.height},
      "url": args.url,
      "refresh": {
        "wallMs": round(refresh_wall_ms, 1),
        "mainImageReadyMs": round(main_image_ms, 1),
        "mainImage": main_image,
        "performance": perf,
        "network": summarise_requests(refresh_requests),
      },
      "sessionButtonsFound": len(session_buttons),
      "switches": switch_results,
    }
  finally:
    client.close()
    chrome.terminate()
    try:
      chrome.wait(timeout=2)
    except subprocess.TimeoutExpired:
      chrome.kill()
    shutil.rmtree(user_data_dir, ignore_errors=True)


def main() -> None:
  parser = argparse.ArgumentParser()
  parser.add_argument("--url", default="http://127.0.0.1:29283/image-chat")
  parser.add_argument("--width", type=int, default=1440)
  parser.add_argument("--height", type=int, default=900)
  parser.add_argument("--switches", type=int, default=5)
  parser.add_argument("--timeout", type=float, default=8)
  args = parser.parse_args()
  print(json.dumps(run(args), ensure_ascii=False, indent=2))


if __name__ == "__main__":
  main()
