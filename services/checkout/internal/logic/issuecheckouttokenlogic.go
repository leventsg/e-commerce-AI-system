package logic

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"math"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	model "github.com/leventsg/e-commerce-AI-system/dal/model/checkout"

	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type IssueCheckoutTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewIssueCheckoutTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *IssueCheckoutTokenLogic {
	return &IssueCheckoutTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

var intentIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

const checkoutTokenTTL = 10 * time.Minute

func (l *IssueCheckoutTokenLogic) IssueCheckoutToken(in *checkout.IssueCheckoutTokenReq) (*checkout.IssueCheckoutTokenResp, error) {
	if in == nil || in.UserId == 0 || in.UserId > math.MaxInt32 || !intentIDPattern.MatchString(in.IntentId) {
		return &checkout.IssueCheckoutTokenResp{StatusCode: code.InvalidParams, StatusMsg: code.InvalidParamsMsg}, nil
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	preOrderID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	row, err := l.svcCtx.CheckoutIntents.Issue(l.ctx, &model.Checkouts{
		PreOrderId: preOrderID.String(), UserId: uint64(in.UserId), Status: int64(checkout.CheckoutStatus_PREPARING),
		CheckoutToken:    sql.NullString{String: hex.EncodeToString(random[:]), Valid: true},
		CheckoutIntentId: sql.NullString{String: in.IntentId, Valid: true},
		TokenExpiresAt:   time.Now().Add(checkoutTokenTTL).Unix(), PrepareState: model.PrepareIssued,
	})
	if err != nil {
		return nil, err
	}
	return &checkout.IssueCheckoutTokenResp{CheckoutToken: row.CheckoutToken.String, ExpiresAt: row.TokenExpiresAt}, nil
}
