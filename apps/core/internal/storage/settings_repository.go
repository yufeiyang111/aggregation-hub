package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnknownSettingKey    = errors.New("未知设置项")
	ErrInvalidSettingValue  = errors.New("设置值不是有效 JSON")
	ErrCorruptSettingValue  = errors.New("存储的设置值已损坏")
	ErrSettingNotFound      = errors.New("设置项不存在")
	ErrInvalidSettingsStore = errors.New("设置仓储数据库无效")
)

var allowedSettingKeys = map[string]struct{}{
	"gateway.listen_port":        {},
	"gateway.request_timeout_ms": {},
	"retention.request_days":     {},
	"retention.log_days":         {},
	"desktop.start_on_login":     {},
	"ui.theme":                   {},
	"ui.locale":                  {},
}

// SettingsRepository 只允许保存受控的非秘密应用设置。
type SettingsRepository interface {
	Get(ctx context.Context, key string) (json.RawMessage, error)
	Set(ctx context.Context, key string, value json.RawMessage, now time.Time) error
}

type settingsRepository struct {
	database *sql.DB
}

func NewSettingsRepository(database *sql.DB) (SettingsRepository, error) {
	if database == nil {
		return nil, ErrInvalidSettingsStore
	}
	return settingsRepository{database: database}, nil
}

func (repository settingsRepository) Get(ctx context.Context, key string) (json.RawMessage, error) {
	if err := validateSettingKey(key); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("设置读取上下文不能为空")
	}

	var value string
	err := repository.database.QueryRowContext(ctx, `SELECT value_json FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrSettingNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("读取设置失败: %w", err)
	}
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("%w: %s", ErrCorruptSettingValue, key)
	}
	return json.RawMessage(append([]byte(nil), value...)), nil
}

func (repository settingsRepository) Set(ctx context.Context, key string, value json.RawMessage, now time.Time) error {
	if err := validateSettingKey(key); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("设置写入上下文不能为空")
	}
	if !json.Valid(value) {
		return fmt.Errorf("%w: %s", ErrInvalidSettingValue, key)
	}

	_, err := repository.database.ExecContext(ctx, `
		INSERT INTO app_settings(key, value_json, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key,
		string(value),
		now.UTC().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("写入设置失败: %w", err)
	}
	return nil
}

func validateSettingKey(key string) error {
	if _, allowed := allowedSettingKeys[key]; !allowed {
		return fmt.Errorf("%w: %s", ErrUnknownSettingKey, key)
	}
	return nil
}
