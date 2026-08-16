package openai_responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"aggregationhub.local/core/internal/normalize"
)

type responsesSSEEmitter struct {
	writer   http.ResponseWriter
	id       string
	model    string
	usage    *normalize.Usage
	contents map[string]*responsesOutputState
	next     int
	terminal bool
}

type responsesOutputState struct {
	index     int
	kind      normalize.PartKind
	callID    string
	name      string
	text      strings.Builder
	arguments strings.Builder
	started   bool
	ended     bool
}

func (value *Handler) serveStream(writer http.ResponseWriter, request *http.Request, input normalize.NormalizedRequest) {
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)

	emitter := &responsesSSEEmitter{writer: writer, contents: make(map[string]*responsesOutputState)}
	if err := value.gateway.Stream(request.Context(), input, emitter); err != nil && !emitter.terminal {
		_ = emitter.writeError("gateway_error", "上游流式请求失败")
	}
}

func (value *responsesSSEEmitter) Emit(_ context.Context, event normalize.NormalizedEvent) error {
	switch typed := event.(type) {
	case normalize.ResponseStartEvent:
		value.id = typed.ResponseID
		value.model = typed.Model
		return value.write("response.created", map[string]any{"type": "response.created", "response": value.responsePayload("in_progress")})
	case normalize.ContentStartEvent:
		if _, exists := value.contents[typed.ContentID]; exists {
			return errors.New("Responses 输出项重复开始")
		}
		state := &responsesOutputState{index: value.next, kind: typed.Kind, started: true}
		value.next++
		value.contents[typed.ContentID] = state
		if typed.Kind == normalize.PartKindText {
			return value.writeOutputItemAdded(typed.ContentID, state)
		}
		if typed.Kind != normalize.PartKindToolCall {
			return errors.New("当前 Responses SSE 不支持该内容块类型")
		}
		return nil
	case normalize.TextDeltaEvent:
		state, err := value.content(typed.ContentID, normalize.PartKindText)
		if err != nil {
			return err
		}
		state.text.WriteString(typed.Text)
		return value.write("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": state.index, "content_index": 0, "delta": typed.Text})
	case normalize.ToolCallStartEvent:
		state, err := value.content(typed.ContentID, normalize.PartKindToolCall)
		if err != nil {
			return err
		}
		state.callID, state.name = typed.CallID, typed.Name
		return value.writeOutputItemAdded(typed.ContentID, state)
	case normalize.ToolCallArgumentsDeltaEvent:
		state, err := value.content(typed.ContentID, normalize.PartKindToolCall)
		if err != nil {
			return err
		}
		state.arguments.WriteString(typed.Delta)
		return value.write("response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "output_index": state.index, "item_id": typed.ContentID, "delta": typed.Delta})
	case normalize.ContentEndEvent:
		state, err := value.content(typed.ContentID, "")
		if err != nil {
			return err
		}
		if state.kind == normalize.PartKindToolCall && !json.Valid([]byte(state.arguments.String())) {
			return errors.New("Responses Function 参数不是 JSON")
		}
		state.ended = true
		return value.write("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": state.index, "item": value.outputItemPayload(typed.ContentID, state, "completed")})
	case normalize.UsageUpdateEvent:
		usage := typed.Usage
		value.usage = &usage
		return nil
	case normalize.ResponseEndEvent:
		status, event := responsesTerminal(typed.FinishReason)
		if err := value.write(event, map[string]any{"type": event, "response": value.responsePayload(status)}); err != nil {
			return err
		}
		value.terminal = true
		return nil
	case normalize.ErrorEvent:
		if err := value.writeError(typed.Code, typed.Message); err != nil {
			return err
		}
		value.terminal = true
		return nil
	default:
		return errors.New("当前 Responses SSE 不支持该事件类型")
	}
}

func (value *responsesSSEEmitter) content(id string, kind normalize.PartKind) (*responsesOutputState, error) {
	state, exists := value.contents[id]
	if !exists || state.ended || (kind != "" && state.kind != kind) {
		return nil, errors.New("Responses 输出项状态无效")
	}
	return state, nil
}

func (value *responsesSSEEmitter) writeOutputItemAdded(id string, state *responsesOutputState) error {
	if state.kind == normalize.PartKindToolCall && (state.callID == "" || state.name == "") {
		return errors.New("Responses Function Call 缺少标识")
	}
	return value.write("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": state.index, "item": value.outputItemPayload(id, state, "in_progress")})
}

func (value *responsesSSEEmitter) outputItemPayload(id string, state *responsesOutputState, status string) any {
	switch state.kind {
	case normalize.PartKindText:
		return map[string]any{"id": id, "type": "message", "role": "assistant", "status": status, "content": []map[string]string{{"type": "output_text", "text": state.text.String()}}}
	case normalize.PartKindToolCall:
		return map[string]any{"id": id, "type": "function_call", "status": status, "call_id": state.callID, "name": state.name, "arguments": state.arguments.String()}
	default:
		return map[string]any{"id": id, "type": string(state.kind), "status": status}
	}
}

func (value *responsesSSEEmitter) responsePayload(status string) any {
	result := map[string]any{"id": value.id, "object": "response", "created_at": 0, "status": status, "model": value.model, "output": value.outputItems()}
	if value.usage != nil {
		result["usage"] = map[string]any{"input_tokens": value.usage.InputTokens, "output_tokens": value.usage.OutputTokens}
	}
	return result
}

func (value *responsesSSEEmitter) outputItems() []any {
	states := make([]struct {
		id    string
		state *responsesOutputState
	}, 0, len(value.contents))
	for id, state := range value.contents {
		states = append(states, struct {
			id    string
			state *responsesOutputState
		}{id: id, state: state})
	}
	sort.Slice(states, func(left, right int) bool { return states[left].state.index < states[right].state.index })
	output := make([]any, 0, len(states))
	for _, item := range states {
		output = append(output, value.outputItemPayload(item.id, item.state, "completed"))
	}
	return output
}

func (value *responsesSSEEmitter) writeError(code, message string) error {
	return value.write("error", map[string]any{"type": "error", "error": map[string]string{"code": strings.TrimSpace(code), "message": strings.TrimSpace(message)}})
}

func (value *responsesSSEEmitter) write(event string, payload any) error {
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

func responsesTerminal(reason normalize.FinishReason) (string, string) {
	switch reason {
	case normalize.FinishReasonStop, normalize.FinishReasonToolCalls:
		return "completed", "response.completed"
	case normalize.FinishReasonLength:
		return "incomplete", "response.incomplete"
	default:
		return "failed", "response.failed"
	}
}
