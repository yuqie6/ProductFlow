# ADR 0002: 统一商品图片身份与媒体所有权

## 状态

Accepted

## 背景

旧实现分别使用 SourceAsset、PosterVariant 和 ImageSessionAsset 表达图片，并曾为同一生成结果保存多份字节。文件名、数组位置和存储路径因此被迫承担业务身份，删除、封面、节点引用和 lineage 难以由数据库约束。

## 决策

- `MediaObject` 表示不可变且经过验证的媒体字节及其路径、MIME、尺寸、字节数和哈希。
- `ProductImageAsset` 表示图片进入某个商品后的稳定身份、来源、显示名、目录、类型和派生关系。
- 工作流节点、商品封面、图库和交付派生统一引用 ProductImageAsset id。
- ImageSession 图片明确保存到商品时创建 ProductImageAsset，并复用允许共享的 MediaObject；不复制同一字节。
- preview/thumbnail 是可重建缓存，不是交付产物。
- 新存储写入与数据库提交失败之间使用精确补偿，只删除本次新建文件。

## 后果

- 图片移动、改名和系统分类变化不会破坏工作流引用。
- 节点重跑产生新资产并更新 current binding，旧资产仍可追溯。
- 删除资产前必须检查节点、封面、派生、图库和任务引用。
- 迁移期 `legacy_import` 可读，但不构成新的在线写入来源。

## 排除方案

- 用相同 storage path 字符串表达共享所有权。
- 由前端按文件名或并行数组去重。
- 把 thumbnail 当作具有独立交付合同的图片。
