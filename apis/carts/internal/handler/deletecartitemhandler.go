package handler

import (
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/x/errors"
	xhttp "github.com/zeromicro/x/http"
	"github.com/leventsg/e-commerce-AI-system/apis/carts/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/carts/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/carts/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"net/http"
)

func DeleteCartItemHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DeleteCartReq
		if err := httpx.Parse(r, &req); err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, errors.New(code.Fail, err.Error()))
			return
		}

		l := logic.NewDeleteCartItemLogic(r.Context(), svcCtx)
		resp, err := l.DeleteCartItem(&req)
		if err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, err)
		} else {
			xhttp.JsonBaseResponseCtx(r.Context(), w, resp)
		}
	}
}
