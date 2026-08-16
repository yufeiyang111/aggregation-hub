package openai_chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

type sseEmitter struct {
	writer http.ResponseWriter
	model  string
	id     string
	index  map[string]int
	next   int
}

func (value *Handler) serveStream(writer http.ResponseWriter, request *http.Request, input normalize.NormalizedRequest, lifecycle observability.RequestLifecycle) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	emitter := &sseEmitter{writer: writer, index: make(map[string]int)}
	recordedEmitter := observability.NewRecordingStreamEmitter(emitter, lifecycle)
	if err := value.gateway.Stream(request.Context(), input, recordedEmitter); err != nil {
		recordedEmitter.Finish(request.Context(), err)
		_ = emitter.write(map[string]any{"error": map[string]string{"code": "gateway_error", "message": "上游流式请求失败"}})
		return
	}
	recordedEmitter.Finish(request.Context(), nil)
	_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
}
func (value *sseEmitter) Emit(_ context.Context, event normalize.NormalizedEvent) error {
	switch typed := event.(type) {
	case normalize.ResponseStartEvent:
		value.id = typed.ResponseID
		value.model = typed.Model
		return value.write(chunk(value.id, value.model, map[string]any{"role": "assistant"}, ""))
	case normalize.TextDeltaEvent:
		return value.write(chunk(value.id, value.model, map[string]any{"content": typed.Text}, ""))
	case normalize.ToolCallStartEvent:
		index := value.toolIndex(typed.ContentID)
		return value.write(chunk(value.id, value.model, map[string]any{"tool_calls": []any{map[string]any{"index": index, "id": typed.CallID, "type": "function", "function": map[string]string{"name": typed.Name, "arguments": ""}}}}, ""))
	case normalize.ToolCallArgumentsDeltaEvent:
		index := value.toolIndex(typed.ContentID)
		return value.write(chunk(value.id, value.model, map[string]any{"tool_calls": []any{map[string]any{"index": index, "function": map[string]string{"arguments": typed.Delta}}}}, ""))
	case normalize.ResponseEndEvent:
		return value.write(chunk(value.id, value.model, map[string]any{}, string(typed.FinishReason)))
	case normalize.ErrorEvent:
		return value.write(map[string]any{"error": map[string]string{"code": typed.Code, "message": typed.Message}})
	}
	return nil
}
func (value *sseEmitter) toolIndex(id string) int {
	if index, ok := value.index[id]; ok {
		return index
	}
	index := value.next
	value.next++
	value.index[id] = index
	return index
}
func (value *sseEmitter) write(payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(value.writer, "data: %s\n\n", encoded); err != nil {
		return err
	}
	if flusher, ok := value.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
func chunk(id, model string, delta any, reason string) any {
	return map[string]any{"id": id, "object": "chat.completion.chunk", "created": 0, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nilOrReason(reason)}}}
}
func nilOrReason(value string) any {
	if value == "" {
		return nil
	}
	return value
}
