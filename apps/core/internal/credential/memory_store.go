package credential

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mutex  sync.RWMutex
	values map[Ref]SecretValue
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{values: make(map[Ref]SecretValue)} }
func (store *MemoryStore) Put(ctx context.Context, ref Ref, value SecretValue) error {
	if ctx == nil {
		return ErrInvalidReference
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	if !value.valid() {
		return ErrInvalidSecret
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.values[ref] = value.Clone()
	return nil
}
func (store *MemoryStore) Get(ctx context.Context, ref Ref) (SecretValue, error) {
	if ctx == nil {
		return SecretValue{}, ErrInvalidReference
	}
	if err := ref.Validate(); err != nil {
		return SecretValue{}, err
	}
	store.mutex.RLock()
	value, ok := store.values[ref]
	store.mutex.RUnlock()
	if !ok {
		return SecretValue{}, ErrNotFound
	}
	return value.Clone(), nil
}
func (store *MemoryStore) Delete(ctx context.Context, ref Ref) error {
	if ctx == nil {
		return ErrInvalidReference
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, ok := store.values[ref]; !ok {
		return ErrNotFound
	}
	delete(store.values, ref)
	return nil
}
func (*MemoryStore) Probe(context.Context) Status { return Status{Available: true, Backend: "memory"} }
