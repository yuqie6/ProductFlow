# 主线退休 Python 业务后端

FastAPI / Dramatiq / Alembic 树离开默认 checkout。取回路径是 `git checkout retired/python`。主线不再启动、测试或对照 Python HTTP。

## 状态

Accepted。

## 背景

ADR 0011 把业务 API、worker、dispatcher 迁到 Go，Python `backend/` 留在主线作封印对照与 Compose profile `python` 回退。Go 已是唯一业务运行时。主线再保留该树会继续出现在工作区，文档也会把可选回退写成当前事实。

## 决策

- 从仍含 `backend/` 的提交打出 `retired/python` 整仓快照。不 rewrite git 历史。
- 主线删除 `backend/`、Compose profile `python`、`just backend-*` Python 配方，以及从 FastAPI 导合同的脚本。
- 仓库根 `contracts/` 留下 2026-08-29 HTTP 封印快照，供 Go 路由测试对照。Go 是现行 HTTP 行为来源；默认不再从任何 Python 进程重生 json。
- 仓库脚本（`scripts/check_docs.py`、`scripts/wipe_dev_data.py` 等）继续用系统 `python3`。它们不是业务后端。

## 后果

- 主线开发、Compose 和 `just dev` 只跑 Go API / worker / dispatcher。
- 取回封印树：`git checkout retired/python`。不要把该分支合并回主线。
- 现行代码所有权见 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md) 与 [`go/AGENTS.md`](../../go/AGENTS.md)。
