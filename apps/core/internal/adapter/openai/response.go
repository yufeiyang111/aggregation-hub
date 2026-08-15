package openai

import "aggregationhub.local/core/internal/normalize"

type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
	} `json:"usage"`
}

func normalizeUsage(value struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`
}) *normalize.Usage {
	if value.PromptTokens == nil && value.CompletionTokens == nil {
		return nil
	}
	return &normalize.Usage{InputTokens: value.PromptTokens, OutputTokens: value.CompletionTokens, Source: normalize.UsageSourceUpstreamReported}
}
func mapFinishReason(value string) normalize.FinishReason {
	switch value {
	case "stop":
		return normalize.FinishReasonStop
	case "length":
		return normalize.FinishReasonLength
	case "tool_calls", "function_call":
		return normalize.FinishReasonToolCalls
	default:
		return normalize.FinishReasonStop
	}
}
