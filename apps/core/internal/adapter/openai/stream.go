package openai

import (
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/transport"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

const maxSSEEventBytes = 256 * 1024

type streamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
	} `json:"usage"`
}

type streamToolState struct {
	contentID string
	callID    string
	name      string
}

func (value *Adapter) ParseStream(ctx context.Context, route routing.RoutePlan, response *http.Response, emitter normalize.StreamEmitter) error {
	if ctx == nil || response == nil || emitter == nil {
		return adapterError("invalid_request", "流式请求无效", http.StatusBadRequest, false, route.ProviderID, nil)
	}
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
			return err
		}
		return emitter.Emit(ctx, event)
	}
	started, terminal, textOpen := false, false, false
	tools := make(map[int]streamToolState)
	for {
		event, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if terminal {
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
		if event.Data == "[DONE]" {
			if terminal {
				return validator.Finalize()
			}
			return adapterError("upstream_stream_truncated", "上游流式响应缺少终态", http.StatusBadGateway, true, route.ProviderID, nil)
		}
		if terminal {
			return adapterError("upstream_stream_invalid", "上游在终态后继续发送数据", http.StatusBadGateway, false, route.ProviderID, nil)
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil || chunk.ID == "" || chunk.Model == "" || len(chunk.Choices) == 0 {
			return adapterError("upstream_stream_invalid", "上游流式事件格式无效", http.StatusBadGateway, true, route.ProviderID, err)
		}
		if !started {
			if err := emit(normalize.ResponseStartEvent{ResponseID: chunk.ID, Model: chunk.Model}); err != nil {
				return err
			}
			started = true
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			if !textOpen {
				if err := emit(normalize.ContentStartEvent{ContentID: "text_0", Kind: normalize.PartKindText}); err != nil {
					return err
				}
				textOpen = true
			}
			if err := emit(normalize.TextDeltaEvent{ContentID: "text_0", Text: choice.Delta.Content}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			state, exists := tools[call.Index]
			if !exists {
				if call.ID == "" || call.Function.Name == "" {
					return adapterError("upstream_stream_invalid", "上游 Tool Call 缺少标识", http.StatusBadGateway, true, route.ProviderID, nil)
				}
				state = streamToolState{contentID: "tool_" + strconv.Itoa(call.Index), callID: call.ID, name: call.Function.Name}
				tools[call.Index] = state
				if err := emit(normalize.ContentStartEvent{ContentID: state.contentID, Kind: normalize.PartKindToolCall}); err != nil {
					return err
				}
				if err := emit(normalize.ToolCallStartEvent{ContentID: state.contentID, CallID: state.callID, Name: state.name}); err != nil {
					return err
				}
			}
			if call.Function.Arguments != "" {
				if err := emit(normalize.ToolCallArgumentsDeltaEvent{ContentID: state.contentID, CallID: state.callID, Delta: call.Function.Arguments}); err != nil {
					return err
				}
			}
		}
		if usage := normalizeUsage(chunk.Usage); usage != nil {
			if err := emit(normalize.UsageUpdateEvent{Usage: *usage}); err != nil {
				return err
			}
		}
		if choice.FinishReason != nil {
			if textOpen {
				if err := emit(normalize.ContentEndEvent{ContentID: "text_0"}); err != nil {
					return err
				}
				textOpen = false
			}
			for _, state := range tools {
				if err := emit(normalize.ContentEndEvent{ContentID: state.contentID}); err != nil {
					return err
				}
			}
			if err := emit(normalize.ResponseEndEvent{FinishReason: mapFinishReason(*choice.FinishReason)}); err != nil {
				return err
			}
			terminal = true
		}
	}
}
