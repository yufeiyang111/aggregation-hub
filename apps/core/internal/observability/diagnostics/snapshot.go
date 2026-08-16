package diagnostics

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"aggregationhub.local/core/internal/observability"
)

const FormatVersion = "diagnostics/v1"

// Summary 是桌面端可安全显示的诊断概览。
type Summary struct {
	FormatVersion    string `json:"format_version"`
	RecentErrorCount int    `json:"recent_error_count"`
	ExportAvailable  bool   `json:"export_available"`
}

// Export 描述已生成诊断包的受控元数据，不包含绝对路径。
type Export struct {
	FileName      string    `json:"file_name"`
	SizeBytes     int64     `json:"size_bytes"`
	GeneratedAt   time.Time `json:"generated_at"`
	FormatVersion string    `json:"format_version"`
}

// Service 定义控制面使用的最小诊断能力。
type Service interface {
	Summary(context.Context) (Summary, error)
	Export(context.Context) (Export, error)
}

// RuntimeSnapshot 只保存可安全导出的运行时摘要。
type RuntimeSnapshot struct {
	State        string
	DataPlaneURL string
	StartedAt    string
	Version      string
}

// Migration 是已执行迁移的受限投影。
type Migration struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
}

// CredentialStore 是凭据存储的探针结果，不包含凭据引用或内容。
type CredentialStore struct {
	Available bool   `json:"available"`
	Backend   string `json:"backend"`
}

// ProviderHealth 是服务健康状态的受限投影。
type ProviderHealth struct {
	Slug    string `json:"slug"`
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
}

// Options 把运行时依赖注入 Exporter，避免诊断包直接依赖数据库或 Core 生命周期实现。
type Options struct {
	DataDir         string
	Runtime         func() RuntimeSnapshot
	Logger          *observability.SafeLogger
	Migrations      func(context.Context) ([]Migration, error)
	CredentialProbe func(context.Context) CredentialStore
	ProviderHealth  func(context.Context) ([]ProviderHealth, error)
	Now             func() time.Time
}

// Exporter 收集固定字段并写入应用受控的 diagnostics 目录。
type Exporter struct {
	options Options
}

func NewExporter(options Options) (*Exporter, error) {
	if !filepath.IsAbs(options.DataDir) || options.Runtime == nil || options.Logger == nil || options.Migrations == nil || options.CredentialProbe == nil || options.ProviderHealth == nil {
		return nil, errors.New("诊断导出依赖无效")
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	return &Exporter{options: options}, nil
}

func (exporter *Exporter) Summary(context.Context) (Summary, error) {
	return Summary{
		FormatVersion:    FormatVersion,
		RecentErrorCount: len(exporter.options.Logger.RecentErrors()),
		ExportAvailable:  true,
	}, nil
}
