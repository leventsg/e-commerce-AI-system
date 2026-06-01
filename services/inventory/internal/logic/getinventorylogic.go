package logic

import (
	"context"
	"errors"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetInventoryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetInventoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetInventoryLogic {
	return &GetInventoryLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetInventory 查询库存
func (l *GetInventoryLogic) GetInventory(in *inventory.GetInventoryReq) (*inventory.GetInventoryResp, error) {

	inventoryResp, err := l.svcCtx.InventoryModel.FindOne(l.ctx, int64(in.ProductId))
	res := new(inventory.GetInventoryResp)
	if err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			l.Logger.Infow("product not in inventory", logx.Field("product_id", in.ProductId))
			res.StatusCode = code.ProductNotFoundInventory
			res.StatusMsg = code.ProductNotFoundInventoryMsg
			return res, nil
		}
		l.Logger.Errorw("product inventory get failed", logx.Field("product_id", in.ProductId))
		return nil, err
	}

	res.Inventory = inventoryResp.Total
	res.SoldCount = inventoryResp.Sold
	return res, nil
}
