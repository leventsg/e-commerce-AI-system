package bizerr

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const prefix = "BIZ_ERROR:"

func Encode(code int, msg string) string {
	return fmt.Sprintf("%s%d:%s", prefix, code, msg)
}

func Aborted(code int, msg string) error {
	return status.Error(codes.Aborted, Encode(code, msg))
}

func Parse(err error) (int, string, bool) {
	if err == nil {
		return 0, "", false
	}
	text := err.Error()
	// 找到 prefix 的位置下标
	idx := strings.Index(text, prefix)
	if idx < 0 {
		return 0, "", false
	}
	payload := text[idx+len(prefix):]
	// 将 payload 按照 ":" 分割为 code 和 msg
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	code, convErr := strconv.Atoi(parts[0])
	if convErr != nil {
		return 0, "", false
	}
	return code, parts[1], true
}
