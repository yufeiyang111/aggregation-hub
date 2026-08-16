package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func writeMaintenanceAudit(ctx context.Context, transaction *sql.Tx, eventType string, detail map[string]any, now time.Time) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("编码维护审计详情失败: %w", err)
	}
	identifier, err := newMaintenanceAuditID(now)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO audit_events(id,event_type,entity_type,detail_json,created_at) VALUES(?,?,?,?,?)`, identifier, eventType, "maintenance", string(encoded), now.UTC().UnixMilli()); err != nil {
		return fmt.Errorf("写入维护审计事件失败: %w", err)
	}
	return nil
}

func newMaintenanceAuditID(now time.Time) (string, error) {
	bytes := make([]byte, 10)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成维护审计标识失败: %w", err)
	}
	return fmt.Sprintf("maintenance-%d-%s", now.UTC().UnixMilli(), hex.EncodeToString(bytes)), nil
}
