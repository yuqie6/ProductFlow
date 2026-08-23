# ADR 0004: V1 只读归档与单执行器切换

## 状态

Accepted. References to schema-v2 as the online replacement describe the original cutover decision and are superseded by ADR 0008. The current online graph is schema-v3 `workflow_graphs`; the V1 freeze, archive, evidence gate, and cleanup constraints remain in force.

## 背景

已部署数据库可能包含 V1 workflow、运行、用户模板、Canvas Agent 历史和旧图片关系。V1 语义无法无损转换为 V2。长期保留双编辑器和双执行器会持续扩大在线状态空间。

## 决策

- 在线系统只维护 schema-v2 编辑、运行和重试语义。
- V1 数据在迁移窗口冻结后生成版本化、不可变、带稳定 hash 的 archive。
- 历史 UI 只提供查看、下载和导出；Agent rebuild 只生成待审阅的 V2 Draft seed。
- 旧 source 表在 archive、canonical mapping 和恢复证据完成前保留。
- revision `20260816_0042` 只增加 durable evidence gate，不删除 source/archive 数据。
- 任何未来 destructive cleanup 必须在同一事务开头通过 `assert_legacy_cutover_cleanup_ready`。
- gate 需要 source/archive/canonical 三类 SHA-256、恢复验证时间和零 active/unknown V1 execution。

## 后果

- 源码删除不代表某个部署已经完成生产切换。
- gate 处于 pending 时，生产证据仍未完成。
- 首笔 V2 正式写入后的恢复依赖切换前数据库与 storage 备份，不重新开放 V1 executor。
- Alembic 历史 migration 保留；后续删表是独立、再次审阅的 migration。

## 排除方案

- 把旧 DAG 自动改写并宣称为等价 V2。
- 通过跳过异常 workflow 或手工改终态取得绿色报告。
- 在普通 schema migration 中读取 storage、调用 provider 或删除媒体。
