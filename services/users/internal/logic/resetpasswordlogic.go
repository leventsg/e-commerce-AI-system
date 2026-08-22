package logic

import (
	"context"
	"database/sql"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/common/utils/cryptx"
	"github.com/leventsg/e-commerce-AI-system/dal/model/user"
	"github.com/leventsg/e-commerce-AI-system/services/audit/audit"
	"github.com/leventsg/e-commerce-AI-system/services/users/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResetPasswordLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewResetPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetPasswordLogic {
	return &ResetPasswordLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ResetPasswordLogic) ResetPassword(in *users.ResetPasswordRequest) (*users.ResetPasswordResponse, error) {
	if in.GetPassword() == "" || in.GetConfirmPassword() == "" || in.GetPassword() != in.GetConfirmPassword() {
		return &users.ResetPasswordResponse{StatusCode: code.PasswordNotMatch, StatusMsg: code.PasswordNotMatchMsg}, nil
	}

	scene := in.GetScene()
	var (
		email    string
		target   *user.Users
		queryErr error
	)

	switch scene {
	case emailCodeSceneForgotPassword:
		email = normalizeEmail(in.GetEmail())
		if !validateEmail(email) {
			return &users.ResetPasswordResponse{StatusCode: code.EmailFormatError, StatusMsg: code.EmailFormatErrorMsg}, nil
		}
		target, queryErr = findUserByEmail(l.ctx, l.svcCtx.UsersModel, email)
	case emailCodeSceneUpdatePassword:
		if in.GetUserId() == 0 {
			return &users.ResetPasswordResponse{StatusCode: code.UserNotFound, StatusMsg: code.UserNotFoundMsg}, nil
		}
		target, queryErr = l.svcCtx.UsersModel.FindOne(l.ctx, int64(in.GetUserId()))
	default:
		return &users.ResetPasswordResponse{StatusCode: code.Fail, StatusMsg: code.FailMsg}, nil
	}

	if queryErr != nil {
		if errors.Is(queryErr, sql.ErrNoRows) {
			return &users.ResetPasswordResponse{StatusCode: code.UserNotFound, StatusMsg: code.UserNotFoundMsg}, nil
		}
		l.Logger.Errorw("reset password query user failed", logx.Field("err", queryErr), logx.Field("scene", scene), logx.Field("email", email), logx.Field("user_id", in.GetUserId()))
		return nil, queryErr
	}
	if target.UserDeleted {
		return &users.ResetPasswordResponse{StatusCode: code.UserHaveDeleted, StatusMsg: code.UserHaveDeletedMsg}, nil
	}
	email = normalizeEmail(target.Email.String)

	ok, err := verifyEmailCode(l.ctx, l.svcCtx.RedisClient, scene, email, in.GetCode())
	if err != nil {
		l.Logger.Errorw("verify email code failed", logx.Field("err", err), logx.Field("scene", scene), logx.Field("email", email))
		return nil, err
	}
	if !ok {
		return &users.ResetPasswordResponse{StatusCode: code.EmailCodeInvalid, StatusMsg: code.EmailCodeInvalidMsg}, nil
	}

	passwordHash := cryptx.PasswordEncrypt(in.GetPassword())
	if err := l.svcCtx.UsersModel.UpdatePasswordHash(l.ctx, target.UserId, passwordHash); err != nil {
		l.Logger.Errorw("reset password update hash failed", logx.Field("err", err), logx.Field("user_id", target.UserId))
		return &users.ResetPasswordResponse{StatusCode: code.PasswordResetFailed, StatusMsg: code.PasswordResetFailedMsg}, nil
	}

	auditReq := &audit.CreateAuditLogReq{
		UserId:            uint32(target.UserId),
		ActionType:        biz.Update,
		TargetTable:       "user",
		ActionDescription: "reset password",
		TargetId:          target.UserId,
		ServiceName:       "users",
		ClientIp:          in.GetIp(),
	}
	if _, err := l.svcCtx.AuditRpc.CreateAuditLog(l.ctx, auditReq); err != nil {
		l.Logger.Infow("reset password audit failed", logx.Field("err", err), logx.Field("body", auditReq))
	}

	return &users.ResetPasswordResponse{StatusCode: code.Success, StatusMsg: code.SuccessMsg}, nil
}
