package observability_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/migrations"
)

func TestRecoverInFlightRequestsOnlyChangesPendingAndStreaming(t *testing.T) {
	database, err := storage.Open(filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatalf("打开临时数据库失败: %v", err)
	}
	defer database.Close()
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	for _, status := range []string{"pending", "streaming", "succeeded", "failed", "cancelled"} {
		if _, err := database.Exec(`INSERT INTO requests(id,provider_slug_snapshot,public_model_snapshot,upstream_model_snapshot,source_protocol,endpoint,streaming,status,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, "req-"+status, "p", "p/model", "model", "openai_chat", "/v1/chat/completions", 0, status, 1); err != nil {
			t.Fatalf("插入 %s 请求失败: %v", status, err)
		}
	}
	now := time.UnixMilli(1_700_000_200_000).UTC()
	count, err := observability.RecoverInFlightRequests(context.Background(), database, now)
	if err != nil || count != 2 {
		t.Fatalf("恢复结果 count=%d err=%v", count, err)
	}
	for _, id := range []string{"req-pending", "req-streaming"} {
		var status, errorCode string
		var completed int64
		if err := database.QueryRow(`SELECT status,error_code,completed_at FROM requests WHERE id=?`, id).Scan(&status, &errorCode, &completed); err != nil {
			t.Fatalf("读取 %s 失败: %v", id, err)
		}
		if status != "aborted_by_restart" || errorCode != "aborted_by_restart" || completed != now.UnixMilli() {
			t.Fatalf("恢复字段错误: %s %s %d", status, errorCode, completed)
		}
	}
	for _, id := range []string{"req-succeeded", "req-failed", "req-cancelled"} {
		var status string
		if err := database.QueryRow(`SELECT status FROM requests WHERE id=?`, id).Scan(&status); err != nil || status[4:] == "" {
			t.Fatalf("终态请求被错误修改: id=%s status=%s err=%v", id, status, err)
		}
	}
}
