#!/usr/bin/env python3
"""Replace this process after asking Linux to kill it if the parent exits."""

from __future__ import annotations

import os
import signal
import sys

PR_SET_PDEATHSIG = 1


def set_parent_death_signal(signum: int) -> None:
    """Kill this process if its current parent exits.

    `prctl(PR_SET_PDEATHSIG)` is Linux-only and is preserved across `exec` and
    inherited by `fork` children. Just/uv exiting otherwise leaves Dramatiq
    workers reparented to init, still holding PostgreSQL locks.
    """

    if sys.platform != "linux":
        return
    import ctypes

    parent = os.getppid()
    libc = ctypes.CDLL("libc.so.6", use_errno=True)
    if libc.prctl(PR_SET_PDEATHSIG, signum) != 0:
        errno = ctypes.get_errno()
        raise OSError(errno, os.strerror(errno), "prctl(PR_SET_PDEATHSIG)")
    if os.getppid() != parent:
        os.kill(os.getpid(), signum)


def main(argv: list[str]) -> None:
    if len(argv) < 2:
        print("usage: exec_with_pdeathsig.py command [args...]", file=sys.stderr)
        raise SystemExit(2)
    set_parent_death_signal(signal.SIGKILL)
    os.execvp(argv[1], argv[1:])


if __name__ == "__main__":
    main(sys.argv)
