package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"aggregationhub.local/core/internal/id"
)

const (
	LocalKeyTokenPrefix  = "ah_local_"
	LocalKeyHashBytes    = sha256.Size
	localKeyRandomBytes  = 32
	localKeyPrefixBytes  = 16
	localKeySuffixBytes  = 6
	maxLocalKeyNameRunes = 128
)

const (
	LocalKeyStatusActive  = "active"
	LocalKeyStatusRevoked = "revoked"
	LocalKeyStatusExpired = "expired"
)

var (
	ErrInvalidLocalKeyStore = errors.New("Local Access Key 仓储无效")
	ErrInvalidKeyName       = errors.New("Local Access Key 名称无效")
	ErrInvalidKeyExpiration = errors.New("Local Access Key 过期时间无效")
	ErrLocalKeyNotFound     = errors.New("Local Access Key 不存在或不可用")
)

type LocalKeyRecord struct {
	ID         string
	Name       string
	TokenHash  []byte
	Prefix     string
	Suffix     string
	Status     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

type LocalKeyStore interface {
	Create(ctx context.Context, record LocalKeyRecord) error
	FindActiveByPrefix(ctx context.Context, prefix string, now time.Time) ([]LocalKeyRecord, error)
	Revoke(ctx context.Context, id string, now time.Time) error
	MarkUsed(ctx context.Context, id string, now time.Time) error
}

type LocalKeyServiceOptions struct {
	Random io.Reader
	Now    func() time.Time
}

type LocalKeyService struct {
	store  LocalKeyStore
	random io.Reader
	now    func() time.Time
}

func NewLocalKeyService(store LocalKeyStore, options LocalKeyServiceOptions) (*LocalKeyService, error) {
	if store == nil {
		return nil, ErrInvalidLocalKeyStore
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &LocalKeyService{store: store, random: options.Random, now: options.Now}, nil
}

// Create 只向调用者返回一次完整密钥；仓储只接收哈希和展示所需前后缀。
func (service *LocalKeyService) Create(ctx context.Context, name string, expiresAt *time.Time) (string, LocalKeyRecord, error) {
	if ctx == nil {
		return "", LocalKeyRecord{}, errors.New("创建 Local Access Key 的上下文不能为空")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxLocalKeyNameRunes {
		return "", LocalKeyRecord{}, ErrInvalidKeyName
	}
	now := service.now().UTC()
	if expiresAt != nil && !expiresAt.UTC().After(now) {
		return "", LocalKeyRecord{}, ErrInvalidKeyExpiration
	}

	randomBytes := make([]byte, localKeyRandomBytes)
	if _, err := io.ReadFull(service.random, randomBytes); err != nil {
		return "", LocalKeyRecord{}, fmt.Errorf("生成 Local Access Key 随机值失败: %w", err)
	}
	plaintext := LocalKeyTokenPrefix + base64.RawURLEncoding.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(plaintext))
	id, err := id.NewULID(now, service.random)
	if err != nil {
		return "", LocalKeyRecord{}, err
	}
	record := LocalKeyRecord{
		ID:        id,
		Name:      name,
		TokenHash: append([]byte(nil), hash[:]...),
		Prefix:    plaintext[:localKeyPrefixBytes],
		Suffix:    plaintext[len(plaintext)-localKeySuffixBytes:],
		Status:    LocalKeyStatusActive,
		CreatedAt: now,
	}
	if expiresAt != nil {
		value := expiresAt.UTC()
		record.ExpiresAt = &value
	}
	if err := service.store.Create(ctx, cloneLocalKeyRecord(record)); err != nil {
		return "", LocalKeyRecord{}, fmt.Errorf("保存 Local Access Key 失败: %w", err)
	}
	return plaintext, cloneLocalKeyRecord(record), nil
}

func (service *LocalKeyService) Verify(ctx context.Context, plaintext string) (LocalKeyRecord, bool, error) {
	if ctx == nil {
		return LocalKeyRecord{}, false, errors.New("校验 Local Access Key 的上下文不能为空")
	}
	if !isWellFormedLocalKey(plaintext) {
		return LocalKeyRecord{}, false, nil
	}
	now := service.now().UTC()
	candidates, err := service.store.FindActiveByPrefix(ctx, plaintext[:localKeyPrefixBytes], now)
	if err != nil {
		return LocalKeyRecord{}, false, fmt.Errorf("读取 Local Access Key 候选失败: %w", err)
	}
	hash := sha256.Sum256([]byte(plaintext))
	for _, candidate := range candidates {
		if len(candidate.TokenHash) != LocalKeyHashBytes {
			continue
		}
		if subtle.ConstantTimeCompare(candidate.TokenHash, hash[:]) != 1 {
			continue
		}
		if err := service.store.MarkUsed(ctx, candidate.ID, now); err != nil {
			return LocalKeyRecord{}, false, fmt.Errorf("更新 Local Access Key 使用时间失败: %w", err)
		}
		candidate.LastUsedAt = &now
		return cloneLocalKeyRecord(candidate), true, nil
	}
	return LocalKeyRecord{}, false, nil
}

func (service *LocalKeyService) Revoke(ctx context.Context, id string) error {
	if ctx == nil {
		return errors.New("吊销 Local Access Key 的上下文不能为空")
	}
	if strings.TrimSpace(id) == "" {
		return ErrLocalKeyNotFound
	}
	return service.store.Revoke(ctx, id, service.now().UTC())
}

func isWellFormedLocalKey(value string) bool {
	if !strings.HasPrefix(value, LocalKeyTokenPrefix) || len(value) <= localKeyPrefixBytes {
		return false
	}
	encoded := strings.TrimPrefix(value, LocalKeyTokenPrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	return err == nil && len(decoded) == localKeyRandomBytes
}

func cloneLocalKeyRecord(record LocalKeyRecord) LocalKeyRecord {
	result := record
	result.TokenHash = append([]byte(nil), record.TokenHash...)
	if record.LastUsedAt != nil {
		value := *record.LastUsedAt
		result.LastUsedAt = &value
	}
	if record.ExpiresAt != nil {
		value := *record.ExpiresAt
		result.ExpiresAt = &value
	}
	if record.RevokedAt != nil {
		value := *record.RevokedAt
		result.RevokedAt = &value
	}
	return result
}
