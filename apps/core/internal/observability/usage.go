package observability

import (
	"math/bits"
	"time"
)

// DailyUsageKey 标识一个 Provider/模型在一个 UTC 日期内的脱敏用量汇总。
type DailyUsageKey struct {
	DateUTC              string
	ProviderSlugSnapshot string
	PublicModelSnapshot  string
}

// DailyUsage 保存 Token 与请求计数；reported_count 用于区分上游未知与真实的零 Token。
type DailyUsage struct {
	DailyUsageKey
	RequestCount                   int64
	SucceededCount                 int64
	FailedCount                    int64
	CancelledCount                 int64
	InputTokens                    int64
	OutputTokens                   int64
	CachedInputTokens              int64
	CacheWriteTokens               int64
	ReasoningTokens                int64
	InputTokenReportedCount        int64
	OutputTokenReportedCount       int64
	CachedInputTokenReportedCount  int64
	ReasoningTokenReportedCount    int64
	CacheEligibleInputTokens       int64
	CacheEligibleCachedInputTokens int64
	UpdatedAt                      time.Time
}

// CacheHitRateBasisPoints 返回 0~10000 的整数百分比基点；未知或上游不一致时 known=false。
func CacheHitRateBasisPoints(cachedInputTokens, eligibleInputTokens int64) (basisPoints int64, known bool) {
	if cachedInputTokens < 0 || eligibleInputTokens <= 0 || cachedInputTokens > eligibleInputTokens {
		return 0, false
	}
	hi, lo := bits.Mul64(uint64(cachedInputTokens), 10_000)
	quotient, _ := bits.Div64(hi, lo, uint64(eligibleInputTokens))
	return int64(quotient), true
}

func (value DailyUsage) CacheHitRateBasisPoints() (int64, bool) {
	return CacheHitRateBasisPoints(value.CacheEligibleCachedInputTokens, value.CacheEligibleInputTokens)
}
