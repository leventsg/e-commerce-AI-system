package logic

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"net/mail"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/dal/model/user"
	"github.com/leventsg/e-commerce-AI-system/services/users/internal/config"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	emailCodeSceneRegister       = "register"
	emailCodeSceneForgotPassword = "forgot_password"
	emailCodeSceneUpdatePassword = "update_password"

	defaultEmailCodeTTLSeconds     = 300
	defaultEmailCodeCooldownSecond = 60
)

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil && strings.Contains(email, "@")
}

func emailCodeKey(scene, email string) string {
	return fmt.Sprintf("user:email_code:%s:%s", scene, normalizeEmail(email))
}

func emailCodeCooldownKey(scene, email string) string {
	return fmt.Sprintf("user:email_code_cooldown:%s:%s", scene, normalizeEmail(email))
}

func emailCodeTTL(conf config.EmailCodeConfig) int {
	if conf.TTLSeconds > 0 {
		return conf.TTLSeconds
	}
	return defaultEmailCodeTTLSeconds
}

func emailCodeCooldown(conf config.EmailCodeConfig) int {
	if conf.ResendCooldown > 0 {
		return conf.ResendCooldown
	}
	return defaultEmailCodeCooldownSecond
}

func generateEmailCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func findUserByEmail(ctx context.Context, usersModel user.UsersModel, email string) (*user.Users, error) {
	return usersModel.FindOneByEmail(ctx, sql.NullString{String: normalizeEmail(email), Valid: true})
}

func verifyEmailCode(ctx context.Context, rdb *redis.Redis, scene, email, input string) (bool, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return false, nil
	}
	key := emailCodeKey(scene, email)
	stored, err := rdb.GetCtx(ctx, key)
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	if stored != input {
		return false, nil
	}
	_, _ = rdb.DelCtx(ctx, key)
	return true, nil
}

func emailCodeFailureResponse() *usersEmailCodeStatus {
	return &usersEmailCodeStatus{statusCode: code.EmailCodeInvalid, statusMsg: code.EmailCodeInvalidMsg}
}

type usersEmailCodeStatus struct {
	statusCode int
	statusMsg  string
}
