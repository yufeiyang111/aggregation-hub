package credential

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrNotFound         = errors.New("凭据不存在")
	ErrInvalidReference = errors.New("凭据引用无效")
	ErrInvalidSecret    = errors.New("凭据值无效")
	ErrUnsupported      = errors.New("当前平台不支持系统凭据库")
)

type SecretValue struct{ Bytes []byte }

// MarshalJSON 阻止秘密被意外写入日志、响应或测试快照。
func (SecretValue) MarshalJSON() ([]byte, error) {
	return nil, errors.New("秘密值禁止 JSON 序列化")
}
func (value SecretValue) Clone() SecretValue {
	return SecretValue{Bytes: append([]byte(nil), value.Bytes...)}
}
func (value SecretValue) valid() bool { return len(value.Bytes) > 0 && len(value.Bytes) <= 5120 }

var _ json.Marshaler = SecretValue{}

type Status struct {
	Available bool
	Backend   string
	Detail    string
}
type Store interface {
	Put(context.Context, Ref, SecretValue) error
	Get(context.Context, Ref) (SecretValue, error)
	Delete(context.Context, Ref) error
	Probe(context.Context) Status
}
