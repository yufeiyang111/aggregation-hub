package openai_responses

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"aggregationhub.local/core/internal/normalize"
)

type gateway interface {
	Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error)
	Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error
}

type Handler struct{ gateway gateway }

func NewHandler(value gateway) (*Handler, error) {
	if value == nil {
		return nil, ErrInvalidGateway
	}
	return &Handler{gateway: value}, nil
}

func (value *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 POST")
		return
	}
	input, err := decodeRequest(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	normalized, err := normalizeInput(input)
	if err != nil {
		code := "invalid_request"
		if strings.Contains(err.Error(), "尚未启用") || strings.Contains(err.Error(), "未支持") {
			code = "unsupported_feature"
		}
		writeError(writer, http.StatusBadRequest, code, "Responses 请求当前不受支持或字段无效")
		return
	}
	if _, err := normalize.ValidateRequest(normalized, normalize.DefaultValidationLimits()); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "Responses 请求字段无效")
		return
	}
	writeError(writer, http.StatusNotImplemented, "unsupported_feature", "Responses 上游适配器尚未完成")
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"code": code, "message": strings.TrimSpace(message)}})
}
