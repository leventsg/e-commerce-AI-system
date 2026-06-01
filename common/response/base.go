package response

import (
	"context"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"net/http"
)

type Response struct {
	StatusCode int    `json:"code"`
	StatusMsg  string `json:"msg"`
}

func NewResponse(statusCode int, statusMsg string) *Response {
	return &Response{
		StatusCode: statusCode,
		StatusMsg:  statusMsg,
	}
}
func NewParamError(ctx context.Context, w http.ResponseWriter, err error) {
	logx.Infow("params invalid", logx.Field("err", err))
	httpx.OkJsonCtx(ctx, w, NewResponse(code.Fail, code.FailMsg))
}
