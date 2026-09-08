# AI 客服系统面经

## 1. 项目简介

基于 Go 和 go-zero 为电商系统新增 AI 客服编排层，提供 SSE 流式对话、Eino Agent 多工具编排、业务工具安全执行、高风险操作确认、对话记忆和审计追踪能力。项目重点不是重写电商后端，而是把现有业务服务封装成可控的 AI 工具，并通过服务端身份注入、确认状态机和结构化工具结果保证 AI 操作可控、可追踪、可恢复。

## 2. 简历 Bullet

- **可扩展 AI 编排：** 针对单一 Agent 绑定大量业务工具后容易误选工具、提示词边界不清的问题，设计为对外流式网关和内部 Agent 编排服务两层结构，由 Supervisor 负责意图判断和任务拆解，领域 SubAgent 只暴露本领域工具集合；结果是新增客服能力主要通过注册工具和补充领域提示词接入，核心对话链路保持稳定，线上收益待压测和真实使用数据验证。
- **安全工具执行：** 针对模型输出不可被信任、客户端可能伪造身份字段的问题，将业务动作统一收敛到工具注册表和执行网关：工具 schema 不暴露 `user_id`，执行前由服务端上下文注入用户身份，并统一处理参数校验、超时、失败封装和审计；结果是 AI 工具调用和用户数据访问形成统一安全边界，可通过伪造身份参数的测试验证。
- **高风险确认闭环：** 针对下单、取消订单、删除购物车等不可直接由模型落库的操作，引入确认状态机和可恢复执行机制：高风险工具先创建确认记录并中断执行，用户批准后基于保存的运行态恢复同一次工具调用，拒绝、过期、重复确认和跨用户确认均被后端拦截；结果是模型建议和真实写操作被隔离，可通过确认并发、重复点击和过期场景验证。
- **上下文工程治理：** 针对多轮客服历史过长、工具结果过大和用户偏好需要长期复用的问题，采用近期消息窗口、滚动摘要、长期记忆事件、用户画像和工具结果按需读取的分层上下文机制；结果是模型输入不依赖无限历史，写操作前仍以业务服务重查结果为准，避免旧上下文直接驱动敏感操作。
- **失败与审计治理：** 针对工具调用失败后模型可能生成成功话术、排障缺少链路证据的问题，将工具结果统一封装为结构化 envelope，并将对话、消息、工具调用、确认和运行态写入 AI 专属数据表；结果是每次工具调用都有用户、会话、参数摘要、状态、耗时和错误信息，可支撑失败复盘和安全审计。
- **知识库增强降级：** 针对普通客服问答需要接入外部知识但不能拖垮主对话链路的问题，设计 RAG 分类、检索、向量缓存、来源注入和静默降级流程；结果是知识库可作为增强上下文进入当轮回答，检索失败时仍能回到基础 Agent 对话，完整线上效果待验收。

## 3. 面试问题

### 3.1 架构与主链路

**主问 1：你这个 AI 客服系统整体是怎么设计的？**  
口播：我会先把它定义成一个 AI 编排层，而不是电商后端的重写。用户从 `apis/ai` 发起 SSE 请求，网关只负责鉴权、请求校验和事件流转发；真正的智能编排在 `services/aiagent`，里面用 Eino 的 Agent 和 Tool 机制完成意图识别、工具调用和最终回复。我的设计目标是让模型只负责理解和编排，真实业务事实仍来自已有业务服务。这样做的结果是 AI 客服可以查询商品、订单、优惠券，也可以发起低风险写操作；涉及下单、取消订单等高风险动作时，会进入确认流程，避免模型一句话直接改业务数据。

追问 1：为什么不把 AI 客服直接写在现有订单或商品服务里？  
口播：我没有把 AI 能力塞进订单或商品服务，是因为 AI 客服的核心变化点在对话、上下文、工具编排和风险控制，而不是某一个业务域。如果侵入业务服务，会让订单、商品这些稳定服务依赖 Eino、模型配置、prompt 和流式协议，边界会变得很乱。现在的方式是新增 `apis/ai` 和 `services/aiagent`，业务服务仍作为事实来源，AI 层通过 RPC 或内部 API 调用它们。这样新增工具、换模型、调整确认策略，都主要发生在 AI 编排层，业务服务只需要保持原有契约稳定。

追问 2：一次用户消息从进来到返回，大概经过哪些步骤？  
口播：一次请求进来后，网关先从登录态取用户 ID，校验客户端消息类型和 `client_message_id`，然后调用 aiagent 的流式 RPC。aiagent 会先保存用户消息和会话信息，再构造 Agent 运行上下文，MemoryMiddleware 注入历史摘要、近期消息和工具事实。Supervisor 根据用户输入选择合适的领域 SubAgent，SubAgent 再调用经过注册的工具。工具执行后返回结构化结果，Agent 基于结果生成最终 assistant 消息，后端持久化后再通过 SSE 推给前端。这个链路里模型不直接拿到可信身份，也不直接操作数据库。

**主问 2：为什么用了 Supervisor + SubAgent，而不是一个 Agent 解决全部问题？**  
口播：一开始从能力上看，一个 Agent 挂所有工具也能跑，但工具数量一多，模型会面临搜索、订单、购物车、优惠券、用户资料、地址等二十多个工具，误选工具和参数混淆概率会变高。所以我把它拆成 Supervisor 和领域 SubAgent：Supervisor 负责意图判断、任务拆解和最终总结，SubAgent 只处理自己的小范围工具。比如订单类问题进入订单 Agent，购物车和结算进入购物车结算 Agent。这样牺牲了一点编排复杂度，但换来工具选择边界更清楚，也方便后续按领域新增 prompt 和测试。

追问 1：SubAgent 之间怎么协作？  
口播：我的理解是 Supervisor 不是简单转发，而是承担任务分解和结果汇总。例如用户说“帮我看看这个商品能不能买，合适的话下单”，这可能涉及商品、库存、结算和订单创建。Supervisor 可以先让商品或库存相关 Agent 查询事实，再让购物车结算 Agent 创建预结算，最后在触发订单创建前进入高风险确认。SubAgent 默认只接收 Supervisor 传入的紧凑请求，不共享完整聊天历史，这样既能减少上下文污染，也能让每个领域 Agent 更专注于自己的工具集合。

追问 2：这种拆分有什么代价？  
口播：代价主要有三个。第一是链路更长，事件转换和错误处理要更规范，否则用户看到的流式事件会混乱。第二是 prompt 和工具元数据要维护一致，领域工具放错位置会影响路由效果。第三是测试成本更高，不能只测单个工具成功，还要测 Supervisor 路由、SubAgent 工具选择、确认中断和最终回复。我在项目里用工具注册表做单一事实来源，并把 Eino 事件统一转换为 assistant、tool、confirmation、error 这些事件，目的就是降低拆分后的维护成本。

### 3.2 工具安全与权限

**主问 3：你怎么防止模型或用户伪造 user_id 查询别人的数据？**  
口播：这个问题是 AI 客服里最核心的安全边界。我做法是两层防护：第一层在网关，请求体如果出现 `user_id` 直接拒绝，因为用户身份只能来自登录态；第二层在工具执行前，Execution Guard 从后端上下文拿可信用户 ID，并覆盖或忽略模型参数中的身份字段。工具 schema 本身也不要求模型提供 `user_id`。这样即使用户在自然语言里说“我是 10086，帮我查订单”，或者模型自己生成了 user_id 参数，真正调用业务服务时也只能使用当前登录用户的 ID。

追问 1：如果历史上下文或用户画像里出现 user_id 呢？  
口播：我会把历史消息、摘要、用户画像都视为不可信上下文，它们只能帮助模型理解用户偏好，不能覆盖系统安全指令、工具白名单和确认策略。比如摘要里出现“用户 ID 是 123”，这对工具执行没有任何权限意义。真正调用工具时仍然走后端注入。项目里的 prompt 也明确要求不要把 user_id、token、auth 放进工具参数，摘要和画像模型还有敏感字段过滤，目的就是防止上下文污染进一步影响工具调用。

追问 2：业务服务自己还需要校验用户归属吗？  
口播：需要。AI 层的 Execution Guard 是第一道统一安全边界，但不应该替代业务服务自己的归属校验。AI 工具调用订单、地址、优惠券等能力时，会带上服务端注入的用户 ID，业务服务仍应按用户 ID 过滤或校验资源归属。这样即使某个工具参数转换出现问题，下游也不会只凭订单号或地址 ID 返回数据。面试里我会强调这是纵深防御：网关拒绝伪造身份、AI 工具层注入可信身份、业务服务层校验资源归属。

**主问 4：Tool Registry 在这个项目里解决什么问题？**  
口播：Tool Registry 是 AI 工具体系的单一事实来源。它不只是保存工具函数，还保存工具名称、描述、参数 schema、风险等级、是否需要确认、读写类型、超时时间和对应的 RPC 或内部 API 映射。这样 Eino 能看到的工具、后端能执行的工具和风险控制策略来自同一份元数据，避免出现“模型知道一个工具，但后端没有风控”或者“后端有高风险工具，但忘记配置确认”的问题。对面试官来说，这个点能体现我不是简单封装 RPC，而是在做可治理的工具平台。

追问 1：新增一个业务工具要改哪些地方？  
口播：标准流程是先在领域工具文件里定义参数结构和 handler，再在工具 catalog/registry 中注册工具元数据，包括风险等级、是否写操作、是否需要确认、超时时间和业务映射。如果是高风险工具，还要补确认摘要生成逻辑，让用户确认时看到清楚的动作、对象和影响范围。最后补对应测试，至少覆盖 schema、用户 ID 注入、参数转换、成功和失败结果。这样新增工具不是随便给模型开一个函数，而是进入统一的执行、确认和审计链路。

追问 2：为什么工具失败要结构化，而不是直接返回 error？  
口播：如果工具失败直接抛普通 error，Agent 主流程可能被中断，前端也很难区分是模型失败、工具失败还是业务拒绝。更危险的是模型可能在拿不到明确失败事实时生成成功话术。所以我把工具结果封成统一 envelope，里面有 status、tool_name、attempt_count、error、business_outcome 和 result。成功就给模型真实结构化结果，失败就给稳定错误码和安全提示，同时写入工具调用表。这样前端能展示失败，模型也不能把失败总结成成功。

### 3.3 高风险确认

**主问 5：高风险确认流程怎么实现？**  
口播：高风险确认的核心是把“模型决定要做什么”和“系统真正执行写操作”拆开。模型或 SubAgent 触发高风险工具时，Eino 的工具调用会先被 approval middleware 拦截，后端创建一条确认记录，里面保存用户、会话、工具名、参数摘要、过期时间、运行 ID 和 checkpoint 信息，然后返回 `confirmation_required` 事件给前端。此时不会调用真实业务 RPC。用户点击确认后，后端校验确认记录仍属于当前用户、未过期、未执行，再恢复原来的工具调用。执行成功后确认状态变为 executed，失败则记录 failed。

追问 1：怎么防止用户重复点击确认导致重复下单？  
口播：我会从状态机和并发锁两层解释。确认记录有 pending、approved、rejected、expired、executed、failed 等状态，只有 pending 状态才能被领取执行。领取时使用 Redis 短锁合并并发请求，如果 Redis 不可用，则降级到 MySQL 条件更新，用 user_id、conversation_id、旧状态和过期时间做 CAS。这样多个确认请求同时进来，只有一个能把状态从 pending 推进，其他请求会拿到已处理或忙碌状态，不会重复调用下游业务。

追问 2：用户拒绝确认时为什么不恢复 Agent？  
口播：拒绝确认是一个确定性的终态，不需要再让模型基于旧 checkpoint 继续推理。后端可以直接持久化一个取消类 tool_result 和最终 assistant_message，告诉用户操作已取消。这样做可以避免模型恢复后再次复述旧的确认请求，或者误以为还要继续执行。只有用户批准时才需要 resume 原来的高风险工具调用，因为这时我们要尽量保留当时模型已经构造好的参数和任务上下文。

**主问 6：系统宕机后，确认中的操作怎么恢复？**  
口播：项目里把 Redis 当热缓存，把 MySQL 当事实来源。高风险工具中断时，除了 Redis checkpoint，还会在 AgentRun 和 Confirmation 相关记录里保存运行态、checkpoint ID、确认 ID 和参数信息。如果系统宕机或 Redis 丢失，服务恢复后可以基于 MySQL 里的确认状态和 checkpoint blob 找回待确认动作。面试时我不会说它天然百分百恢复所有业务副作用，而是说这个设计目标是让“是否执行过、是否还能执行、执行参数是什么”有持久化依据，再配合下游业务幂等来完成恢复。

追问 1：如果业务 RPC 执行成功但审计写失败怎么办？  
口播：这个场景不能简单让用户重试，因为业务写操作可能已经发生了。设计文档里明确了这种情况要返回失败并标记业务已执行，同时确认状态仍推进到 executed，避免用户再次点击造成重复写入。后续应该依赖审计补偿或后台修复去补齐审计记录，而不是把业务状态回滚成未执行。这个回答的重点是：外部副作用一旦发生，系统要保护幂等和可追踪，不要用用户重试制造二次副作用。

追问 2：哪些操作必须确认，哪些不用？  
口播：项目里把工具按风险等级区分。查询、商品推荐、添加购物车、减少购物车数量、领取优惠券、修改用户资料、地址新增更新删除属于低风险或可恢复操作，不需要高风险确认。但删除购物车商品、创建订单、取消订单、使用优惠券参与下单这类会影响资产、订单或不可轻易撤销的动作，需要确认。这个划分不是固定真理，而是基于业务影响面、可逆性和用户预期做的风险分层，后续可以通过工具元数据继续调整。

### 3.4 上下文、记忆与 RAG

**主问 7：多轮对话上下文怎么控制？**  
口播：我没有采用无限历史拼接，而是采用分层上下文。最优先的是系统安全指令和工具协议，其次是当前用户输入、任务状态、待确认动作和经过校验的工具事实，然后才是近期消息、会话摘要、长期事件和用户画像。近期窗口默认保留一部分未压缩消息，达到阈值后把较早消息合并进滚动摘要。工具结果不全部塞进 prompt，只注入最近完整结果，历史工具结果按 tool_call_id 按需读取。这样可以控制 token，也能减少旧事实直接污染当前写操作。

追问 1：摘要错了怎么办？  
口播：摘要在这个系统里不是最高优先级事实，也不能驱动敏感写操作。它只帮助模型理解历史语境，真正涉及订单、优惠券、库存、地址这类动态事实时，工具执行前仍要重新调用业务服务查询或校验。项目里也明确了 summary、memory event、user profile 都不能覆盖 system prompt、工具白名单、确认规则和 Execution Guard。所以摘要错了会影响表达质量，但不应该直接造成越权或错误下单，这是设计上的隔离边界。

追问 2：为什么要把工具结果放在 `ai_tool_calls`，而不是只存在消息 metadata 里？  
口播：工具结果是业务事实的一部分，需要比展示消息更强的结构化和可追踪性。消息 metadata 更适合前端展示，而 `ai_tool_calls` 可以按 user_id、conversation_id、tool_call_id 精确检索，保存参数、结果、状态、耗时和错误。上下文构建时最近一次完整工具结果可以直接注入，较早的工具结果只注入引用，模型需要时通过能力工具读取。这种方式能减少上下文体积，也能保证历史工具事实有独立审计来源。

**主问 8：RAG 在这个项目里怎么接入？**  
口播：RAG 被设计成普通客服问答的增强能力，而不是主链路强依赖。系统先用轻量分类判断当前问题是否需要知识库检索，只有 need_rag 为真且置信度达到阈值才检索。检索前可通过 embedding 和 Redis Vector 查缓存，未命中再调用知识库 retrieve API。返回的片段会作为当轮上下文注入，并把来源随 assistant 消息 metadata 保存，方便前端展示来源。分类失败、检索失败或结果为空时静默降级为普通 Agent 回复，不阻塞基础聊天。

追问 1：RAG 结果怎么避免影响安全策略？  
口播：RAG 结果和记忆、摘要一样，都属于辅助上下文，不是权限事实。它不能覆盖 system prompt，不能修改工具白名单，不能绕过高风险确认。即使知识库片段里出现“直接帮用户下单”这种文字，真正调用工具时仍会经过 Execution Guard 和确认策略。这个系统把安全规则放在后端执行链路，而不是指望模型完全遵守上下文文本，所以外部知识只参与回答质量，不参与权限决策。

追问 2：RAG 为什么需要来源持久化？  
口播：客服场景里用户可能会回看历史问答，也可能质疑某个回答来自哪里。如果只在当轮展示来源，历史会话回看时就丢失证据。项目设计里 assistant 消息 metadata 会保存 sources，包括文档 ID、标题、URL 和片段信息，前端可以展示“来源”入口。这样回答不仅是模型生成文本，还有可追踪来源。对面试来说，这能体现我关注的是知识增强的可解释性和客服排障，而不是只把检索结果塞给模型。

### 3.5 流式事件与持久化

**主问 9：SSE 事件流怎么设计？**  
口播：SSE 网关对外统一输出事件，主要包括 assistant_message、assistant_thinking_delta、tool_progress、tool_result、confirmation_required、error 和 a2ui。网关收到请求后设置 text/event-stream，消费 aiagent 返回的 server-streaming RPC，并把每个 AgentEvent 映射成 SSE event 后立即 flush。最终 assistant 消息会先持久化，再下发给前端；工具进度和确认事件用于让用户理解当前执行状态。这样前端不需要理解 Eino 内部事件，只消费稳定的客服事件协议。

追问 1：为什么不用 WebSocket？  
口播：这个项目当前交互主要是“客户端发一条消息，服务端持续返回一串事件”，SSE 已经能满足单向流式输出，而且实现和部署相对简单，浏览器端也容易消费。确认动作可以作为新的 POST 请求进入，再重新建立或继续消费事件流。WebSocket 更适合强双向、低延迟、高频互动场景，但会增加连接状态管理复杂度。这里选择 SSE 是根据业务交互形态做的取舍，而不是说 WebSocket 不好。

追问 2：前端怎么知道工具调用失败？  
口播：工具失败不会藏在自然语言里，而是通过 `tool_result` 或 `error` 事件传给前端，事件中有 tool、status、data_json 等字段。后端的工具执行器会把失败封装成稳定结构，网关只负责透传或映射为前端协议。最终 assistant 回复也必须基于真实工具结果生成，不能在工具失败时说“已完成”。所以前端可以在 UI 上展示失败状态，用户也能看到明确的稍后重试或业务失败提示。

**主问 10：AI 客服的数据表怎么设计？**  
口播：AI 客服的数据表围绕可追踪链路设计，而不是只存聊天文本。核心包括会话表、消息表、工具调用表、确认表、用户画像表、长期记忆事件表、会话摘要表和 Agent 运行态表。会话和消息支持历史回看，工具调用表记录真实工具参数和结果，确认表记录高风险操作状态，运行态表支持中断恢复，画像和记忆事件用于长期上下文。所有这些表都带 user_id 或 conversation_id 维度，用来保证查询隔离和排障定位。

追问 1：`client_message_id` 有什么作用？  
口播：`client_message_id` 是用户消息的幂等标识。网络抖动或前端重试时，同一条用户消息可能被提交多次，如果后端每次都跑模型和工具，就可能重复执行写操作。项目的数据模型里对用户消息做了用户维度的唯一约束，重复提交相同 client_message_id 时，可以复用已有 assistant 或 tool 结果，而不是重新触发模型和业务工具。这个设计对 AI 客服尤其重要，因为一次用户消息可能背后触发多个工具调用。

追问 2：历史会话怎么保证只能看自己的？  
口播：历史会话接口不允许客户端传用户身份，网关从登录态拿用户 ID，再转发到 aiagent。aiagent 查询会话列表和消息时也按 user_id 过滤，并在查某个 conversation_id 的消息前做归属校验。如果会话不存在或不属于当前用户，会返回拒绝或不存在，不会仅凭 conversation_id 直接查消息。这样历史消息、工具结果和记忆上下文都维持同一个隔离原则。

### 3.6 失败治理与测试

**主问 11：模型不可用时怎么办？**  
口播：模型不可用不能编造业务结果。项目里的策略是明确降级，返回类似“AI 服务暂时不可用，请稍后重试”的错误提示；如果是业务工具失败，也返回工具失败的结构化结果，而不是让模型猜结果。对查询类工具可以做有限重试，对写操作默认不自动重试，除非下游有明确幂等语义。这样虽然用户当下可能拿不到回答，但不会产生“系统没查到却说查到了”或“下单失败却说成功”的严重问题。

追问 1：工具调用怎么做重试？  
口播：工具重试要区分读写和错误类型。查询类或能力类工具可以对瞬时异常做有限重试，配置最大次数、退避和总耗时上限；写工具默认不自动重试，因为重复写入可能产生副作用。即使要重试写工具，也必须确认下游有幂等键或状态机保护。项目里的 executor 和 retry 逻辑就是为了把这种策略统一到工具层，而不是让每个 handler 自己随意 retry。

追问 2：熔断现在做到什么程度？  
口播：设计文档里有工具失败治理的分层规划，包括工具层重试、对话预算、熔断降级和结构化日志。但从实施计划看，当前主要落地的是工具层重试、结构化结果和部分观测；更完整的全局预算、熔断指标和治理日志属于后续任务。面试时我会如实说：这部分架构设计已经考虑了，但不要把未完全验收的熔断能力说成线上成熟能力。

**主问 12：你会怎么验证这个系统？**  
口播：我会按链路和风险分层验证。第一层是单元测试，覆盖模型工厂、工具注册、参数转换、Execution Guard 用户 ID 注入、确认状态机、上下文摘要和 RAG 客户端。第二层是 API/RPC 集成测试，覆盖 SSE 事件流、未登录拒绝、历史会话查询、普通查询工具和高风险确认。第三层是安全测试，包括伪造 user_id、跨用户 conversation_id、过期确认、重复确认、工具失败不能总结成功。最后再跑 `go test ./services/aiagent/...`、`go test ./apis/ai/...` 和必要的全仓测试。

追问 1：哪些测试最能体现项目价值？  
口播：我认为最有价值的是三类测试。第一类是身份隔离测试，证明用户和模型都不能覆盖后端注入的 user_id。第二类是高风险确认测试，证明下单或取消订单在确认前不会调用业务服务，确认后只能执行一次。第三类是工具失败测试，证明下游失败会作为失败事件和工具记录持久化，不会被 assistant 总结成成功。这三类测试直接对应 AI 客服最大的风险，不只是验证功能能跑。

追问 2：当前项目还有哪些不足？  
口播：我会把不足说清楚。第一，真实线上指标缺失，所以不能宣称转化率或客服成本改善，只能说架构上支持这类目标。第二，RAG 虽然已有代码和设计，但完整验收需要继续补齐前端来源展示、缓存命中效果和失败降级测试。第三，工具治理里的全局预算、熔断和指标体系还可以继续加强。第四，高风险写操作还依赖下游业务幂等和状态机配合，AI 层不能单独解决所有分布式一致性问题。

## 4. 源码证据索引

| 主题 | 关键路径与内部符号 | 对应正文位置 |
| --- | --- | --- |
| 产品与架构边界 | `docs/ai-customer-service-prd.md`、`docs/ai-customer-service-design.md`、`docs/ai-customer-service-implementation-plan.md` | 项目简介、主问 1 |
| SSE 网关 | `apis/ai/ai.api`、`apis/ai/internal/logic/chatlogic.go`、`apis/ai/internal/types/protocol.go` | 主问 1、主问 9 |
| 网关拒绝 user_id | `apis/ai/internal/logic/chatlogic.go` 中请求解析与 `UserID` 检查 | 主问 3 |
| Agent 编排 | `services/aiagent/internal/eino/agent.go`、`services/aiagent/internal/eino/event_helpers.go`、`services/aiagent/internal/prompts/agent` | 主问 2 |
| Eino 事件转换 | `services/aiagent/internal/eino/iterator_events_test.go`、`services/aiagent/internal/eino/a2ui.go` | 主问 9 |
| 工具元数据 | `services/aiagent/internal/tools/registry.go`、`services/aiagent/internal/tools/catalog.go`、`services/aiagent/internal/domain/tool.go` | 主问 4 |
| 工具执行与失败封装 | `services/aiagent/internal/tools/executor.go`、`services/aiagent/internal/tools/retry.go`、`services/aiagent/internal/tools/executor_retry_test.go` | 主问 4、主问 11 |
| 执行上下文与用户注入 | `services/aiagent/internal/tools/helper/tool_helper.go`、各 `services/aiagent/internal/tools/business/*` | 主问 3 |
| 高风险确认中断 | `services/aiagent/internal/eino/approval.go`、`services/aiagent/internal/tools/approval_manager.go` | 主问 5 |
| 确认状态机 | `services/aiagent/internal/confirmation/manager.go`、`services/aiagent/internal/confirmation/locker.go` | 主问 5、主问 6 |
| 确认恢复入口 | `services/aiagent/internal/logic/confirmactionlogic.go`、`services/aiagent/internal/eino/checkpoint_store.go` | 主问 5、主问 6 |
| 上下文工程 | `services/aiagent/internal/memory/provider.go`、`services/aiagent/internal/memory/customer_service_provider.go`、`services/aiagent/internal/eino/memory_middleware.go` | 主问 7 |
| 摘要与画像 | `services/aiagent/internal/contextmanager/summary.go`、`services/aiagent/internal/profileextractor`、`services/aiagent/internal/memoryeventextractor` | 主问 7 |
| 工具结果引用 | `services/aiagent/internal/contextmanager/tool_results.go`、`services/aiagent/internal/tools/capability/tool_call_result.go`、`dal/model/ai/tool_calls` | 主问 7、主问 10 |
| RAG | `services/aiagent/internal/rag`、`services/aiagent/internal/prompts/rag/rag.go`、`services/aiagent/internal/domain/rag.go` | 主问 8 |
| AI 数据模型 | `dal/model/ai/conversations`、`dal/model/ai/messages`、`dal/model/ai/confirmations`、`dal/model/ai/agent_runs` | 主问 10 |
| 历史会话接口 | `services/aiagent/internal/logic/listconversationslogic.go`、`services/aiagent/internal/logic/listmessageslogic.go`、`apis/ai/internal/logic/historylogic.go` | 主问 10 |
| 审计 | `services/aiagent/internal/audit/recorder.go`、`services/aiagent/internal/audit/recorder_test.go` | 简历 bullet、主问 4 |

## 5. 交给 /interview 的高风险 Claim 清单

- Ownership Claim：可以说“参与/负责 AI 客服编排层、安全工具执行、高风险确认、上下文工程等模块设计与实现”，除非你能证明是单人主导，不建议说“独立负责整个 AI 客服系统”。
- Metric Claim：仓库没有真实线上转化率、响应耗时、成本下降、准确率指标，不要写百分比；可以写“具备压测和线上指标验证计划”。
- Architecture Claim：Supervisor + SubAgent、Execution Guard、确认状态机、滚动摘要、工具结果引用都有源码或文档证据，可以重点讲。
- Result Claim：可以说“实现了 AI 客服的主要工程闭环和多类测试”，但全量 `go test ./...` 是否通过需要当场验证后再说。
- Risk Claim：RAG、熔断、全局预算、完整观测属于待完善或后续增强，不要包装成已经大规模线上成熟。
