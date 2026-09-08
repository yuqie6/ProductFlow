# 任务：图片比较显式记录展示目的与可比依据

状态：完成
类型：实现
认领者：root
认领于：2026-09-08T11:00:00+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：依据稳定标注选择生成链改进，不自动改生产提示词

遵循 [任务协议](../README.md)。

## 问题来源与目标

[评委校准](category-annotation-rubric.md) 原8个候选×3中，家电detail三次解释内部结构与外部局部摄影目的不同，却有两次仍填写对照分数。当前比较器只凭分数存在判断可比，展示目的仅在自由文本中。需要在模型输出、校验、比较与报告的同一条链上明确可比依据，防止不相同目的的图位进入分差/胜负统计。

## 冻结合同

comparison模式模型须提供comparison_basis：status为comparable/different_purpose/insufficient_evidence，target_purpose与reference_purpose说明各自目的，shared_requirements列共同适用要求，reason说明判定，evidence_asset_ids引用两张对应图片。comparable必须有共同要求及足够依据；不得因同属detail而自动可比，也不得因美观风格不同直接判不同目的。reference模式没有comparison_basis。

different_purpose/insufficient_evidence不得提供quality_reference_scores；不计算delta或胜负，保留目标分数/优缺点/不确定项及理由。complete且comparable须有对照分数。缺字段或矛盾输出作为invalid_output保留诊断，不能静默补成comparable。已有身份unknown/mismatch/critical规则保留，平均分不得掩盖身份问题。

输出schema与annotation contract升级v2，selection结构仍v1且按新contract生成；当前读写只支持新合同，不增加旧shape兼容读者。旧报告/selection文件保持原样，不覆写历史分数；校准用独立新selection记录仅合同元信息变化与相同图片哈希。

## 所有权与并行

account_backend独占go/internal/imageeval/annotation*.go及必要go/cmd/productflow-image-evals测试；只改上述因果链，不动历史slot judge、图生成/主体合成、Agent Skill/评测题集、数据库或前端。root持有本任务/父章程/go README/Git/最终审核与固定多模态复验。eval_baseline运行独立r6，不读写本范围；另会话subject-preserve live smoke保持暂停。执行者不commit/push/reset/revert，不能回退其它人的编辑。

## 验收与复验

复用既有类型/校验/报告；搜索所有当前读写端。确定性回归覆盖同目的合法对比、不同目的或证据不足拒绝分差/胜负、矛盾响应保留诊断、缺字段/错证据、reference无比较字段、身份关键问题仍阻止胜负。相关imageeval与CLI包测试，静态/构建按修改合同验证；自审完整diff交root。

root冻结与上轮相同8个候选/身份/质量对照图片，保持四维量表/比较算法/模型gpt-5.6-luna，每项3次共24；另原3次跨品类负控。只评委请求，无生成调用/共享DB。记录全部失败和原文诊断，不为绿补跑；比较可比判定一致性、有效率、身份/负控与具体理由。新合同变更单独声明，不直接将分数变化当生成改进。真实调用沿用户已授权开发验证，无自动晋升。

实现门与复验完成后root审阅并提交，只有证据支持的局部结论可以进入父章程。新合同存在实质失败则保留证据修正或明确未完成，不降低验收条件。

执行者五文件实现已停写交回，root审阅发现并修正CLI不可比较摘要对新状态的漏计；root持有go/cmd/productflow-image-evals/main.go。最终采用固定c7204726基线叠加限定六文件的独立checkout与二进制，明确记录base/patch/源码指纹，不把未提交候选称为干净commit。

## 验收证据与剩余缺口

交付随本任务提交。account_backend完成五文件实现与自审，root接手审核/CLI摘要修正/固定实测。六个代码文件经SHA复核与实测二进制来源一致；未修改生成链、原始图片、Agent题集或旧评分材料。

固定候选两包测试通过（imageeval 3.547s，CLI 0.014s），构建通过；执行者race测试通过。root运行27次真实评委，全部有效，未补采。8组各三次可比判定一致7/8、身份一致7/8、胜负一致5/8，负控3/3为mismatch/critical。家电detail三次均different_purpose且无分差/胜负；两类hero各三次仍可比。CLI摘要与6份报告实际不可比计数一致。docs-check与完整diff检查通过。

产物根 `storage-dev/image-annotation-basis-0908`：`candidate-source.json`、`candidate.patch`、`selection-lineage.json`、`frozen-inputs.json`、`execution.json`、`repeatability.json`/`.md`、`review.md`、六份原始报告与日志。固定checkout `/tmp/productflow-image-annotation-basis-0908` 的base为c7204726，叠加限定候选diff；不宣称候选为干净commit。输入与候选源码前后指纹不变，旧v1证据原样保留。

3c/scene的主要职责范围判定仍波动；家电scene/selling_point的胜负仍波动。部分非必需的接口/控制区信息仍误入weaknesses。旧合同胜负一致6/8，本轮5/8，不能宣称整体稳定性改善或标注准确率达标。本任务完成显式可比合同与局部复验，未证明生成质量提升、全池非劣性或自动修改提示词安全。
