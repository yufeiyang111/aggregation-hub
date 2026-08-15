package adapter

import (
	"errors"
	"strings"
	"sync"
)

type Factory func() Adapter

// Registry 只保存 Factory；每次 Create 生成独立实例，避免不同 Provider 之间共享可变请求状态。
type Registry struct {
	mutex     sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry { return &Registry{factories: make(map[string]Factory)} }

func (registry *Registry) Register(factory Factory) error {
	if registry == nil || factory == nil {
		return ErrInvalidAdapterType
	}
	prototype := factory()
	if prototype == nil || !validType(prototype.Type()) {
		return ErrInvalidAdapterType
	}
	kind := prototype.Type()
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if _, exists := registry.factories[kind]; exists {
		return ErrDuplicateAdapterType
	}
	registry.factories[kind] = factory
	return nil
}

func (registry *Registry) Create(kind string) (Adapter, error) {
	if registry == nil || !validType(kind) {
		return nil, ErrInvalidAdapterType
	}
	registry.mutex.RLock()
	factory := registry.factories[kind]
	registry.mutex.RUnlock()
	if factory == nil {
		return nil, ErrAdapterNotFound
	}
	value := factory()
	if value == nil || value.Type() != kind {
		return nil, errors.New("Adapter Factory 返回了无效实例")
	}
	return value, nil
}

func validType(value string) bool {
	if len(value) < 1 || len(value) > 64 || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}
