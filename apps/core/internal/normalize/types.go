package normalize

import (
	"context"
	"encoding/json"

	"aggregationhub.local/core/internal/provider"
)

// Role 表示归一化消息的语义角色；System 必须使用 NormalizedRequest.System 单独表达。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// PartKind 用于保留内容语义，入口协议不得把 Tool Result 等降级为普通文本。
type PartKind string

const (
	PartKindText       PartKind = "text"
	PartKindImage      PartKind = "image"
	PartKindReasoning  PartKind = "reasoning"
	PartKindToolCall   PartKind = "tool_call"
	PartKindToolResult PartKind = "tool_result"
)

// ContentPart 是显式内容联合；仅允许本包定义的具体类型进入归一化主路径。
type ContentPart interface {
	contentPart()
	Kind() PartKind
}

type TextPart struct{ Text string }

func (TextPart) contentPart()   {}
func (TextPart) Kind() PartKind { return PartKindText }

type ImagePart struct {
	URL       string
	MediaType string
}

func (ImagePart) contentPart()   {}
func (ImagePart) Kind() PartKind { return PartKindImage }

type ReasoningPart struct{ Text string }

func (ReasoningPart) contentPart()   {}
func (ReasoningPart) Kind() PartKind { return PartKindReasoning }

type ToolCallPart struct {
	CallID    string
	Name      string
	Arguments string
}

func (ToolCallPart) contentPart()   {}
func (ToolCallPart) Kind() PartKind { return PartKindToolCall }

type ToolResultPart struct {
	CallID  string
	Content string
	IsError bool
}

func (ToolResultPart) contentPart()   {}
func (ToolResultPart) Kind() PartKind { return PartKindToolResult }

// Message 不允许携带 System Role，避免入口把不同协议的 System 语义混入普通对话。
type Message struct {
	Role  Role
	Parts []ContentPart
}

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceNamed    ToolChoiceMode = "named"
)

type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
}

// NormalizedRequest 是协议入口与 Provider Adapter 之间的稳定请求边界。
type NormalizedRequest struct {
	Model             string
	System            []TextPart
	Messages          []Message
	Tools             []ToolDefinition
	ToolChoice        ToolChoice
	Stream            bool
	ParallelToolCalls bool
	Temperature       *float64
	MaxOutputTokens   *int64
	StopSequences     []string
}

type UsageSource string

const (
	UsageSourceUpstreamReported UsageSource = "upstream_reported"
	UsageSourceLocallyEstimated UsageSource = "locally_estimated"
	UsageSourceUnknown          UsageSource = "unknown"
)

type Usage struct {
	InputTokens       *int64
	OutputTokens      *int64
	CachedInputTokens *int64
	CacheWriteTokens  *int64
	ReasoningTokens   *int64
	Source            UsageSource
}

type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonToolCalls FinishReason = "tool_calls"
	FinishReasonCancelled FinishReason = "cancelled"
	FinishReasonError     FinishReason = "error"
)

type NormalizedResponse struct {
	ID           string
	Model        string
	Parts        []ContentPart
	Usage        *Usage
	FinishReason FinishReason
}

type EventType string

const (
	EventResponseStart          EventType = "response_start"
	EventContentStart           EventType = "content_start"
	EventTextDelta              EventType = "text_delta"
	EventReasoningDelta         EventType = "reasoning_delta"
	EventToolCallStart          EventType = "tool_call_start"
	EventToolCallArgumentsDelta EventType = "tool_call_arguments_delta"
	EventContentEnd             EventType = "content_end"
	EventUsageUpdate            EventType = "usage_update"
	EventResponseEnd            EventType = "response_end"
	EventError                  EventType = "error"
)

// NormalizedEvent 是流式协议的显式联合，禁止通过松散 JSON 对象传递事件。
type NormalizedEvent interface {
	normalizedEvent()
	Type() EventType
}

type ResponseStartEvent struct {
	ResponseID string
	Model      string
}

func (ResponseStartEvent) normalizedEvent() {}
func (ResponseStartEvent) Type() EventType  { return EventResponseStart }

type ContentStartEvent struct {
	ContentID string
	Kind      PartKind
}

func (ContentStartEvent) normalizedEvent() {}
func (ContentStartEvent) Type() EventType  { return EventContentStart }

type TextDeltaEvent struct {
	ContentID string
	Text      string
}

func (TextDeltaEvent) normalizedEvent() {}
func (TextDeltaEvent) Type() EventType  { return EventTextDelta }

type ReasoningDeltaEvent struct {
	ContentID string
	Text      string
}

func (ReasoningDeltaEvent) normalizedEvent() {}
func (ReasoningDeltaEvent) Type() EventType  { return EventReasoningDelta }

type ToolCallStartEvent struct {
	ContentID string
	CallID    string
	Name      string
}

func (ToolCallStartEvent) normalizedEvent() {}
func (ToolCallStartEvent) Type() EventType  { return EventToolCallStart }

type ToolCallArgumentsDeltaEvent struct {
	ContentID string
	CallID    string
	Delta     string
}

func (ToolCallArgumentsDeltaEvent) normalizedEvent() {}
func (ToolCallArgumentsDeltaEvent) Type() EventType  { return EventToolCallArgumentsDelta }

type ContentEndEvent struct{ ContentID string }

func (ContentEndEvent) normalizedEvent() {}
func (ContentEndEvent) Type() EventType  { return EventContentEnd }

type UsageUpdateEvent struct{ Usage Usage }

func (UsageUpdateEvent) normalizedEvent() {}
func (UsageUpdateEvent) Type() EventType  { return EventUsageUpdate }

type ResponseEndEvent struct{ FinishReason FinishReason }

func (ResponseEndEvent) normalizedEvent() {}
func (ResponseEndEvent) Type() EventType  { return EventResponseEnd }

type ErrorEvent struct {
	Code    string
	Message string
}

func (ErrorEvent) normalizedEvent() {}
func (ErrorEvent) Type() EventType  { return EventError }

// StreamEmitter 由入口层实现。Adapter 必须等待 Emit 返回，避免慢客户端时无界缓冲。
type StreamEmitter interface {
	Emit(context.Context, NormalizedEvent) error
}

// RequiredCapabilities 从请求中提取，供 Router 在选择模型前进行明确能力校验。
func (request NormalizedRequest) RequiredCapabilities() provider.RequiredCapabilities {
	required := provider.RequiredCapabilities{
		Streaming:     request.Stream,
		Tools:         len(request.Tools) > 0,
		ParallelTools: request.ParallelToolCalls,
	}
	for _, message := range request.Messages {
		for _, part := range message.Parts {
			if part == nil {
				continue
			}
			switch part.Kind() {
			case PartKindReasoning:
				required.Reasoning = true
			case PartKindImage:
				required.Vision = true
			case PartKindToolCall, PartKindToolResult:
				required.Tools = true
			}
		}
	}
	return required
}
