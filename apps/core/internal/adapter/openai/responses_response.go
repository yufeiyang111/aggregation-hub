package openai

import (
	"encoding/json"
	"io"
	"net/http"

	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/routing"
)

type responsesResponse struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"output"`
	Usage *struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (value *Adapter) parseResponsesResponse(route routing.RoutePlan, response *http.Response) (normalize.NormalizedResponse, error) {
	if response == nil {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游服务返回无效响应", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return normalize.NormalizedResponse{}, gatewayFromResponse(route.ProviderID, response)
	}
	defer response.Body.Close()
	var body responsesResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&body); err != nil {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游 Responses 响应格式无效", http.StatusBadGateway, true, route.ProviderID, err)
	}
	if body.ID == "" || body.Model == "" {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游 Responses 响应缺少标识", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	parts := make([]normalize.ContentPart, 0, len(body.Output))
	finish := normalize.FinishReasonStop
	for _, item := range body.Output {
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				if content.Type == "output_text" && content.Text != "" {
					parts = append(parts, normalize.TextPart{Text: content.Text})
				} else if content.Type != "output_text" {
					return normalize.NormalizedResponse{}, adapterError("unsupported_feature", "上游 Responses 返回了未支持的内容类型", http.StatusBadGateway, false, route.ProviderID, nil)
				}
			}
		case "function_call":
			if !validModelID(item.CallID) || !validModelID(item.Name) || !json.Valid([]byte(item.Arguments)) {
				return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游 Responses Function Call 无效", http.StatusBadGateway, true, route.ProviderID, nil)
			}
			parts = append(parts, normalize.ToolCallPart{CallID: item.CallID, Name: item.Name, Arguments: item.Arguments})
			finish = normalize.FinishReasonToolCalls
		case "reasoning":
			return normalize.NormalizedResponse{}, adapterError("unsupported_feature", "上游 Responses Reasoning 尚未启用", http.StatusBadGateway, false, route.ProviderID, nil)
		default:
			return normalize.NormalizedResponse{}, adapterError("unsupported_feature", "上游 Responses 返回了未支持的输出类型", http.StatusBadGateway, false, route.ProviderID, nil)
		}
	}
	if body.Status == "incomplete" {
		finish = normalize.FinishReasonLength
	} else if body.Status == "failed" {
		finish = normalize.FinishReasonError
	}
	var usage *normalize.Usage
	if body.Usage != nil && (body.Usage.InputTokens != nil || body.Usage.OutputTokens != nil) {
		usage = &normalize.Usage{InputTokens: body.Usage.InputTokens, OutputTokens: body.Usage.OutputTokens, Source: normalize.UsageSourceUpstreamReported}
	}
	if len(parts) == 0 && finish == normalize.FinishReasonStop {
		return normalize.NormalizedResponse{}, adapterError("upstream_invalid_response", "上游 Responses 响应缺少可用输出", http.StatusBadGateway, true, route.ProviderID, nil)
	}
	return normalize.NormalizedResponse{ID: body.ID, Model: body.Model, Parts: parts, Usage: usage, FinishReason: finish}, nil
}
