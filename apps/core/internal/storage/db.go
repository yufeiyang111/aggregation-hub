package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrInvalidDatabasePath = errors.New("数据库路径无效")

const databaseInitializationTimeout = 5 * time.Second

// Open 以单连接、WAL 和外键约束打开 SQLite 文件。V1 优先保证单用户写入正确性。
func Open(path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrInvalidDatabasePath
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), databaseInitializationTimeout)
	defer cancel()

	for _, pragma := range []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := database.ExecContext(ctx, pragma); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("配置 SQLite 失败: %w", err)
		}
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("连接 SQLite 失败: %w", err)
	}

	return database, nil
}
