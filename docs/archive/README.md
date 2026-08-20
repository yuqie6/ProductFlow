# Archived Documentation

这里保存已经退出默认阅读路径、但仍可能用于追溯历史决策和实施过程的文档。

归档文档不代表当前实现。需要判断当前行为时，优先读取 `CONTEXT.md`、`docs/PRD.md`、`docs/ARCHITECTURE.md`、相关 ADR 和 rollout 文档。

## 当前归档

- `specs/2026-08-17-global-agent-human-workflow-design.md`：Global Agent 长版设计和阶段审计。产品边界已收缩到 `docs/specs/global-agent-human-workflow-design.md`，Pi runtime 细节已迁移到 `docs/specs/pi-agent-runtime-integration.md`。
- `specs/2026-08-16-media-library-design.md`：素材库转型的历史实现设计。当前产品合同见 `docs/PRD.md` 与 `docs/specs/media-library-prd.md`，当前实现见 `docs/ARCHITECTURE.md`，未完成证据见 `docs/rollout/media-library-transition.md`。
- `aegis/`：本地 method-pack 过程记录（intent、checkpoint、JSON draft）。`aegis/` 被 gitignore，不进入默认阅读，也不替代 PRD、ADR 或 ARCHITECTURE。其他 clone 不必拥有这份目录。
