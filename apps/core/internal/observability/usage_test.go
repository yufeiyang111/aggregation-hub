package observability_test

import (
	"math"
	"testing"

	"aggregationhub.local/core/internal/observability"
)

func TestCacheHitRateBasisPointsDistinguishesUnknownAndZero(t *testing.T) {
	cases := []struct {
		name     string
		cached   int64
		eligible int64
		want     int64
		known    bool
	}{
		{name: "正常命中", cached: 25, eligible: 100, want: 2500, known: true},
		{name: "零命中", cached: 0, eligible: 100, want: 0, known: true},
		{name: "未知分母", cached: 0, eligible: 0, known: false},
		{name: "上游不一致", cached: 101, eligible: 100, known: false},
		{name: "最大整数", cached: math.MaxInt64, eligible: math.MaxInt64, want: 10000, known: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, known := observability.CacheHitRateBasisPoints(testCase.cached, testCase.eligible)
			if actual != testCase.want || known != testCase.known {
				t.Fatalf("rate=%d known=%v，期望 rate=%d known=%v", actual, known, testCase.want, testCase.known)
			}
		})
	}
}
