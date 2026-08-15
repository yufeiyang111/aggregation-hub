package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrInvalidCapabilityOverride = errors.New("模型能力覆盖无效")

type RequiredCapabilities struct {
	Streaming     bool
	Tools         bool
	ParallelTools bool
	Reasoning     bool
	Thinking      bool
	Vision        bool
}

// CapabilityOverride 仅保存用户明确声明的模型能力覆盖；nil 表示继续使用上游声明。
type CapabilityOverride struct {
	Streaming     *bool `json:"supports_streaming,omitempty"`
	Tools         *bool `json:"supports_tools,omitempty"`
	ParallelTools *bool `json:"supports_parallel_tools,omitempty"`
	Reasoning     *bool `json:"supports_reasoning,omitempty"`
	Thinking      *bool `json:"supports_thinking,omitempty"`
	Vision        *bool `json:"supports_vision,omitempty"`
}

func (override CapabilityOverride) Empty() bool {
	return override.Streaming == nil && override.Tools == nil && override.ParallelTools == nil && override.Reasoning == nil && override.Thinking == nil && override.Vision == nil
}

// JSON 使用固定的字段 allowlist 编码，避免未知能力进入持久化层。
func (override CapabilityOverride) JSON() (json.RawMessage, error) {
	encoded, err := json.Marshal(override)
	if err != nil {
		return nil, fmt.Errorf("编码模型能力覆盖失败: %w", err)
	}
	return json.RawMessage(encoded), nil
}

// ParseCapabilityOverride 校验并解析数据库或 Control Plane 使用的能力覆盖对象。
func ParseCapabilityOverride(raw json.RawMessage) (CapabilityOverride, error) {
	if len(raw) == 0 {
		return CapabilityOverride{}, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "{}" {
		return CapabilityOverride{}, nil
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return CapabilityOverride{}, ErrInvalidCapabilityOverride
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	firstToken, err := decoder.Token()
	if err != nil || firstToken != json.Delim('{') {
		return CapabilityOverride{}, ErrInvalidCapabilityOverride
	}
	var override CapabilityOverride
	seen := make(map[string]struct{}, 6)
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return CapabilityOverride{}, ErrInvalidCapabilityOverride
		}
		name, ok := nameToken.(string)
		if !ok {
			return CapabilityOverride{}, ErrInvalidCapabilityOverride
		}
		if _, exists := seen[name]; exists {
			return CapabilityOverride{}, ErrInvalidCapabilityOverride
		}
		seen[name] = struct{}{}

		var encoded json.RawMessage
		if err := decoder.Decode(&encoded); err != nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
			return CapabilityOverride{}, ErrInvalidCapabilityOverride
		}
		var value bool
		if err := json.Unmarshal(encoded, &value); err != nil {
			return CapabilityOverride{}, ErrInvalidCapabilityOverride
		}
		switch name {
		case "supports_streaming":
			override.Streaming = &value
		case "supports_tools":
			override.Tools = &value
		case "supports_parallel_tools":
			override.ParallelTools = &value
		case "supports_reasoning":
			override.Reasoning = &value
		case "supports_thinking":
			override.Thinking = &value
		case "supports_vision":
			override.Vision = &value
		default:
			return CapabilityOverride{}, ErrInvalidCapabilityOverride
		}
	}
	if endToken, err := decoder.Token(); err != nil || endToken != json.Delim('}') {
		return CapabilityOverride{}, ErrInvalidCapabilityOverride
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CapabilityOverride{}, ErrInvalidCapabilityOverride
	}
	return override, nil
}

func ValidateCapabilityOverride(raw json.RawMessage) error {
	_, err := ParseCapabilityOverride(raw)
	return err
}

type UnsupportedCapabilityError struct{ Feature string }

func (err *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("模型不支持能力: %s", err.Feature)
}

// EffectiveCapabilities 将用户字段级覆盖应用到上游声明，未知字段不会被静默接受。
func EffectiveCapabilities(base Capabilities, raw json.RawMessage) (Capabilities, error) {
	override, err := ParseCapabilityOverride(raw)
	if err != nil {
		return Capabilities{}, err
	}
	if override.Streaming != nil {
		base.Streaming = *override.Streaming
	}
	if override.Tools != nil {
		base.Tools = *override.Tools
	}
	if override.ParallelTools != nil {
		base.ParallelTools = *override.ParallelTools
	}
	if override.Reasoning != nil {
		base.Reasoning = *override.Reasoning
	}
	if override.Thinking != nil {
		base.Thinking = *override.Thinking
	}
	if override.Vision != nil {
		base.Vision = *override.Vision
	}
	return base, nil
}

func (available Capabilities) Validate(required RequiredCapabilities) error {
	for _, capability := range []struct {
		required bool
		provided bool
		name     string
	}{
		{required.Streaming, available.Streaming, "streaming"},
		{required.Tools, available.Tools, "tools"},
		{required.ParallelTools, available.ParallelTools, "parallel_tools"},
		{required.Reasoning, available.Reasoning, "reasoning"},
		{required.Thinking, available.Thinking, "thinking"},
		{required.Vision, available.Vision, "vision"},
	} {
		if capability.required && !capability.provided {
			return &UnsupportedCapabilityError{Feature: capability.name}
		}
	}
	return nil
}
