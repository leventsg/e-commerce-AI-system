package logic

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ReturnPreInventoryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReturnPreInventoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReturnPreInventoryLogic {
	return &ReturnPreInventoryLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ReturnPreInventory 退还预扣减的库存（）
func (l *ReturnPreInventoryLogic) ReturnPreInventory(in *inventory.InventoryReq) (*inventory.InventoryResp, error) {
	resp := &inventory.InventoryResp{}
	// 构建幂等锁Key（用户ID+预订单ID）
	lockKey := fmt.Sprintf("%s:%d:%s", biz.InventoryDeductLockPrefix, in.UserId, in.PreOrderId)
	returnedKey := fmt.Sprintf("%s:returned", lockKey)

	//准备参数
	keys := make([]string, len(in.Items)+2)
	args := make([]interface{}, len(in.Items)+1)

	keys[0] = lockKey
	keys[1] = returnedKey
	args[0] = in.PreOrderId // 构造库存Key列表

	// 构造库存Key列表

	for i, item := range in.Items {
		if item.Quantity <= 0 {
			l.Logger.Infow("商品数量不合法",
				logx.Field("product_id", item.ProductId))
			resp.StatusCode = code.InvalidParams
			resp.StatusMsg = code.InvalidParamsMsg
			return resp, nil
		}
		productKey := fmt.Sprintf("%s:%d", biz.InventoryProductKey, item.ProductId)
		keys[i+2] = productKey
		args[i+1] = item.Quantity
	}

	// 执行Lua脚本（使用go-zero的Evalsah方法）
	val, err := l.svcCtx.Rdb.EvalSha(l.svcCtx.ReturnInventoryShal, keys, args)
	if err != nil {
		l.Logger.Errorw("LUA脚本执行失败",
			logx.Field("error", err),
			logx.Field("pre_order_id", in.PreOrderId))
		return nil, status.Error(codes.Internal, "系统繁忙")
	}

	// 类型转换处理
	result, ok := val.(int64)
	if !ok {
		l.Logger.Errorw("脚本返回类型异常",
			logx.Field("result", val),
			logx.Field("type", fmt.Sprintf("%T", val)))
		return nil, status.Error(codes.Internal, "系统异常")
	}

	// 处理结果
	switch result {
	case 0: // 归还成功
		return &inventory.InventoryResp{}, nil
	case 1: // 已回滚过，幂等成功
		l.Logger.Infow("预扣库存已回滚",
			logx.Field("pre_order_id", in.PreOrderId))
		return &inventory.InventoryResp{}, nil
	case 2: // 预扣记录不存在或已过期，状态不可确认
		l.Logger.Errorw("预扣库存记录不存在或已过期",
			logx.Field("pre_order_id", in.PreOrderId),
			logx.Field("user_id", in.UserId))
		resp.StatusCode = code.InventoryReservationNotFound
		resp.StatusMsg = code.InventoryReservationNotFoundMsg
		return resp, nil
	default:
		l.Logger.Errorw("未知返回码",
			logx.Field("result", result))
		return nil, status.Error(codes.Internal, "系统异常")
	}

}
