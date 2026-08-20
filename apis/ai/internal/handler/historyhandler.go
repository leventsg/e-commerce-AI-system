package handler

import (
	"net/http"

	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/zeromicro/go-zero/rest/httpx"
	xerrors "github.com/zeromicro/x/errors"
	xhttp "github.com/zeromicro/x/http"
)

// ListSessionsHandler 返回当前用户的全部历史会话。
func ListSessionsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ListSessionsRequest
		if err := httpx.Parse(r, &req); err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, xerrors.New(code.Fail, err.Error()))
			return
		}
		l := logic.NewHistoryLogic(r.Context(), svcCtx)
		resp, err := l.ListSessions(&req)
		if err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, err)
		} else {
			xhttp.JsonBaseResponseCtx(r.Context(), w, resp)
		}
	}
}

// ListMessagesHandler 返回当前用户指定会话的全部历史消息。
func ListMessagesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ListMessagesRequest
		if err := httpx.Parse(r, &req); err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, xerrors.New(code.Fail, err.Error()))
			return
		}
		l := logic.NewHistoryLogic(r.Context(), svcCtx)
		resp, err := l.ListMessages(&req)
		if err != nil {
			xhttp.JsonBaseResponseCtx(r.Context(), w, err)
		} else {
			xhttp.JsonBaseResponseCtx(r.Context(), w, resp)
		}
	}
}
