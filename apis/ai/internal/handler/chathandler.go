package handler

import (
	"net/http"

	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
)

func ChatHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewChatLogic(r.Context(), svcCtx)
		l.ServeHTTP(w, r)
	}
}
