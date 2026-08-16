package observability

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// SafeLoggingRecorder 在既有请求生命周期外层记录失败摘要，避免入口层接触诊断缓冲实现。
type SafeLoggingRecorder struct {
	delegate RequestRecorder
	logger   *SafeLogger
	clock    func() time.Time
}

func NewSafeLoggingRecorder(delegate RequestRecorder, logger *SafeLogger) (*SafeLoggingRecorder, error) {
	if delegate == nil || logger == nil {
		return nil, errors.New("安全日志 Recorder 依赖无效")
	}

	return &SafeLoggingRecorder{
		delegate: delegate,
		logger:   logger,
		clock:    time.Now,
	}, nil
}

func (recorder *SafeLoggingRecorder) Start(ctx context.Context, start RequestStart) (RequestLifecycle, error) {
	lifecycle, err := recorder.delegate.Start(ctx, start)
	if err != nil {
		return nil, err
	}
	if lifecycle == nil {
		return nil, errors.New("请求生命周期无效")
	}

	return &safeLoggingLifecycle{
		delegate:  lifecycle,
		logger:    recorder.logger,
		startedAt: recorder.clock().UTC(),
		start:     start,
		clock:     recorder.clock,
	}, nil
}

type safeLoggingLifecycle struct {
	delegate  RequestLifecycle
	logger    *SafeLogger
	startedAt time.Time
	start     RequestStart
	clock     func() time.Time
	mutex     sync.Mutex
	terminal  bool
}

func (lifecycle *safeLoggingLifecycle) MarkStreaming(ctx context.Context) error {
	return lifecycle.delegate.MarkStreaming(ctx)
}

func (lifecycle *safeLoggingLifecycle) Complete(ctx context.Context, completion Completion) error {
	if !lifecycle.claimTerminal() {
		return ErrInvalidRequestTransition
	}

	return lifecycle.delegate.Complete(ctx, completion)
}

func (lifecycle *safeLoggingLifecycle) Fail(ctx context.Context, failure Failure) error {
	if !lifecycle.claimTerminal() {
		return ErrInvalidRequestTransition
	}

	lifecycle.recordFailure(failure)
	return lifecycle.delegate.Fail(ctx, failure)
}

func (lifecycle *safeLoggingLifecycle) recordFailure(failure Failure) {
	providerSlug := ""
	if provider, _, found := strings.Cut(lifecycle.start.PublicModelSnapshot, "/"); found {
		providerSlug = provider
	}
	occurredAt := lifecycle.clock().UTC()
	lifecycle.logger.Record(SafeErrorEvent{
		EventCode:      "gateway.request_failed",
		ErrorCode:      failure.Code,
		ProviderSlug:   providerSlug,
		PublicModelID:  lifecycle.start.PublicModelSnapshot,
		SourceProtocol: string(lifecycle.start.SourceProtocol),
		HTTPStatus:     failure.HTTPStatus,
		DurationMS:     elapsedMilliseconds(lifecycle.startedAt, occurredAt),
		OccurredAt:     occurredAt,
	})
}

func (lifecycle *safeLoggingLifecycle) Cancel(ctx context.Context, code string) error {
	if !lifecycle.claimTerminal() {
		return ErrInvalidRequestTransition
	}

	return lifecycle.delegate.Cancel(ctx, code)
}

func (lifecycle *safeLoggingLifecycle) claimTerminal() bool {
	lifecycle.mutex.Lock()
	defer lifecycle.mutex.Unlock()
	if lifecycle.terminal {
		return false
	}

	lifecycle.terminal = true
	return true
}

func elapsedMilliseconds(startedAt time.Time, occurredAt time.Time) int64 {
	if startedAt.IsZero() || occurredAt.Before(startedAt) {
		return 0
	}

	return occurredAt.Sub(startedAt).Milliseconds()
}
