# ADR 0010: 主仓库不保兼容

主仓库处于快速开发、可破坏性更新阶段。需要稳定运行的部署自行 fork。本仓库只维护当前主线：schema-v3 图、Agent-first 创建、工作台、素材库和现行 provider 设置。不维护旧运行时、旧 API、旧 JSON/列形状、双序列化、409 兼容桩，也不为已部署数据写回填、冻结、归档闸门或升级迁移。已部署数据不是合同；跟上主仓库可以重建数据库和 storage。

本 ADR 取代 ADR 0004 的冻结、归档、证据闸门和 destructive cleanup 约束。ADR 0006 的全局图库权威仍有效；其中旧 `ImageGalleryEntry` 回填、cutover gate 和 Gallery-only bridge 不再是主线义务。
