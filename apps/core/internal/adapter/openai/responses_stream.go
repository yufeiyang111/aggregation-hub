package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/transport"
)

type responsesStreamEnvelope struct {
	Type        string `json:"type"`
	OutputIndex *int   `json:"output_index"`
	ItemID      string `json:"item_id"`
	Delta       string `json:"delta"`
	Arguments   string `json:"arguments"`
	Response    struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		Usage  *struct {
			InputTokens  *int64 `json:"input_tokens"`
			OutputTokens *int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"response"`
	Item struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type responsesStreamItemState struct {
	contentID string
	kind      normalize.PartKind
	callID    string
	name      string
	arguments strings.Builder
	argsDone  bool
}

func (value *Adapter) parseResponsesStream(ctx context.Context, route routing.RoutePlan, response *http.Response, emitter normalize.StreamEmitter) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return gatewayFromResponse(route.ProviderID, response)
	}
	defer response.Body.Close()

	reader := transport.NewSSEReader(response.Body, maxSSEEventBytes)
	validator := normalize.NewEventSequenceValidator()
	emit := func(event normalize.NormalizedEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validator.Validate(event); err != nil {
			return adapterError("upstream_stream_invalid", "上游 Responses 流式事件顺序无效", http.StatusBadGateway, true, route.ProviderID, err)
		}
		return emitter.Emit(ctx, event)
	}

	started := false
	terminal := false
	responseID := ""
	items := make(map[int]*responsesStreamItemState)
	for {
		event, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if terminal {
				return validator.Finalize()
			}
			return adapterError("upstream_stream_truncated", "上游 Responses 流式响应意外结束", http.StatusBadGateway, true, route.ProviderID, err)
		}
		if err != nil {
			return adapterError("upstream_stream_invalid", "上游 Responses 流式响应无效", http.StatusBadGateway, true, route.ProviderID, err)
		}
		if event.Data == "[DONE]" {
			continue
		}
		var payload responsesStreamEnvelope
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return adapterError("upstream_stream_invalid", "上游 Responses 流式事件格式无效", http.StatusBadGateway, true, route.ProviderID, err)
		}
		name := strings.TrimSpace(payload.Type)
		if name == "" {
			name = strings.TrimSpace(event.Event)
		} else if event.Event != "" && strings.TrimSpace(event.Event) != name {
			return adapterError("upstream_stream_invalid", "上游 Responses 事件名称不一致", http.StatusBadGateway, true, route.ProviderID, nil)
		}

		switch name {
		case "response.created":
			if started || !validModelID(payload.Response.ID) || !validModelID(payload.Response.Model) {
				return adapterError("upstream_stream_invalid", "上游 Responses 创建事件无效", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			if err := emit(normalize.ResponseStartEvent{ResponseID: payload.Response.ID, Model: payload.Response.Model}); err != nil {
				return err
			}
			started = true
			responseID = payload.Response.ID
		case "response.output_item.added":
			if !started || payload.OutputIndex == nil || *payload.OutputIndex < 0 {
				return adapterError("upstream_stream_invalid", "上游 Responses 输出项事件无效", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			if _, exists := items[*payload.OutputIndex]; exists {
				return adapterError("upstream_stream_invalid", "上游 Responses 输出项重复", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			contentID := payload.Item.ID
			if !validModelID(contentID) {
				contentID = fmt.Sprintf("%s_output_%d", responseID, *payload.OutputIndex)
			}
			switch payload.Item.Type {
			case "message":
				state := &responsesStreamItemState{contentID: contentID, kind: normalize.PartKindText}
				if err := emit(normalize.ContentStartEvent{ContentID: state.contentID, Kind: state.kind}); err != nil {
					return err
				}
				items[*payload.OutputIndex] = state
			case "function_call":
				if !validModelID(payload.Item.CallID) || !validModelID(payload.Item.Name) {
					return adapterError("upstream_stream_invalid", "上游 Responses Function Call 无效", http.StatusBadGateway, true, route.ProviderID, nil)
				}
				state := &responsesStreamItemState{contentID: contentID, kind: normalize.PartKindToolCall, callID: payload.Item.CallID, name: payload.Item.Name}
				if err := emit(normalize.ContentStartEvent{ContentID: state.contentID, Kind: state.kind}); err != nil {
					return err
				}
				if err := emit(normalize.ToolCallStartEvent{ContentID: state.contentID, CallID: state.callID, Name: state.name}); err != nil {
					return err
				}
				items[*payload.OutputIndex] = state
			case "reasoning":
				return adapterError("unsupported_feature", "上游 Responses Reasoning 尚未启用", http.StatusBadGateway, false, route.ProviderID, nil)
			default:
				return adapterError("unsupported_feature", "上游 Responses 返回了未支持的输出类型", http.StatusBadGateway, false, route.ProviderID, nil)
			}
		case "response.output_text.delta":
			state, err := responsesStreamState(items, payload.OutputIndex, normalize.PartKindText)
			if err != nil || payload.Delta == "" {
				return responsesStreamInvalid(route.ProviderID, err)
			}
			if err := emit(normalize.TextDeltaEvent{ContentID: state.contentID, Text: payload.Delta}); err != nil {
				return err
			}
		case "response.function_call_arguments.delta":
			state, err := responsesStreamState(items, payload.OutputIndex, normalize.PartKindToolCall)
			if err != nil || payload.Delta == "" {
				return responsesStreamInvalid(route.ProviderID, err)
			}
			state.arguments.WriteString(payload.Delta)
			if err := emit(normalize.ToolCallArgumentsDeltaEvent{ContentID: state.contentID, CallID: state.callID, Delta: payload.Delta}); err != nil {
				return err
			}
		case "response.function_call_arguments.done":
			state, err := responsesStreamState(items, payload.OutputIndex, normalize.PartKindToolCall)
			if err != nil {
				return responsesStreamInvalid(route.ProviderID, err)
			}
			if err := completeFunctionArguments(emit, state, payload.Arguments); err != nil {
				return adapterError("upstream_stream_invalid", "上游 Responses Function 参数无效", http.StatusBadGateway, true, route.ProviderID, err)
			}
		case "response.output_item.done":
			state, err := responsesStreamState(items, payload.OutputIndex, "")
			if err != nil {
				return responsesStreamInvalid(route.ProviderID, err)
			}
			if payload.Item.Type != "" && payload.Item.Type != responsesStreamItemType(state.kind) {
				return adapterError("upstream_stream_invalid", "上游 Responses 输出项类型不一致", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			if state.kind == normalize.PartKindToolCall {
				if err := completeFunctionArguments(emit, state, payload.Item.Arguments); err != nil {
					return adapterError("upstream_stream_invalid", "上游 Responses Function 参数无效", http.StatusBadGateway, true, route.ProviderID, err)
				}
			}
			if err := emit(normalize.ContentEndEvent{ContentID: state.contentID}); err != nil {
				return err
			}
			delete(items, *payload.OutputIndex)
		case "response.completed", "response.incomplete", "response.failed":
			if !started || len(items) != 0 {
				return adapterError("upstream_stream_invalid", "上游 Responses 终态事件无效", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			if usage := responsesStreamUsage(payload.Response.Usage); usage != nil {
				if err := emit(normalize.UsageUpdateEvent{Usage: *usage}); err != nil {
					return err
				}
			}
			finish := normalize.FinishReasonStop
			if name == "response.incomplete" {
				finish = normalize.FinishReasonLength
			} else if name == "response.failed" {
				finish = normalize.FinishReasonError
			}
			if err := emit(normalize.ResponseEndEvent{FinishReason: finish}); err != nil {
				return err
			}
			terminal = true
		case "error":
			if !started {
				return adapterError("upstream_stream_invalid", "上游 Responses 错误事件缺少响应起点", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			code := strings.TrimSpace(payload.Error.Code)
			if !validModelID(code) {
				code = "upstream_error"
			}
			if err := emit(normalize.ErrorEvent{Code: code, Message: "上游 Responses 流式请求失败"}); err != nil {
				return err
			}
			terminal = true
		case "response.reasoning_summary_text.delta", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
			return adapterError("unsupported_feature", "上游 Responses Reasoning 尚未启用", http.StatusBadGateway, false, route.ProviderID, nil)
		default:
			// 官方允许新事件以向后兼容方式加入；未知事件不改变已知状态机。
			continue
		}
	}
}

func responsesStreamState(items map[int]*responsesStreamItemState, index *int, kind normalize.PartKind) (*responsesStreamItemState, error) {
	if index == nil || *index < 0 {
		return nil, errors.New("输出索引无效")
	}
	state, exists := items[*index]
	if !exists || (kind != "" && state.kind != kind) {
		return nil, errors.New("输出项状态无效")
	}
	return state, nil
}

func responsesStreamInvalid(providerID string, cause error) error {
	return adapterError("upstream_stream_invalid", "上游 Responses 流式事件无效", http.StatusBadGateway, true, providerID, cause)
}

func completeFunctionArguments(emit func(normalize.NormalizedEvent) error, state *responsesStreamItemState, completed string) error {
	arguments := state.arguments.String()
	if completed != "" {
		if arguments == "" {
			state.arguments.WriteString(completed)
			arguments = completed
			if err := emit(normalize.ToolCallArgumentsDeltaEvent{ContentID: state.contentID, CallID: state.callID, Delta: completed}); err != nil {
				return err
			}
		} else if arguments != completed {
			return errors.New("参数快照与增量不一致")
		}
	}
	if !json.Valid([]byte(arguments)) {
		return errors.New("参数不是 JSON 对象")
	}
	state.argsDone = true
	return nil
}

func responsesStreamUsage(value *struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
}) *normalize.Usage {
	if value == nil || (value.InputTokens == nil && value.OutputTokens == nil) {
		return nil
	}
	return &normalize.Usage{InputTokens: value.InputTokens, OutputTokens: value.OutputTokens, Source: normalize.UsageSourceUpstreamReported}
}

func responsesStreamItemType(kind normalize.PartKind) string {
	switch kind {
	case normalize.PartKindText:
		return "message"
	case normalize.PartKindToolCall:
		return "function_call"
	default:
		return string(kind)
	}
}
