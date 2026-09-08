#!/usr/bin/env python3
"""Offline positive and refusal tests for the capacity evidence contracts."""

import asyncio
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import capacity_events
import capacity_identity
from run_contention import provider_events
from run_fault_recovery import provider_trace_report
import run_load


class ReadMeasurementTest(unittest.TestCase):
    def test_warmup_latency_and_errors_do_not_enter_sample_statistics(self):
        rows = [
            run_load.RequestRecord("products", "m1", 200, 9000, phase="warmup"),
            run_load.RequestRecord("products", "m1", 503, 8000, "unavailable", "warmup"),
            run_load.RequestRecord("products", "m1", 200, 10),
            run_load.RequestRecord("products", "m1", 200, 20),
            run_load.RequestRecord("products", "m1", 503, 30, "unavailable"),
        ]
        report = run_load.summarize_reads(rows)["products"]
        self.assertEqual(report["completed"], 2)
        self.assertEqual(report["unexpected_failures"], 1)
        self.assertEqual(report["p95_ms"], 20)
        self.assertEqual(report["p99_ms"], 20)
        self.assertEqual(report["max_ms"], 20)
        self.assertEqual(report["warmup_requests"], 2)
        self.assertEqual(report["sample_requests"], 3)

    def test_warmup_alone_does_not_claim_measurement(self):
        report = run_load.summarize_reads([
            run_load.RequestRecord("products", "m1", 200, 10, phase="warmup"),
        ])["products"]
        self.assertEqual(report["completed"], 0)
        self.assertEqual(report["sample_requests"], 0)
        self.assertIsNone(report["p95_ms"])
        self.assertIsNone(report["max_ms"])


class EventEvidenceTest(unittest.TestCase):
    @staticmethod
    def provider_lines(request_id, prompt):
        phases = [("received", None), ("started", None), ("finished", None), ("responded", None)]
        return [
            {
                "request_id": request_id,
                "prompt": prompt,
                "phase": phase,
                "received_monotonic": 1.0,
                "started_monotonic": 1.1,
                "finished_monotonic": 2.0,
                "responded_monotonic": 2.1,
            }
            for phase, _unused in phases
        ]

    def test_snapshot_is_hashed_and_write_once(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "provider-events.jsonl"
            destination = root / "round-events.jsonl"
            lines = [
                {
                    "request_id": "r1",
                    "prompt": "Create an image from the current user request.\nCurrent user request:\ncapacity-L1-round-1-slot-00\nGenerate the image directly.",
                    "phase": "received",
                },
                {
                    "request_id": "r1",
                    "prompt": "Create an image from the current user request.\nCurrent user request:\ncapacity-L1-round-1-slot-00\nGenerate the image directly.",
                    "phase": "responded",
                },
                {"request_id": "other", "prompt": "unrelated", "phase": "responded"},
            ]
            source.write_text("".join(json.dumps(item) + "\n" for item in lines), encoding="utf-8")
            result = capacity_events.snapshot_events(source, destination, ("capacity-L1-round-1-",))
            self.assertEqual(result["request_count"], 1)
            self.assertEqual(result["line_count"], 2)
            self.assertEqual(result["sha256"], capacity_events.file_sha256(destination))
            with self.assertRaisesRegex(RuntimeError, "already exists"):
                capacity_events.snapshot_events(source, destination, ("capacity-L1-round-1-",))

    def test_snapshot_rejects_malformed_source_lines(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "provider-events.jsonl"
            source.write_text('{"request_id":"r1","prompt":"capacity-L1-round-1-"}\nnot-json\n', encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "invalid provider event JSON"):
                capacity_events.snapshot_events(source, root / "round-events.jsonl", ("capacity-L1-round-1-",))

    def test_provider_summary_separates_execution_and_http_intervals(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            phases = [
                {"phase": "received", "received_monotonic": 1.0},
                {"phase": "started", "received_monotonic": 1.0, "started_monotonic": 1.5},
                {"phase": "finished", "received_monotonic": 1.0, "started_monotonic": 1.5, "finished_monotonic": 3.0},
                {"phase": "responded", "received_monotonic": 1.0, "started_monotonic": 1.5, "finished_monotonic": 3.0, "responded_monotonic": 3.5},
            ]
            with path.open("w", encoding="utf-8") as stream:
                for phase in phases:
                    stream.write(json.dumps({
                        "request_id": "r1",
                        "prompt": "Create an image from the current user request.\nCurrent user request:\ncapacity-L1-round-1-slot-00\nGenerate the image directly.",
                        **phase,
                    }) + "\n")
            summary = run_load.provider_summary(str(path), "capacity-L1-round-1-")
            self.assertEqual(summary["execution_max_concurrency"], 1)
            self.assertEqual(summary["http_max_concurrency"], 1)
            self.assertEqual(summary["requests_completed"], 1)

    def test_provider_summary_rejects_negative_duration(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            phases = ["received", "started", "finished", "responded"]
            with path.open("w", encoding="utf-8") as stream:
                for phase in phases:
                    stream.write(
                        json.dumps(
                            {
                                "request_id": "r1",
                                "prompt": "capacity-L1-round-1-slot-00",
                                "phase": phase,
                                "received_monotonic": 2.0,
                                "started_monotonic": 1.0,
                                "finished_monotonic": 3.0,
                                "responded_monotonic": 3.5,
                            }
                        )
                        + "\n"
                    )
            with self.assertRaisesRegex(RuntimeError, "negative or non-monotonic"):
                run_load.provider_summary(str(path), "capacity-L1-round-1-")

    def test_provider_wait_requires_http_response_phase(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            path.write_text(
                json.dumps(
                    {
                        "request_id": "r1",
                        "prompt": "Create an image.\nCurrent user request:\ncapacity-L1-round-1-slot-00",
                        "phase": "finished",
                        "finished_epoch": 3.0,
                    }
                )
                + "\n",
                encoding="utf-8",
            )
            self.assertIsNone(run_load.provider_prompt_finish_epoch(str(path), "capacity-L1-round-1-slot-00"))

    def test_contention_provider_events_reject_incomplete_lifecycle(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            path.write_text(
                json.dumps(
                    {
                        "request_id": "r1",
                        "prompt": "capacity-A-short-000",
                        "phase": "finished",
                        "received_monotonic": 1.0,
                        "started_monotonic": 1.5,
                        "finished_monotonic": 2.0,
                        "responded_monotonic": 2.5,
                    }
                )
                + "\n",
                encoding="utf-8",
            )
            with self.assertRaisesRegex(RuntimeError, "complete lifecycle"):
                provider_events(str(path))

    def test_fault_provider_trace_requires_one_complete_call_per_prompt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "provider-events.jsonl"
            destination = root / "fault-provider-events.jsonl"
            lines = self.provider_lines("r1", "capacity-fault-task")
            source.write_text("".join(json.dumps(item) + "\n" for item in lines), encoding="utf-8")
            report = provider_trace_report(source, destination, ["capacity-fault-task"])
            self.assertTrue(report["pass"])
            self.assertEqual(report["by_prompt"]["capacity-fault-task"], ["r1"])

    def test_fault_provider_trace_rejects_duplicate_effect_attempts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "provider-events.jsonl"
            destination = root / "fault-provider-events.jsonl"
            lines = self.provider_lines("r1", "capacity-fault-task") + self.provider_lines("r2", "capacity-fault-task")
            source.write_text("".join(json.dumps(item) + "\n" for item in lines), encoding="utf-8")
            report = provider_trace_report(source, destination, ["capacity-fault-task"])
            self.assertFalse(report["pass"])
            self.assertIn("capacity-fault-task", report["duplicate_prompts"])


class IdentityEvidenceTest(unittest.TestCase):
    def test_source_identity_requires_attached_image_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            repo_root = Path(directory)
            fixture = repo_root / capacity_identity.FIXTURE_PATH
            fixture.parent.mkdir(parents=True)
            fixture.write_text("package auth_test\n", encoding="utf-8")
            identity_path = repo_root / "source-identity.json"
            # Repository cleanliness is tested separately; image attachment
            # must not depend on unrelated changes in the developer checkout.
            with patch.object(capacity_identity, "_git", side_effect=lambda _root, *args: "" if args[0] == "status" else "frozen-commit"):
                capacity_identity.write_identity(repo_root, identity_path, "pf-capacity-test:image")
                with self.assertRaisesRegex(RuntimeError, "image identity is incomplete"):
                    capacity_identity.verify_identity(repo_root, identity_path)
                capacity_identity.attach_image(identity_path, "pf-capacity-test:image", "sha256:frozen", [])
                verified = capacity_identity.verify_identity(repo_root, identity_path)
                self.assertEqual(verified["image"]["id"], "sha256:frozen")

    def test_source_identity_preserves_porcelain_status_columns(self):
        with patch.object(capacity_identity, "_git", return_value=" M scripts/saas_capacity/run_load.py\n"):
            self.assertEqual(capacity_identity._tracked_status(Path("/tmp")), [])
        with patch.object(capacity_identity, "_git", return_value=" M go/internal/imagesession/http.go\n"):
            self.assertEqual(
                capacity_identity._tracked_status(Path("/tmp")),
                [" M go/internal/imagesession/http.go"],
            )


class SseReconnectTest(unittest.TestCase):
    def test_idle_stream_close_is_reconnected_until_stop(self):
        class EmptyContent:
            async def __aiter__(self):
                if False:
                    yield b""

        class Response:
            status = 200
            content = EmptyContent()

            async def __aenter__(self):
                return self

            async def __aexit__(self, *_args):
                return False

        class Client:
            def __init__(self):
                self.calls = 0

            def get(self, *_args, **_kwargs):
                self.calls += 1
                if self.calls > 2:
                    raise asyncio.CancelledError()
                return Response()

        class DB:
            async def task_rows(self, _task_ids):
                return {}

        async def no_sleep(_seconds):
            return None

        client = Client()
        stats = run_load.SSEStats("merchant")
        with patch.object(run_load.time, "monotonic", return_value=0.0), patch.object(run_load.asyncio, "sleep", new=no_sleep):
            with self.assertRaises(asyncio.CancelledError):
                asyncio.run(run_load.sse_reader(client, "http://example", 0, "cookie", stats, DB(), set(), 0.0, 1.0))
        self.assertEqual(client.calls, 3)
        self.assertEqual(stats.connections_opened, 2)
        self.assertEqual(stats.idle_closures, 2)
        self.assertEqual(stats.reconnects, 2)


if __name__ == "__main__":
    unittest.main()
