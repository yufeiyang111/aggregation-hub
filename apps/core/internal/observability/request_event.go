package observability

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"aggregationhub.local/core/internal/id"
	"aggregationhub.local/core/internal/normalize"
)

const defaultPersistenceTimeout = time.Second

var (
	ErrInvalidRequestTransition = errors.New("请求状态转换无效")
	ErrInvalidRequestRecord     = errors.New("请求元数据无效")
)

type RequestStatus string

const (
	RequestStatusPending          RequestStatus = "pending"
	RequestStatusStreaming        RequestStatus = "streaming"
	RequestStatusSucceeded        RequestStatus = "succeeded"
	RequestStatusFailed           RequestStatus = "failed"
	RequestStatusCancelled        RequestStatus = "cancelled"
	RequestStatusAbortedByRestart RequestStatus = "aborted_by_restart"
)

type SourceProtocol string

const (
	ProtocolAnthropicMessages SourceProtocol = "anthropic_messages"
	ProtocolOpenAIResponses   SourceProtocol = "openai_responses"
	ProtocolOpenAIChat        SourceProtocol = "openai_chat"
)

// RequestRecord 是可持久化的脱敏请求元数据；类型中不包含正文、Header、Tool 参数或上游错误正文。
type RequestRecord struct {
	ID                    string
	ProviderSlugSnapshot  string
	PublicModelSnapshot   string
	UpstreamModelSnapshot string
	SourceProtocol        SourceProtocol
	Endpoint              string
	Streaming             bool
	Status                RequestStatus
	CreatedAt             time.Time
}

type RequestStart struct {
	SourceProtocol      SourceProtocol
	Endpoint            string
	PublicModelSnapshot string
	Streaming           bool
}

// RequestTransition 只描述状态、计量和安全错误类别，禁止扩展为诊断正文载体。
type RequestTransition struct {
	ID         string
	From       RequestStatus
	Status     RequestStatus
	HTTPStatus int
	ErrorCode  string
	Retryable  bool
	Usage      *normalize.Usage
	At         time.Time
}

type Completion struct {
	HTTPStatus int
	Usage      *normalize.Usage
}

type Failure struct {
	HTTPStatus int
	Code       string
	Retryable  bool
}

// RequestStore 由 Storage 层实现，使生命周期规则不依赖具体 SQLite 实现。
type RequestStore interface {
	Create(context.Context, RequestRecord) error
	Transition(context.Context, RequestTransition) error
}

// RequestRecorder 是入口层唯一需要的观测边界。
type RequestRecorder interface {
	Start(context.Context, RequestStart) (RequestLifecycle, error)
}

type RequestLifecycle interface {
	MarkStreaming(context.Context) error
	Complete(context.Context, Completion) error
	Fail(context.Context, Failure) error
	Cancel(context.Context, string) error
}

type RecorderOptions struct {
	Clock              func() time.Time
	NewID              func(time.Time) (string, error)
	PersistenceTimeout time.Duration
}

type Recorder struct {
	store              RequestStore
	clock              func() time.Time
	newID              func(time.Time) (string, error)
	persistenceTimeout time.Duration
}

func NewRecorder(store RequestStore, options RecorderOptions) (*Recorder, error) {
	if store == nil {
		return nil, errors.New("请求观测仓储不能为空")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.NewID == nil {
		options.NewID = id.RandomULID
	}
	if options.PersistenceTimeout <= 0 {
		options.PersistenceTimeout = defaultPersistenceTimeout
	}
	return &Recorder{store: store, clock: options.Clock, newID: options.NewID, persistenceTimeout: options.PersistenceTimeout}, nil
}

func (recorder *Recorder) Start(ctx context.Context, input RequestStart) (RequestLifecycle, error) {
	now := recorder.clock().UTC()
	requestID, err := recorder.newID(now)
	if err != nil {
		return nil, fmt.Errorf("生成请求 ID 失败: %w", err)
	}
	providerSlug, upstreamModel, err := splitPublicModelID(input.PublicModelSnapshot)
	if err != nil {
		return nil, err
	}
	record := RequestRecord{
		ID:                    requestID,
		ProviderSlugSnapshot:  providerSlug,
		PublicModelSnapshot:   input.PublicModelSnapshot,
		UpstreamModelSnapshot: upstreamModel,
		SourceProtocol:        input.SourceProtocol,
		Endpoint:              input.Endpoint,
		Streaming:             input.Streaming,
		Status:                RequestStatusPending,
		CreatedAt:             now,
	}
	if err := ValidateRequestRecord(record); err != nil {
		return nil, err
	}
	persistenceContext, cancel := recorder.persistenceContext(ctx)
	defer cancel()
	if err := recorder.store.Create(persistenceContext, record); err != nil {
		return nil, fmt.Errorf("创建请求观测记录失败: %w", err)
	}
	return &lifecycle{recorder: recorder, record: record, status: RequestStatusPending}, nil
}

func (recorder *Recorder) persistenceContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, recorder.persistenceTimeout)
}

type lifecycle struct {
	recorder *Recorder
	record   RequestRecord
	status   RequestStatus
	terminal bool
	mutex    sync.Mutex
}

func (value *lifecycle) MarkStreaming(ctx context.Context) error {
	return value.transition(ctx, RequestStatusStreaming, 0, "", false, nil)
}

func (value *lifecycle) Complete(ctx context.Context, completion Completion) error {
	status := completion.HTTPStatus
	if status == 0 {
		status = 200
	}
	return value.transition(ctx, RequestStatusSucceeded, status, "", false, completion.Usage)
}

func (value *lifecycle) Fail(ctx context.Context, failure Failure) error {
	status := failure.HTTPStatus
	if status == 0 {
		status = 502
	}
	return value.transition(ctx, RequestStatusFailed, status, safeErrorCode(failure.Code, "gateway_error"), failure.Retryable, nil)
}

func (value *lifecycle) Cancel(ctx context.Context, code string) error {
	return value.transition(ctx, RequestStatusCancelled, 0, safeErrorCode(code, "client_cancelled"), false, nil)
}

func (value *lifecycle) transition(ctx context.Context, target RequestStatus, httpStatus int, errorCode string, retryable bool, usage *normalize.Usage) error {
	value.mutex.Lock()
	defer value.mutex.Unlock()
	if value.terminal {
		return ErrInvalidRequestTransition
	}
	if err := ValidateRequestTransition(value.status, target); err != nil {
		return err
	}
	now := value.recorder.clock().UTC()
	transition := RequestTransition{ID: value.record.ID, From: value.status, Status: target, HTTPStatus: httpStatus, ErrorCode: errorCode, Retryable: retryable, Usage: cloneUsage(usage), At: now}
	persistenceContext, cancel := value.recorder.persistenceContext(ctx)
	defer cancel()
	if err := value.recorder.store.Transition(persistenceContext, transition); err != nil {
		return fmt.Errorf("写入请求状态失败: %w", err)
	}
	value.status = target
	value.terminal = isTerminalStatus(target)
	return nil
}

func ValidateRequestTransition(from, to RequestStatus) error {
	allowed := (from == RequestStatusPending && (to == RequestStatusStreaming || isTerminalStatus(to))) || (from == RequestStatusStreaming && isTerminalStatus(to))
	if !allowed {
		return ErrInvalidRequestTransition
	}
	return nil
}

func ValidateRequestTransitionData(transition RequestTransition) error {
	if err := ValidateRequestTransition(transition.From, transition.Status); err != nil || transition.ID == "" || transition.At.IsZero() {
		return ErrInvalidRequestTransition
	}
	if transition.HTTPStatus != 0 && (transition.HTTPStatus < 100 || transition.HTTPStatus > 599) {
		return ErrInvalidRequestTransition
	}
	if transition.ErrorCode != "" && safeErrorCode(transition.ErrorCode, "") != transition.ErrorCode {
		return ErrInvalidRequestTransition
	}
	if transition.Usage == nil {
		return nil
	}
	if transition.Usage.Source != normalize.UsageSourceUpstreamReported && transition.Usage.Source != normalize.UsageSourceLocallyEstimated && transition.Usage.Source != normalize.UsageSourceUnknown {
		return ErrInvalidRequestTransition
	}
	for _, tokens := range []*int64{transition.Usage.InputTokens, transition.Usage.OutputTokens, transition.Usage.CachedInputTokens, transition.Usage.CacheWriteTokens, transition.Usage.ReasoningTokens} {
		if tokens != nil && *tokens < 0 {
			return ErrInvalidRequestTransition
		}
	}
	return nil
}

func ValidateRequestRecord(record RequestRecord) error {
	if len(record.ID) == 0 || len(record.ID) > 64 || record.CreatedAt.IsZero() || record.Status != RequestStatusPending {
		return ErrInvalidRequestRecord
	}
	providerSlug, upstreamModel, err := splitPublicModelID(record.PublicModelSnapshot)
	if err != nil || record.ProviderSlugSnapshot != providerSlug || record.UpstreamModelSnapshot != upstreamModel || !validEndpoint(record.SourceProtocol, record.Endpoint) {
		return ErrInvalidRequestRecord
	}
	return nil
}

func splitPublicModelID(value string) (string, string, error) {
	providerSlug, upstreamModel, found := strings.Cut(strings.TrimSpace(value), "/")
	if !found || providerSlug == "" || upstreamModel == "" || len(value) > 304 {
		return "", "", ErrInvalidRequestRecord
	}
	return providerSlug, upstreamModel, nil
}

func validEndpoint(protocol SourceProtocol, endpoint string) bool {
	switch protocol {
	case ProtocolAnthropicMessages:
		return endpoint == "/v1/messages"
	case ProtocolOpenAIResponses:
		return endpoint == "/v1/responses"
	case ProtocolOpenAIChat:
		return endpoint == "/v1/chat/completions"
	default:
		return false
	}
}

func isTerminalStatus(status RequestStatus) bool {
	return status == RequestStatusSucceeded || status == RequestStatusFailed || status == RequestStatusCancelled || status == RequestStatusAbortedByRestart
}

func safeErrorCode(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return fallback
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.') {
			return fallback
		}
	}
	return value
}

func cloneUsage(value *normalize.Usage) *normalize.Usage {
	if value == nil {
		return nil
	}
	result := *value
	result.InputTokens = cloneInt64(value.InputTokens)
	result.OutputTokens = cloneInt64(value.OutputTokens)
	result.CachedInputTokens = cloneInt64(value.CachedInputTokens)
	result.CacheWriteTokens = cloneInt64(value.CacheWriteTokens)
	result.ReasoningTokens = cloneInt64(value.ReasoningTokens)
	return &result
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

type noopRecorder struct{}
type noopLifecycle struct{}

// ReportPersistenceError 只记录固定的安全提示，不输出错误对象、路径、请求 ID 或任何上游内容。
func ReportPersistenceError(err error) {
	if err != nil {
		log.Print("请求观测元数据写入失败")
	}
}

func NewNoopRecorder() RequestRecorder       { return noopRecorder{} }
func NoopRequestLifecycle() RequestLifecycle { return noopLifecycle{} }
func (noopRecorder) Start(context.Context, RequestStart) (RequestLifecycle, error) {
	return noopLifecycle{}, nil
}
func (noopLifecycle) MarkStreaming(context.Context) error        { return nil }
func (noopLifecycle) Complete(context.Context, Completion) error { return nil }
func (noopLifecycle) Fail(context.Context, Failure) error        { return nil }
func (noopLifecycle) Cancel(context.Context, string) error       { return nil }
