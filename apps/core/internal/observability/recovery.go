package observability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RecoverInFlightRequests 将异常退出时未完成的请求标记为可审计的终态，不改写已完成请求。
func RecoverInFlightRequests(ctx context.Context, database *sql.DB, now time.Time) (int64, error) {
	if ctx == nil || database == nil {
		return 0, errors.New("请求恢复依赖无效")
	}
	result, err := database.ExecContext(ctx, `UPDATE requests SET status='aborted_by_restart',error_code='aborted_by_restart',retryable=0,completed_at=? WHERE status IN ('pending','streaming')`, now.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("恢复未完成请求失败: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("读取请求恢复结果失败: %w", err)
	}
	return count, nil
}
