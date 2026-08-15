package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/transport"
)

const maxStreamEventBytes = 256 * 1024

type streamMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens *int64 `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type streamContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

type streamContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
	} `json:"delta"`
}

type streamMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	} `json:"usage"`
}

type streamErrorEvent struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type streamBlockState struct {
	contentID string
	kind      normalize.PartKind
	callID    string
	name      string
	closed    bool
}

func parseAnthropicStream(ctx context.Context, route routing.RoutePlan, response *http.Response, emitter normalize.StreamEmitter) error {
	if ctx == nil || response == nil || emitter == nil {
		return adapterError("invalid_request", "流式请求无效", http.StatusBadRequest, false, route.ProviderID, nil)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return gatewayFromResponse(route.ProviderID, response)
	}
	defer response.Body.Close()

	reader := transport.NewSSEReader(response.Body, maxStreamEventBytes)
	validator := normalize.NewEventSequenceValidator()
	blocks := make(map[int]streamBlockState)
	started := false
	nextBlockIndex := 0
	var inputTokens *int64
	messageDeltaSeen := false
	messageStopSeen := false

	emit := func(event normalize.NormalizedEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validator.Validate(event); err != nil {
			return adapterError("upstream_stream_invalid", "上游流式事件顺序无效", http.StatusBadGateway, false, route.ProviderID, err)
		}
		return emitter.Emit(ctx, event)
	}

	for {
		event, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if messageStopSeen && messageDeltaSeen {
				return validator.Finalize()
			}
			return adapterError("upstream_stream_truncated", "上游流式响应意外结束", http.StatusBadGateway, true, route.ProviderID, err)
		}
		if err != nil {
			return adapterError("upstream_stream_invalid", "上游流式响应格式无效", http.StatusBadGateway, true, route.ProviderID, err)
		}
		if event.Data == "" {
			continue
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(event.Data), &envelope); err != nil {
			return adapterError("upstream_stream_invalid", "上游流式事件格式无效", http.StatusBadGateway, true, route.ProviderID, err)
		}
		switch envelope.Type {
		case "ping":
			continue
		case "error":
			var payload streamErrorEvent
			if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
				return adapterError("upstream_stream_invalid", "上游错误事件格式无效", http.StatusBadGateway, false, route.ProviderID, err)
			}
			return adapterError("upstream_error", "上游流式请求失败", http.StatusBadGateway, payload.Error.Type == "overloaded_error", route.ProviderID, nil)
		case "message_start":
			if started {
				return adapterError("upstream_stream_invalid", "上游重复发送 message_start", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			var payload streamMessageStart
			if err := json.Unmarshal([]byte(event.Data), &payload); err != nil || payload.Message.ID == "" || payload.Message.Model == "" {
				return adapterError("upstream_stream_invalid", "上游 message_start 无效", http.StatusBadGateway, true, route.ProviderID, err)
			}
			if err := emit(normalize.ResponseStartEvent{ResponseID: payload.Message.ID, Model: payload.Message.Model}); err != nil {
				return err
			}
			inputTokens = payload.Message.Usage.InputTokens
			started = true
		case "content_block_start":
			if !started || messageDeltaSeen {
				return adapterError("upstream_stream_invalid", "上游内容块顺序无效", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			var payload streamContentBlockStart
			if err := json.Unmarshal([]byte(event.Data), &payload); err != nil || payload.Index < 0 {
				return adapterError("upstream_stream_invalid", "上游 content_block_start 无效", http.StatusBadGateway, true, route.ProviderID, err)
			}
			if payload.Index != nextBlockIndex {
				return adapterError("upstream_stream_invalid", "上游内容块索引未按顺序递增", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			nextBlockIndex++
			contentID := "content_" + strconv.Itoa(payload.Index)
			if _, exists := blocks[payload.Index]; exists {
				return adapterError("upstream_stream_invalid", "上游内容块索引重复", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			var kind normalize.PartKind
			switch payload.ContentBlock.Type {
			case "text":
				kind = normalize.PartKindText
			case "tool_use":
				if payload.ContentBlock.ID == "" || payload.ContentBlock.Name == "" {
					return adapterError("upstream_stream_invalid", "上游 Tool Call 标识无效", http.StatusBadGateway, true, route.ProviderID, nil)
				}
				kind = normalize.PartKindToolCall
			case "thinking":
				return adapterError("unsupported_feature", "上游返回了当前未启用的 Thinking 内容", http.StatusBadGateway, false, route.ProviderID, nil)
			default:
				return adapterError("unsupported_feature", "上游返回了当前未支持的内容块", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			state := streamBlockState{contentID: contentID, kind: kind, callID: payload.ContentBlock.ID, name: payload.ContentBlock.Name}
			blocks[payload.Index] = state
			if err := emit(normalize.ContentStartEvent{ContentID: contentID, Kind: kind}); err != nil {
				return err
			}
			if kind == normalize.PartKindToolCall {
				if err := emit(normalize.ToolCallStartEvent{ContentID: contentID, CallID: state.callID, Name: state.name}); err != nil {
					return err
				}
			}
		case "content_block_delta":
			if !started || messageDeltaSeen {
				return adapterError("upstream_stream_invalid", "上游内容增量顺序无效", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			var payload streamContentBlockDelta
			if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
				return adapterError("upstream_stream_invalid", "上游 content_block_delta 无效", http.StatusBadGateway, true, route.ProviderID, err)
			}
			state, exists := blocks[payload.Index]
			if !exists || state.closed {
				return adapterError("upstream_stream_invalid", "上游内容增量缺少开放内容块", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			switch payload.Delta.Type {
			case "text_delta":
				if state.kind != normalize.PartKindText || payload.Delta.Text == "" {
					return adapterError("upstream_stream_invalid", "上游文本增量无效", http.StatusBadGateway, false, route.ProviderID, nil)
				}
				if err := emit(normalize.TextDeltaEvent{ContentID: state.contentID, Text: payload.Delta.Text}); err != nil {
					return err
				}
			case "input_json_delta":
				if state.kind != normalize.PartKindToolCall {
					return adapterError("upstream_stream_invalid", "上游 Tool 参数增量无效", http.StatusBadGateway, false, route.ProviderID, nil)
				}
				if err := emit(normalize.ToolCallArgumentsDeltaEvent{ContentID: state.contentID, CallID: state.callID, Delta: payload.Delta.PartialJSON}); err != nil {
					return err
				}
			case "thinking_delta", "signature_delta":
				return adapterError("unsupported_feature", "上游返回了当前未启用的 Thinking 增量", http.StatusBadGateway, false, route.ProviderID, nil)
			default:
				return adapterError("unsupported_feature", "上游返回了当前未支持的内容增量", http.StatusBadGateway, false, route.ProviderID, nil)
			}
		case "content_block_stop":
			if !started || messageDeltaSeen {
				return adapterError("upstream_stream_invalid", "上游内容块结束顺序无效", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			var payload struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
			}
			if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
				return adapterError("upstream_stream_invalid", "上游 content_block_stop 无效", http.StatusBadGateway, true, route.ProviderID, err)
			}
			state, exists := blocks[payload.Index]
			if !exists || state.closed {
				return adapterError("upstream_stream_invalid", "上游内容块结束无效", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			state.closed = true
			blocks[payload.Index] = state
			if err := emit(normalize.ContentEndEvent{ContentID: state.contentID}); err != nil {
				return err
			}
		case "message_delta":
			if !started || messageDeltaSeen {
				return adapterError("upstream_stream_invalid", "上游 message_delta 顺序无效", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			var payload streamMessageDelta
			if err := json.Unmarshal([]byte(event.Data), &payload); err != nil || payload.Delta.StopReason == "" {
				return adapterError("upstream_stream_invalid", "上游 message_delta 无效", http.StatusBadGateway, true, route.ProviderID, err)
			}
			for _, state := range blocks {
				if !state.closed {
					return adapterError("upstream_stream_invalid", "上游仍有未关闭内容块", http.StatusBadGateway, false, route.ProviderID, nil)
				}
			}
			reportedInputTokens := payload.Usage.InputTokens
			if reportedInputTokens == nil {
				reportedInputTokens = inputTokens
			}
			if reportedInputTokens != nil || payload.Usage.OutputTokens != nil {
				usage := normalize.Usage{InputTokens: reportedInputTokens, OutputTokens: payload.Usage.OutputTokens, Source: normalize.UsageSourceUpstreamReported}
				if err := emit(normalize.UsageUpdateEvent{Usage: usage}); err != nil {
					return err
				}
			}
			if err := emit(normalize.ResponseEndEvent{FinishReason: mapStreamFinish(payload.Delta.StopReason)}); err != nil {
				return err
			}
			messageDeltaSeen = true
		case "message_stop":
			if !messageDeltaSeen {
				return adapterError("upstream_stream_invalid", "上游缺少 message_delta 终态", http.StatusBadGateway, false, route.ProviderID, nil)
			}
			messageStopSeen = true
		default:
			// 顶层未知事件按协议前向兼容处理；已知但未支持的能力仍在具体分支返回结构化错误。
			continue
		}
		if messageStopSeen {
			return validator.Finalize()
		}
	}
}

func mapStreamFinish(value string) normalize.FinishReason {
	switch value {
	case "end_turn":
		return normalize.FinishReasonStop
	case "max_tokens":
		return normalize.FinishReasonLength
	case "tool_use":
		return normalize.FinishReasonToolCalls
	case "stop_sequence":
		return normalize.FinishReasonStop
	default:
		return normalize.FinishReasonError
	}
}
