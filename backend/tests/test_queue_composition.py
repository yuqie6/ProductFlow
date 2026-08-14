from __future__ import annotations

import ast
import os
import subprocess
import sys
from collections import defaultdict
from pathlib import Path

import pytest
from dramatiq.message import Message

from productflow_backend.infrastructure import queue


class _RecordingBroker:
    def __init__(self) -> None:
        self.calls: list[tuple[Message, int | None]] = []

    def enqueue(self, message: Message, *, delay: int | None = None) -> Message:
        self.calls.append((message, delay))
        return message


def _module_name(path: Path, src_dir: Path) -> str:
    relative = path.relative_to(src_dir).with_suffix("")
    parts = list(relative.parts)
    if parts[-1] == "__init__":
        parts.pop()
    return ".".join(parts)


def _resolve_import(current_module: str, level: int, imported_module: str | None) -> str:
    if level == 0:
        return imported_module or ""
    package_parts = current_module.split(".")[:-1]
    if level > 1:
        package_parts = package_parts[: -(level - 1)]
    if imported_module:
        package_parts.extend(imported_module.split("."))
    return ".".join(package_parts)


def _internal_import_graph(src_dir: Path) -> dict[str, set[str]]:
    package_prefix = "productflow_backend"
    module_paths = {
        _module_name(path, src_dir)
        for path in (src_dir / package_prefix).rglob("*.py")
    }
    graph: dict[str, set[str]] = defaultdict(set)
    for path in (src_dir / package_prefix).rglob("*.py"):
        current_module = _module_name(path, src_dir)
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        for node in ast.walk(tree):
            imported_modules: list[str] = []
            if isinstance(node, ast.Import):
                imported_modules = [alias.name for alias in node.names]
            elif isinstance(node, ast.ImportFrom):
                imported_module = _resolve_import(current_module, node.level, node.module)
                imported_modules = [imported_module]
                for alias in node.names:
                    candidate = f"{imported_module}.{alias.name}" if imported_module else alias.name
                    if candidate in module_paths:
                        imported_modules.append(candidate)
            for imported_module in imported_modules:
                if imported_module.startswith(package_prefix) and imported_module in module_paths:
                    graph[current_module].add(imported_module)
        graph.setdefault(current_module, set())
    return graph


def _strongly_connected_components(graph: dict[str, set[str]]) -> list[set[str]]:
    index = 0
    indices: dict[str, int] = {}
    lowlinks: dict[str, int] = {}
    stack: list[str] = []
    on_stack: set[str] = set()
    components: list[set[str]] = []

    def visit(module: str) -> None:
        nonlocal index
        indices[module] = index
        lowlinks[module] = index
        index += 1
        stack.append(module)
        on_stack.add(module)

        for dependency in graph[module]:
            if dependency not in indices:
                visit(dependency)
                lowlinks[module] = min(lowlinks[module], lowlinks[dependency])
            elif dependency in on_stack:
                lowlinks[module] = min(lowlinks[module], indices[dependency])

        if lowlinks[module] == indices[module]:
            component: set[str] = set()
            while True:
                dependency = stack.pop()
                on_stack.remove(dependency)
                component.add(dependency)
                if dependency == module:
                    break
            components.append(component)

    for module in graph:
        if module not in indices:
            visit(module)
    return components


@pytest.mark.parametrize(
    ("sender_name", "actor_name", "delay_ms"),
    [
        ("enqueue_workflow_run", "run_product_workflow_run", None),
        ("enqueue_workflow_run_later", "run_product_workflow_run", 1234),
        ("enqueue_workflow_node_run", "run_product_workflow_node_run", None),
        ("enqueue_workflow_node_run_later", "run_product_workflow_node_run", 2345),
        ("enqueue_image_session_generation_task", "run_image_session_generation_task", None),
        ("enqueue_image_session_generation_task_later", "run_image_session_generation_task", 3456),
        ("enqueue_agent_turn_sync", "run_agent_turn_sync", None),
        ("enqueue_agent_turn_sync_later", "run_agent_turn_sync", 4567),
        ("enqueue_delivery_rendition_job", "run_delivery_rendition_job", None),
    ],
)
def test_queue_sender_preserves_durable_actor_message_contract(
    sender_name: str,
    actor_name: str,
    delay_ms: int | None,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    broker = _RecordingBroker()
    monkeypatch.setattr(queue, "get_broker", lambda: broker)
    sender = getattr(queue, sender_name)

    if delay_ms is None:
        sender("durable-id")
    else:
        sender("durable-id", delay_ms=delay_ms)

    assert len(broker.calls) == 1
    message, actual_delay = broker.calls[0]
    assert message.queue_name == "default"
    assert message.actor_name == actor_name
    assert message.args == ("durable-id",)
    assert message.kwargs == {}
    assert message.options == {}
    assert actual_delay == delay_ms


def test_importing_queue_and_application_does_not_load_workers() -> None:
    src_dir = Path(__file__).resolve().parents[1] / "src"
    script = """
import sys

import productflow_backend.application.durable_recovery
assert "productflow_backend.infrastructure.queue" not in sys.modules
assert "productflow_backend.workers" not in sys.modules

import productflow_backend.infrastructure.queue
assert "productflow_backend.workers" not in sys.modules

import productflow_backend.application.product_workflow.execution
assert "productflow_backend.workers" not in sys.modules
"""
    env = {**os.environ, "PYTHONPATH": str(src_dir)}
    result = subprocess.run(
        [sys.executable, "-c", script],
        cwd=src_dir.parent,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_queue_worker_application_import_graph_has_no_strongly_connected_component() -> None:
    src_dir = Path(__file__).resolve().parents[1] / "src"
    graph = _internal_import_graph(src_dir)
    queue_module = "productflow_backend.infrastructure.queue"
    worker_module = "productflow_backend.workers"

    for component in _strongly_connected_components(graph):
        has_queue = queue_module in component
        has_worker = worker_module in component
        has_application = any(module.startswith("productflow_backend.application") for module in component)
        assert not (has_queue and (has_worker or has_application)), component
