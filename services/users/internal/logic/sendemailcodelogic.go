package logic

import (
	"context"
	"database/sql"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/users/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendEmailCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendEmailCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendEmailCodeLogic {
	return &SendEmailCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SendEmailCodeLogic) SendEmailCode(in *users.EmailCodeRequest) (*users.EmailCodeResponse, error) {
	scene := in.GetScene()
	email := normalizeEmail(in.GetEmail())

	switch scene {
	case emailCodeSceneRegister:
		if !validateEmail(email) {
			return &users.EmailCodeResponse{StatusCode: code.EmailFormatError, StatusMsg: code.EmailFormatErrorMsg}, nil
		}
		existUser, err := findUserByEmail(l.ctx, l.svcCtx.UsersModel, email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			l.Logger.Errorw("send register email code query user failed", logx.Field("err", err), logx.Field("email", email))
			return nil, err
		}
		if existUser != nil && !existUser.UserDeleted {
			return &users.EmailCodeResponse{StatusCode: code.UserAlreadyExists, StatusMsg: code.UserAlreadyExistsMsg}, nil
		}
	case emailCodeSceneForgotPassword:
		if !validateEmail(email) {
			return &users.EmailCodeResponse{StatusCode: code.EmailFormatError, StatusMsg: code.EmailFormatErrorMsg}, nil
		}
		existUser, err := findUserByEmail(l.ctx, l.svcCtx.UsersModel, email)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &users.EmailCodeResponse{StatusCode: code.UserNotFound, StatusMsg: code.UserNotFoundMsg}, nil
			}
			l.Logger.Errorw("send forgot password email code query user failed", logx.Field("err", err), logx.Field("email", email))
			return nil, err
		}
		if existUser.UserDeleted {
			return &users.EmailCodeResponse{StatusCode: code.UserHaveDeleted, StatusMsg: code.UserHaveDeletedMsg}, nil
		}
	case emailCodeSceneUpdatePassword:
		if in.GetUserId() == 0 {
			return &users.EmailCodeResponse{StatusCode: code.UserNotFound, StatusMsg: code.UserNotFoundMsg}, nil
		}
		existUser, err := l.svcCtx.UsersModel.FindOne(l.ctx, int64(in.GetUserId()))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &users.EmailCodeResponse{StatusCode: code.UserNotFound, StatusMsg: code.UserNotFoundMsg}, nil
			}
			l.Logger.Errorw("send update password email code query user failed", logx.Field("err", err), logx.Field("user_id", in.GetUserId()))
			return nil, err
		}
		if existUser.UserDeleted {
			return &users.EmailCodeResponse{StatusCode: code.UserHaveDeleted, StatusMsg: code.UserHaveDeletedMsg}, nil
		}
		email = normalizeEmail(existUser.Email.String)
	default:
		return &users.EmailCodeResponse{StatusCode: code.Fail, StatusMsg: code.FailMsg}, nil
	}

	cooldownKey := emailCodeCooldownKey(scene, email)
	ok, err := l.svcCtx.RedisClient.SetnxExCtx(l.ctx, cooldownKey, "1", emailCodeCooldown(l.svcCtx.Config.EmailCode))
	if err != nil {
		l.Logger.Errorw("set email code cooldown failed", logx.Field("err", err), logx.Field("scene", scene), logx.Field("email", email))
		return nil, err
	}
	if !ok {
		return &users.EmailCodeResponse{StatusCode: code.EmailCodeSendTooOften, StatusMsg: code.EmailCodeSendTooOftenMsg}, nil
	}

	emailCode, err := generateEmailCode()
	if err != nil {
		return nil, err
	}
	codeKey := emailCodeKey(scene, email)
	if err := l.svcCtx.RedisClient.SetexCtx(l.ctx, codeKey, emailCode, emailCodeTTL(l.svcCtx.Config.EmailCode)); err != nil {
		l.Logger.Errorw("save email code failed", logx.Field("err", err), logx.Field("scene", scene), logx.Field("email", email))
		return nil, err
	}
	if err := l.svcCtx.EmailSender.SendCode(email, scene, emailCode); err != nil {
		_, _ = l.svcCtx.RedisClient.DelCtx(l.ctx, codeKey, cooldownKey)
		l.Logger.Errorw("send email code failed", logx.Field("err", err), logx.Field("scene", scene), logx.Field("email", email))
		return &users.EmailCodeResponse{StatusCode: code.EmailCodeSendFailed, StatusMsg: code.EmailCodeSendFailedMsg}, nil
	}

	return &users.EmailCodeResponse{StatusCode: code.Success, StatusMsg: code.SuccessMsg}, nil
}
