package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"aggregationhub.local/core/internal/storage"
)

func TestRetentionPrunesTerminalRequestsInBatchesWithoutLosingDailyUsage(t *testing.T) {
	database := openMigratedDatabase(t)
	repository, err := storage.NewRetentionRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 16, 10, 0, 0, 0, time.UTC)
	oldCompleted := now.AddDate(0, 0, -31).UnixMilli()
	newCompleted := now.AddDate(0, 0, -2).UnixMilli()

	for index := 0; index < 501; index++ {
		insertRetentionRequest(t, database, fmt.Sprintf("retention-old-%03d", index), "succeeded", oldCompleted)
	}
	insertRetentionRequest(t, database, "retention-new", "succeeded", newCompleted)
	insertRetentionRequest(t, database, "retention-pending", "pending", 0)
	if _, err := database.Exec(`INSERT INTO usage_daily(date_utc,provider_slug_snapshot,public_model_snapshot,request_count,succeeded_count,failed_count,cancelled_count,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,reasoning_tokens,estimated_cost_microusd,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "2026-07-16", "demo", "demo/model", 501, 501, 0, 0, 1234, 0, 0, 0, 0, 0, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	result, err := repository.PruneRequests(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("清理过期请求失败: %v", err)
	}
	if result.DeletedRequests != 501 || result.Batches != 2 {
		t.Fatalf("清理结果=%+v，期望 501 条、2 批", result)
	}
	assertRequestCount(t, database, 2)
	var requestCount, tokenCount int64
	if err := database.QueryRow(`SELECT request_count,input_tokens FROM usage_daily WHERE date_utc='2026-07-16' AND provider_slug_snapshot='demo' AND public_model_snapshot='demo/model'`).Scan(&requestCount, &tokenCount); err != nil {
		t.Fatalf("读取日用量失败: %v", err)
	}
	if requestCount != 501 || tokenCount != 1234 {
		t.Fatalf("保留清理不得丢失已聚合日用量: requests=%d tokens=%d", requestCount, tokenCount)
	}
	var audits int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type='request_retention_pruned'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("保留清理审计数量=%d，期望 2", audits)
	}

	second, err := repository.PruneRequests(context.Background(), 30, now)
	if err != nil || second.DeletedRequests != 0 || second.Batches != 0 {
		t.Fatalf("重复清理结果=%+v err=%v，期望幂等", second, err)
	}
}

func TestRetentionHonorsCancelledContext(t *testing.T) {
	repository, err := storage.NewRetentionRepository(openMigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.PruneRequests(ctx, 30, time.Now().UTC()); err == nil {
		t.Fatal("已取消的保留任务不应继续执行")
	}
}

func insertRetentionRequest(t *testing.T, database *sql.DB, id string, status string, completedAt int64) {
	t.Helper()
	var completed any
	if completedAt > 0 {
		completed = completedAt
	}
	if _, err := database.Exec(`INSERT INTO requests(id,provider_slug_snapshot,public_model_snapshot,upstream_model_snapshot,source_protocol,endpoint,streaming,status,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, "demo", "demo/model", "model", "openai_chat", "/v1/chat/completions", 0, status, completedAt-1000, completed); err != nil {
		t.Fatalf("写入保留测试请求失败: %v", err)
	}
}

func assertRequestCount(t *testing.T, database *sql.DB, expected int) {
	t.Helper()
	var actual int
	if err := database.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("请求记录数量=%d，期望=%d", actual, expected)
	}
}
