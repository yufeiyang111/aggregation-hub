package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidModelLimitOverride = errors.New("模型参数覆盖无效")

// ModelLimitOverride 仅保存用户明确设置的上下文窗口和最大输出覆盖；空对象表示恢复上游声明。
type ModelLimitOverride struct {
	ContextWindowTokens *int64 `json:"context_window_tokens,omitempty"`
	MaxOutputTokens     *int64 `json:"max_output_tokens,omitempty"`
}

func (override ModelLimitOverride) Empty() bool {
	return override.ContextWindowTokens == nil && override.MaxOutputTokens == nil
}

func (override *ModelLimitOverride) UnmarshalJSON(raw []byte) error {
	parsed, err := parseModelLimitOverride(raw)
	if err != nil {
		return err
	}
	*override = parsed
	return nil
}

func ParseModelLimitOverride(raw json.RawMessage) (ModelLimitOverride, error) {
	return parseModelLimitOverride(raw)
}

func parseModelLimitOverride(raw json.RawMessage) (ModelLimitOverride, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "{}" {
		return ModelLimitOverride{}, nil
	}
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return ModelLimitOverride{}, ErrInvalidModelLimitOverride
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	firstToken, err := decoder.Token()
	if err != nil || firstToken != json.Delim('{') {
		return ModelLimitOverride{}, ErrInvalidModelLimitOverride
	}
	var override ModelLimitOverride
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return ModelLimitOverride{}, ErrInvalidModelLimitOverride
		}
		name, ok := nameToken.(string)
		if !ok {
			return ModelLimitOverride{}, ErrInvalidModelLimitOverride
		}
		if _, exists := seen[name]; exists {
			return ModelLimitOverride{}, ErrInvalidModelLimitOverride
		}
		seen[name] = struct{}{}

		var encoded json.RawMessage
		if err := decoder.Decode(&encoded); err != nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
			return ModelLimitOverride{}, ErrInvalidModelLimitOverride
		}
		var value int64
		if err := json.Unmarshal(encoded, &value); err != nil || value < 1 {
			return ModelLimitOverride{}, ErrInvalidModelLimitOverride
		}
		switch name {
		case "context_window_tokens":
			override.ContextWindowTokens = &value
		case "max_output_tokens":
			override.MaxOutputTokens = &value
		default:
			return ModelLimitOverride{}, ErrInvalidModelLimitOverride
		}
	}
	if endToken, err := decoder.Token(); err != nil || endToken != json.Delim('}') {
		return ModelLimitOverride{}, ErrInvalidModelLimitOverride
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ModelLimitOverride{}, ErrInvalidModelLimitOverride
	}
	return override, nil
}

func EffectiveModelLimits(contextWindowTokens, maxOutputTokens, contextWindowOverrideTokens, maxOutputOverrideTokens *int64) (*int64, *int64) {
	return effectiveLimit(contextWindowTokens, contextWindowOverrideTokens), effectiveLimit(maxOutputTokens, maxOutputOverrideTokens)
}

func effectiveLimit(base, override *int64) *int64 {
	if override != nil {
		value := *override
		return &value
	}
	if base == nil {
		return nil
	}
	value := *base
	return &value
}
