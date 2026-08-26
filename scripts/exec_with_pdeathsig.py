#!/usr/bin/env python3
"""在请求 Linux 于父进程退出时杀掉本进程之后，再 exec 替换当前进程。"""

from __future__ import annotations

import os
import signal
import sys

PR_SET_PDEATHSIG = 1


def set_parent_death_signal(signum: int) -> None:
    """当前父进程退出时杀掉本进程。

    `prctl(PR_SET_PDEATHSIG)` 仅 Linux 可用，跨 `exec` 保留，并由 `fork` 子进程继承。
    Just/uv 退出后 Dramatiq worker 否则会被 init 收养，仍占着 PostgreSQL 锁。
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
