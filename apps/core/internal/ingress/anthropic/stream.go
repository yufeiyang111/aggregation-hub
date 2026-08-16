package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

// sseEmitter 将已校验的归一化事件转换为 Anthropic Messages SSE；每次 Emit 同步写入，避免慢客户端产生无界缓冲。
type sseEmitter struct {
	writer  http.ResponseWriter
	id      string
	model   string
	indexes map[string]int
	next    int
}

func (value *Handler) serveStream(writer http.ResponseWriter, request *http.Request, input normalize.NormalizedRequest, lifecycle observability.RequestLifecycle) {
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)

	emitter := &sseEmitter{writer: writer, indexes: make(map[string]int)}
	recordedEmitter := observability.NewRecordingStreamEmitter(emitter, lifecycle)
	if err := value.gateway.Stream(request.Context(), input, recordedEmitter); err != nil {
		recordedEmitter.Finish(request.Context(), err)
		_ = emitter.writeError("api_error", "上游流式请求失败")
		return
	}
	recordedEmitter.Finish(request.Context(), nil)
}

func (value *sseEmitter) Emit(_ context.Context, event normalize.NormalizedEvent) error {
	switch typed := event.(type) {
	case normalize.ResponseStartEvent:
		value.id = typed.ResponseID
		value.model = typed.Model
		return value.write("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            typed.ResponseID,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         typed.Model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]int64{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		})
	case normalize.ContentStartEvent:
		index := value.contentIndex(typed.ContentID)
		switch typed.Kind {
		case normalize.PartKindText:
			return value.write("content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]string{"type": "text", "text": ""},
			})
		case normalize.PartKindToolCall:
			return nil
		default:
			return errors.New("当前 Anthropic SSE 不支持该内容块类型")
		}
	case normalize.TextDeltaEvent:
		return value.write("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": value.contentIndex(typed.ContentID),
			"delta": map[string]string{"type": "text_delta", "text": typed.Text},
		})
	case normalize.ToolCallStartEvent:
		return value.write("content_block_start", map[string]any{
			"type": "content_block_start", "index": value.contentIndex(typed.ContentID),
			"content_block": map[string]any{"type": "tool_use", "id": typed.CallID, "name": typed.Name, "input": map[string]any{}},
		})
	case normalize.ToolCallArgumentsDeltaEvent:
		return value.write("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": value.contentIndex(typed.ContentID),
			"delta": map[string]string{"type": "input_json_delta", "partial_json": typed.Delta},
		})
	case normalize.ContentEndEvent:
		return value.write("content_block_stop", map[string]any{"type": "content_block_stop", "index": value.contentIndex(typed.ContentID)})
	case normalize.UsageUpdateEvent:
		return value.writeUsage(typed.Usage)
	case normalize.ResponseEndEvent:
		if err := value.write("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": anthropicStopReason(typed.FinishReason), "stop_sequence": nil},
			"usage": usagePayload(nil),
		}); err != nil {
			return err
		}
		return value.write("message_stop", map[string]string{"type": "message_stop"})
	case normalize.ErrorEvent:
		return value.writeError("api_error", typed.Message)
	default:
		return errors.New("当前 Anthropic SSE 不支持该事件类型")
	}
}

func (value *sseEmitter) writeUsage(usage normalize.Usage) error {
	return value.write("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": nil, "stop_sequence": nil},
		"usage": usagePayload(&usage),
	})
}

func (value *sseEmitter) writeError(errorType, message string) error {
	return value.write("error", map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errorType, "message": message},
	})
}

func (value *sseEmitter) contentIndex(id string) int {
	if index, ok := value.indexes[id]; ok {
		return index
	}
	index := value.next
	value.next++
	value.indexes[id] = index
	return index
}

func (value *sseEmitter) write(event string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(value.writer, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
		return err
	}
	if flusher, ok := value.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func anthropicStopReason(value normalize.FinishReason) string {
	switch value {
	case normalize.FinishReasonStop:
		return "end_turn"
	case normalize.FinishReasonLength:
		return "max_tokens"
	case normalize.FinishReasonToolCalls:
		return "tool_use"
	default:
		return "error"
	}
}

func usagePayload(usage *normalize.Usage) map[string]int64 {
	result := map[string]int64{"output_tokens": 0}
	if usage == nil {
		return result
	}
	if usage.InputTokens != nil {
		result["input_tokens"] = *usage.InputTokens
	}
	if usage.OutputTokens != nil {
		result["output_tokens"] = *usage.OutputTokens
	}
	return result
}
