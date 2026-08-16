package observability

import (
	"strings"
	"sync"
	"time"

	"aggregationhub.local/core/internal/security"
)

const DefaultSafeLoggerCapacity = 100

// SafeErrorEvent 是允许进入诊断缓冲的固定错误元数据，故意不包含原始错误文本、Header 或请求正文。
type SafeErrorEvent struct {
	EventCode      string
	ErrorCode      string
	ProviderSlug   string
	PublicModelID  string
	SourceProtocol string
	HTTPStatus     int
	DurationMS     int64
	OccurredAt     time.Time
}

// SafeErrorSummary 是可安全导出到诊断包的错误摘要。
type SafeErrorSummary struct {
	EventCode      string    `json:"event_code"`
	ErrorCode      string    `json:"error_code"`
	ProviderSlug   string    `json:"provider_slug,omitempty"`
	PublicModelID  string    `json:"public_model_id,omitempty"`
	SourceProtocol string    `json:"source_protocol,omitempty"`
	HTTPStatus     int       `json:"http_status,omitempty"`
	DurationMS     int64     `json:"duration_ms,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// SafeLogger 保存有限数量的结构化安全错误摘要，重启后不保留历史记录。
type SafeLogger struct {
	mutex    sync.RWMutex
	capacity int
	entries  []SafeErrorSummary
}

func NewSafeLogger(capacity int) *SafeLogger {
	if capacity <= 0 {
		capacity = DefaultSafeLoggerCapacity
	}
	return &SafeLogger{capacity: capacity, entries: make([]SafeErrorSummary, 0, capacity)}
}

// Record 仅接受受限字段；不安全格式会替换为稳定占位值，避免调用方借字段输出任意文本。
func (logger *SafeLogger) Record(event SafeErrorEvent) {
	if logger == nil {
		return
	}
	summary := SafeErrorSummary{
		EventCode:      safeLogIdentifier(event.EventCode),
		ErrorCode:      safeLogIdentifier(event.ErrorCode),
		ProviderSlug:   safeLogIdentifier(event.ProviderSlug),
		PublicModelID:  safeLogIdentifier(event.PublicModelID),
		SourceProtocol: safeLogIdentifier(event.SourceProtocol),
		HTTPStatus:     safeHTTPStatus(event.HTTPStatus),
		DurationMS:     safeDurationMS(event.DurationMS),
		OccurredAt:     event.OccurredAt.UTC(),
	}
	if summary.OccurredAt.IsZero() {
		summary.OccurredAt = time.Now().UTC()
	}
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	if len(logger.entries) == logger.capacity {
		copy(logger.entries, logger.entries[1:])
		logger.entries[len(logger.entries)-1] = summary
		return
	}
	logger.entries = append(logger.entries, summary)
}

// RecentErrors 返回独立副本，调用方不能修改内部诊断缓冲。
func (logger *SafeLogger) RecentErrors() []SafeErrorSummary {
	if logger == nil {
		return nil
	}
	logger.mutex.RLock()
	defer logger.mutex.RUnlock()
	return append([]SafeErrorSummary(nil), logger.entries...)
}

func safeLogIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 128 || security.ContainsDiagnosticSecret(value) {
		return "invalid"
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._/-", character) {
			continue
		}
		return "invalid"
	}
	return value
}

func safeHTTPStatus(value int) int {
	if value < 100 || value > 599 {
		return 0
	}
	return value
}

func safeDurationMS(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
