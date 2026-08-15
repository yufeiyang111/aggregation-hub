package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"aggregationhub.local/core/internal/storage"
)

func TestSettingsRepositoryStoresAllowedJSONValue(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewSettingsRepository(database)
	if err != nil {
		t.Fatalf("创建设置仓储失败: %v", err)
	}

	value := json.RawMessage(`{"theme":"dark"}`)
	updatedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	if err := repository.Set(context.Background(), "ui.theme", value, updatedAt); err != nil {
		t.Fatalf("写入允许的设置失败: %v", err)
	}

	actual, err := repository.Get(context.Background(), "ui.theme")
	if err != nil {
		t.Fatalf("读取已写入的设置失败: %v", err)
	}
	if string(actual) != string(value) {
		t.Fatalf("设置值=%s，期望 %s", actual, value)
	}
}

func TestSettingsRepositoryRejectsUnknownKeyAndInvalidJSON(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewSettingsRepository(database)
	if err != nil {
		t.Fatalf("创建设置仓储失败: %v", err)
	}

	if err := repository.Set(context.Background(), "credential.secret", json.RawMessage(`"not allowed"`), time.Now().UTC()); !errors.Is(err, storage.ErrUnknownSettingKey) {
		t.Fatalf("未知设置 key 错误=%v，期望 ErrUnknownSettingKey", err)
	}
	if _, err := repository.Get(context.Background(), "credential.secret"); !errors.Is(err, storage.ErrUnknownSettingKey) {
		t.Fatalf("未知设置 key 读取错误=%v，期望 ErrUnknownSettingKey", err)
	}
	if err := repository.Set(context.Background(), "ui.locale", json.RawMessage(`{"broken"`), time.Now().UTC()); !errors.Is(err, storage.ErrInvalidSettingValue) {
		t.Fatalf("非法 JSON 错误=%v，期望 ErrInvalidSettingValue", err)
	}
}

func TestSettingsRepositoryRejectsCorruptedStoredJSON(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewSettingsRepository(database)
	if err != nil {
		t.Fatalf("创建设置仓储失败: %v", err)
	}

	if _, err := database.Exec("INSERT INTO app_settings(key, value_json, updated_at) VALUES (?, ?, ?)", "ui.locale", "{broken", time.Now().UTC().UnixMilli()); err != nil {
		t.Fatalf("写入损坏测试数据失败: %v", err)
	}

	if _, err := repository.Get(context.Background(), "ui.locale"); !errors.Is(err, storage.ErrCorruptSettingValue) {
		t.Fatalf("损坏设置错误=%v，期望 ErrCorruptSettingValue", err)
	}
}

func TestSettingsRepositoryReportsMissingAllowedValue(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewSettingsRepository(database)
	if err != nil {
		t.Fatalf("创建设置仓储失败: %v", err)
	}

	if _, err := repository.Get(context.Background(), "gateway.listen_port"); !errors.Is(err, storage.ErrSettingNotFound) {
		t.Fatalf("缺失设置错误=%v，期望 ErrSettingNotFound", err)
	}
}
