# 全局图库与工作流子图库升级 PRD

## 1. 状态与范围

- 文档状态：Approved
- 批准依据：Accepted `docs/adr/0006-media-library-authority.md`
- 已交付能力：`docs/PRD.md` 的全局素材库、工作流子图库和 Agent 整理 Draft；当前实现见 `docs/ARCHITECTURE.md`
- 身份与不变量：`CONTEXT.md` 的 Gallery And Library Terms、`docs/adr/0006-media-library-authority.md`
- 未完成证据：`docs/rollout/media-library-transition.md`
- 历史实现设计：`docs/archive/specs/2026-08-16-media-library-design.md`

本文只保留尚未完成的产品项。已交付行为以 `PRD.md` 和代码为准。

## 2. 仍待完成

- current-schema 旧 `ImageGalleryEntry` 表的部署级回填、引用审计、观察窗和物理清理资格。
- 旧 `legacy_canvas_agent_20260518_0032` Gallery-only bridge 的真实部署演练、备份恢复证据和 approval。Agent archive 不在这条 bridge 范围内。
- 跨商品等更高范围的 Agent 写操作。
- 清理只允许删除旧 Gallery 行和表，不得删除全局素材、共享媒体或工作流引用。

## 3. 剩余产品约束

- 旧 Gallery 图片收藏数据不能静默丢失。正式切换证据包括 snapshot、preflight、apply、reconcile 和 zero-delta。
- 旧 `/api/gallery` 不得作为在线 fallback；`/gallery` 只保留兼容重定向。
- 迁移窗口处理旧 ImageSession source 时，必须显式核对旧 `ImageGalleryEntry`。在线 ImageSession 删除路径不再写入或读取旧 Gallery。
- 删除 ImageSession 不得删除已经保存到全局图库的资产，也不得删除仍被工作流使用的媒体。
- 现有孤儿 Gallery 条目必须先审计，再按有界、可回滚的迁移/清理流程处理。
- drop 旧表需要单独确认和备份/恢复证据。
