# 任务：R6 干净冻结候选重跑门

状态：开放
类型：证据
认领者：—
认领于：—
业务组：平台可靠性
父账本：performance-governance.md
完成后可拆：总纲 R6 关闭裁定（仅当本门与所需 D4 证据齐且全 PASS）

按 [Issue 协议](README.md) 认领。承接 [B6 汇总 FAIL](archive/release-r6-readiness-gate.md)。

## 问题来源

B6 已诚实记 R6 未通过：G-07 脏树/HEAD 漂移 FAIL；安装 pin 与 G-07 HEAD 非同一候选；无冻结稳定版对正式 D4。

## 做成什么样

在**干净固定 checkout** 上选定**单一冻结候选**（发行 pin = G-07 源码身份或明确映射）；无缓存重跑 G-07；优先在冻结稳定版对上补正式 D4（若仍无稳定对则诚实记缺口，不得用等价 retag 冒充）。资源预算若本窗不做须标明。**全项未齐不得标 R6 通过。**

## 前置与并行

- 前置：B6 汇总已归档（FAIL）。
- 冻结输入：认领时钉死 commit / 发行 tag；禁止共享工作树并发提交污染窗口。
- 隔离项目；禁止共享 `productflow` down。
- 排他写入：本文件、父章程 R6 条目、必要时 release 备注；修 G-07 红灯时最窄修补另列清单。

## 只改这些文件

认领后补齐。

## 不要碰

- 把 B6 FAIL 改写成 PASS；降低 G-07 / D4 门槛；Skill/grader。

## 现在代码在哪

- 失败证据：[release-r6-readiness-gate](archive/release-r6-readiness-gate.md)
- 父章程：[performance-governance.md](../performance-governance.md) B6
- Go 红灯：agent catalog fixture；media/generation/metrics `merchant_id`
- Web：bundle 预算；lint 已在协调树修过 unused

## 合同

- 总纲 R6；完成可 FAIL；缺干净候选或 D4 缺口则不得关闭 R6。

## 怎么验收

- 干净 `git status`；起点/终点同一 HEAD
- G-07 逐步 exit 表；D4 或诚实缺口
- 父章程 R6 结论与证据链接一致

## 阻塞与交接

- 原因：无
- 解除条件：无
- 跟进者：无
- 交接：无

## 证据

- 命令 / 日期 / 结果：
- 审核者 / 结论：
- Issue 结果 / 业务门槛结果 / 剩余缺口：
