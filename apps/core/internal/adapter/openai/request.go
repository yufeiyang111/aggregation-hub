package openai

import (
	"encoding/json"
	"errors"

	"aggregationhub.local/core/internal/normalize"
)

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int64        `json:"max_tokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Tools       []chatTool    `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
}
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}
type chatTool struct {
	Type     string                 `json:"type"`
	Function chatFunctionDefinition `json:"function"`
}
type chatFunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}
type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}
type chatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func buildChatBody(upstreamModel string, request normalize.NormalizedRequest) (chatRequest, error) {
	body := chatRequest{Model: upstreamModel, Stream: request.Stream, Temperature: request.Temperature, MaxTokens: request.MaxOutputTokens, Stop: request.StopSequences}
	for _, part := range request.System {
		body.Messages = append(body.Messages, chatMessage{Role: "system", Content: part.Text})
	}
	for _, message := range request.Messages {
		mapped := chatMessage{Role: string(message.Role)}
		for _, part := range message.Parts {
			switch value := part.(type) {
			case normalize.TextPart:
				if mapped.Content != "" {
					mapped.Content += "\n"
				}
				mapped.Content += value.Text
			case normalize.ToolCallPart:
				mapped.ToolCalls = append(mapped.ToolCalls, chatToolCall{ID: value.CallID, Type: "function", Function: chatToolFunction{Name: value.Name, Arguments: value.Arguments}})
			case normalize.ToolResultPart:
				if mapped.Content != "" {
					return chatRequest{}, errors.New("Tool Result 只允许一个内容块")
				}
				mapped.Content = value.Content
				mapped.ToolCallID = value.CallID
			case normalize.ImagePart, normalize.ReasoningPart:
				return chatRequest{}, errors.New("当前 OpenAI Chat 映射不支持该内容类型")
			default:
				return chatRequest{}, errors.New("未知内容类型")
			}
		}
		body.Messages = append(body.Messages, mapped)
	}
	for _, tool := range request.Tools {
		body.Tools = append(body.Tools, chatTool{Type: "function", Function: chatFunctionDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}})
	}
	switch request.ToolChoice.Mode {
	case "", normalize.ToolChoiceAuto:
	case normalize.ToolChoiceNone:
		body.ToolChoice = "none"
	case normalize.ToolChoiceRequired:
		body.ToolChoice = "required"
	case normalize.ToolChoiceNamed:
		body.ToolChoice = struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}{Type: "function", Function: struct {
			Name string `json:"name"`
		}{Name: request.ToolChoice.Name}}
	default:
		return chatRequest{}, errors.New("Tool Choice 无效")
	}
	return body, nil
}
