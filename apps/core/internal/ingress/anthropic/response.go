package anthropic

import (
	"encoding/json"
	"errors"

	"aggregationhub.local/core/internal/normalize"
)

type messageResponse struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Model        string          `json:"model"`
	Content      []contentOutput `json:"content"`
	StopReason   *string         `json:"stop_reason"`
	StopSequence *string         `json:"stop_sequence"`
	Usage        *usageOutput    `json:"usage,omitempty"`
}

type contentOutput struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type usageOutput struct {
	InputTokens  *int64 `json:"input_tokens,omitempty"`
	OutputTokens *int64 `json:"output_tokens,omitempty"`
}

func renderResponse(value normalize.NormalizedResponse) (messageResponse, error) {
	if !requiredString(value.ID) || !requiredString(value.Model) {
		return messageResponse{}, errors.New("上游 Anthropic 响应无效")
	}
	result := messageResponse{ID: value.ID, Type: "message", Role: "assistant", Model: value.Model, Content: make([]contentOutput, 0, len(value.Parts))}
	for _, part := range value.Parts {
		switch typed := part.(type) {
		case normalize.TextPart:
			result.Content = append(result.Content, contentOutput{Type: "text", Text: typed.Text})
		case normalize.ToolCallPart:
			if !validJSONObject(json.RawMessage(typed.Arguments)) {
				return messageResponse{}, errors.New("Tool 参数无效")
			}
			result.Content = append(result.Content, contentOutput{Type: "tool_use", ID: typed.CallID, Name: typed.Name, Input: json.RawMessage(typed.Arguments)})
		case normalize.ReasoningPart, normalize.ImagePart, normalize.ToolResultPart:
			return messageResponse{}, errors.New("上游响应包含当前入口不支持的内容块")
		default:
			return messageResponse{}, errors.New("上游响应内容块无效")
		}
	}
	if len(result.Content) == 0 {
		return messageResponse{}, errors.New("上游响应缺少内容")
	}
	stopReason, err := mapFinishReason(value.FinishReason)
	if err != nil {
		return messageResponse{}, err
	}
	result.StopReason = &stopReason
	if value.Usage != nil {
		result.Usage = &usageOutput{InputTokens: cloneInt64(value.Usage.InputTokens), OutputTokens: cloneInt64(value.Usage.OutputTokens)}
	}
	return result, nil
}

func mapFinishReason(value normalize.FinishReason) (string, error) {
	switch value {
	case normalize.FinishReasonStop:
		return "end_turn", nil
	case normalize.FinishReasonLength:
		return "max_tokens", nil
	case normalize.FinishReasonToolCalls:
		return "tool_use", nil
	default:
		return "", errors.New("上游响应终态无法映射为 Anthropic stop_reason")
	}
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
