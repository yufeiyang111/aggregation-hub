package openai

import "aggregationhub.local/core/internal/adapter"

// Register 将公网与本地 OpenAI Compatible 类型显式加入注册表；实际网络范围由 Transport 按 Adapter Type 强制区分。
func Register(registry *adapter.Registry) error {
	for _, kind := range []string{"openai-compatible", "local-openai-compatible"} {
		adapterType := kind
		if err := registry.Register(func() adapter.Adapter {
			value, err := New(adapterType)
			if err != nil {
				return nil
			}
			return value
		}); err != nil {
			return err
		}
	}
	return nil
}
