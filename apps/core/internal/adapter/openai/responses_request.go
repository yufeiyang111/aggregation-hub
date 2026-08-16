package openai

import (
	"encoding/json"
	"errors"

	"aggregationhub.local/core/internal/normalize"
)

type responsesRequest struct {
	Model           string           `json:"model"`
	Input           []responsesInput `json:"input"`
	Instructions    string           `json:"instructions,omitempty"`
	Tools           []responsesTool  `json:"tools,omitempty"`
	ToolChoice      any              `json:"tool_choice,omitempty"`
	MaxOutputTokens *int64           `json:"max_output_tokens,omitempty"`
	Stream          bool             `json:"stream,omitempty"`
}

type responsesInput struct {
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

func buildResponsesBody(upstreamModel string, request normalize.NormalizedRequest) (responsesRequest, error) {
	body := responsesRequest{Model: upstreamModel, MaxOutputTokens: request.MaxOutputTokens, Stream: request.Stream}
	for _, part := range request.System {
		if body.Instructions != "" {
			body.Instructions += "\n"
		}
		body.Instructions += part.Text
	}
	for _, message := range request.Messages {
		for _, part := range message.Parts {
			switch value := part.(type) {
			case normalize.TextPart:
				body.Input = append(body.Input, responsesInput{
					Type: "message", Role: string(message.Role),
					Content: []map[string]string{{"type": inputTextType(message.Role), "text": value.Text}},
				})
			case normalize.ToolCallPart:
				body.Input = append(body.Input, responsesInput{Type: "function_call", CallID: value.CallID, Name: value.Name, Arguments: value.Arguments})
			case normalize.ToolResultPart:
				body.Input = append(body.Input, responsesInput{Type: "function_call_output", CallID: value.CallID, Output: value.Content})
			case normalize.ImagePart, normalize.ReasoningPart:
				return responsesRequest{}, errors.New("当前 Responses 映射不支持该内容类型")
			default:
				return responsesRequest{}, errors.New("未知内容类型")
			}
		}
	}
	for _, tool := range request.Tools {
		body.Tools = append(body.Tools, responsesTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}
	switch request.ToolChoice.Mode {
	case "", normalize.ToolChoiceAuto:
	case normalize.ToolChoiceNone:
		body.ToolChoice = "none"
	case normalize.ToolChoiceRequired:
		body.ToolChoice = "required"
	case normalize.ToolChoiceNamed:
		body.ToolChoice = map[string]string{"type": "function", "name": request.ToolChoice.Name}
	default:
		return responsesRequest{}, errors.New("Tool Choice 无效")
	}
	return body, nil
}

func inputTextType(role normalize.Role) string {
	if role == normalize.RoleAssistant {
		return "output_text"
	}
	return "input_text"
}
