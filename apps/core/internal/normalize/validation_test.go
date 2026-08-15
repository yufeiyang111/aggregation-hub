package normalize_test

import (
	"errors"
	"testing"

	"aggregationhub.local/core/internal/normalize"
)

func TestValidateRequestRejectsSystemMessageInConversation(t *testing.T) {
	request := normalize.NormalizedRequest{
		Model: "demo/model",
		Messages: []normalize.Message{{
			Role:  normalize.RoleSystem,
			Parts: []normalize.ContentPart{normalize.TextPart{Text: "不要放在 messages 中"}},
		}},
	}

	_, err := normalize.ValidateRequest(request, normalize.DefaultValidationLimits())
	if !errors.Is(err, normalize.ErrSystemMustBeSeparate) {
		t.Fatalf("系统消息未被拒绝: %v", err)
	}
}

func TestValidateRequestRejectsToolSchemaOverDepthLimit(t *testing.T) {
	request := normalize.NormalizedRequest{
		Model: "demo/model",
		Messages: []normalize.Message{{
			Role:  normalize.RoleUser,
			Parts: []normalize.ContentPart{normalize.TextPart{Text: "你好"}},
		}},
		Tools: []normalize.ToolDefinition{{
			Name:        "lookup",
			Description: "查询",
			InputSchema: []byte(`{"type":{"nested":{"too_deep":true}}}`),
		}},
	}
	limits := normalize.DefaultValidationLimits()
	limits.MaxToolSchemaDepth = 2

	_, err := normalize.ValidateRequest(request, limits)
	if !errors.Is(err, normalize.ErrToolSchemaTooDeep) {
		t.Fatalf("过深工具 Schema 未被拒绝: %v", err)
	}
}

func TestValidateRequestRejectsUnknownToolResultID(t *testing.T) {
	request := normalize.NormalizedRequest{
		Model: "demo/model",
		Messages: []normalize.Message{{
			Role: normalize.RoleTool,
			Parts: []normalize.ContentPart{normalize.ToolResultPart{
				CallID:  "call_missing",
				Content: "结果",
			}},
		}},
	}

	_, err := normalize.ValidateRequest(request, normalize.DefaultValidationLimits())
	if !errors.Is(err, normalize.ErrInvalidToolResult) {
		t.Fatalf("未知 Tool Result ID 未被拒绝: %v", err)
	}
}

func TestEventSequenceRequiresOneTerminalEvent(t *testing.T) {
	validator := normalize.NewEventSequenceValidator()
	if err := validator.Validate(normalize.ResponseStartEvent{ResponseID: "resp_1", Model: "demo/model"}); err != nil {
		t.Fatalf("response_start 不应失败: %v", err)
	}
	if err := validator.Finalize(); !errors.Is(err, normalize.ErrTerminalEventRequired) {
		t.Fatalf("缺失终态未被拒绝: %v", err)
	}
	if err := validator.Validate(normalize.ResponseEndEvent{FinishReason: normalize.FinishReasonStop}); err != nil {
		t.Fatalf("response_end 不应失败: %v", err)
	}
	if err := validator.Validate(normalize.ErrorEvent{Code: "unexpected", Message: "终态后不应再发送"}); !errors.Is(err, normalize.ErrEventAfterTerminal) {
		t.Fatalf("第二个终态未被拒绝: %v", err)
	}
	if err := validator.Finalize(); err != nil {
		t.Fatalf("单个终态的序列不应失败: %v", err)
	}
}
