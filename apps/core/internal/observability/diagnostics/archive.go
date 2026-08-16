package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"aggregationhub.local/core/internal/security"
)

var allowedArchiveEntries = map[string]struct{}{
	"credential-store.json": {},
	"manifest.json":         {},
	"migration.json":        {},
	"provider-health.json":  {},
	"recent-errors.json":    {},
	"runtime.json":          {},
}

func buildArchive(entries map[string]any) ([]byte, error) {
	names, err := validatedEntryNames(entries)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		if err := writeArchiveEntry(writer, name, entries[name]); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, errors.New("完成诊断包失败")
	}

	return buffer.Bytes(), nil
}

func diagnosticContentEntryNames() []string {
	return []string{
		"credential-store.json",
		"migration.json",
		"provider-health.json",
		"recent-errors.json",
		"runtime.json",
	}
}

func validatedEntryNames(entries map[string]any) ([]string, error) {
	if len(entries) != len(allowedArchiveEntries) {
		return nil, errors.New("诊断条目不完整")
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		if !isAllowedEntryName(name) {
			return nil, errors.New("诊断条目无效")
		}
		names = append(names, name)
	}

	for allowedName := range allowedArchiveEntries {
		if _, ok := entries[allowedName]; !ok {
			return nil, errors.New("诊断条目不完整")
		}
	}

	sort.Strings(names)
	return names, nil
}

func isAllowedEntryName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return false
	}

	_, ok := allowedArchiveEntries[name]
	return ok
}

func writeArchiveEntry(writer *zip.Writer, name string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return errors.New("编码诊断条目失败")
	}
	if security.ContainsDiagnosticSecret(string(payload)) {
		return errors.New("诊断内容包含敏感标记")
	}

	file, err := writer.Create(name)
	if err != nil {
		return errors.New("创建诊断条目失败")
	}
	if _, err := file.Write(payload); err != nil {
		return errors.New("写入诊断条目失败")
	}

	return nil
}
