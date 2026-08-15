package provider_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

type recordingStore struct {
	inner      *credential.MemoryStore
	puts       []credential.Ref
	deletes    []credential.Ref
	failDelete bool
}

func newRecordingStore() *recordingStore { return &recordingStore{inner: credential.NewMemoryStore()} }
func (store *recordingStore) Put(ctx context.Context, ref credential.Ref, value credential.SecretValue) error {
	store.puts = append(store.puts, ref)
	return store.inner.Put(ctx, ref, value)
}
func (store *recordingStore) Get(ctx context.Context, ref credential.Ref) (credential.SecretValue, error) {
	return store.inner.Get(ctx, ref)
}
func (store *recordingStore) Delete(ctx context.Context, ref credential.Ref) error {
	store.deletes = append(store.deletes, ref)
	if store.failDelete {
		return errors.New("测试凭据删除失败")
	}
	return store.inner.Delete(ctx, ref)
}
func (store *recordingStore) Probe(ctx context.Context) credential.Status {
	return store.inner.Probe(ctx)
}

type idSequence struct {
	values []string
	index  int
}

func (sequence *idSequence) next(time.Time) (string, error) {
	if sequence.index >= len(sequence.values) {
		return "", errors.New("测试 ID 已耗尽")
	}
	value := sequence.values[sequence.index]
	sequence.index++
	return value, nil
}

func openProviderService(t *testing.T) (*sql.DB, *storage.ProviderRepository, *recordingStore, *provider.Service) {
	t.Helper()
	database, err := storage.Open(filepath.Join(t.TempDir(), "provider-service.db"))
	if err != nil {
		t.Fatalf("打开临时数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("迁移临时数据库失败: %v", err)
	}
	repository, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	store := newRecordingStore()
	sequence := &idSequence{values: []string{
		"01H00000000000000000000101", "01H00000000000000000000102", "01H00000000000000000000103",
		"01H00000000000000000000104", "01H00000000000000000000105", "01H00000000000000000000106",
		"01H00000000000000000000107", "01H00000000000000000000108", "01H00000000000000000000109",
		"01H00000000000000000000110", "01H00000000000000000000111", "01H00000000000000000000112",
		"01H00000000000000000000113", "01H00000000000000000000114", "01H00000000000000000000115",
	}}
	service, err := provider.NewService(repository, store, provider.ServiceOptions{Now: func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() }, NewID: sequence.next})
	if err != nil {
		t.Fatalf("创建 Provider 服务失败: %v", err)
	}
	return database, repository, store, service
}

func apiKeyInput(slug string, secret *credential.SecretValue) provider.CreateProviderInput {
	return provider.CreateProviderInput{Slug: slug, Name: "套餐", AdapterType: "openai-compatible", AuthType: provider.AuthTypeAPIKey, BaseURL: "https://example.test/v1/", Timeout: 30 * time.Second, AdapterConfigJSON: json.RawMessage(`{"wire_api":"chat_completions"}`), Credential: secret}
}

func TestProviderServiceValidatesInputAndNeverReturnsSecretOrReference(t *testing.T) {
	database, repository, _, service := openProviderService(t)
	secret := credential.SecretValue{Bytes: []byte("test-secret-value-9876")}
	invalid := apiKeyInput("Invalid", &secret)
	if _, err := service.Create(context.Background(), invalid); !errors.Is(err, provider.ErrInvalidProvider) {
		t.Fatalf("非法 slug 错误=%v", err)
	}
	missing := apiKeyInput("missing", nil)
	if _, err := service.Create(context.Background(), missing); !errors.Is(err, provider.ErrInvalidProvider) {
		t.Fatalf("缺失凭据错误=%v", err)
	}
	secretConfig := apiKeyInput("secret-config", &secret)
	secretConfig.AdapterConfigJSON = json.RawMessage(`{"api_key":"not-allowed"}`)
	if _, err := service.Create(context.Background(), secretConfig); !errors.Is(err, provider.ErrInvalidProvider) {
		t.Fatalf("配置中的秘密字段错误=%v", err)
	}
	publicHTTP := apiKeyInput("public-http", &secret)
	publicHTTP.BaseURL = "http://public.example.test/v1"
	if _, err := service.Create(context.Background(), publicHTTP); !errors.Is(err, provider.ErrInvalidProvider) {
		t.Fatalf("Public HTTP Base URL 错误=%v", err)
	}
	localHTTP := provider.CreateProviderInput{Slug: "local-http", Name: "本地服务", AdapterType: "local-openai-compatible", AuthType: provider.AuthTypeNone, BaseURL: "http://127.0.0.1:11434/v1", Timeout: 30 * time.Second, AdapterConfigJSON: json.RawMessage(`{}`)}
	if _, err := service.Create(context.Background(), localHTTP); err != nil {
		t.Fatalf("Local HTTP Base URL 不应被拒绝: %v", err)
	}
	oauth := apiKeyInput("oauth", &secret)
	oauth.AuthType = provider.AuthTypeOAuth
	if _, err := service.Create(context.Background(), oauth); !errors.Is(err, provider.ErrOAuthNotConfigured) {
		t.Fatalf("OAuth 阶段错误=%v", err)
	}

	result, err := service.Create(context.Background(), apiKeyInput("safe", &secret))
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("编码 Provider DTO 失败: %v", err)
	}
	if string(encoded) == "" || containsSensitive(encoded, secret.Bytes) {
		t.Fatalf("Provider DTO 泄露了秘密")
	}
	if result.Credential.MaskedHint == "" || !result.Credential.Configured {
		t.Fatalf("凭据 DTO 状态错误: %+v", result.Credential)
	}
	stored, err := repository.FindByID(context.Background(), result.ID)
	if err != nil || stored.CredentialRef == nil {
		t.Fatalf("读取持久化 Provider 错误: %+v, %v", stored, err)
	}
	var reference string
	if err := database.QueryRow(`SELECT credential_ref FROM providers WHERE id=?`, result.ID).Scan(&reference); err != nil {
		t.Fatalf("读取凭据引用失败: %v", err)
	}
	if reference == string(secret.Bytes) {
		t.Fatal("数据库保存了完整 Provider 凭据")
	}
}

func TestProviderServiceCompensatesCredentialWhenDuplicateSlugFails(t *testing.T) {
	_, _, store, service := openProviderService(t)
	firstSecret := credential.SecretValue{Bytes: []byte("first-test-secret-1234")}
	if _, err := service.Create(context.Background(), apiKeyInput("duplicate", &firstSecret)); err != nil {
		t.Fatalf("创建首个 Provider 失败: %v", err)
	}
	secondSecret := credential.SecretValue{Bytes: []byte("second-test-secret-5678")}
	if _, err := service.Create(context.Background(), apiKeyInput("duplicate", &secondSecret)); !errors.Is(err, provider.ErrDuplicateProvider) {
		t.Fatalf("重复 slug 错误=%v", err)
	}
	if len(store.puts) != 2 || len(store.deletes) != 1 || store.puts[1] != store.deletes[0] {
		t.Fatalf("凭据补偿顺序错误: put=%d delete=%d", len(store.puts), len(store.deletes))
	}
	if _, err := store.Get(context.Background(), store.puts[1]); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("失败创建的新凭据未被补偿删除: %v", err)
	}
}

func TestProviderServiceReplacesAndDeletesCredentialsAfterDatabaseCommit(t *testing.T) {
	_, repository, store, service := openProviderService(t)
	oldSecret := credential.SecretValue{Bytes: []byte("old-test-secret-0000")}
	created, err := service.Create(context.Background(), apiKeyInput("replace", &oldSecret))
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	before, err := repository.FindByID(context.Background(), created.ID)
	if err != nil || before.CredentialRef == nil {
		t.Fatalf("读取旧 Provider 失败: %+v, %v", before, err)
	}
	newSecret := credential.SecretValue{Bytes: []byte("new-test-secret-1111")}
	updated, err := service.Update(context.Background(), created.ID, provider.UpdateProviderInput{ExpectedVersion: created.Version, Name: "更新套餐", BaseURL: "https://example.test/v1", Timeout: 45 * time.Second, AdapterConfigJSON: json.RawMessage(`{}`), Credential: &newSecret})
	if err != nil {
		t.Fatalf("替换 Provider 凭据失败: %v", err)
	}
	after, err := repository.FindByID(context.Background(), created.ID)
	if err != nil || after.CredentialRef == nil || *after.CredentialRef == *before.CredentialRef {
		t.Fatalf("替换后的凭据引用错误: %+v, %v", after, err)
	}
	if _, err := store.Get(context.Background(), *before.CredentialRef); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("旧凭据未被删除: %v", err)
	}
	if _, err := store.Get(context.Background(), *after.CredentialRef); err != nil {
		t.Fatalf("新凭据不可读取: %v", err)
	}
	if updated.Version != created.Version+1 || updated.Credential.MaskedHint == "" {
		t.Fatalf("更新 DTO 错误: %+v", updated)
	}
	if err := service.Delete(context.Background(), created.ID, updated.Version); err != nil {
		t.Fatalf("删除 Provider 失败: %v", err)
	}
	if _, err := repository.FindByID(context.Background(), created.ID); !errors.Is(err, provider.ErrProviderNotFound) {
		t.Fatalf("软删除 Provider 后读取错误=%v", err)
	}
	if _, err := store.Get(context.Background(), *after.CredentialRef); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("删除 Provider 后凭据未清理: %v", err)
	}
}

func TestProviderServiceReportsCommittedStateWhenOldCredentialCleanupFails(t *testing.T) {
	database, repository, store, service := openProviderService(t)
	oldSecret := credential.SecretValue{Bytes: []byte("old-cleanup-secret-000")}
	created, err := service.Create(context.Background(), apiKeyInput("cleanup", &oldSecret))
	if err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	store.failDelete = true
	newSecret := credential.SecretValue{Bytes: []byte("new-cleanup-secret-111")}
	updated, err := service.Update(context.Background(), created.ID, provider.UpdateProviderInput{ExpectedVersion: created.Version, Name: "更新套餐", BaseURL: "https://example.test/v1", Timeout: 30 * time.Second, AdapterConfigJSON: json.RawMessage(`{}`), Credential: &newSecret})
	if !errors.Is(err, provider.ErrCredentialCleanup) || updated.ID != created.ID || updated.Version != created.Version+1 {
		t.Fatalf("凭据清理失败返回错误: dto=%+v err=%v", updated, err)
	}
	stored, err := repository.FindByID(context.Background(), created.ID)
	if err != nil || stored.CredentialRef == nil {
		t.Fatalf("清理失败后 Provider 不应回滚: %+v, %v", stored, err)
	}
	var auditCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type='provider_credential_cleanup_failed'`).Scan(&auditCount); err != nil {
		t.Fatalf("读取清理失败审计错误: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("清理失败审计数量=%d，期望 1", auditCount)
	}
}

func containsSensitive(value []byte, secret []byte) bool {
	for start := 0; start+len(secret) <= len(value); start++ {
		match := true
		for offset := range secret {
			if value[start+offset] != secret[offset] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
func TestProviderServiceNeverPersistsPlaintextCredentialToSQLiteFiles(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "provider-secret-scan.db")
	database, err := storage.Open(databasePath)
	if err != nil {
		t.Fatalf("打开临时数据库失败: %v", err)
	}
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("迁移临时数据库失败: %v", err)
	}
	repository, err := storage.NewProviderRepository(database)
	if err != nil {
		t.Fatalf("创建 Provider 仓储失败: %v", err)
	}
	sequence := &idSequence{values: []string{"01H00000000000000000000201", "01H00000000000000000000202", "01H00000000000000000000203"}}
	service, err := provider.NewService(repository, credential.NewMemoryStore(), provider.ServiceOptions{Now: func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() }, NewID: sequence.next})
	if err != nil {
		t.Fatalf("创建 Provider 服务失败: %v", err)
	}
	plaintext := credential.SecretValue{Bytes: []byte("provider-sqlite-plaintext-sentinel")}
	if _, err := service.Create(context.Background(), apiKeyInput("sqlite-scan", &plaintext)); err != nil {
		t.Fatalf("创建 Provider 失败: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("关闭临时数据库失败: %v", err)
	}
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		contents, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("读取 SQLite 文件 %s 失败: %v", path, err)
		}
		if bytes.Contains(contents, plaintext.Bytes) {
			t.Fatalf("SQLite 文件 %s 包含完整 Provider 凭据", path)
		}
	}
}
