package observability

import (
	"context"
	"errors"
	"sync"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
)

// RecordingStreamEmitter 在转发已归一化的流式事件后写入唯一终态，不保存任何内容块正文或 Tool 参数。
type RecordingStreamEmitter struct {
	downstream RequestStreamEmitter
	lifecycle  RequestLifecycle
	mutex      sync.Mutex
	usage      *normalize.Usage
	terminal   bool
}

// RequestStreamEmitter 复用 Normalize 的事件边界，避免观测层接触 HTTP Writer。
type RequestStreamEmitter interface {
	Emit(context.Context, normalize.NormalizedEvent) error
}

func NewRecordingStreamEmitter(downstream RequestStreamEmitter, lifecycle RequestLifecycle) *RecordingStreamEmitter {
	return &RecordingStreamEmitter{downstream: downstream, lifecycle: lifecycle}
}

func (value *RecordingStreamEmitter) Emit(ctx context.Context, event normalize.NormalizedEvent) error {
	if err := value.downstream.Emit(ctx, event); err != nil {
		if value.claimTerminal() {
			value.finishError(ctx, err)
		}
		return err
	}

	switch typed := event.(type) {
	case normalize.UsageUpdateEvent:
		value.mutex.Lock()
		value.usage = cloneUsage(&typed.Usage)
		value.mutex.Unlock()
	case normalize.ResponseEndEvent:
		value.finishResponse(ctx, typed.FinishReason)
	case normalize.ErrorEvent:
		if value.claimTerminal() {
			ReportPersistenceError(value.lifecycle.Fail(ctx, Failure{Code: safeErrorCode(typed.Code, "stream_error")}))
		}
	}
	return nil
}

// Finish 必须在 Gateway.Stream 返回后调用，确保没有终态事件的异常流也被安全标记。
func (value *RecordingStreamEmitter) Finish(ctx context.Context, streamErr error) {
	if !value.claimTerminal() {
		return
	}
	if streamErr == nil {
		ReportPersistenceError(value.lifecycle.Fail(ctx, Failure{Code: "stream_ended_without_terminal"}))
		return
	}
	value.finishError(ctx, streamErr)
}

func (value *RecordingStreamEmitter) finishResponse(ctx context.Context, reason normalize.FinishReason) {
	if !value.claimTerminal() {
		return
	}
	switch reason {
	case normalize.FinishReasonStop, normalize.FinishReasonLength, normalize.FinishReasonToolCalls:
		value.mutex.Lock()
		usage := cloneUsage(value.usage)
		value.mutex.Unlock()
		ReportPersistenceError(value.lifecycle.Complete(ctx, Completion{HTTPStatus: 200, Usage: usage}))
	case normalize.FinishReasonCancelled:
		ReportPersistenceError(value.lifecycle.Cancel(ctx, "client_cancelled"))
	default:
		ReportPersistenceError(value.lifecycle.Fail(ctx, Failure{Code: "stream_error"}))
	}
}

func (value *RecordingStreamEmitter) finishError(ctx context.Context, err error) {
	if errors.Is(err, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
		ReportPersistenceError(value.lifecycle.Cancel(ctx, "client_cancelled"))
		return
	}
	ReportPersistenceError(value.lifecycle.Fail(ctx, FailureFromError(err)))
}

func (value *RecordingStreamEmitter) claimTerminal() bool {
	value.mutex.Lock()
	defer value.mutex.Unlock()
	if value.terminal {
		return false
	}
	value.terminal = true
	return true
}

// FailureFromError 提取稳定的安全分类，绝不持久化 Error 文本、Cause 或上游响应正文。
func FailureFromError(err error) Failure {
	if errors.Is(err, context.Canceled) {
		return Failure{Code: "client_cancelled"}
	}
	var gatewayError *adapter.GatewayError
	if errors.As(err, &gatewayError) && gatewayError != nil {
		return Failure{HTTPStatus: gatewayError.HTTPStatus, Code: safeErrorCode(gatewayError.Code, "gateway_error"), Retryable: gatewayError.Retryable}
	}
	var capabilityError *provider.UnsupportedCapabilityError
	if errors.As(err, &capabilityError) {
		return Failure{HTTPStatus: 400, Code: "unsupported_capability"}
	}
	if errors.Is(err, provider.ErrModelNotFound) {
		return Failure{HTTPStatus: 404, Code: "model_not_found"}
	}
	return Failure{Code: "gateway_error"}
}

// FinishWithError 将取消和失败区分为不同终态，入口层不得直接持久化原始错误文本。
func FinishWithError(lifecycle RequestLifecycle, ctx context.Context, err error) {
	if lifecycle == nil {
		return
	}
	if errors.Is(err, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
		ReportPersistenceError(lifecycle.Cancel(ctx, "client_cancelled"))
		return
	}
	ReportPersistenceError(lifecycle.Fail(ctx, FailureFromError(err)))
}
