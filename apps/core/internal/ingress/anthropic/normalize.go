package anthropic

import (
	"encoding/json"
	"errors"

	"aggregationhub.local/core/internal/normalize"
)

func normalizeInput(input requestDTO) (normalize.NormalizedRequest, error) {
	if !requiredString(input.Model) || input.MaxTokens == nil || *input.MaxTokens < 1 || len(input.Messages) == 0 {
		return normalize.NormalizedRequest{}, errInvalidRequest
	}
	result := normalize.NormalizedRequest{
		Model:           input.Model,
		Stream:          input.Stream,
		Temperature:     input.Temperature,
		MaxOutputTokens: input.MaxTokens,
		StopSequences:   append([]string(nil), input.StopSequences...),
	}
	system, err := decodeSystem(input.System)
	if err != nil {
		return normalize.NormalizedRequest{}, err
	}
	for _, part := range system {
		if part.Type != "text" || !requiredString(part.Text) {
			return normalize.NormalizedRequest{}, errInvalidRequest
		}
		result.System = append(result.System, normalize.TextPart{Text: part.Text})
	}
	for _, message := range input.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return normalize.NormalizedRequest{}, errInvalidRequest
		}
		blocks, err := decodeContentBlocks(message.Content)
		if err != nil {
			return normalize.NormalizedRequest{}, err
		}
		mapped := normalize.Message{Role: normalize.Role(message.Role)}
		hasToolResult := false
		for _, block := range blocks {
			part, err := normalizeBlock(message.Role, block)
			if err != nil {
				return normalize.NormalizedRequest{}, err
			}
			if _, ok := part.(normalize.ToolResultPart); ok {
				hasToolResult = true
			} else if hasToolResult {
				return normalize.NormalizedRequest{}, errInvalidContentBlocks
			}
			mapped.Parts = append(mapped.Parts, part)
		}
		if hasToolResult {
			if message.Role != "user" {
				return normalize.NormalizedRequest{}, errInvalidContentBlocks
			}
			for _, part := range mapped.Parts {
				if _, ok := part.(normalize.ToolResultPart); !ok {
					return normalize.NormalizedRequest{}, errInvalidContentBlocks
				}
			}
			mapped.Role = normalize.RoleTool
		}
		result.Messages = append(result.Messages, mapped)
	}
	for _, tool := range input.Tools {
		if !requiredString(tool.Name) || !validJSONObject(tool.InputSchema) {
			return normalize.NormalizedRequest{}, errInvalidRequest
		}
		result.Tools = append(result.Tools, normalize.ToolDefinition{Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...)})
	}
	choice, err := normalizeToolChoice(input.ToolChoice)
	if err != nil {
		return normalize.NormalizedRequest{}, err
	}
	if (choice.Mode == normalize.ToolChoiceRequired || choice.Mode == normalize.ToolChoiceNamed) && len(result.Tools) == 0 {
		return normalize.NormalizedRequest{}, errInvalidRequest
	}
	result.ToolChoice = choice
	if _, err := normalize.ValidateRequest(result, normalize.DefaultValidationLimits()); err != nil {
		return normalize.NormalizedRequest{}, errInvalidRequest
	}
	return result, nil
}

func normalizeBlock(role string, block contentBlockDTO) (normalize.ContentPart, error) {
	switch block.Type {
	case "text":
		if !requiredString(block.Text) || role != "user" && role != "assistant" {
			return nil, errInvalidContentBlocks
		}
		return normalize.TextPart{Text: block.Text}, nil
	case "tool_use":
		if role != "assistant" || !requiredString(block.ID) || !requiredString(block.Name) || !validJSONObject(block.Input) {
			return nil, errInvalidContentBlocks
		}
		return normalize.ToolCallPart{CallID: block.ID, Name: block.Name, Arguments: string(block.Input)}, nil
	case "tool_result":
		if role != "user" || !requiredString(block.ToolUseID) {
			return nil, errInvalidContentBlocks
		}
		content, err := textOnlyToolResult(block.Content)
		if err != nil {
			return nil, err
		}
		return normalize.ToolResultPart{CallID: block.ToolUseID, Content: content, IsError: block.IsError}, nil
	case "thinking":
		if role != "assistant" || !requiredString(block.Thinking) {
			return nil, errInvalidContentBlocks
		}
		return normalize.ReasoningPart{Text: block.Thinking}, nil
	case "image", "document", "redacted_thinking", "server_tool_use", "web_search_tool_result":
		return nil, errUnsupportedFeature
	default:
		return nil, errInvalidContentBlocks
	}
}

func textOnlyToolResult(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", errInvalidContentBlocks
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if !requiredString(text) {
			return "", errInvalidContentBlocks
		}
		return text, nil
	}
	blocks, err := decodeContentBlocks(raw)
	if err != nil {
		return "", err
	}
	var result string
	for _, block := range blocks {
		if block.Type != "text" || !requiredString(block.Text) {
			return "", errUnsupportedFeature
		}
		if result != "" {
			result += "\n"
		}
		result += block.Text
	}
	return result, nil
}

func normalizeToolChoice(raw json.RawMessage) (normalize.ToolChoice, error) {
	if len(raw) == 0 {
		return normalize.ToolChoice{}, nil
	}
	var input toolChoiceDTO
	if err := decodeStrict(raw, &input); err != nil {
		return normalize.ToolChoice{}, errInvalidRequest
	}
	switch input.Type {
	case "auto":
		if input.Name != "" {
			return normalize.ToolChoice{}, errInvalidRequest
		}
		return normalize.ToolChoice{Mode: normalize.ToolChoiceAuto}, nil
	case "any":
		if input.Name != "" {
			return normalize.ToolChoice{}, errInvalidRequest
		}
		return normalize.ToolChoice{Mode: normalize.ToolChoiceRequired}, nil
	case "tool":
		if !requiredString(input.Name) {
			return normalize.ToolChoice{}, errInvalidRequest
		}
		return normalize.ToolChoice{Mode: normalize.ToolChoiceNamed, Name: input.Name}, nil
	default:
		return normalize.ToolChoice{}, errInvalidRequest
	}
}

func isUnsupported(err error) bool {
	return errors.Is(err, errUnsupportedFeature)
}
