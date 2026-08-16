package diagnostics

import "testing"

func TestValidatedEntryNamesRejectsUnexpectedEntry(t *testing.T) {
	entries := map[string]any{
		"credential-store.json": true,
		"manifest.json":         true,
		"migration.json":        true,
		"provider-health.json":  true,
		"recent-errors.json":    true,
		"runtime.json":          true,
		"unexpected.json":       true,
	}

	if _, err := validatedEntryNames(entries); err == nil {
		t.Fatal("未知诊断条目必须被拒绝")
	}
}

func TestValidatedEntryNamesRequiresFixedAllowlist(t *testing.T) {
	entries := map[string]any{
		"credential-store.json": true,
		"manifest.json":         true,
		"migration.json":        true,
		"provider-health.json":  true,
		"recent-errors.json":    true,
	}

	if _, err := validatedEntryNames(entries); err == nil {
		t.Fatal("缺少固定诊断条目时必须拒绝")
	}
}
