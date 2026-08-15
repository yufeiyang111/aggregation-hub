package openai_test

import (
	"errors"
	"testing"

	"aggregationhub.local/core/internal/adapter/openai"
)

func TestParseConfigAppliesSafeDefaults(t *testing.T) {
	config, err := openai.ParseConfig([]byte(`{}`))
	if err != nil {
		t.Fatalf("解析默认配置失败: %v", err)
	}
	if config.WireAPI != openai.WireAPIChatCompletions || config.ChatCompletionsPath != "/v1/chat/completions" || config.ModelsPath != "/v1/models" || config.AuthHeaderMode != openai.AuthHeaderAuthorizationBearer {
		t.Fatalf("默认配置错误: %+v", config)
	}
}

func TestParseConfigRejectsUnknownSecretAndTrailingFields(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"unexpected":true}`),
		[]byte(`{"api_key":"do-not-store"}`),
		[]byte(`{"models_path":"https://example.test/v1/models"}`),
		[]byte(`{"wire_api":"guess"}`),
		[]byte(`{} {}`),
	} {
		if _, err := openai.ParseConfig(raw); !errors.Is(err, openai.ErrInvalidConfig) {
			t.Fatalf("配置未被拒绝 raw=%s err=%v", raw, err)
		}
	}
}
