// Package id 提供仅用于本地持久化对象的 ULID 标识生成。
package id

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"
)

const ulidLength = 26

// NewULID 使用给定时间和随机源生成可按时间排序的 ULID。
func NewULID(now time.Time, reader io.Reader) (string, error) {
	if reader == nil {
		return "", errors.New("ULID 随机源不能为空")
	}
	milliseconds := now.UTC().UnixMilli()
	if milliseconds < 0 || milliseconds >= 1<<48 {
		return "", errors.New("ULID 时间超出范围")
	}

	var payload [16]byte
	payload[0] = byte(milliseconds >> 40)
	payload[1] = byte(milliseconds >> 32)
	payload[2] = byte(milliseconds >> 24)
	payload[3] = byte(milliseconds >> 16)
	payload[4] = byte(milliseconds >> 8)
	payload[5] = byte(milliseconds)
	if _, err := io.ReadFull(reader, payload[6:]); err != nil {
		return "", fmt.Errorf("生成 ULID 随机部分失败: %w", err)
	}

	value := new(big.Int).SetBytes(payload[:])
	quotient := new(big.Int)
	remainder := new(big.Int)
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var encoded [ulidLength]byte
	for index := len(encoded) - 1; index >= 0; index-- {
		quotient.DivMod(value, big.NewInt(32), remainder)
		encoded[index] = alphabet[remainder.Int64()]
		value.Set(quotient)
	}
	return string(encoded[:]), nil
}

// RandomULID 使用系统安全随机源生成 ULID。
func RandomULID(now time.Time) (string, error) {
	return NewULID(now, rand.Reader)
}
