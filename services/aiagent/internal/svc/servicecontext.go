package svc

import (
	aiconfirmations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	aiusermemories "github.com/leventsg/e-commerce-AI-system/dal/model/ai/user_memories"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
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
	Config             config.Config
	Mysql              sqlx.SqlConn
	RedisClient        *redis.Redis
	ConversationsModel aiconversations.AiConversationsModel
	MessagesModel      aimessages.AiMessagesModel
	ToolCallsModel     aitoolcalls.AiToolCallsModel
	ConfirmationsModel aiconfirmations.AiConfirmationsModel
	UserMemoriesModel  aiusermemories.AiUserMemoriesModel
	ProductRpc         productcatalogservice.ProductCatalogService
	InventoryRpc       inventoryclient.Inventory
	OrderRpc           orderservice.OrderService
	CheckoutRpc        checkoutservice.CheckoutService
	CartRpc            cartsclient.Cart
	CouponRpc          couponsclient.Coupons
	UserRpc            usersclient.Users
	AuditRpc           auditclient.Audit
}

func NewServiceContext(c config.Config) *ServiceContext {
	mysql := sqlx.NewMysql(c.MysqlConfig.DataSource)

	return &ServiceContext{
		Config:             c,
		Mysql:              mysql,
		RedisClient:        redis.MustNewRedis(c.RedisConf),
		ConversationsModel: aiconversations.NewAiConversationsModel(mysql, c.Cache),
		MessagesModel:      aimessages.NewAiMessagesModel(mysql, c.Cache),
		ToolCallsModel:     aitoolcalls.NewAiToolCallsModel(mysql, c.Cache),
		ConfirmationsModel: aiconfirmations.NewAiConfirmationsModel(mysql, c.Cache),
		UserMemoriesModel:  aiusermemories.NewAiUserMemoriesModel(mysql, c.Cache),
		ProductRpc:         productcatalogservice.NewProductCatalogService(zrpc.MustNewClient(c.ProductRpc)),
		InventoryRpc:       inventoryclient.NewInventory(zrpc.MustNewClient(c.InventoryRpc)),
		OrderRpc:           orderservice.NewOrderService(zrpc.MustNewClient(c.OrderRpc)),
		CheckoutRpc:        checkoutservice.NewCheckoutService(zrpc.MustNewClient(c.CheckoutRpc)),
		CartRpc:            cartsclient.NewCart(zrpc.MustNewClient(c.CartRpc)),
		CouponRpc:          couponsclient.NewCoupons(zrpc.MustNewClient(c.CouponRpc)),
		UserRpc:            usersclient.NewUsers(zrpc.MustNewClient(c.UserRpc)),
		AuditRpc:           auditclient.NewAudit(zrpc.MustNewClient(c.AuditRpc)),
	}
}
