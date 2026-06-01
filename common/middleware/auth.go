package middleware

import (
	"context"
	"net/http"
	"path"
	"sync"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/common/response"
	"github.com/leventsg/e-commerce-AI-system/services/auths/auths"
	"github.com/leventsg/e-commerce-AI-system/services/auths/authsclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/zrpc"
)

func WrapperAuthMiddleware(rpcConf zrpc.RpcClientConf, whitePaths, optionPaths []string) func(next http.HandlerFunc) http.HandlerFunc {
	whitePathSet := make(map[string]struct{}, len(whitePaths))
	optionPathSet := make(map[string]struct{}, len(optionPaths))
	logx.Infow("auth middleware init",
		logx.Field("white_paths", whitePaths), logx.Field("option_paths", optionPaths))

	for _, path := range whitePaths {
		whitePathSet[normalizeURLPath(path)] = struct{}{}
	}
	for _, path := range optionPaths {
		optionPathSet[normalizeURLPath(path)] = struct{}{}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		var (
			once    sync.Once
			authRpc authsclient.Auths
		)

		return func(w http.ResponseWriter, r *http.Request) {
			normalizedPath := normalizeURLPath(r.URL.Path)
			r.URL.Path = normalizedPath

			// 白名单路径直接放行
			if _, ok := whitePathSet[normalizedPath]; ok {
				next(w, r)
				return
			}
			// 获取认证令牌
			token := r.Header.Get(biz.TokenKey)

			// 处理可选令牌路径
			if _, ok := optionPathSet[normalizedPath]; ok && token == "" {
				setUserContext(r, 0)
				next(w, r)
				return
			}

			cookies := r.Cookies()
			refreshToken := ""
			for _, cookie := range cookies {
				if cookie.Name == biz.RefreshTokenKey {
					refreshToken = cookie.Value
					break
				}
			}
			if refreshToken == "" {
				logx.Errorw("no refresh token cookie", logx.Field("path", normalizedPath))
				sendAuthError(w, r, code.AuthBlank, code.AuthBlankMsg)
				return
			}

			// 非可选路径必须携带令牌
			if token == "" {
				sendAuthError(w, r, code.AuthBlank, code.AuthBlankMsg)
				return
			}

			// 延迟初始化认证客户端
			once.Do(func() {
				authRpc = authsclient.NewAuths(zrpc.MustNewClient(rpcConf))
			})
			clientIP := r.Context().Value(biz.ClientIPKey).(string)
			if clientIP == "" {
				sendAuthError(w, r, code.IllegalProxyAddress, code.IllegalProxyAddressMsg)
				return
			}
			// 执行认证流程
			authRes, err := authRpc.Authentication(r.Context(), &auths.AuthReq{Token: token, ClientIp: clientIP})
			if err != nil {
				logx.Errorw("back err", logx.Field("err", err),
					logx.Field("client_ip", clientIP),
					logx.Field("token", maskToken(token)), logx.Field("path", normalizedPath))
				sendServerError(w, r)
				return
			}
			// 处理认证结果
			switch authRes.StatusCode {
			case code.Success:
				setUserContext(r, authRes.UserId)
				next(w, r)
			case code.AuthExpired:
				handleTokenExpiration(w, r, authRpc, refreshToken, clientIP)
			default:
				sendAuthError(w, r, int(authRes.StatusCode), authRes.StatusMsg)
			}
		}
	}
}

// 设置用户上下文
func setUserContext(r *http.Request, userId uint32) {
	ctx := context.WithValue(r.Context(), biz.UserIDKey, userId)
	*r = *r.WithContext(ctx)
}

// 处理令牌过期
func handleTokenExpiration(w http.ResponseWriter, r *http.Request, client authsclient.Auths, refreshToken string, clientIP string) {
	if refreshToken == "" {
		sendAuthError(w, r, code.AuthExpired, code.AuthExpiredMsg)
		return
	}

	renewRes, err := client.RenewToken(r.Context(), &auths.AuthRenewalReq{RefreshToken: refreshToken, ClientIp: clientIP})
	if err != nil {
		logx.Errorw("refresh token err",
			logx.Field("err", err),
			logx.Field("refreshToken", maskToken(refreshToken)),
			logx.Field("path", r.URL.Path),
			logx.Field("client_ip", clientIP),
		)
		sendServerError(w, r)
		return
	}
	if renewRes.StatusCode == code.Success {
		// 由客户端处理刷新结果，进行再次请求
		// 返回状态码和刷新结果
		httpx.OkJsonCtx(r.Context(), w, response.NewRefreshResponse(renewRes))
		return
	}

	sendAuthError(w, r, int(renewRes.StatusCode), renewRes.StatusMsg)
}

func sendAuthError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	logx.Errorw("认证信息错误", logx.Field("请求参数Header", r.Header), logx.Field("请求参数url", r.URL))
	httpx.OkJsonCtx(r.Context(), w, response.NewResponse(statusCode, message))
}

func sendServerError(w http.ResponseWriter, r *http.Request) {
	httpx.OkJsonCtx(r.Context(), w, response.NewResponse(code.ServerError, code.ServerErrorMsg))
}

// 令牌脱敏处理
func maskToken(token string) string {
	if len(token) < 8 {
		return "***"
	}
	return token[:3] + "****" + token[len(token)-3:]
}

func normalizeURLPath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	normalizedPath := path.Clean(rawPath)
	if normalizedPath == "." {
		return "/"
	}
	if normalizedPath[0] != '/' {
		return "/" + normalizedPath
	}
	return normalizedPath
}
