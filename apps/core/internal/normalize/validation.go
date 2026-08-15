package normalize

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"aggregationhub.local/core/internal/provider"
)

var (
	ErrInvalidRequest         = errors.New("归一化请求无效")
	ErrSystemMustBeSeparate   = errors.New("System 必须与普通消息分离")
	ErrToolSchemaTooDeep      = errors.New("工具 Schema 嵌套过深")
	ErrInvalidToolResult      = errors.New("Tool Result 未引用当前请求中的 Tool Call")
	ErrInvalidEvent           = errors.New("归一化流事件无效")
	ErrResponseStartRequired  = errors.New("缺少 response_start 事件")
	ErrResponseAlreadyStarted = errors.New("response_start 事件重复")
	ErrTerminalEventRequired  = errors.New("缺少流式终态事件")
	ErrEventAfterTerminal     = errors.New("终态事件后不允许继续发送事件")
)

// ValidationLimits 限制不可信入口负载，所有零值会回退到默认值。
type ValidationLimits struct {
	MaxMessages          int
	MaxPartsPerMessage   int
	MaxTextBytes         int
	MaxTools             int
	MaxToolSchemaBytes   int
	MaxToolSchemaDepth   int
	MaxStopSequences     int
	MaxStopSequenceBytes int
	MaxIdentifierBytes   int
}

func DefaultValidationLimits() ValidationLimits {
	return ValidationLimits{
		MaxMessages:          256,
		MaxPartsPerMessage:   64,
		MaxTextBytes:         256 * 1024,
		MaxTools:             64,
		MaxToolSchemaBytes:   64 * 1024,
		MaxToolSchemaDepth:   32,
		MaxStopSequences:     16,
		MaxStopSequenceBytes: 1024,
		MaxIdentifierBytes:   304,
	}
}

func (limits ValidationLimits) normalized() ValidationLimits {
	defaults := DefaultValidationLimits()
	if limits.MaxMessages == 0 {
		limits.MaxMessages = defaults.MaxMessages
	}
	if limits.MaxPartsPerMessage == 0 {
		limits.MaxPartsPerMessage = defaults.MaxPartsPerMessage
	}
	if limits.MaxTextBytes == 0 {
		limits.MaxTextBytes = defaults.MaxTextBytes
	}
	if limits.MaxTools == 0 {
		limits.MaxTools = defaults.MaxTools
	}
	if limits.MaxToolSchemaBytes == 0 {
		limits.MaxToolSchemaBytes = defaults.MaxToolSchemaBytes
	}
	if limits.MaxToolSchemaDepth == 0 {
		limits.MaxToolSchemaDepth = defaults.MaxToolSchemaDepth
	}
	if limits.MaxStopSequences == 0 {
		limits.MaxStopSequences = defaults.MaxStopSequences
	}
	if limits.MaxStopSequenceBytes == 0 {
		limits.MaxStopSequenceBytes = defaults.MaxStopSequenceBytes
	}
	if limits.MaxIdentifierBytes == 0 {
		limits.MaxIdentifierBytes = defaults.MaxIdentifierBytes
	}
	return limits
}

// ValidateRequest 校验稳定协议边界并返回路由所需的能力集合。
func ValidateRequest(request NormalizedRequest, limits ValidationLimits) (provider.RequiredCapabilities, error) {
	limits = limits.normalized()
	if !validIdentifier(request.Model, limits.MaxIdentifierBytes) || len(request.Messages) == 0 || len(request.Messages) > limits.MaxMessages {
		return provider.RequiredCapabilities{}, ErrInvalidRequest
	}
	if request.Temperature != nil && (*request.Temperature < 0 || *request.Temperature > 2) {
		return provider.RequiredCapabilities{}, ErrInvalidRequest
	}
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens < 1 {
		return provider.RequiredCapabilities{}, ErrInvalidRequest
	}
	if err := validateSystem(request.System, limits); err != nil {
		return provider.RequiredCapabilities{}, err
	}
	if err := validateTools(request.Tools, limits); err != nil {
		return provider.RequiredCapabilities{}, err
	}
	if err := validateToolChoice(request.ToolChoice, request.Tools, limits); err != nil {
		return provider.RequiredCapabilities{}, err
	}
	if err := validateStopSequences(request.StopSequences, limits); err != nil {
		return provider.RequiredCapabilities{}, err
	}
	if err := validateMessages(request.Messages, limits); err != nil {
		return provider.RequiredCapabilities{}, err
	}
	return request.RequiredCapabilities(), nil
}

func validateSystem(parts []TextPart, limits ValidationLimits) error {
	for _, part := range parts {
		if !validText(part.Text, limits.MaxTextBytes) {
			return ErrInvalidRequest
		}
	}
	return nil
}

func validateMessages(messages []Message, limits ValidationLimits) error {
	knownToolCalls := make(map[string]struct{})
	for _, message := range messages {
		if message.Role == RoleSystem {
			return ErrSystemMustBeSeparate
		}
		if message.Role != RoleUser && message.Role != RoleAssistant && message.Role != RoleTool || len(message.Parts) == 0 || len(message.Parts) > limits.MaxPartsPerMessage {
			return ErrInvalidRequest
		}
		for _, part := range message.Parts {
			switch value := part.(type) {
			case TextPart:
				if !validText(value.Text, limits.MaxTextBytes) {
					return ErrInvalidRequest
				}
			case ImagePart:
				if message.Role != RoleUser || !validText(value.URL, limits.MaxTextBytes) || !validIdentifier(value.MediaType, 128) {
					return ErrInvalidRequest
				}
			case ReasoningPart:
				if message.Role != RoleAssistant || !validText(value.Text, limits.MaxTextBytes) {
					return ErrInvalidRequest
				}
			case ToolCallPart:
				if message.Role != RoleAssistant || !validIdentifier(value.CallID, limits.MaxIdentifierBytes) || !validToolName(value.Name) || !validJSONObjectString(value.Arguments) {
					return ErrInvalidRequest
				}
				if _, exists := knownToolCalls[value.CallID]; exists {
					return ErrInvalidRequest
				}
				knownToolCalls[value.CallID] = struct{}{}
			case ToolResultPart:
				if message.Role != RoleTool || !validIdentifier(value.CallID, limits.MaxIdentifierBytes) || !validText(value.Content, limits.MaxTextBytes) {
					return ErrInvalidRequest
				}
				if _, exists := knownToolCalls[value.CallID]; !exists {
					return ErrInvalidToolResult
				}
			default:
				return ErrInvalidRequest
			}
		}
	}
	return nil
}

func validateTools(tools []ToolDefinition, limits ValidationLimits) error {
	if len(tools) > limits.MaxTools {
		return ErrInvalidRequest
	}
	names := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if !validToolName(tool.Name) || len(tool.Description) > limits.MaxTextBytes || len(tool.InputSchema) == 0 || len(tool.InputSchema) > limits.MaxToolSchemaBytes {
			return ErrInvalidRequest
		}
		if _, exists := names[tool.Name]; exists {
			return ErrInvalidRequest
		}
		names[tool.Name] = struct{}{}
		if err := validateJSONObjectDepth(tool.InputSchema, limits.MaxToolSchemaDepth); err != nil {
			return err
		}
	}
	return nil
}

func validateToolChoice(choice ToolChoice, tools []ToolDefinition, limits ValidationLimits) error {
	if choice.Mode == "" || choice.Mode == ToolChoiceAuto || choice.Mode == ToolChoiceNone || choice.Mode == ToolChoiceRequired {
		return nil
	}
	if choice.Mode != ToolChoiceNamed || !validToolName(choice.Name) {
		return ErrInvalidRequest
	}
	for _, tool := range tools {
		if tool.Name == choice.Name {
			return nil
		}
	}
	return ErrInvalidRequest
}

func validateStopSequences(sequences []string, limits ValidationLimits) error {
	if len(sequences) > limits.MaxStopSequences {
		return ErrInvalidRequest
	}
	for _, sequence := range sequences {
		if !validText(sequence, limits.MaxStopSequenceBytes) {
			return ErrInvalidRequest
		}
	}
	return nil
}

func validateJSONObjectDepth(raw json.RawMessage, maxDepth int) error {
	if maxDepth < 1 || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return ErrInvalidRequest
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return ErrInvalidRequest
	}
	depth, err := jsonValueDepth(raw)
	if err != nil {
		return ErrInvalidRequest
	}
	if depth > maxDepth {
		return ErrToolSchemaTooDeep
	}
	return nil
}

func jsonValueDepth(raw json.RawMessage) (int, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		depth := 1
		for _, child := range object {
			childDepth, err := jsonValueDepth(child)
			if err != nil {
				return 0, err
			}
			if childDepth+1 > depth {
				depth = childDepth + 1
			}
		}
		return depth, nil
	}
	var array []json.RawMessage
	if err := json.Unmarshal(raw, &array); err == nil {
		depth := 1
		for _, child := range array {
			childDepth, err := jsonValueDepth(child)
			if err != nil {
				return 0, err
			}
			if childDepth+1 > depth {
				depth = childDepth + 1
			}
		}
		return depth, nil
	}
	var primitive json.RawMessage
	if err := json.Unmarshal(raw, &primitive); err != nil || !json.Valid(raw) {
		return 0, errors.New("JSON 无效")
	}
	return 1, nil
}

func validJSONObjectString(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed))
}
func validText(value string, maxBytes int) bool {
	return value != "" && len(value) <= maxBytes && utf8.ValidString(value)
}
func validIdentifier(value string, maxBytes int) bool {
	return value != "" && len(value) <= maxBytes && strings.TrimSpace(value) == value && utf8.ValidString(value)
}
func validToolName(value string) bool {
	if !validIdentifier(value, 128) {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

// EventSequenceValidator 保护流式输出的顺序：恰好一个 start，恰好一个 response_end 或 error。
type EventSequenceValidator struct {
	started  bool
	terminal bool
	contents map[string]PartKind
}

func NewEventSequenceValidator() *EventSequenceValidator {
	return &EventSequenceValidator{contents: make(map[string]PartKind)}
}

func (validator *EventSequenceValidator) Validate(event NormalizedEvent) error {
	if validator == nil || event == nil {
		return ErrInvalidEvent
	}
	if validator.terminal {
		return ErrEventAfterTerminal
	}
	switch value := event.(type) {
	case ResponseStartEvent:
		if validator.started || !validIdentifier(value.ResponseID, 304) || !validIdentifier(value.Model, 304) {
			return eventStartError(validator.started)
		}
		validator.started = true
	case ContentStartEvent:
		if !validator.started || !validIdentifier(value.ContentID, 304) || value.Kind == "" {
			return ErrInvalidEvent
		}
		if _, exists := validator.contents[value.ContentID]; exists {
			return ErrInvalidEvent
		}
		validator.contents[value.ContentID] = value.Kind
	case TextDeltaEvent:
		if !validator.acceptsContent(value.ContentID, PartKindText) || value.Text == "" {
			return ErrInvalidEvent
		}
	case ReasoningDeltaEvent:
		if !validator.acceptsContent(value.ContentID, PartKindReasoning) || value.Text == "" {
			return ErrInvalidEvent
		}
	case ToolCallStartEvent:
		if !validator.acceptsContent(value.ContentID, PartKindToolCall) || !validIdentifier(value.CallID, 304) || !validToolName(value.Name) {
			return ErrInvalidEvent
		}
	case ToolCallArgumentsDeltaEvent:
		if !validator.acceptsContent(value.ContentID, PartKindToolCall) || !validIdentifier(value.CallID, 304) || value.Delta == "" {
			return ErrInvalidEvent
		}
	case ContentEndEvent:
		if !validator.started || !validIdentifier(value.ContentID, 304) {
			return ErrInvalidEvent
		}
		if _, exists := validator.contents[value.ContentID]; !exists {
			return ErrInvalidEvent
		}
		delete(validator.contents, value.ContentID)
	case UsageUpdateEvent:
		if !validator.started || !validUsage(value.Usage) {
			return ErrInvalidEvent
		}
	case ResponseEndEvent:
		if !validator.started || value.FinishReason == "" {
			return ErrInvalidEvent
		}
		validator.terminal = true
	case ErrorEvent:
		if !validator.started || !validIdentifier(value.Code, 128) || !validText(value.Message, 4096) {
			return ErrInvalidEvent
		}
		validator.terminal = true
	default:
		return fmt.Errorf("%w: 未知事件类型", ErrInvalidEvent)
	}
	return nil
}

func (validator *EventSequenceValidator) Finalize() error {
	if validator == nil || !validator.started {
		return ErrResponseStartRequired
	}
	if !validator.terminal {
		return ErrTerminalEventRequired
	}
	return nil
}
func (validator *EventSequenceValidator) acceptsContent(id string, kind PartKind) bool {
	if !validator.started || !validIdentifier(id, 304) {
		return false
	}
	actual, exists := validator.contents[id]
	return exists && actual == kind
}
func eventStartError(started bool) error {
	if started {
		return ErrResponseAlreadyStarted
	}
	return ErrInvalidEvent
}
func validUsage(value Usage) bool {
	if value.Source == "" {
		return false
	}
	for _, count := range []*int64{value.InputTokens, value.OutputTokens, value.CachedInputTokens, value.CacheWriteTokens, value.ReasoningTokens} {
		if count != nil && *count < 0 {
			return false
		}
	}
	return true
}
