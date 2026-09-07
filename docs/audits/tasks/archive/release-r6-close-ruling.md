# 任务：总纲 R6 关闭裁定

状态：完成
类型：证据
认领者：主代理-cto-r6-close
认领于：2026-09-07T16:12:13+08:00
完成于：2026-09-07T16:12:13+08:00
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：无（R6 残余缺口另记；≠R1–R5 / ≠S3）

按 [Issue 协议](../README.md) 由维护者自认领并裁定。承接资源预算门与既有 D1/D3/D4/G-07/pin 证据链。

## 问题来源

ROADMAP R6 要求：干净安装、固定候选全量检查、重启、备份恢复和升级演练、资源预算与部署规模对应。各分项证据已齐，需维护者对总纲 R6 作关闭裁定，并列出残余非宣称项。

## 做成什么样

1. 按 ROADMAP R6 逐条对照既有归档证据，给出 **通过 / 未通过**。
2. 更新 `docs/ROADMAP.md` R6 行、父章程与 `release/README.md`。
3. **不得**顺带宣称 R1–R5、S3、SLA/RPO/RTO、满载吞吐或正式 registry push。

## 合同

- 关闭依据仅为 ROADMAP R6 条文与已归档证据；残余项写明「不阻塞本裁定」或「阻塞」二选一。
- 维护者自审须注明。

## 证据与裁定（2026-09-07）

### 对照表

| ROADMAP R6 项 | 证据 | 结论 |
|---|---|---|
| 干净安装 | B6 等价 D1：pin `0.0.0-5ed2b916b569`，登录/设置/空 provider/重启 PASS（[release-r6-readiness-gate](release-r6-readiness-gate.md)）；候选 pin 上 bootstrap/登录/四项 health（[release-r6-resource-budget](release-r6-resource-budget.md)） | **齐** |
| 固定候选全量检查 | G-07：干净 checkout `e8cb494d`+修补 → 交付 `67b0f309` 无缓存全量 PASS（[release-r6-clean-candidate-gate](release-r6-clean-candidate-gate.md)） | **齐** |
| 重启 | 含于 B6 等价 D1 | **齐** |
| 备份恢复 | B4 隔离全栈 D3 PASS（在途 lease **UNKNOWN**，见残余）（[release-d3-restore-drill](release-d3-restore-drill.md)） | **齐**（主路径） |
| 升级演练 | 正式 D4：`0.0.0-5ed2b916b569`→`0.0.0-67b0f3092158` 主路径 + migrate fail-stop PASS，非等价 retag（[release-r6-pin-and-formal-d4](release-r6-pin-and-formal-d4.md)） | **齐** |
| 资源预算与部署规模对应 | 同 pin 默认单副本空闲/轻负载足迹 + 配置上限对照 + 观测依据建议 ≥2 vCPU / ≥4 GiB（**非 SLA**）（[release-r6-resource-budget](release-r6-resource-budget.md)） | **齐**（合同口径：空闲/轻负载） |

冻结发行身份：pin `0.0.0-67b0f3092158` ≡ `67b0f3092158d72c5e6e118761808fe5f48ef5a1`。

### 总纲裁定

**总纲 R6：通过。**

依据：上表六项均有可归档主路径证据；资源预算门合同明确以空闲/轻负载采证对应关系，满载不属于本条关闭前置。

### 残余非宣称（不阻塞本裁定）

1. 满载 provider / Graph / 生图 / Agent Turn 打满未测；观测主机建议 **≠ SLA**。
2. 多商家资源公平调度未测（归 R1/商家平台与后续容量，≠本条）。
3. staging 双副本共享卷未测。
4. D3 在途 lease/unknown 夹具仍 UNKNOWN。
5. 浏览器「缺 provider」UI 未采证；等价 D1 主 pin 为 `5ed2b916b569`（候选 pin 另有登录探活）。
6. 默认 `release-build-images` 对本机构 `registry.npmjs.org` TLS 曾失败（已用镜像 registry 同 SHA 构建）；无正式 registry RepoDigest / push。
7. **≠ R1–R5**；**≠ S3 首个稳定自托管正式版**；不写 RPO/RTO/SLA。

### 审核

- 审核者：CTO（本会话，自审）；结论：证据链可关闭 ROADMAP R6；残余项已列。
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/release-r6-close-ruling.md` 查询）
- Issue 结果 / 业务门槛结果 / 剩余缺口：完成；**总纲 R6 通过**；残余见上（不阻塞）。
