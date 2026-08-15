package security

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryLocalKeyStore struct {
	mu      sync.Mutex
	records map[string]LocalKeyRecord
}

func newMemoryLocalKeyStore() *memoryLocalKeyStore {
	return &memoryLocalKeyStore{records: make(map[string]LocalKeyRecord)}
}

func (store *memoryLocalKeyStore) Create(_ context.Context, record LocalKeyRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.records[record.ID] = cloneLocalKeyRecord(record)
	return nil
}

func (store *memoryLocalKeyStore) FindActiveByPrefix(_ context.Context, prefix string, now time.Time) ([]LocalKeyRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	var records []LocalKeyRecord
	for id, record := range store.records {
		if record.Status == LocalKeyStatusActive && record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
			record.Status = LocalKeyStatusExpired
			store.records[id] = record
		}
		if record.Status == LocalKeyStatusActive && record.Prefix == prefix {
			records = append(records, cloneLocalKeyRecord(record))
		}
	}
	return records, nil
}

func (store *memoryLocalKeyStore) Revoke(_ context.Context, id string, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	record, exists := store.records[id]
	if !exists || record.Status != LocalKeyStatusActive {
		return ErrLocalKeyNotFound
	}
	record.Status = LocalKeyStatusRevoked
	record.RevokedAt = &now
	store.records[id] = record
	return nil
}

func (store *memoryLocalKeyStore) MarkUsed(_ context.Context, id string, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	record, exists := store.records[id]
	if !exists || record.Status != LocalKeyStatusActive {
		return ErrLocalKeyNotFound
	}
	record.LastUsedAt = &now
	store.records[id] = record
	return nil
}

func TestCreateReturnsFullKeyButStoresOnlyHash(t *testing.T) {
	store := newMemoryLocalKeyStore()
	service, err := NewLocalKeyService(store, LocalKeyServiceOptions{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x7f}, 128)),
		Now:    func() time.Time { return time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("创建 LocalKeyService 失败: %v", err)
	}

	key, record, err := service.Create(context.Background(), "default", nil)
	if err != nil {
		t.Fatalf("创建 Local Access Key 失败: %v", err)
	}
	if !strings.HasPrefix(key, LocalKeyTokenPrefix) {
		t.Fatalf("密钥前缀错误: %q", key)
	}
	if len(record.TokenHash) != LocalKeyHashBytes {
		t.Fatalf("哈希长度=%d，期望 %d", len(record.TokenHash), LocalKeyHashBytes)
	}
	if bytes.Contains(record.TokenHash, []byte(key)) {
		t.Fatal("记录不应包含完整明文密钥")
	}
	stored := store.records[record.ID]
	if bytes.Contains(stored.TokenHash, []byte(key)) {
		t.Fatal("仓储不应包含完整明文密钥")
	}
	if stored.Prefix == key || stored.Suffix == key {
		t.Fatal("展示前后缀不能等于完整密钥")
	}
}

func TestCreateUsesDistinctRandomTokensAndVerifyHonorsRevocation(t *testing.T) {
	store := newMemoryLocalKeyStore()
	now := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	service, err := NewLocalKeyService(store, LocalKeyServiceOptions{
		Random: bytes.NewReader(append(bytes.Repeat([]byte{0x3c}, 42), bytes.Repeat([]byte{0x6d}, 42)...)),
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("创建 LocalKeyService 失败: %v", err)
	}

	first, firstRecord, err := service.Create(context.Background(), "first", nil)
	if err != nil {
		t.Fatalf("创建第一个密钥失败: %v", err)
	}
	second, secondRecord, err := service.Create(context.Background(), "second", nil)
	if err != nil {
		t.Fatalf("创建第二个密钥失败: %v", err)
	}
	if first == second || bytes.Equal(firstRecord.TokenHash, secondRecord.TokenHash) {
		t.Fatal("不同随机输入必须产生不同密钥和哈希")
	}

	record, valid, err := service.Verify(context.Background(), first)
	if err != nil || !valid || record.ID != firstRecord.ID {
		t.Fatalf("合法密钥校验结果 record=%+v valid=%t err=%v", record, valid, err)
	}
	if store.records[firstRecord.ID].LastUsedAt == nil {
		t.Fatal("成功校验必须更新 last_used_at")
	}
	if err := service.Revoke(context.Background(), firstRecord.ID); err != nil {
		t.Fatalf("吊销密钥失败: %v", err)
	}
	if _, valid, err := service.Verify(context.Background(), first); err != nil || valid {
		t.Fatalf("已吊销密钥不得通过 valid=%t err=%v", valid, err)
	}
}

func TestVerifyRejectsMalformedAndExpiredKey(t *testing.T) {
	store := newMemoryLocalKeyStore()
	now := time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC)
	service, err := NewLocalKeyService(store, LocalKeyServiceOptions{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 64)),
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("创建 LocalKeyService 失败: %v", err)
	}

	if _, valid, err := service.Verify(context.Background(), "invalid"); err != nil || valid {
		t.Fatalf("畸形密钥必须安全拒绝 valid=%t err=%v", valid, err)
	}
	expiresAt := now.Add(-time.Second)
	key, _, err := service.Create(context.Background(), "expired", &expiresAt)
	if !errors.Is(err, ErrInvalidKeyExpiration) || key != "" {
		t.Fatalf("过期时间应被拒绝 key=%q err=%v", key, err)
	}
}
