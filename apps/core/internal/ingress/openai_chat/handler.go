package openai_chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

const maxRequestBytes int64 = 1024 * 1024

var ErrInvalidGateway = errors.New("Chat Gateway 依赖无效")

type gateway interface {
	Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error)
	Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error
}
type Handler struct {
	gateway  gateway
	recorder observability.RequestRecorder
}

func NewHandler(value gateway, recorders ...observability.RequestRecorder) (*Handler, error) {
	if value == nil || len(recorders) > 1 {
		return nil, ErrInvalidGateway
	}
	recorder := observability.NewNoopRecorder()
	if len(recorders) == 1 {
		if recorders[0] == nil {
			return nil, ErrInvalidGateway
		}
		recorder = recorders[0]
	}
	return &Handler{gateway: value, recorder: recorder}, nil
}

func (value *Handler) startObservation(request *http.Request, input normalize.NormalizedRequest) observability.RequestLifecycle {
	lifecycle, err := value.recorder.Start(request.Context(), observability.RequestStart{SourceProtocol: observability.ProtocolOpenAIChat, Endpoint: "/v1/chat/completions", PublicModelSnapshot: input.Model, Streaming: input.Stream})
	if err != nil {
		observability.ReportPersistenceError(err)
		return observability.NoopRequestLifecycle()
	}
	return lifecycle
}
func (value *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 POST")
		return
	}
	input, err := decodeRequest(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	normalized, err := normalizeInput(input)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "请求字段无效")
		return
	}
	lifecycle := value.startObservation(request, normalized)
	if normalized.Stream {
		observability.ReportPersistenceError(lifecycle.MarkStreaming(request.Context()))
		value.serveStream(writer, request, normalized, lifecycle)
		return
	}
	result, err := value.gateway.Complete(request.Context(), normalized)
	if err != nil {
		observability.FinishWithError(lifecycle, request.Context(), err)
		writeGatewayError(writer, err)
		return
	}
	response := renderResponse(result)
	observability.ReportPersistenceError(lifecycle.Complete(request.Context(), observability.Completion{HTTPStatus: http.StatusOK, Usage: result.Usage}))
	writeJSON(writer, http.StatusOK, response)
}

type requestDTO struct {
	Model             string          `json:"model"`
	Messages          []messageDTO    `json:"messages"`
	Stream            bool            `json:"stream,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	MaxTokens         *int64          `json:"max_tokens,omitempty"`
	Stop              []string        `json:"stop,omitempty"`
	Tools             []toolDTO       `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls bool            `json:"parallel_tool_calls,omitempty"`
}
type messageDTO struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCallDTO   `json:"tool_calls,omitempty"`
}
type toolDTO struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
type toolCallDTO struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func decodeRequest(request *http.Request) (requestDTO, error) {
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	var value requestDTO
	if err := decoder.Decode(&value); err != nil {
		return requestDTO{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return requestDTO{}, errors.New("请求包含多余 JSON")
	}
	return value, nil
}
func normalizeInput(value requestDTO) (normalize.NormalizedRequest, error) {
	result := normalize.NormalizedRequest{Model: value.Model, Stream: value.Stream, Temperature: value.Temperature, MaxOutputTokens: value.MaxTokens, StopSequences: value.Stop, ParallelToolCalls: value.ParallelToolCalls}
	for _, message := range value.Messages {
		content, err := stringContent(message.Content)
		if err != nil {
			return normalize.NormalizedRequest{}, err
		}
		switch message.Role {
		case "system":
			result.System = append(result.System, normalize.TextPart{Text: content})
		case "user", "assistant":
			mapped := normalize.Message{Role: normalize.Role(message.Role)}
			if content != "" {
				mapped.Parts = append(mapped.Parts, normalize.TextPart{Text: content})
			}
			for _, call := range message.ToolCalls {
				if call.Type != "function" {
					return normalize.NormalizedRequest{}, errors.New("Tool 类型无效")
				}
				mapped.Parts = append(mapped.Parts, normalize.ToolCallPart{CallID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
			}
			result.Messages = append(result.Messages, mapped)
		case "tool":
			result.Messages = append(result.Messages, normalize.Message{Role: normalize.RoleTool, Parts: []normalize.ContentPart{normalize.ToolResultPart{CallID: message.ToolCallID, Content: content}}})
		default:
			return normalize.NormalizedRequest{}, errors.New("角色无效")
		}
	}
	for _, tool := range value.Tools {
		if tool.Type != "function" {
			return normalize.NormalizedRequest{}, errors.New("Tool 类型无效")
		}
		result.Tools = append(result.Tools, normalize.ToolDefinition{Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: tool.Function.Parameters})
	}
	if len(value.ToolChoice) > 0 {
		choice, err := parseToolChoice(value.ToolChoice)
		if err != nil {
			return normalize.NormalizedRequest{}, err
		}
		result.ToolChoice = choice
	}
	return result, nil
}
func stringContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}
func parseToolChoice(raw json.RawMessage) (normalize.ToolChoice, error) {
	var literal string
	if json.Unmarshal(raw, &literal) == nil {
		switch literal {
		case "auto":
			return normalize.ToolChoice{Mode: normalize.ToolChoiceAuto}, nil
		case "none":
			return normalize.ToolChoice{Mode: normalize.ToolChoiceNone}, nil
		case "required":
			return normalize.ToolChoice{Mode: normalize.ToolChoiceRequired}, nil
		}
	}
	var value struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || value.Type != "function" || value.Function.Name == "" {
		return normalize.ToolChoice{}, errors.New("Tool Choice 无效")
	}
	return normalize.ToolChoice{Mode: normalize.ToolChoiceNamed, Name: value.Function.Name}, nil
}
func renderResponse(value normalize.NormalizedResponse) any {
	message := struct {
		Role      string        `json:"role"`
		Content   string        `json:"content"`
		ToolCalls []toolCallDTO `json:"tool_calls,omitempty"`
	}{Role: "assistant"}
	for _, part := range value.Parts {
		switch typed := part.(type) {
		case normalize.TextPart:
			message.Content += typed.Text
		case normalize.ToolCallPart:
			message.ToolCalls = append(message.ToolCalls, toolCallDTO{ID: typed.CallID, Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: typed.Name, Arguments: typed.Arguments}})
		}
	}
	return struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			Message      any    `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}{ID: value.ID, Object: "chat.completion", Created: 0, Model: value.Model, Choices: []struct {
		Index        int    `json:"index"`
		Message      any    `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{{Index: 0, Message: message, FinishReason: string(value.FinishReason)}}}
}
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": strings.TrimSpace(message)}})
}
func writeGatewayError(writer http.ResponseWriter, err error) {
	writeError(writer, http.StatusBadGateway, "gateway_error", "请求上游服务失败")
}
