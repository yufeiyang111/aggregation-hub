package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMemoryStoreContract(t *testing.T) {
	runStoreContractWithRef(t, NewMemoryStore(), Ref("test/credential-contract"))
}

func runStoreContractWithRef(t *testing.T, store Store, ref Ref) {
	t.Helper()
	ctx := context.Background()
	original := SecretValue{Bytes: []byte("test-secret-value")}
	if err := store.Put(ctx, ref, original); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	original.Bytes[0] = 'X'
	got, err := store.Get(ctx, ref)
	if err != nil || !bytes.Equal(got.Bytes, []byte("test-secret-value")) {
		t.Fatalf("输入副本隔离失败 value=%q err=%v", got.Bytes, err)
	}
	got.Bytes[0] = 'Y'
	again, err := store.Get(ctx, ref)
	if err != nil || !bytes.Equal(again.Bytes, []byte("test-secret-value")) {
		t.Fatalf("Get 副本隔离失败 value=%q err=%v", again.Bytes, err)
	}
	if err := store.Put(ctx, ref, SecretValue{Bytes: []byte("replaced")}); err != nil {
		t.Fatalf("覆盖 Put 失败: %v", err)
	}
	replaced, err := store.Get(ctx, ref)
	if err != nil || !bytes.Equal(replaced.Bytes, []byte("replaced")) {
		t.Fatalf("覆盖结果错误 value=%q err=%v", replaced.Bytes, err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 Get 错误=%v", err)
	}
	if err := store.Delete(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复 Delete 错误=%v", err)
	}
	if status := store.Probe(ctx); !status.Available || status.Backend == "" {
		t.Fatalf("Probe 状态错误: %+v", status)
	}
}

func TestSecretValueCannotBeJSONSerialized(t *testing.T) {
	if _, err := json.Marshal(SecretValue{Bytes: []byte("sentinel")}); err == nil {
		t.Fatal("SecretValue 不得被 JSON 序列化")
	}
}
func TestMemoryStoreRejectsInvalidReferenceAndEmptySecret(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Put(context.Background(), Ref("bad\\ref"), SecretValue{Bytes: []byte("x")}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("无效 ref 错误=%v", err)
	}
	if err := store.Put(context.Background(), Ref("valid"), SecretValue{}); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("空秘密错误=%v", err)
	}
}
