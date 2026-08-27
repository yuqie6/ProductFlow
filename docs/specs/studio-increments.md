# 工作室增量

- 文档状态：Draft
- 当前合同：`CONTEXT.md`、`docs/PRD.md`、`docs/ARCHITECTURE.md`
- 镜头分组装配：[`shot-scene-assembly.md`](shot-scene-assembly.md)
- 画布手感：[`workbench.md`](workbench.md)

相对当前产品多出来、落地前不写进 CONTEXT / PRD / ARCHITECTURE 的东西：

| 项 | 用户可见 | 不做 |
|---|---|---|
| 镜头列表默认主区 | 工作台打开先看镜头行（图种、张数、状态、缩略图、运行此镜头），可切到同一张 schema-v3 画布 | 新 Shot 表、第二套执行器 |
| 生成套图文案 | 商家按钮跑现有整图 DAG（先内容节点，后各镜头生图），进度按镜头投影 | 另一套 run 模型 |
| 创建页推荐套图 | 一键填缺失图种；再次点击不覆盖已有镜头配置 | 官方画布模板配方 |
| 出图后局部修 | 对已有 `ProductImageAsset` 消除 / 换字 / 局部重绘；新资产保留谱系；失败不改节点当前结果 | 第七类节点、去水印货架、批量 200 张 |
| 结果保真核对清单 | 人在结果上核对外形、颜色、文字，不满意则局部修或重跑该镜头 | 自动质量评分当作正式闸门 |

内置 DeliverySpec 模板已在 `application/delivery_renditions/presets.py` 与 `docs/ARCHITECTURE.md` §7。本文不再把预设目录当成未交付北极星。配方库仍只列用户从 live graph 保存的配方。
