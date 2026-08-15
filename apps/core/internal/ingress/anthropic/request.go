package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxRequestBytes int64 = 8 * 1024 * 1024

var (
	errInvalidRequest       = errors.New("Anthropic 请求无效")
	errUnsupportedFeature   = errors.New("当前 Anthropic 功能尚未支持")
	errInvalidContentBlocks = errors.New("内容块无效")
)

type requestDTO struct {
	Model         string          `json:"model"`
	MaxTokens     *int64          `json:"max_tokens"`
	Messages      []messageDTO    `json:"messages"`
	System        json.RawMessage `json:"system"`
	Stream        bool            `json:"stream,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []toolDTO       `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
	Thinking      json.RawMessage `json:"thinking,omitempty"`
}

type messageDTO struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlockDTO struct {
	Type      string
	Text      string
	ID        string
	Name      string
	Input     json.RawMessage
	ToolUseID string
	Content   json.RawMessage
	IsError   bool
	Thinking  string
}

type textBlockDTO struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolUseBlockDTO struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type toolResultBlockDTO struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error,omitempty"`
}

type thinkingBlockDTO struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type systemBlockDTO struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolDTO struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolChoiceDTO struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

func decodeRequest(request *http.Request) (requestDTO, error) {
	if request == nil || request.Body == nil {
		return requestDTO{}, errInvalidRequest
	}
	defer request.Body.Close()
	reader := &io.LimitedReader{R: request.Body, N: maxRequestBytes + 1}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var value requestDTO
	if err := decoder.Decode(&value); err != nil {
		return requestDTO{}, errInvalidRequest
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return requestDTO{}, errInvalidRequest
	}
	if reader.N == 0 {
		return requestDTO{}, errInvalidRequest
	}
	if len(value.System) != 0 && bytes.Equal(bytes.TrimSpace(value.System), []byte("null")) {
		return requestDTO{}, errInvalidRequest
	}
	if len(value.Thinking) != 0 {
		if bytes.Equal(bytes.TrimSpace(value.Thinking), []byte("null")) {
			return requestDTO{}, errInvalidRequest
		}
		return requestDTO{}, errUnsupportedFeature
	}
	return value, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errInvalidContentBlocks
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errInvalidContentBlocks
	}
	return nil
}

func decodeContentBlocks(raw json.RawMessage) ([]contentBlockDTO, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errInvalidContentBlocks
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []contentBlockDTO{{Type: "text", Text: text}}, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) == 0 {
		return nil, errInvalidContentBlocks
	}
	result := make([]contentBlockDTO, 0, len(values))
	for _, value := range values {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(value, &envelope); err != nil || envelope.Type == "" {
			return nil, errInvalidContentBlocks
		}
		switch envelope.Type {
		case "image", "document", "redacted_thinking", "server_tool_use", "web_search_tool_result":
			return nil, errUnsupportedFeature
		case "text":
			var block textBlockDTO
			if err := decodeStrict(value, &block); err != nil {
				return nil, err
			}
			result = append(result, contentBlockDTO{Type: block.Type, Text: block.Text})
		case "tool_use":
			var block toolUseBlockDTO
			if err := decodeStrict(value, &block); err != nil {
				return nil, err
			}
			result = append(result, contentBlockDTO{Type: block.Type, ID: block.ID, Name: block.Name, Input: block.Input})
		case "tool_result":
			var block toolResultBlockDTO
			if err := decodeStrict(value, &block); err != nil {
				return nil, err
			}
			result = append(result, contentBlockDTO{Type: block.Type, ToolUseID: block.ToolUseID, Content: block.Content, IsError: block.IsError})
		case "thinking":
			var block thinkingBlockDTO
			if err := decodeStrict(value, &block); err != nil {
				return nil, err
			}
			result = append(result, contentBlockDTO{Type: block.Type, Thinking: block.Thinking})
		default:
			return nil, errInvalidContentBlocks
		}
	}
	return result, nil
}

func decodeSystem(raw json.RawMessage) ([]systemBlockDTO, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []systemBlockDTO{{Type: "text", Text: text}}, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) == 0 {
		return nil, errInvalidRequest
	}
	result := make([]systemBlockDTO, 0, len(values))
	for _, value := range values {
		var block systemBlockDTO
		if err := decodeStrict(value, &block); err != nil {
			return nil, errInvalidRequest
		}
		result = append(result, block)
	}
	return result, nil
}

func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 1 && trimmed[0] == '{' && json.Valid(trimmed)
}

func requiredString(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}
