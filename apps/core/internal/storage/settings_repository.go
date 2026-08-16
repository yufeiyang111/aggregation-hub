package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrUnknownSettingKey    = errors.New("未知设置项")
	ErrInvalidSettingValue  = errors.New("设置值不是有效 JSON")
	ErrCorruptSettingValue  = errors.New("存储的设置值已损坏")
	ErrSettingNotFound      = errors.New("设置项不存在")
	ErrInvalidSettingsStore = errors.New("设置仓储数据库无效")
	ErrStaleSettings        = errors.New("设置已被其他操作更新")
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

// RuntimeSettingsSnapshot 是受控运行时设置的一致读取快照。
type RuntimeSettingsSnapshot struct {
	Values  map[string]json.RawMessage
	Version int64
}

// SettingsRepository 只允许保存受控的非秘密应用设置。
type SettingsRepository interface {
	Get(ctx context.Context, key string) (json.RawMessage, error)
	Set(ctx context.Context, key string, value json.RawMessage, now time.Time) error
	ReadRuntime(ctx context.Context, keys []string) (RuntimeSettingsSnapshot, error)
	UpdateRuntime(ctx context.Context, values map[string]json.RawMessage, expectedVersion int64, now time.Time) (RuntimeSettingsSnapshot, error)
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
	return validatedSettingValue(key, value)
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

// ReadRuntime 以一个事务读取指定运行时键和乐观锁版本，避免 WebView 读到半套配置。
func (repository settingsRepository) ReadRuntime(ctx context.Context, keys []string) (RuntimeSettingsSnapshot, error) {
	if ctx == nil {
		return RuntimeSettingsSnapshot{}, errors.New("设置读取上下文不能为空")
	}
	if err := validateRuntimeKeys(keys); err != nil {
		return RuntimeSettingsSnapshot{}, err
	}

	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("开始运行时设置读取事务失败: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	snapshot := RuntimeSettingsSnapshot{Values: make(map[string]json.RawMessage, len(keys))}
	var version int64
	err = transaction.QueryRowContext(ctx, `SELECT version FROM runtime_settings_revision WHERE singleton=1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		version = 0
	} else if err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("读取运行时设置版本失败: %w", err)
	}
	snapshot.Version = version

	for _, key := range sortedSettingKeys(keys) {
		var value string
		err := transaction.QueryRowContext(ctx, `SELECT value_json FROM app_settings WHERE key=?`, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return RuntimeSettingsSnapshot{}, fmt.Errorf("读取运行时设置失败: %w", err)
		}
		decoded, err := validatedSettingValue(key, value)
		if err != nil {
			return RuntimeSettingsSnapshot{}, err
		}
		snapshot.Values[key] = decoded
	}
	if err := transaction.Commit(); err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("提交运行时设置读取事务失败: %w", err)
	}
	return snapshot, nil
}

// UpdateRuntime 使用单例版本实现受控运行时设置的原子更新和冲突检测。
func (repository settingsRepository) UpdateRuntime(ctx context.Context, values map[string]json.RawMessage, expectedVersion int64, now time.Time) (RuntimeSettingsSnapshot, error) {
	if ctx == nil {
		return RuntimeSettingsSnapshot{}, errors.New("设置写入上下文不能为空")
	}
	if expectedVersion < 0 || len(values) == 0 {
		return RuntimeSettingsSnapshot{}, ErrInvalidSettingValue
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if err := validateSettingKey(key); err != nil {
			return RuntimeSettingsSnapshot{}, err
		}
		if !json.Valid(value) {
			return RuntimeSettingsSnapshot{}, fmt.Errorf("%w: %s", ErrInvalidSettingValue, key)
		}
		keys = append(keys, key)
	}
	keys = sortedSettingKeys(keys)

	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("开始运行时设置写入事务失败: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var currentVersion int64
	err = transaction.QueryRowContext(ctx, `SELECT version FROM runtime_settings_revision WHERE singleton=1`).Scan(&currentVersion)
	exists := true
	if errors.Is(err, sql.ErrNoRows) {
		currentVersion, exists = 0, false
	} else if err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("读取运行时设置版本失败: %w", err)
	}
	if currentVersion != expectedVersion {
		return RuntimeSettingsSnapshot{}, ErrStaleSettings
	}

	nextVersion := currentVersion + 1
	if exists {
		result, err := transaction.ExecContext(ctx, `UPDATE runtime_settings_revision SET version=?,updated_at=? WHERE singleton=1 AND version=?`, nextVersion, now.UTC().UnixMilli(), expectedVersion)
		if err != nil {
			return RuntimeSettingsSnapshot{}, fmt.Errorf("更新运行时设置版本失败: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return RuntimeSettingsSnapshot{}, fmt.Errorf("读取运行时设置更新结果失败: %w", err)
		}
		if affected != 1 {
			return RuntimeSettingsSnapshot{}, ErrStaleSettings
		}
	} else if _, err := transaction.ExecContext(ctx, `INSERT INTO runtime_settings_revision(singleton,version,updated_at) VALUES(1,?,?)`, nextVersion, now.UTC().UnixMilli()); err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("创建运行时设置版本失败: %w", err)
	}

	for _, key := range keys {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO app_settings(key,value_json,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at`, key, string(values[key]), now.UTC().UnixMilli()); err != nil {
			return RuntimeSettingsSnapshot{}, fmt.Errorf("写入运行时设置失败: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return RuntimeSettingsSnapshot{}, fmt.Errorf("提交运行时设置写入事务失败: %w", err)
	}
	copied := make(map[string]json.RawMessage, len(values))
	for _, key := range keys {
		copied[key] = append(json.RawMessage(nil), values[key]...)
	}
	return RuntimeSettingsSnapshot{Values: copied, Version: nextVersion}, nil
}

func validatedSettingValue(key string, value string) (json.RawMessage, error) {
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("%w: %s", ErrCorruptSettingValue, key)
	}
	return json.RawMessage(append([]byte(nil), value...)), nil
}

func validateRuntimeKeys(keys []string) error {
	if len(keys) == 0 {
		return ErrUnknownSettingKey
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if err := validateSettingKey(key); err != nil {
			return err
		}
		if _, duplicate := seen[key]; duplicate {
			return ErrUnknownSettingKey
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sortedSettingKeys(keys []string) []string {
	result := append([]string(nil), keys...)
	sort.Strings(result)
	return result
}

func validateSettingKey(key string) error {
	if _, allowed := allowedSettingKeys[key]; !allowed {
		return fmt.Errorf("%w: %s", ErrUnknownSettingKey, key)
	}
	return nil
}
