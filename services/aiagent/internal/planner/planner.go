package planner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	intentprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/intent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
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
	Message string
	History []*aimessages.AiMessages
}

type Planner struct {
	registry          *tools.Registry
	modelFactory      eino.ModelFactory
	intentModelConfig config.EinoConfig
	maxLLMAttempts    int
}

type Option func(*Planner)

func WithIntentModel(factory eino.ModelFactory, cfg config.EinoConfig) Option {
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
		if result, ok := p.planWithLLM(ctx, text, req.History); ok {
			return result, nil
		}
	}

	// 当llm识别失败时，使用规则匹配意图（降级处理）
	return p.rulePlan(text)
}

func (p *Planner) planWithLLM(ctx context.Context, text string, history []*aimessages.AiMessages) (PlanResult, bool) {
	for attempt := 0; attempt < p.maxLLMAttempts; attempt++ {
		result, err := p.callLLMPlannerOnce(ctx, text, history)
		if err != nil {
			continue
		}
		if planned, ok := p.validatedPlan(ctx, result); ok {
			return planned, true
		}
	}
	return PlanResult{}, false
}

func (p *Planner) callLLMPlannerOnce(ctx context.Context, text string, history []*aimessages.AiMessages) (PlanResult, error) {
	chatModel, err := p.modelFactory.NewChatModel(ctx, p.intentModelConfig)
	if err != nil {
		return PlanResult{}, err
	}

	// 调用llm进行意图识别
	response, err := chatModel.Generate(ctx, buildLLMPlannerMessages(text, history))
	if err != nil {
		return PlanResult{}, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return PlanResult{}, errors.New("llm planner returned empty content")
	}

	// 解析llm返回的json字符串
	var output llmPlanResult
	if err := json.Unmarshal([]byte(response.Content), &output); err != nil {
		return PlanResult{}, err
	}

	return PlanResult{
		Intent:           Intent(output.Intent),
		ToolName:         output.ToolName,
		Arguments:        output.Arguments,
		AssistantMessage: output.AssistantMessage,
		MissingParams:    output.MissingParams,
	}, nil
}

type llmPlanResult struct {
	Intent           string         `json:"intent"`
	ToolName         string         `json:"tool_name"`
	Arguments        map[string]any `json:"arguments"`
	MissingParams    []string       `json:"missing_params"`
	AssistantMessage string         `json:"assistant_message"`
}

const maxPlannerContextMessages = 8

func buildLLMPlannerMessages(text string, history []*aimessages.AiMessages) []*schema.Message {
	messages := []*schema.Message{schema.SystemMessage(intentprompt.IntentSystemPrompt)}
	messages = append(messages, recentContextMessages(text, history, maxPlannerContextMessages)...)
	return append(messages, schema.UserMessage(text))
}

// 整理最近上下文消息，并保留原始 role 语义。
func recentContextMessages(currentText string, history []*aimessages.AiMessages, limit int) []*schema.Message {
	if limit <= 0 || len(history) == 0 {
		return nil
	}

	currentText = strings.TrimSpace(currentText)
	messages := make([]*schema.Message, 0, limit)
	for i := len(history) - 1; i >= 0 && len(messages) < limit; i-- {
		item := history[i]
		if item == nil {
			continue
		}
		// 过滤掉不需要的角色和内容
		role := normalizedHistoryRole(item.Role)
		if role == "" {
			continue
		}
		// 按照字符数（最多300个字符）进行截断压缩
		content := compactContextContent(item.Content)
		// 内容为空，或者用户消息与当前消息重复，则跳过
		if content == "" || (role == "user" && (content == currentText || strings.TrimSpace(item.Content) == currentText)) {
			continue
		}
		message := historyMessageByRole(role, content, item.Metadata)
		if message == nil {
			continue
		}
		messages = append(messages, message)
	}

	// 将 messages 反转，保持历史消息的时间顺序。
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages
}

// 根据role，创建对应的 schema.Message 对象
func historyMessageByRole(role, content string, metadata sql.NullString) *schema.Message {
	switch role {
	case "user":
		return schema.UserMessage(content)
	case "assistant":
		return schema.AssistantMessage(content, nil)
	case "tool":
		meta := parsePlannerMessageMetadata(metadata)
		return schema.ToolMessage(content, meta.ToolCallID, schema.WithToolName(meta.ToolName))
	default:
		return nil
	}
}

type plannerMessageMetadata struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

// 解析 AiMessages 的 metadata 字段，提取工具调用相关信息
func parsePlannerMessageMetadata(metadata sql.NullString) plannerMessageMetadata {
	if !metadata.Valid || strings.TrimSpace(metadata.String) == "" {
		return plannerMessageMetadata{}
	}
	var meta plannerMessageMetadata
	_ = json.Unmarshal([]byte(metadata.String), &meta)
	return meta
}

func normalizedHistoryRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "assistant", "tool":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return ""
	}
}

// 压缩上下文内容，最多300个字符
func compactContextContent(content string) string {
	content = strings.Join(strings.Fields(redactSensitiveText(content)), " ")

	// 按照字符数进行截断，避免破坏中文字符
	runes := []rune(content)
	if len(runes) > 300 {
		content = string(runes[:300])
	}
	return strings.TrimSpace(content)
}

// 脱敏脱敏，将敏感参数替换为 [redacted]
func redactSensitiveText(text string) string {
	// 处理敏感参数，将敏感参数替换为 [redacted]"
	text = sensitiveAssignmentPattern.ReplaceAllString(text, "$1=[redacted]")
	return sensitiveColonPattern.ReplaceAllString(text, "$1:[redacted]")
}

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
			result.Arguments = sanitizeArguments(result.Arguments)
			return result, true
		}
		return PlanResult{}, false
	}

	// 获取工具元数据
	metadata, err := p.registry.Metadata(result.ToolName)
	if err != nil {
		return PlanResult{}, false
	}

	args := sanitizeArguments(result.Arguments)
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

// 清理参数，过滤敏感信息
func sanitizeArguments(args map[string]any) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}
	cleaned := make(map[string]any, len(args))
	for key, value := range args {
		if isSensitiveArgumentKey(key) {
			continue
		}
		cleaned[key] = sanitizeArgumentValue(value)
	}
	return cleaned
}

// 清理参数值，过滤敏感信息
func sanitizeArgumentValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return sanitizeArguments(v)
	case []any:
		cleaned := make([]any, 0, len(v))
		for _, item := range v {
			cleaned = append(cleaned, sanitizeArgumentValue(item))
		}
		return cleaned
	default:
		return value
	}
}

// 判断参数是否为敏感参数
func isSensitiveArgumentKey(key string) bool {
	for _, sensitiveKey := range sensitiveArgumentKeys {
		if strings.EqualFold(strings.TrimSpace(key), sensitiveKey) {
			return true
		}
	}
	return false
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
	orderIDPattern             = regexp.MustCompile(`(?i)([0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}|\d{6,})`)
	productIDRegexp            = regexp.MustCompile(`商品\s*(\d+)`)
	quantityRegexp             = regexp.MustCompile(`(\d+)\s*件`)
	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*=\s*[^\s,，;；]+`)
	sensitiveColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*[:：]\s*[^\s,，;；]+`)
	sensitiveArgumentKeys      = []string{"user_id", "token", "session_id", "auth"}
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
