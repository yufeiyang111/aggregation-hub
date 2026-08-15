package provider_test

import (
	"encoding/json"
	"errors"
	"testing"

	"aggregationhub.local/core/internal/provider"
)

func TestModelLimitOverrideRejectsUnsafeValuesAndAllowsReset(t *testing.T) {
	value, err := provider.ParseModelLimitOverride(json.RawMessage(`{"context_window_tokens":100000,"max_output_tokens":4096}`))
	if err != nil || value.ContextWindowTokens == nil || *value.ContextWindowTokens != 100000 || value.MaxOutputTokens == nil || *value.MaxOutputTokens != 4096 {
		t.Fatalf("解析模型参数覆盖错误: %+v, %v", value, err)
	}
	for _, raw := range []string{`null`, `[]`, `{"context_window_tokens":0}`, `{"context_window_tokens":-1}`, `{"unknown":1}`, `{"context_window_tokens":1,"context_window_tokens":2}`, `{"context_window_tokens":null}`} {
		if _, err := provider.ParseModelLimitOverride(json.RawMessage(raw)); !errors.Is(err, provider.ErrInvalidModelLimitOverride) {
			t.Fatalf("非法模型参数 %s 错误=%v", raw, err)
		}
	}
	reset, err := provider.ParseModelLimitOverride(json.RawMessage(`{}`))
	if err != nil || !reset.Empty() {
		t.Fatalf("模型参数覆盖恢复默认错误: %+v, %v", reset, err)
	}
}

func TestEffectiveModelLimitsUsesOverrideWithoutMutatingBase(t *testing.T) {
	baseContext, baseOutput := int64(128000), int64(8192)
	overrideContext, overrideOutput := int64(100000), int64(4096)
	contextWindow, maxOutput := provider.EffectiveModelLimits(&baseContext, &baseOutput, &overrideContext, &overrideOutput)
	if contextWindow == nil || *contextWindow != overrideContext || maxOutput == nil || *maxOutput != overrideOutput {
		t.Fatalf("模型参数覆盖未生效: %v %v", contextWindow, maxOutput)
	}
	*contextWindow = 1
	if baseContext != 128000 {
		t.Fatalf("有效模型参数不应修改上游值: %d", baseContext)
	}
}
