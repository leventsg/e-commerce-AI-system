package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/zeromicro/go-zero/core/logx"
)

type Intent string

const (
	IntentChat      Intent = "chat"
	IntentQuery     Intent = "query"
	IntentRecommend Intent = "recommend"
	IntentAction    Intent = "action"
)

type PlanResult struct {
	Intent              Intent
	ToolName            string
	Arguments           map[string]any
	RequireConfirmation bool
	AssistantMessage    string
	MissingParams       []string
}

type PlanRequest struct {
	Message  string
	Messages []domain.ContextMessage
}

type IntentModel interface {
	Generate(ctx context.Context, messages []domain.ContextMessage) (string, error)
}

type IntentModelFactory interface {
	NewIntentModel(ctx context.Context, cfg config.EinoConfig) (IntentModel, error)
}

type Planner struct {
	registry          *tools.Registry
	modelFactory      IntentModelFactory
	intentModelConfig config.EinoConfig
	maxLLMAttempts    int
}

type Option func(*Planner)

func WithIntentModel(factory IntentModelFactory, cfg config.EinoConfig) Option {
	return func(p *Planner) {
		p.modelFactory = factory
		p.intentModelConfig = cfg
	}
}

func WithMaxLLMAttempts(attempts int) Option {
	return func(p *Planner) {
		if attempts > 0 {
			p.maxLLMAttempts = attempts
		}
	}
}

func New(registry *tools.Registry, opts ...Option) *Planner {
	p := &Planner{
		registry:       registry,
		maxLLMAttempts: 2,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Planner) Plan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	text := strings.TrimSpace(req.Message)
	if text == "" {
		return PlanResult{
			Intent:           IntentChat,
			AssistantMessage: "请告诉我你想咨询或办理什么。",
		}, nil
	}

	// 意图识别(llm)
	if p.modelFactory != nil {
		if result, ok := p.planWithLLM(ctx, text, req.Messages); ok {
			return result, nil
		}
	}

	// 当llm识别失败时，使用规则匹配意图（降级处理）
	return p.rulePlan(text)
}

func (p *Planner) planWithLLM(ctx context.Context, text string, messages []domain.ContextMessage) (PlanResult, bool) {
	for attempt := 0; attempt < p.maxLLMAttempts; attempt++ {
		result, err := p.callLLMPlannerOnce(ctx, messages)
		if err != nil {
			logx.WithContext(ctx).Errorw("ai intent planner attempt failed", logx.Field("component", "intent_planner"), logx.Field("attempt", attempt+1), logx.Field("stage", plannerErrorStage(err)), logx.Field("reason", plannerErrorReason(err)), logx.Field("err", err))
			continue
		}
		if planned, ok := p.validatedPlan(ctx, result); ok {
			return planned, true
		}
		logx.WithContext(ctx).Errorw("ai intent planner response invalid", logx.Field("component", "intent_planner"), logx.Field("attempt", attempt+1), logx.Field("stage", "validate"), logx.Field("reason", "planner_response_invalid"))
	}
	return PlanResult{}, false
}

func (p *Planner) callLLMPlannerOnce(ctx context.Context, messages []domain.ContextMessage) (PlanResult, error) {
	intentModel, err := p.modelFactory.NewIntentModel(ctx, p.intentModelConfig)
	if err != nil {
		return PlanResult{}, fmt.Errorf("initialize intent model: %w", err)
	}

	response, err := intentModel.Generate(ctx, messages)
	if err != nil {
		return PlanResult{}, fmt.Errorf("generate intent plan: %w", err)
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return PlanResult{}, fmt.Errorf("generate intent plan: %w", ErrEmptyIntentModelResponse)
	}

	// 解析llm返回的json字符串
	var output llmPlanResult
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return PlanResult{}, fmt.Errorf("invalid intent planner response: %w", err)
	}

	return PlanResult{
		Intent:           Intent(output.Intent),
		ToolName:         output.ToolName,
		Arguments:        output.Arguments,
		AssistantMessage: output.AssistantMessage,
		MissingParams:    output.MissingParams,
	}, nil
}

func plannerErrorStage(err error) string {
	if strings.HasPrefix(err.Error(), "initialize intent model:") {
		return "initialize"
	}
	if strings.HasPrefix(err.Error(), "generate intent plan:") {
		return "generate"
	}
	return "validate"
}

func plannerErrorReason(err error) string {
	if errors.Is(err, ErrEmptyIntentModelResponse) {
		return "model_empty_response"
	}
	if strings.HasPrefix(err.Error(), "invalid intent planner response:") {
		return "planner_response_invalid"
	}
	return "model_error"
}

type llmPlanResult struct {
	Intent           string         `json:"intent"`
	ToolName         string         `json:"tool_name"`
	Arguments        map[string]any `json:"arguments"`
	MissingParams    []string       `json:"missing_params"`
	AssistantMessage string         `json:"assistant_message"`
}

var ErrEmptyIntentModelResponse = errors.New("intent model returned empty response")

// 规则匹配意图
func (p *Planner) rulePlan(text string) (PlanResult, error) {
	// 取消订单
	if isCancelOrder(text) {
		orderID := extractOrderID(text)
		if orderID == "" {
			return PlanResult{
				Intent:           IntentAction,
				AssistantMessage: "请提供要取消的订单号。",
				MissingParams:    []string{"order_id"},
			}, nil
		}
		return p.toolPlan(IntentAction, domain.ToolOrderCancel, map[string]any{
			"order_id": orderID,
		})
	}

	// 查询订单
	if isOrderQuery(text) {
		orderID := extractOrderID(text)
		if orderID == "" {
			return PlanResult{
				Intent:           IntentQuery,
				AssistantMessage: "请提供要查询的订单号。",
				MissingParams:    []string{"order_id"},
			}, nil
		}
		return p.toolPlan(IntentQuery, domain.ToolOrderGet, map[string]any{
			"order_id": orderID,
		})
	}

	// 加入购物车
	if isCartAdd(text) {
		productID, ok := extractProductID(text)
		if !ok {
			return PlanResult{
				Intent:           IntentAction,
				AssistantMessage: "请提供要加入购物车的商品 ID。",
				MissingParams:    []string{"product_id"},
			}, nil
		}
		quantity := extractQuantity(text)
		if quantity <= 0 {
			quantity = 1
		}
		return p.toolPlan(IntentAction, domain.ToolCartAdd, map[string]any{
			"product_id": productID,
			"quantity":   quantity,
		})
	}

	// 商品推荐
	if isRecommendation(text) {
		return p.toolPlan(IntentRecommend, domain.ToolProductRecommend, map[string]any{
			"query": text,
		})
	}

	return PlanResult{Intent: IntentChat}, nil
}

// 校验意图结果，如果需要调用工具，则检查工具是否存在，参数是否完整，返回校验后的结果和是否有效的标志
func (p *Planner) validatedPlan(ctx context.Context, result PlanResult) (PlanResult, bool) {
	if !validIntent(result.Intent) {
		return PlanResult{}, false
	}
	if result.ToolName == "" {
		if result.Intent == IntentChat || len(result.MissingParams) > 0 {
			result.RequireConfirmation = false
			result.Arguments = argx.SanitizeMapKeys(result.Arguments, sensitiveArgumentKeys)
			if result.Intent == IntentChat && len(result.MissingParams) == 0 {
				result.AssistantMessage = ""
			}
			return result, true
		}
		return PlanResult{}, false
	}

	// 获取工具元数据
	metadata, err := p.registry.Metadata(result.ToolName)
	if err != nil {
		return PlanResult{}, false
	}

	args := argx.SanitizeMapKeys(result.Arguments, sensitiveArgumentKeys)
	if missingRequiredArguments(ctx, p.registry, result.ToolName, args) {
		return PlanResult{}, false
	}

	result.ToolName = metadata.Name
	result.Arguments = args
	result.RequireConfirmation = metadata.RequireConfirmation
	return result, true
}

// 返回工具调用的意图结果
func (p *Planner) toolPlan(intent Intent, toolName string, args map[string]any) (PlanResult, error) {
	metadata, err := p.registry.Metadata(toolName)
	if err != nil {
		return PlanResult{}, err
	}

	return PlanResult{
		Intent:              intent,
		ToolName:            metadata.Name,
		Arguments:           args,
		RequireConfirmation: metadata.RequireConfirmation,
	}, nil
}

func validIntent(intent Intent) bool {
	switch intent {
	case IntentChat, IntentQuery, IntentRecommend, IntentAction:
		return true
	default:
		return false
	}
}

// 检查参数是否缺少必填项
func missingRequiredArguments(ctx context.Context, registry *tools.Registry, toolName string, args map[string]any) bool {
	tool, err := registry.Tool(toolName)
	if err != nil {
		return true
	}
	// 获取工具信息
	info, err := tool.Info(ctx)
	if err != nil || info == nil || info.ParamsOneOf == nil {
		return err != nil
	}
	// 将工具参数转换为JSON Schema
	paramsSchema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || paramsSchema == nil {
		return err != nil
	}
	// 遍历 Required 字段，检查每个必填参数是否存在且非空
	for _, required := range paramsSchema.Required {
		value, ok := args[required]
		if !ok || isEmptyArgument(value) {
			return true
		}
	}
	return false
}

func isEmptyArgument(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	default:
		return false
	}
}

func isRecommendation(text string) bool {
	return strings.Contains(text, "推荐")
}

func isOrderQuery(text string) bool {
	hasOrder := strings.Contains(text, "订单")
	hasQuery := strings.Contains(text, "查") || strings.Contains(text, "查询") || strings.Contains(text, "看一下")
	return hasOrder && hasQuery
}

func isCancelOrder(text string) bool {
	return strings.Contains(text, "订单") && strings.Contains(text, "取消")
}

func isCartAdd(text string) bool {
	hasCart := strings.Contains(text, "购物车")
	hasAdd := strings.Contains(text, "加入") || strings.Contains(text, "添加") || strings.Contains(text, "加到")
	return hasCart && hasAdd
}

var (
	orderIDPattern        = regexp.MustCompile(`(?i)([0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}|\d{6,})`)
	productIDRegexp       = regexp.MustCompile(`商品\s*(\d+)`)
	quantityRegexp        = regexp.MustCompile(`(\d+)\s*件`)
	sensitiveArgumentKeys = []string{"user_id", "token", "session_id", "auth"}
)

// 从文本中提取订单ID，正则匹配uuid格式的订单号
func extractOrderID(text string) string {
	return orderIDPattern.FindString(text)
}

func extractProductID(text string) (int64, bool) {
	matches := productIDRegexp.FindStringSubmatch(text)
	if len(matches) != 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func extractQuantity(text string) int64 {
	matches := quantityRegexp.FindStringSubmatch(text)
	if len(matches) != 2 {
		return 0
	}
	value, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0
	}
	return value
}
