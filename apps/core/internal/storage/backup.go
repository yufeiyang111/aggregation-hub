package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const backupRetentionCount = 5

var (
	ErrInvalidBackupID      = errors.New("备份标识无效")
	backupIdentifierPattern = regexp.MustCompile(`^backup-[a-z0-9][a-z0-9.-]{0,94}$`)
)

type BackupRecord struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes"`
}

type RestoreSchedule struct {
	SafetyBackup    BackupRecord `json:"safety_backup"`
	RestartRequired bool         `json:"restart_required"`
}

type BackupManager struct {
	database *sql.DB
	dataDir  string
	now      func() time.Time
}

func NewBackupManager(database *sql.DB, dataDir string) (*BackupManager, error) {
	if database == nil || strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("备份管理器依赖无效")
	}
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("解析数据目录失败: %w", err)
	}
	return &BackupManager{database: database, dataDir: filepath.Clean(absolute), now: time.Now}, nil
}

// Create 将 SQLite 一致性快照写入固定 backups 目录；调用者无法指定路径或文件名。
func (manager *BackupManager) Create(ctx context.Context) (BackupRecord, error) {
	if ctx == nil {
		return BackupRecord{}, errors.New("备份上下文不能为空")
	}
	if err := ctx.Err(); err != nil {
		return BackupRecord{}, err
	}
	if err := os.MkdirAll(manager.backupsDirectory(), 0o700); err != nil {
		return BackupRecord{}, fmt.Errorf("创建备份目录失败: %w", err)
	}
	if _, err := manager.database.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		return BackupRecord{}, fmt.Errorf("SQLite WAL 检查点失败: %w", err)
	}
	identifier, err := newBackupIdentifier(manager.now().UTC())
	if err != nil {
		return BackupRecord{}, err
	}
	finalPath, err := manager.backupPath(identifier)
	if err != nil {
		return BackupRecord{}, err
	}
	temporaryPath := finalPath + ".partial"
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := manager.database.ExecContext(ctx, "VACUUM INTO ?", temporaryPath); err != nil {
		return BackupRecord{}, fmt.Errorf("创建 SQLite 备份快照失败: %w", err)
	}
	if err := verifySQLiteSnapshot(ctx, temporaryPath); err != nil {
		return BackupRecord{}, err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return BackupRecord{}, fmt.Errorf("完成备份快照失败: %w", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return BackupRecord{}, fmt.Errorf("读取备份快照信息失败: %w", err)
	}
	record := BackupRecord{ID: identifier, CreatedAt: info.ModTime().UTC(), SizeBytes: info.Size()}
	if err := manager.audit(ctx, "database_backup_created", map[string]any{"backup_id": record.ID, "size_bytes": record.SizeBytes}); err != nil {
		_ = os.Remove(finalPath)
		return BackupRecord{}, err
	}
	if err := manager.pruneOldBackups(ctx); err != nil {
		return BackupRecord{}, err
	}
	return record, nil
}

func (manager *BackupManager) List(ctx context.Context) ([]BackupRecord, error) {
	if ctx == nil {
		return nil, errors.New("备份读取上下文不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(manager.backupsDirectory())
	if errors.Is(err, os.ErrNotExist) {
		return []BackupRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取备份目录失败: %w", err)
	}
	result := make([]BackupRecord, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		identifier := strings.TrimSuffix(entry.Name(), ".db")
		if !backupIdentifierPattern.MatchString(identifier) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("读取备份文件信息失败: %w", err)
		}
		result = append(result, BackupRecord{ID: identifier, CreatedAt: info.ModTime().UTC(), SizeBytes: info.Size()})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID > result[right].ID
		}
		return result[left].CreatedAt.After(result[right].CreatedAt)
	})
	return result, nil
}

// ScheduleRestore 先验证并暂存所选快照，再生成当前数据库安全备份，最后提交固定 pending 文件。实际替换只在下一次 Core 启动、监听端口之前发生。
func (manager *BackupManager) ScheduleRestore(ctx context.Context, identifier string) (RestoreSchedule, error) {
	if ctx == nil {
		return RestoreSchedule{}, errors.New("恢复上下文不能为空")
	}
	sourcePath, err := manager.backupPath(identifier)
	if err != nil {
		return RestoreSchedule{}, err
	}
	if err := verifySQLiteSnapshot(ctx, sourcePath); err != nil {
		return RestoreSchedule{}, err
	}
	pendingPath := filepath.Join(manager.dataDir, "restore-pending.db")
	temporaryPath := pendingPath + ".partial"
	defer func() { _ = os.Remove(temporaryPath) }()
	// 先把用户选中的快照固定到 pending 临时文件，避免随后创建安全备份时的五份淘汰规则误删所选最旧备份。
	if err := copyFile(ctx, sourcePath, temporaryPath); err != nil {
		return RestoreSchedule{}, err
	}
	if err := verifySQLiteSnapshot(ctx, temporaryPath); err != nil {
		return RestoreSchedule{}, err
	}
	safetyBackup, err := manager.Create(ctx)
	if err != nil {
		return RestoreSchedule{}, err
	}
	if err := os.Rename(temporaryPath, pendingPath); err != nil {
		return RestoreSchedule{}, fmt.Errorf("写入待恢复快照失败: %w", err)
	}
	if err := manager.audit(ctx, "database_restore_scheduled", map[string]any{"backup_id": identifier, "safety_backup_id": safetyBackup.ID}); err != nil {
		_ = os.Remove(pendingPath)
		return RestoreSchedule{}, err
	}
	return RestoreSchedule{SafetyBackup: safetyBackup, RestartRequired: true}, nil
}

// ApplyPendingRestore 仅处理固定 dataDir 内的 pending 快照。旧数据库会先保留为固定 pre-restore 文件，任何失败都不会 reset 数据库。
func ApplyPendingRestore(dataDir string) (bool, error) {
	absolute, err := filepath.Abs(dataDir)
	if err != nil || strings.TrimSpace(dataDir) == "" {
		return false, errors.New("恢复数据目录无效")
	}
	root := filepath.Clean(absolute)
	pendingPath := filepath.Join(root, "restore-pending.db")
	if _, err := os.Stat(pendingPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("读取待恢复快照失败: %w", err)
	}
	if err := verifySQLiteSnapshot(context.Background(), pendingPath); err != nil {
		return false, err
	}
	currentPath := filepath.Join(root, "aggregation-hub.db")
	previousPath := filepath.Join(root, "aggregation-hub.pre-restore.db")
	if _, err := os.Stat(previousPath); err == nil {
		return false, errors.New("检测到未处理的恢复前数据库，已拒绝覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("检查恢复前数据库失败: %w", err)
	}
	currentExists := true
	if _, err := os.Stat(currentPath); errors.Is(err, os.ErrNotExist) {
		currentExists = false
	} else if err != nil {
		return false, fmt.Errorf("检查当前数据库失败: %w", err)
	}
	if currentExists {
		if err := os.Rename(currentPath, previousPath); err != nil {
			return false, fmt.Errorf("保留恢复前数据库失败: %w", err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.Remove(currentPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				_ = os.Rename(previousPath, currentPath)
				return false, fmt.Errorf("清理旧 SQLite 辅助文件失败: %w", err)
			}
		}
	}
	if err := os.Rename(pendingPath, currentPath); err != nil {
		if currentExists {
			_ = os.Rename(previousPath, currentPath)
		}
		return false, fmt.Errorf("应用待恢复快照失败: %w", err)
	}
	return true, nil
}

// FinalizePendingRestore 只在恢复后的主库已通过迁移和启动恢复检查后删除固定的回退副本。
func FinalizePendingRestore(dataDir string) error {
	absolute, err := filepath.Abs(dataDir)
	if err != nil || strings.TrimSpace(dataDir) == "" {
		return errors.New("恢复数据目录无效")
	}
	previousPath := filepath.Join(filepath.Clean(absolute), "aggregation-hub.pre-restore.db")
	if err := os.Remove(previousPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("清理恢复前数据库失败: %w", err)
	}
	return nil
}

func (manager *BackupManager) backupsDirectory() string {
	return filepath.Join(manager.dataDir, "backups")
}

func (manager *BackupManager) backupPath(identifier string) (string, error) {
	if !backupIdentifierPattern.MatchString(identifier) {
		return "", ErrInvalidBackupID
	}
	return filepath.Join(manager.backupsDirectory(), identifier+".db"), nil
}

func (manager *BackupManager) pruneOldBackups(ctx context.Context) error {
	backups, err := manager.List(ctx)
	if err != nil {
		return err
	}
	if len(backups) <= backupRetentionCount {
		return nil
	}
	for _, backup := range backups[backupRetentionCount:] {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := manager.backupPath(backup.ID)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("清理过期备份失败: %w", err)
		}
	}
	return nil
}

func (manager *BackupManager) audit(ctx context.Context, eventType string, detail map[string]any) error {
	transaction, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始备份审计事务失败: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := writeMaintenanceAudit(ctx, transaction, eventType, detail, manager.now().UTC()); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交备份审计事务失败: %w", err)
	}
	return nil
}

func newBackupIdentifier(now time.Time) (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("生成备份标识失败: %w", err)
	}
	return "backup-" + now.UTC().Format("20060102t150405.000z") + "-" + hex.EncodeToString(bytes), nil
}

func verifySQLiteSnapshot(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("读取 SQLite 快照失败: %w", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("打开 SQLite 快照失败: %w", err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return fmt.Errorf("配置 SQLite 快照只读检查失败: %w", err)
	}
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("校验 SQLite 快照失败: %w", err)
	}
	if integrity != "ok" {
		return errors.New("SQLite 快照完整性校验失败")
	}
	return nil
}

func copyFile(ctx context.Context, sourcePath string, destinationPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("读取备份快照失败: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建待恢复快照失败: %w", err)
	}
	defer destination.Close()
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if _, err := destination.Write(buffer[:read]); err != nil {
				return fmt.Errorf("写入待恢复快照失败: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("读取备份快照失败: %w", readErr)
		}
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("同步待恢复快照失败: %w", err)
	}
	return nil
}
