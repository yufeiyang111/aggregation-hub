package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aggregationhub.local/core/internal/security"
	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

func TestLocalKeyRepositoryStoresOnlyHashAndHonorsRevocation(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewLocalKeyRepository(database)
	if err != nil {
		t.Fatalf("创建 Local Key 仓储失败: %v", err)
	}

	plaintext := "ah_local_test_sentinel_not_persisted"
	hash := sha256.Sum256([]byte(plaintext))
	now := time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC)
	record := security.LocalKeyRecord{
		ID:        "01KTESTLOCALKEY00000000000",
		Name:      "test",
		TokenHash: hash[:],
		Prefix:    plaintext[:16],
		Suffix:    plaintext[len(plaintext)-6:],
		Status:    security.LocalKeyStatusActive,
		CreatedAt: now,
	}
	if err := repository.Create(context.Background(), record); err != nil {
		t.Fatalf("保存 Local Key 失败: %v", err)
	}

	candidates, err := repository.FindActiveByPrefix(context.Background(), record.Prefix, now)
	if err != nil {
		t.Fatalf("按前缀读取 Local Key 失败: %v", err)
	}
	if len(candidates) != 1 || !bytes.Equal(candidates[0].TokenHash, hash[:]) {
		t.Fatalf("候选记录不正确: %+v", candidates)
	}

	if err := repository.Revoke(context.Background(), record.ID, now); err != nil {
		t.Fatalf("吊销 Local Key 失败: %v", err)
	}
	if _, err := repository.FindActiveByPrefix(context.Background(), record.Prefix, now); err != nil {
		t.Fatalf("读取吊销后候选失败: %v", err)
	}
}

func TestLocalKeyRepositoryRejectsUnknownRecord(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewLocalKeyRepository(database)
	if err != nil {
		t.Fatalf("创建 Local Key 仓储失败: %v", err)
	}

	if err := repository.Revoke(context.Background(), "missing", time.Now().UTC()); !errors.Is(err, security.ErrLocalKeyNotFound) {
		t.Fatalf("未知 Local Key 错误=%v，期望 ErrLocalKeyNotFound", err)
	}
}

func TestSQLiteFilesDoNotContainPlaintextLocalKey(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "local-key.db")
	database, err := storage.Open(databasePath)
	if err != nil {
		t.Fatalf("打开 SQLite 数据库失败: %v", err)
	}
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}
	repository, err := storage.NewLocalKeyRepository(database)
	if err != nil {
		t.Fatalf("创建 Local Key 仓储失败: %v", err)
	}
	plaintext := "ah_local_database_plaintext_sentinel"
	hash := sha256.Sum256([]byte(plaintext))
	if err := repository.Create(context.Background(), security.LocalKeyRecord{
		ID:        "01KTESTLOCALKEY00000000001",
		Name:      "scan",
		TokenHash: hash[:],
		Prefix:    plaintext[:16],
		Suffix:    plaintext[len(plaintext)-6:],
		Status:    security.LocalKeyStatusActive,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("保存 Local Key 失败: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("关闭 SQLite 数据库失败: %v", err)
	}

	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		contents, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("读取 SQLite 文件 %s 失败: %v", path, err)
		}
		if bytes.Contains(contents, []byte(plaintext)) {
			t.Fatalf("SQLite 文件 %s 包含完整 Local Key", path)
		}
	}
}
