package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"aggregationhub.local/core/internal/provider"
)

const healthRetentionWindow = 7 * 24 * time.Hour

type HealthRepository struct {
	database *sql.DB
	now      func() time.Time
}

func NewHealthRepository(database *sql.DB) (*HealthRepository, error) {
	if database == nil {
		return nil, errors.New("健康记录仓储数据库无效")
	}
	return &HealthRepository{database: database, now: time.Now}, nil
}
func (repository *HealthRepository) Record(ctx context.Context, value provider.HealthCheck) error {
	if ctx == nil || !validHealthCheck(value) {
		return provider.ErrInvalidProvider
	}
	if !provider.IsRetainableHealthCode(value.ErrorCode) {
		return provider.ErrInvalidProvider
	}
	_, err := repository.database.ExecContext(ctx, `INSERT INTO provider_health_checks(id,provider_id,check_type,status,latency_ms,error_code,checked_at) VALUES(?,?,?,?,?,?,?)`, value.ID, value.ProviderID, value.CheckType, value.Status, nullableHealthLatency(value.LatencyMS), nullableHealthError(value.ErrorCode), value.CheckedAt.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("写入健康检查失败: %w", err)
	}
	return repository.Prune(ctx)
}
func (repository *HealthRepository) Recent(ctx context.Context, providerID string, limit int) ([]provider.HealthCheck, error) {
	if ctx == nil || providerID == "" || limit < 1 || limit > 100 {
		return nil, provider.ErrInvalidProvider
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT id,provider_id,check_type,status,latency_ms,error_code,checked_at FROM provider_health_checks WHERE provider_id=? ORDER BY checked_at DESC,id DESC LIMIT ?`, providerID, limit)
	if err != nil {
		return nil, fmt.Errorf("读取健康检查失败: %w", err)
	}
	defer rows.Close()
	result := make([]provider.HealthCheck, 0)
	for rows.Next() {
		var item provider.HealthCheck
		var latency sql.NullInt64
		var code sql.NullString
		var checked int64
		if err := rows.Scan(&item.ID, &item.ProviderID, &item.CheckType, &item.Status, &latency, &code, &checked); err != nil {
			return nil, err
		}
		item.LatencyMS = healthLatencyPointer(latency)
		item.ErrorCode = code.String
		item.CheckedAt = time.UnixMilli(checked).UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}
func (repository *HealthRepository) Prune(ctx context.Context) error {
	if ctx == nil {
		return provider.ErrInvalidProvider
	}
	_, err := repository.database.ExecContext(ctx, `DELETE FROM provider_health_checks WHERE checked_at<?`, repository.now().UTC().Add(-healthRetentionWindow).UnixMilli())
	return err
}
func validHealthCheck(value provider.HealthCheck) bool {
	if value.ID == "" || value.ProviderID == "" || value.CheckedAt.IsZero() {
		return false
	}
	if value.CheckType != provider.HealthCheckConnection && value.CheckType != provider.HealthCheckModels && value.CheckType != provider.HealthCheckCompletion {
		return false
	}
	if value.Status != provider.HealthCheckSucceeded && value.Status != provider.HealthCheckFailed && value.Status != provider.HealthCheckSkipped {
		return false
	}
	return value.LatencyMS == nil || *value.LatencyMS >= 0
}
func nullableHealthLatency(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
func nullableHealthError(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func healthLatencyPointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
