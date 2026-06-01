package handler

import (
	"github.com/zeromicro/x/errors"
	xhttp "github.com/zeromicro/x/http"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/leventsg/e-commerce-AI-system/apis/coupon/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/coupon/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/coupon/internal/types"
)

func GetCouponHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CouponItemReq
		if err := httpx.Parse(r, &req); err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, errors.New(code.Fail, err.Error()))
			return
		}

		l := logic.NewGetCouponLogic(r.Context(), svcCtx)
		resp, err := l.GetCoupon(&req)
		if err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, err)

		} else {
			xhttp.JsonBaseResponseCtx(r.Context(), w, resp)
		}
	}
}
