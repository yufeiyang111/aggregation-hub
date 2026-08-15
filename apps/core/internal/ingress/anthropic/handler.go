package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
)

var ErrInvalidGateway = errors.New("Anthropic Gateway 依赖无效")

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
		writeError(writer, http.StatusMethodNotAllowed, "invalid_request_error", "仅支持 POST 请求")
		return
	}
	input, err := decodeRequest(request)
	if err != nil {
		writeRequestError(writer, err)
		return
	}
	normalized, err := normalizeInput(input)
	if err != nil {
		writeRequestError(writer, err)
		return
	}
	if normalized.Stream {
		writeError(writer, http.StatusBadRequest, "unsupported_feature", "当前版本尚未支持 Anthropic 流式响应")
		return
	}
	result, err := value.gateway.Complete(request.Context(), normalized)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	response, err := renderResponse(result)
	if err != nil {
		writeError(writer, http.StatusBadGateway, "api_error", "上游响应格式无效")
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

type errorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeRequestError(writer http.ResponseWriter, err error) {
	if isUnsupported(err) {
		writeError(writer, http.StatusBadRequest, "unsupported_feature", "请求包含当前版本尚未支持的功能")
		return
	}
	writeError(writer, http.StatusBadRequest, "invalid_request_error", "请求字段无效")
}

func writeGatewayError(writer http.ResponseWriter, err error) {
	var capability *provider.UnsupportedCapabilityError
	switch {
	case errors.Is(err, provider.ErrModelNotFound):
		writeError(writer, http.StatusNotFound, "not_found_error", "请求的模型不可用")
	case errors.As(err, &capability):
		writeError(writer, http.StatusBadRequest, "unsupported_feature", "所选模型不支持请求所需能力")
	default:
		var gatewayError *adapter.GatewayError
		if errors.As(err, &gatewayError) && gatewayError.HTTPStatus >= 400 && gatewayError.HTTPStatus < 600 {
			writeError(writer, gatewayError.HTTPStatus, "api_error", gatewayError.SafeMessage)
			return
		}
		writeError(writer, http.StatusBadGateway, "api_error", "请求上游服务失败")
	}
}

func writeError(writer http.ResponseWriter, status int, errorType, message string) {
	response := errorResponse{Type: "error"}
	response.Error.Type = errorType
	response.Error.Message = message
	writeJSON(writer, status, response)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
