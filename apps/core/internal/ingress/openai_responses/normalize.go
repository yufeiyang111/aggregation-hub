package openai_responses

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"aggregationhub.local/core/internal/normalize"
)

func normalizeInput(value requestDTO) (normalize.NormalizedRequest, error) {
	if value.Stream {
		return normalize.NormalizedRequest{}, errors.New("Responses 流式输出尚未启用")
	}
	if len(value.Reasoning) > 0 && !bytes.Equal(bytes.TrimSpace(value.Reasoning), []byte("null")) {
		return normalize.NormalizedRequest{}, errors.New("Responses Reasoning 尚未启用")
	}
	result := normalize.NormalizedRequest{Model: strings.TrimSpace(value.Model), MaxOutputTokens: value.MaxOutputTokens}
	if result.Model == "" || len(result.Model) > 304 {
		return normalize.NormalizedRequest{}, errors.New("model 无效")
	}
	if value.Instructions != "" {
		if !validText(value.Instructions) {
			return normalize.NormalizedRequest{}, errors.New("instructions 无效")
		}
		result.System = append(result.System, normalize.TextPart{Text: value.Instructions})
	}
	if err := normalizeTools(value.Tools, &result); err != nil {
		return normalize.NormalizedRequest{}, err
	}
	if err := normalizeInputItems(value.Input, &result); err != nil {
		return normalize.NormalizedRequest{}, err
	}
	choice, err := parseToolChoice(value.ToolChoice, result.Tools)
	if err != nil {
		return normalize.NormalizedRequest{}, err
	}
	result.ToolChoice = choice
	return result, nil
}

func normalizeInputItems(raw json.RawMessage, result *normalize.NormalizedRequest) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return errors.New("input 不能为空")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil || !validText(text) {
			return errors.New("input 文本无效")
		}
		result.Messages = append(result.Messages, normalize.Message{Role: normalize.RoleUser, Parts: []normalize.ContentPart{normalize.TextPart{Text: text}}})
		return nil
	}
	var items []inputItemDTO
	if err := json.Unmarshal(trimmed, &items); err != nil || len(items) == 0 {
		return errors.New("input item 无效")
	}
	seenCalls := make(map[string]struct{})
	for _, item := range items {
		switch item.Type {
		case "message":
			if item.Role != "user" && item.Role != "assistant" {
				return errors.New("Responses message role 无效")
			}
			content, err := textContent(item.Content)
			if err != nil {
				return err
			}
			result.Messages = append(result.Messages, normalize.Message{Role: normalize.Role(item.Role), Parts: []normalize.ContentPart{normalize.TextPart{Text: content}}})
		case "function_call":
			if !validIdentifier(item.CallID) || !validToolName(item.Name) || !validJSONObjectString(item.Arguments) {
				return errors.New("function_call item 无效")
			}
			if _, exists := seenCalls[item.CallID]; exists {
				return errors.New("function_call call_id 重复")
			}
			seenCalls[item.CallID] = struct{}{}
			result.Messages = append(result.Messages, normalize.Message{Role: normalize.RoleAssistant, Parts: []normalize.ContentPart{normalize.ToolCallPart{CallID: item.CallID, Name: item.Name, Arguments: item.Arguments}}})
		case "function_call_output":
			output := item.OutputString()
			if !validIdentifier(item.CallID) || !validText(output) {
				return errors.New("function_call_output item 无效")
			}
			if _, exists := seenCalls[item.CallID]; !exists {
				return errors.New("function_call_output 缺少对应 function_call")
			}
			result.Messages = append(result.Messages, normalize.Message{Role: normalize.RoleTool, Parts: []normalize.ContentPart{normalize.ToolResultPart{CallID: item.CallID, Content: output}}})
		default:
			return errors.New("Responses input 类型未支持")
		}
	}
	return nil
}

func (value inputItemDTO) OutputString() string {
	trimmed := bytes.TrimSpace(value.Output)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if json.Unmarshal(trimmed, &text) == nil {
			return text
		}
	}
	return string(trimmed)
}

func normalizeTools(tools []toolDTO, result *normalize.NormalizedRequest) error {
	for _, tool := range tools {
		if tool.Type != "function" || !validToolName(tool.Name) || !validJSONObject(tool.Parameters) {
			return errors.New("Responses function tool 无效")
		}
		result.Tools = append(result.Tools, normalize.ToolDefinition{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.Parameters...)})
	}
	return nil
}

func parseToolChoice(raw json.RawMessage, tools []normalize.ToolDefinition) (normalize.ToolChoice, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return normalize.ToolChoice{}, nil
	}
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
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) != nil || choice.Type != "function" || !validToolName(choice.Name) {
		return normalize.ToolChoice{}, errors.New("Responses tool_choice 无效")
	}
	for _, tool := range tools {
		if tool.Name == choice.Name {
			return normalize.ToolChoice{Mode: normalize.ToolChoiceNamed, Name: choice.Name}, nil
		}
	}
	return normalize.ToolChoice{}, errors.New("Responses tool_choice 找不到对应工具")
}

func textContent(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", errors.New("message content 不能为空")
	}
	if trimmed[0] == '"' {
		var value string
		if json.Unmarshal(trimmed, &value) == nil && validText(value) {
			return value, nil
		}
	}
	return "", errors.New("Responses 当前只支持文本 content")
}

func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && json.Valid(trimmed)
}
func validJSONObjectString(value string) bool { return validJSONObject(json.RawMessage(value)) }
func validText(value string) bool {
	return value != "" && len(value) <= 512*1024 && utf8.ValidString(value)
}
func validIdentifier(value string) bool {
	return value != "" && len(value) <= 304 && strings.TrimSpace(value) == value && utf8.ValidString(value)
}
func validToolName(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}
