package handler

import (
	"context"
	"net/http"

	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/x/errors"
	xhttp "github.com/zeromicro/x/http"
)

func InternalCancelOrderHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CancelOrderReq
		if err := httpx.Parse(r, &req); err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, errors.New(code.Fail, err.Error()))
			return
		}
		if req.UserID == 0 {
			xhttp.JsonBaseResponseCtx(r.Context(), w, errors.New(code.AuthBlank, code.AuthBlankMsg))
			return
		}

		ctx := context.WithValue(r.Context(), biz.UserIDKey, req.UserID)
		l := logic.NewCancelOrderLogic(ctx, svcCtx)
		resp, err := l.CancelOrder(&req)
		if err != nil {
			xhttp.JsonBaseResponseCtx(ctx, w, err)
			return
		}
		xhttp.JsonBaseResponseCtx(ctx, w, resp)
	}
}
