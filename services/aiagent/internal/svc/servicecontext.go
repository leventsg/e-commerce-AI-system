package svc

import (
	"context"
	"net/url"
	"time"

	aiconfirmations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	aiconversationsummaries "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversation_summaries"
	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	aiusermemories "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_memories"
	aiaudit "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/audit"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	aiconfirmation "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/audit/auditclient"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ConfirmationManager interface {
	// 用户确认请求的决策处理，返回确认记录
	Decide(ctx context.Context, req aiconfirmation.DecisionRequest) (*domain.Confirmation, error)
	// 标记确认请求为已执行状态
	MarkExecuted(ctx context.Context, req aiconfirmation.CompletionRequest) (*domain.Confirmation, error)
	// 标记确认请求为已失败状态
	MarkFailed(ctx context.Context, req aiconfirmation.CompletionRequest) (*domain.Confirmation, error)
}

type HighRiskToolExecutor interface {
	ExecuteConfirmed(ctx context.Context, req aitools.ExecuteRequest) domain.AgentEvent
}

type ChatToolExecutor interface {
	Execute(ctx context.Context, req aitools.ExecuteRequest) domain.AgentEvent
}

type ConfirmationRequester interface {
	RequestConfirmation(ctx context.Context, req aitools.ExecuteRequest) domain.AgentEvent
}

type IntentPlanner interface {
	// 根据用户消息和历史对话进行意图识别和规划，返回计划结果
	Plan(ctx context.Context, req planner.PlanRequest) (planner.PlanResult, error)
}

type ServiceContext struct {
	Config              config.Config
	Mysql               sqlx.SqlConn
	RedisClient         *redis.Redis
	ConversationsModel  aiconversations.AiConversationsModel
	MessagesModel       aimessages.AiMessagesModel
	ToolCallsModel      aitoolcalls.AiToolCallsModel
	ConfirmationsModel  aiconfirmations.AiConfirmationsModel
	UserMemoriesModel   aiusermemories.AiUserMemoriesModel
	SummariesModel      aiconversationsummaries.AiConversationSummariesModel
	ProductRpc          productcatalogservice.ProductCatalogService
	InventoryRpc        inventoryclient.Inventory
	OrderRpc            orderservice.OrderService
	CheckoutRpc         checkoutservice.CheckoutService
	CartRpc             cartsclient.Cart
	CouponRpc           couponsclient.Coupons
	UserRpc             usersclient.Users
	AuditRpc            auditclient.Audit
	ToolRegistry        *aitools.Registry
	ToolExecutor        *aitools.Executor
	QueryTools          *aitools.QueryTools
	WriteTools          *aitools.WriteTools
	ConfirmationManager ConfirmationManager
	HighRiskTools       HighRiskToolExecutor
	ConversationManager conversation.Manager
	ContextManager      contextmanager.Manager
	SummaryManager      *contextmanager.SummaryManager
	MemoryPolicy        *contextmanager.MemoryPolicy
	IntentPlanner       IntentPlanner
	AgentRunner         eino.Runner
	QueryChatTools      ChatToolExecutor
	WriteChatTools      ChatToolExecutor
	HighRiskChatTools   ConfirmationRequester
}

func NewServiceContext(c config.Config) *ServiceContext {
	mysql := sqlx.NewMysql(c.MysqlConfig.DataSource)
	productRPC := productcatalogservice.NewProductCatalogService(zrpc.MustNewClient(c.ProductRpc))
	inventoryRPC := inventoryclient.NewInventory(zrpc.MustNewClient(c.InventoryRpc))
	orderRPC := orderservice.NewOrderService(zrpc.MustNewClient(c.OrderRpc))
	checkoutRPC := checkoutservice.NewCheckoutService(zrpc.MustNewClient(c.CheckoutRpc))
	cartRPC := cartsclient.NewCart(zrpc.MustNewClient(c.CartRpc))
	couponRPC := couponsclient.NewCoupons(zrpc.MustNewClient(c.CouponRpc))
	auditRPC := auditclient.NewAudit(zrpc.MustNewClient(c.AuditRpc))
	userRPC := usersclient.NewUsers(zrpc.MustNewClient(c.UserRpc))
	redisClient := redis.MustNewRedis(c.RedisConf)
	toolCallsModel := aitoolcalls.NewAiToolCallsModel(mysql, c.Cache)
	confirmationsModel := aiconfirmations.NewAiConfirmationsModel(mysql, c.Cache)
	toolRegistry := aitools.NewRegistry(c.ToolTimeout)
	toolRecorder := aiaudit.NewRecorder(toolCallsModel, auditRPC)
	toolExecutor := aitools.NewExecutor(toolRegistry, aitools.WithToolCallRecorder(toolRecorder))
	queryTools := aitools.NewQueryTools(toolExecutor, aitools.QueryToolClients{
		Product:   productRPC,
		Inventory: inventoryRPC,
		Order:     orderRPC,
		Cart:      cartRPC,
		Coupon:    couponRPC,
		Checkout:  checkoutRPC,
	})
	writeTools := aitools.NewWriteTools(toolExecutor, aitools.WriteToolClients{
		Cart:     cartRPC,
		Coupon:   couponRPC,
		Checkout: checkoutRPC,
	})
	confirmationManager := aiconfirmation.NewManager(
		confirmationsModel,
		toolRegistry,
		aiconfirmation.NewRedisLocker(redisClient),
		aiconfirmation.WithConfirmationTTL(time.Duration(c.Confirmation.ExpireSeconds)*time.Second),
		aiconfirmation.WithLockTTL(time.Duration(c.Confirmation.LockExpireSeconds)*time.Second),
	)
	highRiskTools := aitools.NewHighRiskTools(toolExecutor, confirmationManager, aitools.HighRiskToolClients{
		Cart:     cartRPC,
		Order:    orderRPC,
		Checkout: checkoutRPC,
		Coupon:   couponRPC,
	})
	conversationsModel := aiconversations.NewAiConversationsModel(mysql, c.Cache)
	messagesModel := aimessages.NewAiMessagesModel(mysql, c.Cache)
	summariesModel := aiconversationsummaries.NewAiConversationSummariesModel(mysql, c.Cache)
	userMemoriesModel := aiusermemories.NewAiUserMemoriesModel(mysql, c.Cache)
	summaryStore := contextmanager.NewSummaryStore(summariesModel)
	memoryStore := contextmanager.NewMemoryStore(userMemoriesModel)
	conversationManager := conversation.NewManager(
		conversationsModel,
		messagesModel,
	)
	contextManager := contextmanager.NewManager(
		contextmanager.NewMessageStore(messagesModel),
		contextmanager.NewToolResultStore(messagesModel),
		contextmanager.WithSummaryStore(summaryStore),
		contextmanager.WithMemoryStore(memoryStore),
		contextmanager.WithUserProfileSource(contextmanager.NewUserProfileSource(userRPC)),
	)
	modelFactory := eino.NewModelFactory()
	summaryManager := contextmanager.NewSummaryManager(
		summaryStore,
		contextmanager.NewSummaryMessageStore(messagesModel),
		eino.NewSummarySummarizer(modelFactory, selectSummaryModelConfig(c.SummaryModel, c.IntentModel)),
	)
	memoryPolicy := contextmanager.NewMemoryPolicy(memoryStore)
	intentPlanner := planner.New(toolRegistry, planner.WithIntentModel(eino.NewIntentModelFactory(modelFactory), c.IntentModel))
	var agentRunner eino.Runner
	if chatModel, err := modelFactory.NewChatModel(context.Background(), c.Eino); err == nil {
		agentRunner = eino.NewRunner(chatModel)
	} else {
		logx.Errorw("ai chat model initialization failed", logx.Field("component", "chat_model"), logx.Field("stage", "initialize"), logx.Field("reason", "model_init_failed"), logx.Field("provider", c.Eino.Provider), logx.Field("model", c.Eino.Model), logx.Field("base_url_host", modelBaseURLHost(c.Eino.BaseURL)), logx.Field("err", err))
	}

	return &ServiceContext{
		Config:              c,
		Mysql:               mysql,
		RedisClient:         redisClient,
		ConversationsModel:  conversationsModel,
		MessagesModel:       messagesModel,
		ToolCallsModel:      toolCallsModel,
		ConfirmationsModel:  confirmationsModel,
		UserMemoriesModel:   userMemoriesModel,
		SummariesModel:      summariesModel,
		ProductRpc:          productRPC,
		InventoryRpc:        inventoryRPC,
		OrderRpc:            orderRPC,
		CheckoutRpc:         checkoutRPC,
		CartRpc:             cartRPC,
		CouponRpc:           couponRPC,
		UserRpc:             userRPC,
		AuditRpc:            auditRPC,
		ToolRegistry:        toolRegistry,
		ToolExecutor:        toolExecutor,
		QueryTools:          queryTools,
		WriteTools:          writeTools,
		ConfirmationManager: confirmationManager,
		HighRiskTools:       highRiskTools,
		ConversationManager: conversationManager,
		ContextManager:      contextManager,
		SummaryManager:      summaryManager,
		MemoryPolicy:        memoryPolicy,
		IntentPlanner:       intentPlanner,
		AgentRunner:         agentRunner,
		QueryChatTools:      queryTools,
		WriteChatTools:      writeTools,
		HighRiskChatTools:   highRiskTools,
	}
}

func selectSummaryModelConfig(summaryConfig, fallback config.EinoConfig) config.EinoConfig {
	if summaryConfig.Provider == "" &&
		summaryConfig.APIKey == "" &&
		summaryConfig.BaseURL == "" &&
		summaryConfig.Model == "" &&
		summaryConfig.Timeout == 0 &&
		summaryConfig.MaxTokens == 0 &&
		summaryConfig.Temperature == 0 {
		return fallback
	}
	return summaryConfig
}

func modelBaseURLHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "<invalid>"
	}
	return parsed.Hostname()
}
