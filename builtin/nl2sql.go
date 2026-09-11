package builtin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Kirby980/agent/tool"
)

type NL2SQL struct {
	db *sql.DB
}

// NewNL2SQL 创建 NL2SQL 实例。传入的 db 建议配置为只读账号或事务。
func NewNL2SQL(db *sql.DB) *NL2SQL {
	return &NL2SQL{db: db}
}

type querySQLArgs struct {
	Query string `json:"query" desc:"只允许执行 SELECT 开头的只读 SQL 查询语句"`
}

// QueryTool 封装只读 SQL 执行工具
func (n *NL2SQL) QueryTool() tool.Tool {
	return tool.NewTypedTool("query_sql", "在数据库中执行只读 SELECT 查询，返回结构化表格文本（最多50行）",
		func(ctx context.Context, a querySQLArgs) (string, error) {
			if n.db == nil {
				return "", fmt.Errorf("数据库连接未配置或为空")
			}
			return runReadOnly(ctx, n.db, a.Query)
		})
}

// isSelectOnly 是代码层粗校验：只是纵深防御的一环，不能替代只读账号。
func isSelectOnly(query string) error {
	q := strings.TrimSpace(strings.ToLower(query))
	if !strings.HasPrefix(q, "select") {
		return fmt.Errorf("只允许 SELECT 查询")
	}
	if strings.Contains(q, ";") && !strings.HasSuffix(q, ";") {
		return fmt.Errorf("禁止多条语句")
	}
	for _, kw := range []string{"insert", "update", "delete", "drop", "alter", "truncate", "grant"} {
		if strings.Contains(q, kw) {
			return fmt.Errorf("检测到禁止的关键字: %s", kw)
		}
	}
	return nil
}

// runReadOnly 在只读连接上执行查询，带超时与行数限制。
func runReadOnly(ctx context.Context, db *sql.DB, query string) (string, error) {
	if err := isSelectOnly(query); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("查询失败: %w", err)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var sb strings.Builder
	sb.WriteString(strings.Join(cols, " | ") + "\n")

	count := 0
	const maxRows = 50
	for rows.Next() && count < maxRows {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		cells := make([]string, len(vals))
		for i, v := range vals {
			cells[i] = fmt.Sprintf("%v", v)
		}
		sb.WriteString(strings.Join(cells, " | ") + "\n")
		count++
	}
	return sb.String(), rows.Err()
}
