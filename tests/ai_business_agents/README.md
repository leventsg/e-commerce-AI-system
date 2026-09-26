# 业务 Agent 重组验收

## 本地测试

在仓库根目录运行：

```powershell
python tests/ai_business_agents/run.py
go test ./services/aiagent/... ./apis/ai/...
go test ./...
```

测试源码保存在本目录，通过 Go overlay 编译到对应 internal 包。`ai_business_agents` build tag 防止根目录直接运行 `go test ./...` 时违反 Go internal 导入规则；因此全仓测试不能代替 overlay 命令。

如果 Windows 默认 Go 缓存出现 `Access is denied`，当前终端可使用独立缓存后重跑：

```powershell
$env:GOCACHE = Join-Path $env:TEMP 'codex-ai-business-agents-go-cache'
python tests/ai_business_agents/run.py
```

覆盖工具白名单与写操作归属、真实 ADK 委派链路、三类高风险中断/恢复、低风险写审计钩子、可信用户 ID、确认状态机、模型不可用、写操作超时、Skill 绑定、上下文注入和事件过滤。业务 Handler/确认存储由测试替身提供，不连接业务 RPC 或数据库。脚本模型指定调用顺序，用于验证编排和权限，不衡量自然语言路由准确率。

## 真实模型路由

在环境中配置 `AI_AGENT_LIVE_ROUTING=1`、`AI_AGENT_LIVE_API_KEY`、`AI_AGENT_LIVE_MODEL`、`AI_AGENT_LIVE_BASE_URL`，然后运行：

```powershell
python tests/ai_business_agents/run.py -run TestLiveBusinessAgentRouting -v
```

密钥通过环境配置，勿提交到仓库。入口使用实际 Supervisor Prompt、AgentTool schema 和合成用户请求，只检查首轮路由，不执行业务工具。未启用时明确输出 SKIP；启用后缺少配置会失败。

真实模型多轮验收还需在隔离业务测试环境中检查：

| 场景 | 期望 |
|---|---|
| 找耳机 → 比较优惠 → 选定数量与地址 → 下单 | 按必要依赖交接，缺参澄清，确认前不下单 |
| 普通订单查询 | 直接进入订单履约，不先进入选购 |
| 地址修改 / 用户名修改 | 分别进入订单履约 / general |
| 取消订单 / 删除购物车 | 分别由售后 / 购物决策触发确认，拒绝后不执行 |
| 退款、退换货、物流操作 | 说明未接入，不用取消订单替代，不声称已办理 |
| 结算处理中、执行结果未知 | 核实原操作，不创建新结算以重试 |
| 无政策依据 / 工具失败 | 明确无法核实，不编造政策或成功结果 |

## 本轮结果（2026-09-26）

- Overlay 测试、AI 服务与 AI API 测试通过。
- 前端 54 项测试及 TypeScript/Vite 构建通过。
- 全仓测试已运行，既有 RPC 集成测试依赖本机服务，存在连接拒绝及失败后空指针错误；全仓验收未通过。
- 真实模型检查未执行：环境缺少有效模型配置，仓库 API Key 为占位值。首轮入口已提供，多轮验收待测试环境就绪。
