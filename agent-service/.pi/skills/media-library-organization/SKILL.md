---
name: media-library-organization
description: Organize bounded global media-library assets through a reviewable ProductFlow draft.
triggers:
  - organize media library
  - rename assets
  - folders
  - archive media
owns_tools:
  - ask_user
  - list_global_media_library_assets_v1
  - inspect_global_media_library_assets_v1
  - propose_global_draft
scope: global
version: 2
---

# 全局素材整理

## 何时使用

用户要查找、分类、重命名、建文件夹、打标签、归档、恢复或关联全局素材。

## 前置事实

先分页列出。只检查明确选中的资产。列表项的 `revision`、`display_name`、`folder_id`、`tag_names`、`is_archived` 原样抄进每条操作的 `before`；`expected_revision` 等于该项 `revision`。`propose_global_draft` 必须是 `schema_version: 1`、`draft_kind: "library_organization"`，并带完整 `library_payload`（`confirmation_summary` + 至少一条 `operations`）。移动时用列表 `folders` 或页面筛选里的 `folder_id`，用户已经点名目标文件夹时不要再问。

## 工作循环

1. `list_global_media_library_assets_v1` 取一页。页面 `selected_asset_ids` 非空时，把这些 ID 作为用户已明确选择的目标；列表中的同 ID 项提供写入所需事实。
2. 需要看图时 `inspect_global_media_library_assets_v1`。检查只补充视觉判断，不会替代列表返回的 ID、revision 和 `before`，检查完成后继续原请求。
3. 重命名：`operation: "rename"`，`target.display_name` 用用户给的新名。移动：`operation: "move"`，`target.folder_id` 用目标文件夹 id。
4. 用户已给出新名称或目标文件夹，且列表能唯一确定目标并返回资产 ID 与 revision 时，必须直接用 `propose_global_draft` 提交完整可审阅草案；不要重复询问，也不要额外请求一次确认。只有列表返回零个匹配目标或多个无法消歧的目标时才提问。
5. 告诉用户必须确认后才会落地。

## 禁止行为

不要调用底层数据库式变更接口。不要一次读完整库。不要在散文里暴露媒体 URL 或 bytes。不要声称文件夹、标签或重命名已经生效。

## 完成判据

后端已接受完整草案，状态为等待确认。
