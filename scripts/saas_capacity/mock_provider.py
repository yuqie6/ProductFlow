#!/usr/bin/env python3
"""Deterministic local image provider used by the capacity campaign.

The Go application still uses its normal provider adapter and worker path. This
server only replaces a paid model endpoint with a local, fixed-latency response.
"""

import argparse
import base64
import json
import signal
import threading
import time
from datetime import datetime, timezone
from email.parser import BytesParser
from email.policy import default
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


ONE_PIXEL_PNG = base64.b64encode(
    bytes.fromhex(
        "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c489"
        "0000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082"
    )
).decode("ascii")


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def monotonic_seconds() -> float:
    return time.monotonic_ns() / 1_000_000_000


def parse_request(headers: object, raw: bytes) -> dict[str, object]:
    content_type = str(headers.get("Content-Type", ""))
    if content_type.startswith("multipart/form-data"):
        message = BytesParser(policy=default).parsebytes(
            (f"Content-Type: {content_type}\r\nMIME-Version: 1.0\r\n\r\n").encode("ascii") + raw
        )
        fields: dict[str, object] = {}
        for part in message.iter_parts():
            disposition = part.get("Content-Disposition", "")
            name = part.get_param("name", header="content-disposition")
            if not name:
                continue
            value = part.get_payload(decode=True) or b""
            if "filename=" in disposition or name in {"image", "mask"}:
                fields[name] = {"bytes": len(value)}
            else:
                fields[name] = value.decode("utf-8", errors="replace")
        return fields
    decoded = raw.decode("utf-8") if raw else "{}"
    value = json.loads(decoded)
    if not isinstance(value, dict):
        raise ValueError("provider request must be an object")
    return value


class ProviderHandler(BaseHTTPRequestHandler):
    server_version = "ProductFlowCapacityMock/1"

    def log_message(self, _format: str, *_args: object) -> None:
        return

    def do_GET(self) -> None:
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(b"ok\n")
            return
        self.send_error(404)

    def do_POST(self) -> None:
        if self.path not in {"/v1/images/generations", "/v1/images/edits"}:
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        try:
            payload = parse_request(self.headers, raw)
        except (ValueError, json.JSONDecodeError):
            self.send_error(400, "invalid json")
            return
        prompt = str(payload.get("prompt", ""))
        request_id = f"capacity-mock-{time.time_ns()}"
        received = time.time()
        received_monotonic = monotonic_seconds()
        delay = self.server.delay_seconds
        if "capacity-long" in prompt:
            delay = self.server.long_delay_seconds
        event = {
            "request_id": request_id,
            "path": self.path,
            "prompt": prompt,
            "received_at": datetime.fromtimestamp(received, timezone.utc).isoformat(),
            "received_epoch": received,
            "received_monotonic": received_monotonic,
            "delay_seconds": delay,
        }
        self.server.append_event(event, "received")
        started = time.time()
        started_monotonic = monotonic_seconds()
        event.update({
            "phase": "started",
            "started_at": now_iso(),
            "started_epoch": started,
            "started_monotonic": started_monotonic,
            "queue_wait_seconds": max(0.0, started_monotonic - received_monotonic),
        })
        self.server.append_event(event, "started")
        time.sleep(delay)
        finished = time.time()
        finished_monotonic = monotonic_seconds()
        event.update({
            "phase": "finished",
            "finished_at": now_iso(),
            "finished_epoch": finished,
            "finished_monotonic": finished_monotonic,
        })
        self.server.append_event(event, "finished")
        body = json.dumps(
            {
                "id": request_id,
                "model": payload.get("model") or "capacity-mock",
                "data": [{"b64_json": ONE_PIXEL_PNG}],
            }
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        self.wfile.flush()
        responded = time.time()
        responded_monotonic = monotonic_seconds()
        event.update({
            "phase": "responded",
            "responded_at": now_iso(),
            "responded_epoch": responded,
            "responded_monotonic": responded_monotonic,
        })
        self.server.append_event(event, "responded")


class CapacityServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(
        self,
        address: tuple[str, int],
        delay_seconds: float,
        long_delay_seconds: float,
        event_path: str,
    ):
        super().__init__(address, ProviderHandler)
        self.delay_seconds = delay_seconds
        self.long_delay_seconds = long_delay_seconds
        self.event_path = event_path
        self.event_lock = threading.Lock()

    def append_event(self, event: dict, phase: str) -> None:
        with self.event_lock:
            with open(self.event_path, "a", encoding="utf-8") as stream:
                snapshot = dict(event)
                snapshot["phase"] = phase
                stream.write(json.dumps(snapshot, sort_keys=True) + "\n")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=30190)
    parser.add_argument("--delay", type=float, default=2.0)
    parser.add_argument("--long-delay", type=float, default=15.0)
    parser.add_argument("--event-path", default="/data/provider-events.jsonl")
    args = parser.parse_args()

    server = CapacityServer((args.host, args.port), args.delay, args.long_delay, args.event_path)

    def stop(_signal: int, _frame: object) -> None:
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    server.serve_forever()


if __name__ == "__main__":
    main()
