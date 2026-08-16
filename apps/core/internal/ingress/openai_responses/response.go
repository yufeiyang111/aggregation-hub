package openai_responses

import (
	"encoding/json"
	"net/http"
	"time"

	"aggregationhub.local/core/internal/normalize"
)

func renderResponse(value normalize.NormalizedResponse) any {
	output := make([]any, 0, len(value.Parts))
	for _, part := range value.Parts {
		switch typed := part.(type) {
		case normalize.TextPart:
			output = append(output, map[string]any{
				"type": "message", "role": "assistant",
				"content": []map[string]string{{"type": "output_text", "text": typed.Text}},
			})
		case normalize.ToolCallPart:
			output = append(output, map[string]string{
				"type": "function_call", "call_id": typed.CallID, "name": typed.Name, "arguments": typed.Arguments,
			})
		}
	}
	status := "completed"
	if value.FinishReason == normalize.FinishReasonLength {
		status = "incomplete"
	} else if value.FinishReason == normalize.FinishReasonError {
		status = "failed"
	}
	result := map[string]any{
		"id": value.ID, "object": "response", "created_at": time.Now().Unix(), "status": status,
		"model": value.Model, "output": output,
	}
	if value.Usage != nil {
		result["usage"] = map[string]any{
			"input_tokens": value.Usage.InputTokens, "output_tokens": value.Usage.OutputTokens,
		}
	}
	return result
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
