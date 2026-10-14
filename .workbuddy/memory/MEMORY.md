# 项目长期笔记

## Self-Evolving Skill：已确认的设计边界与后续方向（2026-10-02）

来自用户对自进化 skill 实现的追问，确认了两个未实现/待补方向（跨会话有用）：

1. **Skill 配置化加载（缺口）**：当前 skill 是 `go:embed` 内嵌 + `agentSkillBindings` 硬编码 map，要新增/修改 skill 绑定到 agent 需改代码。后续方向：`skills` 包支持外置目录扫描 `*/SKILL.md`（内嵌默认 + 外置覆盖/追加），`agentSkillBindings` 改为配置驱动（config 加 `SkillBindings`），Eino `skillBackend` 换数据源（官方支持 filesystem_backend）。这样"人工批准候选 → 放 SKILL.md + 配置加一行绑定 → 重启生效"，零代码改动，且与自进化候选产物天然衔接。

2. **评测环境 vs 生产一致性（边界）**：当前 A/B 用模拟业务环境（evaluation.State + 固定 fixture），不产生副作用、不碰真实 RPC，但**无法保证与生产一致**——只验证"策略语义正确性"（触发/澄清/只读/失败不报成功/安全边界，环境无关）；数据依赖行为需真实数据快照回放才能验证。补缺口方向：脱敏真实数据快照回放离线评测 → 生产影子执行（shadow，写操作不真发 RPC 只记录）→ 灰度。

其他已澄清设计决策（供面试引用）：
- 任务成败标签**不在 Langfuse 记录时打**，Langfuse 只记工具级 `business_outcome`/`status` 等观测事实；任务级 conclusion（success/failure/appropriate_stop/unknown）由**独立分析模型**在自进化分析阶段综合判断（规则：工具成功≠任务完成、拒绝确认≠失败、不推翻服务端结果、证据不足→unknown）。
- 成功案例也要分析，用于提炼 `success_pattern`/`reusable_step`；"结果成功但过程曲折"仍算 success，但会提炼可优化步骤。
- **测试集定位：护栏而非出题人（核心认知，面试易被追问）**。「发现未知」靠**生产轨迹分析**（开放世界），「验证不退化/不越权」靠**固定测试集**（封闭世界，只能验已知的已知）。测试集不是"答案全集"，不是"穷举所有情况"，而是"已知能力的回归护栏 + 安全护栏"。若候选能力在测试集覆盖不足 → 输出 `insufficient_coverage`，不假装通过（针对"人没想到的新能力"确实测不了，需影子执行/灰度/人工补用例）。"针对测试集产 skill"= 过拟合/泄题，是设计里禁止的（生成器不能读测试集）。

## 上下文管理架构：稳定前缀/易变后缀分离 + 三 Builder（2026-10-12）

核心动机：**prompt cache 按前缀字节匹配**，故上下文必须拆成「稳定前缀（turn 内不变，放最前）」+「易变后缀（每次调用变，放最后）」。

- **稳定前缀**：用户画像 + 长期事件 → `RetrieveResult.SystemMessages`，注入 system prompt 后；turn 内只检索一次（ctx 缓存 `memoryStablePrefixKey`），字节不变以命中 prompt cache。
- **易变后缀**：摘要 + 近期历史 + 任务状态 + 工具上下文 + 当前时间 + RAG → `HistoryMessages` + `ContextMessages`，每次模型调用前刷新（含压缩 CompressIfNeeded）。
- **三 Builder 模块化**（`internal/memory`）：`MemoryBuilder` 纯取数（不计算 token）、`ContextBuilder` 编排（压缩 + token 预算 + 分层组装）、`RAGBuilder`（`internal/rag` 接口，`*rag.Service` 实现；检索在 chatlogic turn 入口一次，结果经 meta 传入）。
- **关键决策**：稳定前缀缓存用 **ctx 非 redis**（稳定前缀体量小、查询便宜）；RAG turn 内一次但位置易变；压缩放 ContextBuilder、token 计算放 ContextBuilder（MemoryBuilder 不碰 token）；`RetrieveRequest.SkipStable` 控制首次全量/后续只刷易变。
- **口径注意**：旧的「每 turn 去重只组装一次」已废弃，改为「稳定 turn 内一次 + 易变每次刷新」。
