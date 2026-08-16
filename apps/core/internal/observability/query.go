package observability

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidRequestQuery = errors.New("请求查询参数无效")
	ErrRequestNotFound     = errors.New("请求记录不存在")
)

// RequestListQuery 限制请求记录查询为固定排序和有界筛选条件。
type RequestListQuery struct {
	PageSize       int
	Cursor         string
	Status         RequestStatus
	ProviderSlug   string
	PublicModelID  string
	SourceProtocol SourceProtocol
	FromUTC        *time.Time
	ToUTC          *time.Time
}

// RequestMetadata 是可安全返回到控制面和桌面端的请求投影。
type RequestMetadata struct {
	ID                string         `json:"id"`
	CreatedAt         time.Time      `json:"created_at"`
	CompletedAt       *time.Time     `json:"completed_at"`
	SourceProtocol    SourceProtocol `json:"source_protocol"`
	ProviderSlug      string         `json:"provider_slug"`
	PublicModelID     string         `json:"public_model_id"`
	Streaming         bool           `json:"streaming"`
	Status            RequestStatus  `json:"status"`
	HTTPStatus        *int           `json:"http_status"`
	ErrorCode         *string        `json:"error_code"`
	Retryable         bool           `json:"retryable"`
	InputTokens       *int64         `json:"input_tokens"`
	OutputTokens      *int64         `json:"output_tokens"`
	CachedInputTokens *int64         `json:"cached_input_tokens"`
	CacheWriteTokens  *int64         `json:"cache_write_tokens"`
	ReasoningTokens   *int64         `json:"reasoning_tokens"`
	DurationMS        *int64         `json:"duration_ms"`
}

type RequestPage struct {
	Data       []RequestMetadata `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}

// UsageQuery 按 UTC 时间窗和已知快照筛选日汇总。
type UsageQuery struct {
	ProviderSlug  string
	PublicModelID string
	FromUTC       time.Time
	ToUTC         time.Time
}

// UsageSummary 只包含请求计数和 Token，不包含金额或价格字段。
type UsageSummary struct {
	RequestCount                   int64  `json:"request_count"`
	SucceededCount                 int64  `json:"succeeded_count"`
	FailedCount                    int64  `json:"failed_count"`
	CancelledCount                 int64  `json:"cancelled_count"`
	InputTokens                    int64  `json:"input_tokens"`
	OutputTokens                   int64  `json:"output_tokens"`
	CachedInputTokens              int64  `json:"cached_input_tokens"`
	CacheWriteTokens               int64  `json:"cache_write_tokens"`
	ReasoningTokens                int64  `json:"reasoning_tokens"`
	InputTokenReportedCount        int64  `json:"input_token_reported_count"`
	OutputTokenReportedCount       int64  `json:"output_token_reported_count"`
	CachedInputTokenReportedCount  int64  `json:"cached_input_token_reported_count"`
	ReasoningTokenReportedCount    int64  `json:"reasoning_token_reported_count"`
	CacheEligibleInputTokens       int64  `json:"cache_eligible_input_tokens"`
	CacheEligibleCachedInputTokens int64  `json:"cache_eligible_cached_input_tokens"`
	CacheHitRateBasisPoints        *int64 `json:"cache_hit_rate_basis_points"`
}

type UsageTimeSeriesPoint struct {
	DateUTC string `json:"date_utc"`
	UsageSummary
}

type UsageTimeSeries struct {
	Data []UsageTimeSeriesPoint `json:"data"`
}

func ValidateRequestListQuery(query RequestListQuery) error {
	if query.PageSize < 1 || query.PageSize > 100 || len(query.Cursor) > 64 || len(query.ProviderSlug) > 128 || len(query.PublicModelID) > 512 {
		return ErrInvalidRequestQuery
	}
	if query.Status != "" && !isKnownRequestStatus(query.Status) {
		return ErrInvalidRequestQuery
	}
	if query.SourceProtocol != "" && query.SourceProtocol != ProtocolAnthropicMessages && query.SourceProtocol != ProtocolOpenAIResponses && query.SourceProtocol != ProtocolOpenAIChat {
		return ErrInvalidRequestQuery
	}
	if query.FromUTC != nil && query.ToUTC != nil && query.FromUTC.After(*query.ToUTC) {
		return ErrInvalidRequestQuery
	}
	if query.FromUTC != nil && query.ToUTC != nil && query.ToUTC.Sub(*query.FromUTC) > 366*24*time.Hour {
		return ErrInvalidRequestQuery
	}
	if query.Cursor != "" {
		if _, _, err := DecodeRequestCursor(query.Cursor); err != nil {
			return ErrInvalidRequestQuery
		}
	}
	return nil
}

func ValidateUsageQuery(query UsageQuery) error {
	if len(query.ProviderSlug) > 128 || len(query.PublicModelID) > 512 || query.FromUTC.IsZero() || query.ToUTC.IsZero() || query.FromUTC.After(query.ToUTC) || query.ToUTC.Sub(query.FromUTC) > 366*24*time.Hour {
		return ErrInvalidRequestQuery
	}
	return nil
}

func EncodeRequestCursor(createdAt time.Time, id string) string {
	value := fmt.Sprintf("%d|%s", createdAt.UTC().UnixMilli(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func DecodeRequestCursor(cursor string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", ErrInvalidRequestQuery
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[1] == "" || len(parts[1]) > 64 {
		return time.Time{}, "", ErrInvalidRequestQuery
	}
	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", ErrInvalidRequestQuery
	}
	return time.UnixMilli(millis).UTC(), parts[1], nil
}

func WithCacheHitRate(summary UsageSummary) UsageSummary {
	if rate, known := CacheHitRateBasisPoints(summary.CacheEligibleCachedInputTokens, summary.CacheEligibleInputTokens); known {
		summary.CacheHitRateBasisPoints = &rate
	}
	return summary
}

func isKnownRequestStatus(status RequestStatus) bool {
	return status == RequestStatusPending || status == RequestStatusStreaming || status == RequestStatusSucceeded || status == RequestStatusFailed || status == RequestStatusCancelled || status == RequestStatusAbortedByRestart
}
