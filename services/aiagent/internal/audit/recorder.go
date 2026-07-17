package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/audit/auditclient"
	"google.golang.org/grpc"
)

const (
	unknownClientIP        = "0.0.0.0"
	serviceName            = "aiagent"
	recordOperationTimeout = 3 * time.Second
	maxErrorMessageRunes   = 512
)

var sensitiveArgumentKeys = []string{"user_id", "token", "session_id", "auth"}

type ToolCallModel interface {
	Insert(ctx context.Context, data *aitoolcalls.AiToolCalls) (sql.Result, error)
}

type AuditRPC interface {
	CreateAuditLog(ctx context.Context, in *auditclient.CreateAuditLogReq, opts ...grpc.CallOption) (*auditclient.CreateAuditLogRes, error)
}

type Recorder struct {
	model    ToolCallModel
	auditRPC AuditRPC
}

func NewRecorder(model ToolCallModel, auditRPC AuditRPC) *Recorder {
	return &Recorder{model: model, auditRPC: auditRPC}
}

// RecordToolCall 记录工具调用记录
func (r *Recorder) RecordToolCall(ctx context.Context, record tools.ToolCallRecord) error {
	recordBaseCtx := context.WithoutCancel(ctx)
	// 压缩错误信息长度，避免数据库字段过长
	record.ErrorMessage = truncateRunes(record.ErrorMessage, maxErrorMessageRunes)
	// 清理敏感参数
	arguments := argx.SanitizeMapKeys(record.Arguments, sensitiveArgumentKeys)
	argumentsJSON, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("marshal tool call arguments: %w", err)
	}

	var recordErrors []error
	if r.model == nil {
		recordErrors = append(recordErrors, errors.New("ai tool call model is required"))
	} else {
		// 生成记录ID
		id, idErr := newRecordID()
		if idErr != nil {
			recordErrors = append(recordErrors, idErr)
		} else {
			modelCtx, cancel := context.WithTimeout(recordBaseCtx, recordOperationTimeout)
			// 插入工具调用记录
			_, insertErr := r.model.Insert(modelCtx, &aitoolcalls.AiToolCalls{
				Id:             id,
				ConversationId: record.ConversationID,
				UserId:         record.UserID,
				ToolName:       record.ToolName,
				Arguments:      string(argumentsJSON),
				ResultSummary:  nullableString(record.ResultSummary),
				Status:         record.Status,
				ErrorMessage:   record.ErrorMessage,
				LatencyMs:      record.Latency.Milliseconds(),
			})
			cancel()
			if insertErr != nil {
				recordErrors = append(recordErrors, fmt.Errorf("insert ai tool call: %w", insertErr))
			}
		}
	}

	// 如果是写操作，则记录审计日志
	if record.Metadata.WriteOperation {
		auditCtx, cancel := context.WithTimeout(recordBaseCtx, recordOperationTimeout)
		auditErr := r.recordWriteAudit(auditCtx, record, arguments)
		cancel()
		if auditErr != nil {
			recordErrors = append(recordErrors, auditErr)
		}
	}
	return errors.Join(recordErrors...)
}

// recordWriteAudit 记录写操作的审计日志
func (r *Recorder) recordWriteAudit(ctx context.Context, record tools.ToolCallRecord, arguments map[string]any) error {
	if r.auditRPC == nil {
		return errors.New("audit rpc is required for write operation")
	}
	actionType, targetTable, targetID := auditTarget(record)
	newData, err := json.Marshal(map[string]any{
		"arguments":      arguments,
		"status":         record.Status,
		"result_summary": record.ResultSummary,
		"error_message":  record.ErrorMessage,
		"latency_ms":     record.Latency.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("marshal write audit data: %w", err)
	}
	// 调用审计服务记录日志
	resp, err := r.auditRPC.CreateAuditLog(ctx, &auditclient.CreateAuditLogReq{
		UserId:            uint32(record.UserID),
		ActionType:        actionType,
		ActionDescription: fmt.Sprintf("AI customer service executed %s with status %s", record.ToolName, record.Status),
		TargetTable:       targetTable,
		TargetId:          targetID,
		NewData:           string(newData),
		CreateAt:          time.Now().Unix(),
		ClientIp:          auditClientIP(record.ClientIP),
		ServiceName:       serviceName,
	})
	if err != nil {
		return fmt.Errorf("create write audit log: %w", err)
	}
	if resp == nil || !resp.Ok {
		return errors.New("create write audit log returned unsuccessful response")
	}
	return nil
}

func auditTarget(record tools.ToolCallRecord) (actionType, targetTable string, targetID int64) {
	switch record.ToolName {
	case domain.ToolCartAdd:
		return biz.Create, "cart", firstPositiveInt64(record.ResultData, "cart_item_id", record.Arguments, "product_id", record.UserID)
	case domain.ToolCartSub:
		return biz.Update, "cart", firstPositiveInt64(record.Arguments, "cart_item_id", nil, "", record.UserID)
	case domain.ToolCouponClaim:
		return biz.Create, "user_coupons", int64(record.UserID)
	default:
		return biz.Update, "ai_tool", int64(record.UserID)
	}
}

func firstPositiveInt64(primary any, primaryKey string, fallback any, fallbackKey string, userID uint64) int64 {
	if value := mapInt64(primary, primaryKey); value > 0 {
		return value
	}
	if value := mapInt64(fallback, fallbackKey); value > 0 {
		return value
	}
	return int64(userID)
}

func mapInt64(value any, key string) int64 {
	if key == "" {
		return 0
	}
	values, ok := value.(map[string]any)
	if !ok {
		return 0
	}
	switch number := values[key].(type) {
	case int:
		return int64(number)
	case int32:
		return int64(number)
	case int64:
		return number
	case uint32:
		return int64(number)
	case uint64:
		if number <= uint64(^uint64(0)>>1) {
			return int64(number)
		}
	case float64:
		return int64(number)
	}
	return 0
}

func nullableString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

// 压缩字符数
func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func auditClientIP(value string) string {
	value = strings.TrimSpace(value)
	if net.ParseIP(value) == nil {
		return unknownClientIP
	}
	return value
}

func newRecordID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate ai tool call id: %w", err)
	}
	return id.String(), nil
}
