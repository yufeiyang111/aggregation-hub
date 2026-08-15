package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"aggregationhub.local/core/internal/security"
)

type LocalKeyRepository struct{ database *sql.DB }

func NewLocalKeyRepository(database *sql.DB) (*LocalKeyRepository, error) {
	if database == nil {
		return nil, errors.New("Local Access Key 仓储数据库无效")
	}
	return &LocalKeyRepository{database: database}, nil
}

func (repository *LocalKeyRepository) Create(ctx context.Context, record security.LocalKeyRecord) error {
	if ctx == nil {
		return errors.New("保存 Local Access Key 的上下文不能为空")
	}
	if len(record.TokenHash) != security.LocalKeyHashBytes {
		return errors.New("Local Access Key 哈希长度无效")
	}
	_, err := repository.database.ExecContext(ctx, `INSERT INTO local_access_keys(id,name,token_hash,token_prefix,token_suffix,status,created_at,last_used_at,expires_at,revoked_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, record.ID, record.Name, record.TokenHash, record.Prefix, record.Suffix, record.Status, record.CreatedAt.UTC().UnixMilli(), nullableTime(record.LastUsedAt), nullableTime(record.ExpiresAt), nullableTime(record.RevokedAt))
	if err != nil {
		return fmt.Errorf("保存 Local Access Key 失败: %w", err)
	}
	return nil
}

func (repository *LocalKeyRepository) FindActiveByPrefix(ctx context.Context, prefix string, now time.Time) ([]security.LocalKeyRecord, error) {
	if ctx == nil {
		return nil, errors.New("读取 Local Access Key 的上下文不能为空")
	}
	nowMilliseconds := now.UTC().UnixMilli()
	if _, err := repository.database.ExecContext(ctx, `UPDATE local_access_keys SET status='expired' WHERE status='active' AND expires_at IS NOT NULL AND expires_at <= ?`, nowMilliseconds); err != nil {
		return nil, fmt.Errorf("更新过期 Local Access Key 失败: %w", err)
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT id,name,token_hash,token_prefix,token_suffix,status,created_at,last_used_at,expires_at,revoked_at FROM local_access_keys WHERE token_prefix=? AND status='active' AND (expires_at IS NULL OR expires_at > ?)`, prefix, nowMilliseconds)
	if err != nil {
		return nil, fmt.Errorf("查询 Local Access Key 候选失败: %w", err)
	}
	defer rows.Close()
	var records []security.LocalKeyRecord
	for rows.Next() {
		record, err := scanLocalKeyRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 Local Access Key 候选失败: %w", err)
	}
	return records, nil
}

func (repository *LocalKeyRepository) Revoke(ctx context.Context, id string, now time.Time) error {
	result, err := repository.database.ExecContext(ctx, `UPDATE local_access_keys SET status='revoked', revoked_at=? WHERE id=? AND status='active'`, now.UTC().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("吊销 Local Access Key 失败: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Local Access Key 吊销结果失败: %w", err)
	}
	if affected != 1 {
		return security.ErrLocalKeyNotFound
	}
	return nil
}

func (repository *LocalKeyRepository) MarkUsed(ctx context.Context, id string, now time.Time) error {
	result, err := repository.database.ExecContext(ctx, `UPDATE local_access_keys SET last_used_at=? WHERE id=? AND status='active'`, now.UTC().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("更新 Local Access Key 使用时间失败: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Local Access Key 使用结果失败: %w", err)
	}
	if affected != 1 {
		return security.ErrLocalKeyNotFound
	}
	return nil
}

type localKeyScanner interface{ Scan(...any) error }

func scanLocalKeyRecord(scanner localKeyScanner) (security.LocalKeyRecord, error) {
	var record security.LocalKeyRecord
	var createdAt int64
	var lastUsed, expires, revoked sql.NullInt64
	if err := scanner.Scan(&record.ID, &record.Name, &record.TokenHash, &record.Prefix, &record.Suffix, &record.Status, &createdAt, &lastUsed, &expires, &revoked); err != nil {
		return security.LocalKeyRecord{}, fmt.Errorf("读取 Local Access Key 失败: %w", err)
	}
	record.TokenHash = append([]byte(nil), record.TokenHash...)
	record.CreatedAt = time.UnixMilli(createdAt).UTC()
	record.LastUsedAt = timePointer(lastUsed)
	record.ExpiresAt = timePointer(expires)
	record.RevokedAt = timePointer(revoked)
	return record, nil
}
func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMilli()
}
func timePointer(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64).UTC()
	return &result
}
