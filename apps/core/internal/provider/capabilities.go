package provider

import (
	"encoding/json"
	"errors"
	"fmt"
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

type UnsupportedCapabilityError struct{ Feature string }

func (err *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("模型不支持能力: %s", err.Feature)
}

// EffectiveCapabilities 将用户字段级覆盖应用到上游声明，未知字段不会被静默接受。
func EffectiveCapabilities(base Capabilities, override json.RawMessage) (Capabilities, error) {
	if len(override) == 0 || string(override) == "{}" {
		return base, nil
	}
	var values map[string]bool
	if err := json.Unmarshal(override, &values); err != nil {
		return Capabilities{}, ErrInvalidCapabilityOverride
	}
	for field, value := range values {
		switch field {
		case "supports_streaming":
			base.Streaming = value
		case "supports_tools":
			base.Tools = value
		case "supports_parallel_tools":
			base.ParallelTools = value
		case "supports_reasoning":
			base.Reasoning = value
		case "supports_thinking":
			base.Thinking = value
		case "supports_vision":
			base.Vision = value
		default:
			return Capabilities{}, ErrInvalidCapabilityOverride
		}
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
