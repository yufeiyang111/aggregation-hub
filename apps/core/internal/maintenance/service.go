// Package maintenance 组合受控运行时设置、请求保留与本地数据库维护操作。
package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"aggregationhub.local/core/internal/storage"
)

const (
	DefaultListenPort           = 18443
	DefaultRequestTimeoutMS     = 60000
	DefaultRequestRetentionDays = 30
	minimumListenPort           = 1024
	maximumListenPort           = 65535
	minimumRequestTimeoutMS     = 1000
	maximumRequestTimeoutMS     = 3600000
	minimumRequestRetentionDays = 1
	maximumRequestRetentionDays = 3650
)

var ErrInvalidRuntimeSettings = errors.New("运行时设置无效")

type RuntimeSettings struct {
	ListenPort           int   `json:"listen_port"`
	RequestTimeoutMS     int64 `json:"request_timeout_ms"`
	RequestRetentionDays int   `json:"request_retention_days"`
	Version              int64 `json:"version"`
}

type UpdateRuntimeSettingsInput struct {
	ListenPort           int   `json:"listen_port"`
	RequestTimeoutMS     int64 `json:"request_timeout_ms"`
	RequestRetentionDays int   `json:"request_retention_days"`
	Version              int64 `json:"version"`
}

type UpdateRuntimeSettingsResult struct {
	Settings        RuntimeSettings `json:"settings"`
	RestartRequired bool            `json:"restart_required"`
}

type Service struct {
	settings  storage.SettingsRepository
	retention *storage.RetentionRepository
	backups   *storage.BackupManager
	now       func() time.Time
}

func NewService(settings storage.SettingsRepository, retention *storage.RetentionRepository, backups *storage.BackupManager) (*Service, error) {
	if settings == nil || retention == nil || backups == nil {
		return nil, errors.New("维护服务依赖无效")
	}
	return &Service{settings: settings, retention: retention, backups: backups, now: time.Now}, nil
}

func (service *Service) Settings(ctx context.Context) (RuntimeSettings, error) {
	snapshot, err := service.settings.ReadRuntime(ctx, runtimeSettingKeys())
	if err != nil {
		return RuntimeSettings{}, err
	}
	result := RuntimeSettings{
		ListenPort:           DefaultListenPort,
		RequestTimeoutMS:     DefaultRequestTimeoutMS,
		RequestRetentionDays: DefaultRequestRetentionDays,
		Version:              snapshot.Version,
	}
	if value, exists := snapshot.Values["gateway.listen_port"]; exists {
		if err := json.Unmarshal(value, &result.ListenPort); err != nil {
			return RuntimeSettings{}, fmt.Errorf("端口设置已损坏: %w", err)
		}
	}
	if value, exists := snapshot.Values["gateway.request_timeout_ms"]; exists {
		if err := json.Unmarshal(value, &result.RequestTimeoutMS); err != nil {
			return RuntimeSettings{}, fmt.Errorf("超时设置已损坏: %w", err)
		}
	}
	if value, exists := snapshot.Values["retention.request_days"]; exists {
		if err := json.Unmarshal(value, &result.RequestRetentionDays); err != nil {
			return RuntimeSettings{}, fmt.Errorf("保留期设置已损坏: %w", err)
		}
	}
	if err := validateRuntimeSettings(result); err != nil {
		return RuntimeSettings{}, err
	}
	return result, nil
}

func (service *Service) UpdateSettings(ctx context.Context, input UpdateRuntimeSettingsInput) (UpdateRuntimeSettingsResult, error) {
	candidate := RuntimeSettings{
		ListenPort:           input.ListenPort,
		RequestTimeoutMS:     input.RequestTimeoutMS,
		RequestRetentionDays: input.RequestRetentionDays,
		Version:              input.Version,
	}
	if err := validateRuntimeSettings(candidate); err != nil {
		return UpdateRuntimeSettingsResult{}, err
	}
	current, err := service.Settings(ctx)
	if err != nil {
		return UpdateRuntimeSettingsResult{}, err
	}
	if current.Version != input.Version {
		return UpdateRuntimeSettingsResult{}, storage.ErrStaleSettings
	}
	values := map[string]json.RawMessage{
		"gateway.listen_port":        json.RawMessage(fmt.Sprintf("%d", input.ListenPort)),
		"gateway.request_timeout_ms": json.RawMessage(fmt.Sprintf("%d", input.RequestTimeoutMS)),
		"retention.request_days":     json.RawMessage(fmt.Sprintf("%d", input.RequestRetentionDays)),
	}
	snapshot, err := service.settings.UpdateRuntime(ctx, values, input.Version, service.now().UTC())
	if err != nil {
		return UpdateRuntimeSettingsResult{}, err
	}
	updated := candidate
	updated.Version = snapshot.Version
	return UpdateRuntimeSettingsResult{
		Settings:        updated,
		RestartRequired: current.ListenPort != updated.ListenPort || current.RequestTimeoutMS != updated.RequestTimeoutMS,
	}, nil
}

func (service *Service) PruneRequests(ctx context.Context) (storage.RetentionResult, error) {
	settings, err := service.Settings(ctx)
	if err != nil {
		return storage.RetentionResult{}, err
	}
	return service.retention.PruneRequests(ctx, settings.RequestRetentionDays, service.now().UTC())
}

func (service *Service) CreateBackup(ctx context.Context) (storage.BackupRecord, error) {
	return service.backups.Create(ctx)
}

func (service *Service) ListBackups(ctx context.Context) ([]storage.BackupRecord, error) {
	return service.backups.List(ctx)
}

func (service *Service) ScheduleRestore(ctx context.Context, identifier string) (storage.RestoreSchedule, error) {
	return service.backups.ScheduleRestore(ctx, identifier)
}

func runtimeSettingKeys() []string {
	return []string{"gateway.listen_port", "gateway.request_timeout_ms", "retention.request_days"}
}

func validateRuntimeSettings(value RuntimeSettings) error {
	if value.ListenPort < minimumListenPort || value.ListenPort > maximumListenPort ||
		value.RequestTimeoutMS < minimumRequestTimeoutMS || value.RequestTimeoutMS > maximumRequestTimeoutMS ||
		value.RequestRetentionDays < minimumRequestRetentionDays || value.RequestRetentionDays > maximumRequestRetentionDays ||
		value.Version < 0 {
		return ErrInvalidRuntimeSettings
	}
	return nil
}
