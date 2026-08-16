package openai_responses

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxRequestBytes int64 = 1024 * 1024

var (
	ErrInvalidGateway = errors.New("Responses Gateway 依赖无效")
	ErrInvalidRequest = errors.New("Responses 请求无效")
)

type requestDTO struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Instructions    string          `json:"instructions,omitempty"`
	Tools           []toolDTO       `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	MaxOutputTokens *int64          `json:"max_output_tokens,omitempty"`
	Reasoning       json.RawMessage `json:"reasoning,omitempty"`
}

type toolDTO struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type inputItemDTO struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

func decodeRequest(request *http.Request) (requestDTO, error) {
	if request == nil || request.Body == nil {
		return requestDTO{}, ErrInvalidRequest
	}
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	var value requestDTO
	if err := decoder.Decode(&value); err != nil {
		return requestDTO{}, ErrInvalidRequest
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return requestDTO{}, ErrInvalidRequest
	}
	if strings.TrimSpace(value.Model) == "" || len(bytes.TrimSpace(value.Input)) == 0 {
		return requestDTO{}, ErrInvalidRequest
	}
	if value.MaxOutputTokens != nil && *value.MaxOutputTokens < 1 {
		return requestDTO{}, ErrInvalidRequest
	}
	return value, nil
}
