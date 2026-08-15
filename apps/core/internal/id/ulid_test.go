package id_test

import (
	"bytes"
	"testing"
	"time"

	"aggregationhub.local/core/internal/id"
)

func TestNewULIDIsDeterministicForFixedInput(t *testing.T) {
	value, err := id.NewULID(time.UnixMilli(1_700_000_000_000).UTC(), bytes.NewReader(bytes.Repeat([]byte{0x01}, 10)))
	if err != nil {
		t.Fatalf("生成 ULID 失败: %v", err)
	}
	if len(value) != 26 {
		t.Fatalf("ULID 长度=%d，期望 26", len(value))
	}
	for _, character := range value {
		if !bytes.ContainsRune([]byte("0123456789ABCDEFGHJKMNPQRSTVWXYZ"), character) {
			t.Fatalf("ULID 包含非法字符 %q", character)
		}
	}
}

func TestNewULIDRejectsInvalidInputs(t *testing.T) {
	if _, err := id.NewULID(time.Now(), nil); err == nil {
		t.Fatal("空随机源应返回错误")
	}
	if _, err := id.NewULID(time.UnixMilli(-1), bytes.NewReader(make([]byte, 10))); err == nil {
		t.Fatal("负时间应返回错误")
	}
}
