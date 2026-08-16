package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/security"
)

func (exporter *Exporter) Export(ctx context.Context) (Export, error) {
	snapshot, err := exporter.collectSnapshot(ctx)
	if err != nil {
		return Export{}, err
	}

	archive, err := buildArchive(snapshot.entries())
	if err != nil {
		return Export{}, err
	}

	return exporter.writeArchive(snapshot.generatedAt, archive)
}

type snapshot struct {
	runtime      RuntimeSnapshot
	migrations   []Migration
	credential   CredentialStore
	providers    []ProviderHealth
	recentErrors []observability.SafeErrorSummary
	generatedAt  time.Time
}

type runtimeEntry struct {
	State        string `json:"state"`
	DataPlaneURL string `json:"data_plane_url"`
	StartedAt    string `json:"started_at"`
	Version      string `json:"version"`
}

type migrationEntry struct {
	Applied []Migration `json:"applied"`
}

type manifestEntry struct {
	FormatVersion string    `json:"format_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Entries       []string  `json:"entries"`
}

func (exporter *Exporter) collectSnapshot(ctx context.Context) (snapshot, error) {
	migrations, err := exporter.options.Migrations(ctx)
	if err != nil {
		return snapshot{}, errors.New("读取迁移诊断摘要失败")
	}

	providers, err := exporter.options.ProviderHealth(ctx)
	if err != nil {
		return snapshot{}, errors.New("读取 Provider 健康摘要失败")
	}

	return snapshot{
		runtime:      exporter.options.Runtime(),
		migrations:   migrations,
		credential:   exporter.options.CredentialProbe(ctx),
		providers:    providers,
		recentErrors: exporter.options.Logger.RecentErrors(),
		generatedAt:  exporter.options.Now().UTC(),
	}, nil
}

func (value snapshot) entries() map[string]any {
	return map[string]any{
		"runtime.json": runtimeEntry{
			State:        value.runtime.State,
			DataPlaneURL: redactedDataPlaneURL(value.runtime.DataPlaneURL),
			StartedAt:    value.runtime.StartedAt,
			Version:      value.runtime.Version,
		},
		"migration.json": migrationEntry{
			Applied: value.migrations,
		},
		"credential-store.json": value.credential,
		"provider-health.json":  value.providers,
		"recent-errors.json":    value.recentErrors,
		"manifest.json": manifestEntry{
			FormatVersion: FormatVersion,
			GeneratedAt:   value.generatedAt,
			Entries:       diagnosticContentEntryNames(),
		},
	}
}

func redactedDataPlaneURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return security.RedactURL(parsed)
}

func (exporter *Exporter) writeArchive(generatedAt time.Time, archive []byte) (Export, error) {
	directory := filepath.Join(exporter.options.DataDir, "diagnostics")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Export{}, errors.New("创建诊断目录失败")
	}

	fileName := fmt.Sprintf("aggregation-hub-diagnostics-%s.zip", generatedAt.Format("20060102T150405.000000000Z"))
	filePath := filepath.Join(directory, fileName)
	if err := writeAtomically(directory, filePath, archive); err != nil {
		return Export{}, err
	}

	return Export{
		FileName:      fileName,
		SizeBytes:     int64(len(archive)),
		GeneratedAt:   generatedAt,
		FormatVersion: FormatVersion,
	}, nil
}

func writeAtomically(directory string, destination string, content []byte) (err error) {
	temporary, err := os.CreateTemp(directory, ".diagnostics-*.tmp")
	if err != nil {
		return errors.New("创建诊断临时文件失败")
	}

	temporaryName := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		if removeErr := os.Remove(temporaryName); err == nil && removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.New("清理诊断临时文件失败")
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return errors.New("设置诊断文件权限失败")
	}
	if _, err := temporary.Write(content); err != nil {
		return errors.New("写入诊断包失败")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("关闭诊断包失败")
	}
	closed = true
	if err := os.Rename(temporaryName, destination); err != nil {
		return errors.New("提交诊断包失败")
	}

	return nil
}
