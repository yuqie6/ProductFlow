# 任务：保证图片池参考与金标按图片内容隔离

状态：完成
类型：实现
认领者：主代理-image-quality-0905-2245
认领于：2026-09-05T22:45:11+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：无

## 问题来源

用户授权接管图片组全部开发。接管 image-eval-pool 后，逐文件核验 200 个 manifest、6346 个图片文件；文件大小与哈希一致，但 5 个 SKU 的参考图与金标字节相同，违反 IMG-D-03。当前 imageeval 测试通过，尚未定位准入漏检原因。

## 做成什么样

准入按实际图片内容保证参考与金标互斥，已有污染样本不得进入 live。保持既定图种、数量门槛、评委和闸门公式。复核本地池并如实记录可用规模；原采证任务继续按原分母验收。

## 前置与并行

- 本组协调者已接收并保管原采证任务的未提交文档，释放该任务执行认领；其 live 暂停。
- 独占 go/internal/imageeval 的准入、池读取及对应测试；必要时 evals/image 抽取脚本。只依据实际根因修改。
- 独占 storage-dev/image-evals 池维护；原始像素与历史证据保留，不覆盖历史 report 或旧 run。
- 不使用 DB、provider、worker 或浏览器，不触碰节点合同任务占用的 graph/providers/product/prompts。

## 只改这些文件

- go/internal/imageeval/admit.go、ingest.go、pool.go 及相应测试（按根因选择）。
- 必要的 evals/image 抽取脚本及样本验证。
- 本任务、图片质量章程、原采证任务的阻塞交接、看板与归档。
- 本地池修复产物不提交 Git；原始 manifest 保留为证据。

## 合同与验证

- IMG-D-02/03：合法完整 listing、参考最多 6 张，同一图片不能同时进入参考和金标。
- 不改 judge、naive、harness、评分阈值或生成链，不用本次修复宣称模型质量改善。
- 对字节相同但不同 URL 的输入补回归，覆盖合法样本与旧污染池的实际读取边界。
- go test ./internal/imageeval、全池文件/哈希/隔离复核、固定 n=8 seed=1 抽样、just docs-check；不要求真实 provider 运行。

## 阻塞与交接

- 原因：无；图片组主代理已确认范围与所有权，无相交代码任务。
- 跟进者：主代理-image-quality-0905-2245。
- 交接：无收费进程；原采证任务由协调者保管，待本任务交付后恢复。

## 证据

- 基线：44078831；200 个 manifest、6346 个图片文件、939954504 bytes。5 个重叠 SKU：0ea2fe51aea9d111、49d8b766bbe50ddd、6965c1867a0d810b、7eaedee28d7f671c、9befbf7c566683b9。
- 修复前 go test ./internal/imageeval 通过；测试尚未覆盖上述污染。
- 交付定位：随本任务提交。
- 根因：Admit 仅按 SourceURL 排除参考，CDN 不同 URL 返回相同 bytes 时漏检。下载路径已计算 SHA-256，本次直接复用；参考选择按内容去重，金标剔除相同内容，随后执行原图种门槛。LoadManifest 拒绝既有污染 manifest，LoadAdmitted 的现有无效条目过滤使其不再进入抽样及 live。
- 生产代码只修改 admit.go 与 pool.go。未修改阈值、judge、naive、harness、生成链或原始池 manifest；无兼容或迁移命令。原有 ingest 正向夹具曾为所有 URL 返回同一张 PNG，现改为参考与金标不同内容。
- 确定性验证：`bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imageeval -count=1'` 通过。新增覆盖不同 URL 同内容的参考去重/金标排除、剔除后缺必需图种拒绝、旧污染 manifest 读取拒绝及可用池过滤。
- 全池核验：200 个目录与索引一致，6346 文件共 939954504 bytes，大小和 SHA-256 均与 manifest 相符；仅上述 5 个 SKU 有跨参考/金标哈希重叠。原始文件未改，按新 LoadAdmitted 路径 `just image-evals-sample 200 1` 返回 195 个；3c 19、appliance 28、baby 20、beauty 20、food 21、home 23、menswear 20、sports 25、womenswear 19。
- 固定 `just image-evals-sample 8 1` 返回：61ddf8407f8e62c2、d229379fd27005e5、442ea5992cba1b90、fcdcc8b241d7a254、76a1fb56d0ff2e1a、6d69aa54c5a7794d、4431e47cc4eb8b16、86af53be9a77da71。3c/baby/home 与旧池抽样不同，不跨集合比较旧分数；原 n=8、seed=1 合同不变。
- 审核：主代理-image-quality-0905-2245 自审；修复边界、调用者及全部任务 diff 已核对。未触碰其他组源码和运行资源；原采集任务未完成的长清单和历史笔记保留在工作树，不随本修复提交。
- 文档验证：`just docs-check`、`git diff --check` 通过；真实评分报告与模型质量未在本次验证范围内。
- 结果：内容隔离修复完成；图片池规模与真实质量闸门未通过，后续仍由 [image-eval-pool](../image-eval-pool.md) 按原合同采证。节点生成链正在其他任务中变更，正式 live 暂停；本任务未调用收费模型。
