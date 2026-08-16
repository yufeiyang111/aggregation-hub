package openai_responses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/observability"
)

type gateway interface {
	Complete(context.Context, normalize.NormalizedRequest) (normalize.NormalizedResponse, error)
	Stream(context.Context, normalize.NormalizedRequest, normalize.StreamEmitter) error
}

type Handler struct {
	gateway  gateway
	recorder observability.RequestRecorder
}

func NewHandler(value gateway, recorders ...observability.RequestRecorder) (*Handler, error) {
	if value == nil || len(recorders) > 1 {
		return nil, ErrInvalidGateway
	}
	recorder := observability.NewNoopRecorder()
	if len(recorders) == 1 {
		if recorders[0] == nil {
			return nil, ErrInvalidGateway
		}
		recorder = recorders[0]
	}
	return &Handler{gateway: value, recorder: recorder}, nil
}

func (value *Handler) startObservation(request *http.Request, input normalize.NormalizedRequest) observability.RequestLifecycle {
	lifecycle, err := value.recorder.Start(request.Context(), observability.RequestStart{SourceProtocol: observability.ProtocolOpenAIResponses, Endpoint: "/v1/responses", PublicModelSnapshot: input.Model, Streaming: input.Stream})
	if err != nil {
		observability.ReportPersistenceError(err)
		return observability.NoopRequestLifecycle()
	}
	return lifecycle
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
	lifecycle := value.startObservation(request, normalized)
	if normalized.Stream {
		observability.ReportPersistenceError(lifecycle.MarkStreaming(request.Context()))
		value.serveStream(writer, request, normalized, lifecycle)
		return
	}
	result, err := value.gateway.Complete(request.Context(), normalized)
	if err != nil {
		observability.FinishWithError(lifecycle, request.Context(), err)
		writeGatewayError(writer, err)
		return
	}
	observability.ReportPersistenceError(lifecycle.Complete(request.Context(), observability.Completion{HTTPStatus: http.StatusOK, Usage: result.Usage}))
	writeJSON(writer, http.StatusOK, renderResponse(result))
}

func writeGatewayError(writer http.ResponseWriter, err error) {
	var gatewayErr *adapter.GatewayError
	if errors.As(err, &gatewayErr) && gatewayErr != nil {
		status := gatewayErr.HTTPStatus
		if status < http.StatusBadRequest || status > http.StatusInternalServerError {
			status = http.StatusBadGateway
		}
		code := gatewayErr.Code
		if code == "" {
			code = "gateway_error"
		}
		message := gatewayErr.SafeMessage
		if message == "" {
			message = "请求上游服务失败"
		}
		writeError(writer, status, code, message)
		return
	}
	writeError(writer, http.StatusBadGateway, "gateway_error", "请求上游服务失败")
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"code": code, "message": strings.TrimSpace(message)}})
}
