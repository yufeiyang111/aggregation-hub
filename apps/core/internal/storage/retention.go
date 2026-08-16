package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const retentionDeleteBatchSize = 500

type RetentionRepository struct {
	database *sql.DB
}

type RetentionResult struct {
	DeletedRequests int64     `json:"deleted_requests"`
	Batches         int       `json:"batches"`
	Cutoff          time.Time `json:"cutoff"`
}

func NewRetentionRepository(database *sql.DB) (*RetentionRepository, error) {
	if database == nil {
		return nil, errors.New("数据保留仓储数据库无效")
	}
	return &RetentionRepository{database: database}, nil
}

// PruneRequests 只删除已终态且已完成的请求元数据。日用量在请求进入终态的同一事务中已聚合，清理不会重算或丢弃 Token 汇总。
func (repository *RetentionRepository) PruneRequests(ctx context.Context, retentionDays int, now time.Time) (RetentionResult, error) {
	if ctx == nil || retentionDays < 1 || retentionDays > 3650 || now.IsZero() {
		return RetentionResult{}, errors.New("数据保留参数无效")
	}
	result := RetentionResult{Cutoff: now.UTC().AddDate(0, 0, -retentionDays)}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		deleted, err := repository.pruneRequestBatch(ctx, retentionDays, result.Cutoff, now)
		if err != nil {
			return result, err
		}
		if deleted == 0 {
			return result, nil
		}
		result.DeletedRequests += deleted
		result.Batches++
	}
}

func (repository *RetentionRepository) pruneRequestBatch(ctx context.Context, retentionDays int, cutoff time.Time, now time.Time) (int64, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始请求保留事务失败: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	deleted, err := transaction.ExecContext(ctx, `DELETE FROM requests WHERE id IN (
		SELECT id FROM requests
		WHERE completed_at IS NOT NULL
		  AND completed_at < ?
		  AND status IN ('succeeded','failed','cancelled','aborted_by_restart')
		ORDER BY completed_at ASC, id ASC
		LIMIT ?
	)`, cutoff.UTC().UnixMilli(), retentionDeleteBatchSize)
	if err != nil {
		return 0, fmt.Errorf("删除过期请求记录失败: %w", err)
	}
	affected, err := deleted.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("读取请求保留结果失败: %w", err)
	}
	if affected > 0 {
		if err := writeMaintenanceAudit(ctx, transaction, "request_retention_pruned", map[string]any{
			"deleted_requests": affected,
			"retention_days":   retentionDays,
		}, now); err != nil {
			return 0, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("提交请求保留事务失败: %w", err)
	}
	return affected, nil
}
