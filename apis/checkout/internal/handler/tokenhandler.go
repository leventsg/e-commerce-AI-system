package handler

import (
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	xerrors "github.com/zeromicro/x/errors"
	xhttp "github.com/zeromicro/x/http"
	"net/http"

	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func TokenHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CheckoutTokenReq
		if err := httpx.Parse(r, &req); err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, xerrors.New(code.Fail, err.Error()))
			return
		}

		l := logic.NewTokenLogic(r.Context(), svcCtx)
		resp, err := l.Token(&req)
		if err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, err)
		} else {
			xhttp.JsonBaseResponseCtx(r.Context(), w, resp)
		}
	}
}
