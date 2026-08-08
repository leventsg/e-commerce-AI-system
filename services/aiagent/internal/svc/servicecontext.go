package svc

import (
	"context"
	"net/url"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	aiagentruns "github.com/leventsg/e-commerce-AI-system/dal/model/ai/agent_runs"
	aiconfirmations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	aiconversationsummaries "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversation_summaries"
	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	aiusermemories "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_memories"
	aiuserprofiles "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_profiles"
	aiaudit "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/audit"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	aiconfirmation "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/audit/auditclient"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ConfirmationManager interface {
	// 创建高风险确认请求
	Create(ctx context.Context, req aiconfirmation.CreateRequest) (*domain.Confirmation, error)
	// 用户确认请求的决策处理，返回确认记录
	Decide(ctx context.Context, req aiconfirmation.DecisionRequest) (*domain.Confirmation, error)
	// 标记确认请求为已执行状态
	MarkExecuted(ctx context.Context, req aiconfirmation.CompletionRequest) (*domain.Confirmation, error)
	// 标记确认请求为已失败状态
	MarkFailed(ctx context.Context, req aiconfirmation.CompletionRequest) (*domain.Confirmation, error)
	// 绑定 Eino checkpoint interrupt 恢复目标
	BindResumeTarget(ctx context.Context, req aiconfirmation.ResumeTargetRequest) (*domain.Confirmation, error)
}

type ServiceContext struct {
	Config                 config.Config
	Mysql                  sqlx.SqlConn
	RedisClient            *redis.Redis
	ConversationsModel     aiconversations.AiConversationsModel
	MessagesModel          aimessages.AiMessagesModel
	ToolCallsModel         aitoolcalls.AiToolCallsModel
	ConfirmationsModel     aiconfirmations.AiConfirmationsModel
	AgentRunsModel         aiagentruns.AiAgentRunsModel
	UserMemoriesModel      aiusermemories.AiUserMemoriesModel
	UserProfilesModel      aiuserprofiles.AiUserProfilesModel
	SummariesModel         aiconversationsummaries.AiConversationSummariesModel
	ProductRpc             productcatalogservice.ProductCatalogService
	InventoryRpc           inventoryclient.Inventory
	OrderRpc               orderservice.OrderService
	CheckoutRpc            checkoutservice.CheckoutService
	CartRpc                cartsclient.Cart
	CouponRpc              couponsclient.Coupons
	AuditRpc               auditclient.Audit
	ToolRegistry           *aitools.Registry
	ToolExecutor           *aitools.Executor
	ConfirmationManager    ConfirmationManager
	ConversationManager    conversation.Manager
	ContextManager         contextmanager.Manager
	SummaryManager         *contextmanager.SummaryManager
	MemoryPolicy           *contextmanager.MemoryPolicy
	UserProfileStore       *contextmanager.UserProfileModelStore
	ProfileUpdatePublisher profileextractor.Publisher
	ProfileExtractor       *profileextractor.Extractor
	AgentRunner            eino.Runner
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
	redisClient := redis.MustNewRedis(c.RedisConf)
	toolCallsModel := aitoolcalls.NewAiToolCallsModel(mysql, c.Cache)
	confirmationsModel := aiconfirmations.NewAiConfirmationsModel(mysql, c.Cache)
	agentRunsModel := aiagentruns.NewAiAgentRunsModel(mysql, c.Cache)
	toolRecorder := aiaudit.NewRecorder(toolCallsModel, auditRPC)
	defaultTools := aitools.DefaultTools(aitools.DefaultToolClients{
		Product:   productRPC,
		Inventory: inventoryRPC,
		Order:     orderRPC,
		Cart:      cartRPC,
		Coupon:    couponRPC,
		Checkout:  checkoutRPC,
	}, c.ToolTimeout)
	toolRegistry := aitools.NewRegistry(defaultTools)
	toolExecutor := aitools.NewExecutor(toolRegistry, aitools.WithToolCallRecorder(toolRecorder))
	// 数据层，工具执行数据管理器
	confirmationManager := aiconfirmation.NewManager(
		confirmationsModel,
		toolRegistry,
		aiconfirmation.NewRedisLocker(redisClient),
		aiconfirmation.WithConfirmationTTL(time.Duration(c.Confirmation.ExpireSeconds)*time.Second),
		aiconfirmation.WithLockTTL(time.Duration(c.Confirmation.LockExpireSeconds)*time.Second),
	)
	// 编排层，工具管理器负责高风险操作的确认请求和恢复点绑定
	approvalManager := aitools.NewApprovalManager(toolRegistry, toolExecutor, confirmationManager)
	conversationsModel := aiconversations.NewAiConversationsModel(mysql, c.Cache)
	messagesModel := aimessages.NewAiMessagesModel(mysql, c.Cache)
	summariesModel := aiconversationsummaries.NewAiConversationSummariesModel(mysql, c.Cache)
	userMemoriesModel := aiusermemories.NewAiUserMemoriesModel(mysql, c.Cache)
	userProfilesModel := aiuserprofiles.NewAiUserProfilesModel(mysql, c.Cache)
	summaryStore := contextmanager.NewSummaryStore(summariesModel)
	memoryStore := contextmanager.NewMemoryStore(userMemoriesModel)
	userProfileStore := contextmanager.NewUserProfileStore(userProfilesModel)
	conversationManager := conversation.NewManager(
		conversationsModel,
		messagesModel,
	)
	contextManager := contextmanager.NewManager(
		contextmanager.NewMessageStore(messagesModel),
		contextmanager.NewToolResultStore(messagesModel),
		contextmanager.WithSummaryStore(summaryStore),
		contextmanager.WithMemoryStore(memoryStore),
		contextmanager.WithUserProfileStore(userProfileStore),
	)
	modelFactory := eino.NewModelFactory()
	summaryManager := contextmanager.NewSummaryManager(
		summaryStore,
		contextmanager.NewSummaryMessageStore(messagesModel),
		eino.NewSummarySummarizer(modelFactory, selectSummaryModelConfig(c.SummaryModel, c.Eino)),
	)
	memoryPolicy := contextmanager.NewMemoryPolicy(memoryStore)
	profileUpdatePublisher := newProfileUpdatePublisher(c)
	profileExtractor := profileextractor.NewExtractor(
		messagesModel,
		userProfileStore,
		memoryStore,
		eino.NewProfileExtractorModel(modelFactory, selectProfileModelConfig(c.ProfileModel, c.SummaryModel, c.Eino)),
	)
	var agentRunner eino.Runner
	checkpointTTL := time.Duration(c.Confirmation.ExpireSeconds) * time.Second
	if runner, err := eino.NewSupervisorAgent(context.Background(), modelFactory, c.Eino, toolRegistry,
		eino.WithApprovalManager(approvalManager),
		eino.WithCheckpointStore(eino.NewPersistentCheckpointStore(redisClient, agentRunsModel, checkpointTTL)),
	); err == nil {
		agentRunner = runner
	} else {
		logx.Errorw("ai chat model initialization failed", logx.Field("component", "chat_model"), logx.Field("stage", "initialize"), logx.Field("reason", "model_init_failed"), logx.Field("provider", c.Eino.Provider), logx.Field("model", c.Eino.Model), logx.Field("base_url_host", modelBaseURLHost(c.Eino.BaseURL)), logx.Field("err", err))
	}

	return &ServiceContext{
		Config:                 c,
		Mysql:                  mysql,
		RedisClient:            redisClient,
		ConversationsModel:     conversationsModel,
		MessagesModel:          messagesModel,
		ToolCallsModel:         toolCallsModel,
		ConfirmationsModel:     confirmationsModel,
		AgentRunsModel:         agentRunsModel,
		UserMemoriesModel:      userMemoriesModel,
		UserProfilesModel:      userProfilesModel,
		SummariesModel:         summariesModel,
		ProductRpc:             productRPC,
		InventoryRpc:           inventoryRPC,
		OrderRpc:               orderRPC,
		CheckoutRpc:            checkoutRPC,
		CartRpc:                cartRPC,
		CouponRpc:              couponRPC,
		AuditRpc:               auditRPC,
		ToolRegistry:           toolRegistry,
		ToolExecutor:           toolExecutor,
		ConfirmationManager:    confirmationManager,
		ConversationManager:    conversationManager,
		ContextManager:         contextManager,
		SummaryManager:         summaryManager,
		MemoryPolicy:           memoryPolicy,
		UserProfileStore:       userProfileStore,
		ProfileUpdatePublisher: profileUpdatePublisher,
		ProfileExtractor:       profileExtractor,
		AgentRunner:            agentRunner,
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

func selectProfileModelConfig(profileConfig, summaryConfig, intentConfig config.EinoConfig) config.EinoConfig {
	if profileConfig.Provider == "" &&
		profileConfig.APIKey == "" &&
		profileConfig.BaseURL == "" &&
		profileConfig.Model == "" &&
		profileConfig.Timeout == 0 &&
		profileConfig.MaxTokens == 0 &&
		profileConfig.Temperature == 0 {
		return selectSummaryModelConfig(summaryConfig, intentConfig)
	}
	return profileConfig
}

func newProfileUpdatePublisher(c config.Config) profileextractor.Publisher {
	kafkaConf, err := c.KafkaMQ.TopicConfig(profileextractor.TopicKeyAiUserProfileUpdates)
	if err != nil {
		logx.Errorw("ai user profile update publisher disabled",
			logx.Field("component", "profile_extractor"),
			logx.Field("stage", "publisher_init"),
			logx.Field("err", err))
		return nil
	}
	producer, err := mq.NewKafkaProducer(c.KafkaMQ)
	if err != nil {
		logx.Errorw("ai user profile update publisher disabled",
			logx.Field("component", "profile_extractor"),
			logx.Field("stage", "producer_init"),
			logx.Field("err", err))
		return nil
	}
	return profileextractor.NewKafkaPublisher(producer, kafkaConf.Topic)
}

func modelBaseURLHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "<invalid>"
	}
	return parsed.Hostname()
}
