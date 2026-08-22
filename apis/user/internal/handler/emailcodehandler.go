package handler

import (
	"net/http"

	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func EmailCodeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.EmailCodeRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := logic.NewEmailCodeLogic(r.Context(), svcCtx)
		resp, err := l.EmailCode(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
