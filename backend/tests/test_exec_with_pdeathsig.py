from __future__ import annotations

import os
import subprocess
import sys
import time
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "exec_with_pdeathsig.py"


@pytest.mark.skipif(sys.platform != "linux", reason="prctl parent-death signal is Linux-only")
def test_exec_with_pdeathsig_kills_child_when_parent_exits() -> None:
    launcher = r"""
import os
import subprocess
import sys
import time
from pathlib import Path

script = sys.argv[1]
child = subprocess.Popen([sys.executable, script, "sleep", "30"])
print(child.pid, flush=True)
deadline = time.monotonic() + 5
while time.monotonic() < deadline:
    comm_path = Path(f"/proc/{child.pid}/comm")
    try:
        comm = comm_path.read_text(encoding="utf-8").strip()
    except FileNotFoundError:
        time.sleep(0.01)
        continue
    if comm == "sleep":
        os._exit(0)
    time.sleep(0.01)
raise SystemExit(f"child pid {child.pid} did not exec sleep before parent exit")
"""
    parent = subprocess.Popen(
        [sys.executable, "-c", launcher, str(SCRIPT)],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    assert parent.stdout is not None
    child_line = parent.stdout.readline()
    child_pid = int(child_line.strip())
    returncode = parent.wait(timeout=10)
    stderr = parent.stderr.read() if parent.stderr is not None else ""
    assert returncode == 0, stderr
    deadline = time.monotonic() + 3
    while time.monotonic() < deadline:
        try:
            os.kill(child_pid, 0)
        except ProcessLookupError:
            return
        time.sleep(0.05)
    raise AssertionError(f"child pid {child_pid} survived parent exit")
