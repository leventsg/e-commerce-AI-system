package svc

import (
	"time"

	aiconfirmations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	aiusermemories "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_memories"
	aiaudit "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/audit"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	aiconfirmation "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/audit/auditclient"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config              config.Config
	Mysql               sqlx.SqlConn
	RedisClient         *redis.Redis
	ConversationsModel  aiconversations.AiConversationsModel
	MessagesModel       aimessages.AiMessagesModel
	ToolCallsModel      aitoolcalls.AiToolCallsModel
	ConfirmationsModel  aiconfirmations.AiConfirmationsModel
	UserMemoriesModel   aiusermemories.AiUserMemoriesModel
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
	ConfirmationManager *aiconfirmation.Manager
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
		Cart:   cartRPC,
		Coupon: couponRPC,
	})
	confirmationManager := aiconfirmation.NewManager(
		confirmationsModel,
		toolRegistry,
		aiconfirmation.NewRedisLocker(redisClient),
		aiconfirmation.WithConfirmationTTL(time.Duration(c.Confirmation.ExpireSeconds)*time.Second),
		aiconfirmation.WithLockTTL(time.Duration(c.Confirmation.LockExpireSeconds)*time.Second),
	)

	return &ServiceContext{
		Config:              c,
		Mysql:               mysql,
		RedisClient:         redisClient,
		ConversationsModel:  aiconversations.NewAiConversationsModel(mysql, c.Cache),
		MessagesModel:       aimessages.NewAiMessagesModel(mysql, c.Cache),
		ToolCallsModel:      toolCallsModel,
		ConfirmationsModel:  confirmationsModel,
		UserMemoriesModel:   aiusermemories.NewAiUserMemoriesModel(mysql, c.Cache),
		ProductRpc:          productRPC,
		InventoryRpc:        inventoryRPC,
		OrderRpc:            orderRPC,
		CheckoutRpc:         checkoutRPC,
		CartRpc:             cartRPC,
		CouponRpc:           couponRPC,
		UserRpc:             usersclient.NewUsers(zrpc.MustNewClient(c.UserRpc)),
		AuditRpc:            auditRPC,
		ToolRegistry:        toolRegistry,
		ToolExecutor:        toolExecutor,
		QueryTools:          queryTools,
		WriteTools:          writeTools,
		ConfirmationManager: confirmationManager,
	}
}
