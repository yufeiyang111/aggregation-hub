// Package migrations 暴露嵌入到 Core 二进制中的前向 SQLite 迁移。
package migrations

import "embed"

// FS 只包含按版本排序的 SQL 迁移文件；运行时不得从任意本地路径加载迁移。
//
//go:embed *.sql
var FS embed.FS
