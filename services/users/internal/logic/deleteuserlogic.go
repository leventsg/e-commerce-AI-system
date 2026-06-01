package logic

import (
	"context"
	"database/sql"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/audit/audit"
	"github.com/leventsg/e-commerce-AI-system/services/users/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserLogic {
	return &DeleteUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 删除用户方法
func (l *DeleteUserLogic) DeleteUser(in *users.DeleteUserRequest) (*users.DeleteUserResponse, error) {

	exituser, err := l.svcCtx.UsersModel.FindOne(l.ctx, int64(in.UserId))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {

			return &users.DeleteUserResponse{

				StatusCode: code.UserAddressNotFound,
				StatusMsg:  code.UserAddressNotFoundMsg,
			}, nil
		}
		logx.Errorw(code.ServerErrorMsg, logx.Field("err", err), logx.Field("user_id", in.UserId))
		return &users.DeleteUserResponse{}, err
	}
	// 删除用户
	if exituser.UserDeleted {
		l.Logger.Infow("delete user have deleted", logx.Field("user_id", in.UserId))
		return &users.DeleteUserResponse{

			StatusCode: code.UserHaveDeleted,
			StatusMsg:  code.UserHaveDeletedMsg,
		}, nil

	}
	err = l.svcCtx.UsersModel.UpdateDeletebyId(l.ctx, int64(in.UserId), true)
	if err != nil {
		l.Logger.Infow("delete update delete by id failed", logx.Field("err", err),
			logx.Field("user_id", in.UserId))

		return &users.DeleteUserResponse{}, err
	}

	//添加审计服务
	auditreq := &audit.CreateAuditLogReq{
		UserId:            uint32(in.UserId),
		ActionType:        biz.Delete,
		TargetTable:       "user",
		ActionDescription: "用户注销",
		TargetId:          int64(in.UserId),
		ServiceName:       "users",
		ClientIp:          in.Ip,
	}
	_, err = l.svcCtx.AuditRpc.CreateAuditLog(l.ctx, auditreq)
	if err != nil {

		l.Logger.Infow("add address audit failed", logx.Field("err", err),
			logx.Field("body", auditreq))

	}

	return &users.DeleteUserResponse{
		UserId: in.UserId,
	}, nil
}
