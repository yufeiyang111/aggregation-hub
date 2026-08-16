package diagnostics_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/observability/diagnostics"
)

func TestExporterWritesAllowlistedRedactedSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	logger := observability.NewSafeLogger(100)
	logger.Record(observability.SafeErrorEvent{
		EventCode:    "gateway.request_failed",
		ErrorCode:    "upstream_timeout",
		ProviderSlug: "demo",
		OccurredAt:   time.Date(2026, 8, 16, 17, 0, 0, 0, time.UTC),
	})

	exporter := newTestExporter(t, dataDir, logger, func(context.Context) ([]diagnostics.ProviderHealth, error) {
		return []diagnostics.ProviderHealth{{
			Slug:    "demo",
			Status:  "enabled",
			Enabled: true,
		}}, nil
	},
	)

	result, err := exporter.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dataDir, "diagnostics", result.FileName)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	assertArchiveEntries(t, archive)
	if bytes.Contains(archive, []byte("diagnostic-query-secret")) {
		t.Fatal("ZIP 泄漏 Data Plane Query")
	}
	if bytes.Contains(archive, []byte(dataDir)) {
		t.Fatal("ZIP 泄漏诊断目录绝对路径")
	}
}

func TestExporterRejectsSecretMarkedPayloadWithoutArchive(t *testing.T) {
	dataDir := t.TempDir()
	exporter := newTestExporter(t, dataDir, observability.NewSafeLogger(1), func(context.Context) ([]diagnostics.ProviderHealth, error) {
		return []diagnostics.ProviderHealth{{
			Slug:   "Bearer diagnostic-export-secret",
			Status: "enabled",
		}}, nil
	},
	)

	if _, err := exporter.Export(context.Background()); err == nil {
		t.Fatal("包含 Sentinel 的诊断内容必须被拒绝")
	}

	directory := filepath.Join(dataDir, "diagnostics")
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".zip") {
			t.Fatalf("拒绝导出后不应保留 ZIP: %s", entry.Name())
		}
	}
}

func TestNewExporterRejectsIncompleteDependencies(t *testing.T) {
	_, err := diagnostics.NewExporter(diagnostics.Options{DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("缺少诊断依赖时必须拒绝创建 Exporter")
	}
}

func newTestExporter(
	t *testing.T,
	dataDir string,
	logger *observability.SafeLogger,
	providerHealth func(context.Context) ([]diagnostics.ProviderHealth, error),
) *diagnostics.Exporter {
	t.Helper()

	exporter, err := diagnostics.NewExporter(diagnostics.Options{
		DataDir: dataDir,
		Runtime: func() diagnostics.RuntimeSnapshot {
			return diagnostics.RuntimeSnapshot{
				State:        "running",
				DataPlaneURL: "http://127.0.0.1:18443?local_key=diagnostic-query-secret",
				Version:      "0.1.0-rc.6",
			}
		},
		Logger: logger,
		Migrations: func(context.Context) ([]diagnostics.Migration, error) {
			return []diagnostics.Migration{{
				Version: 3,
				Name:    "0003_usage_token_reporting.sql",
			}}, nil
		},
		CredentialProbe: func(context.Context) diagnostics.CredentialStore {
			return diagnostics.CredentialStore{
				Available: true,
				Backend:   "memory",
			}
		},
		ProviderHealth: providerHealth,
		Now: func() time.Time {
			return time.Date(2026, 8, 16, 17, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return exporter
}

func assertArchiveEntries(t *testing.T, archive []byte) {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)

	expected := []string{
		"credential-store.json",
		"manifest.json",
		"migration.json",
		"provider-health.json",
		"recent-errors.json",
		"runtime.json",
	}
	if strings.Join(names, ",") != strings.Join(expected, ",") {
		t.Fatalf("ZIP 条目=%v", names)
	}
}
