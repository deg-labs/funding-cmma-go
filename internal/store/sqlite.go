// Package store は SQLite へのアクセスを提供します。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Options は SQLite 接続の設定です。
type Options struct {
	// ReadOnly が true の場合は読み取り専用 URI で接続します。
	ReadOnly bool
	// BusyTimeout はロック待機時間です。
	BusyTimeout time.Duration
	// MaxOpenConns は同時に開く接続数の上限です。0 の場合は既定値を使います。
	MaxOpenConns int
}

// Open は SQLite を開きます。
//
// 書き込み側は接続数 1、読み取り側は複数接続を使います。
// PRAGMA は DSN に埋め込み、プール内の各接続へ確実に適用します。
func Open(path string, opts Options) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("db path is empty")
	}
	if opts.BusyTimeout <= 0 {
		opts.BusyTimeout = 10 * time.Second
	}
	if !opts.ReadOnly {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	pragmas := []string{
		fmt.Sprintf("busy_timeout(%d)", opts.BusyTimeout.Milliseconds()),
	}
	if opts.ReadOnly {
		pragmas = append(pragmas, "query_only(1)")
	} else {
		pragmas = append(pragmas, "journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)")
	}

	dsn := "file:" + path + "?_pragma=" + url.QueryEscape(pragmas[0])
	for _, pragma := range pragmas[1:] {
		dsn += "&_pragma=" + url.QueryEscape(pragma)
	}
	if opts.ReadOnly {
		dsn += "&mode=ro"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	maxOpen := opts.MaxOpenConns
	if maxOpen <= 0 {
		if opts.ReadOnly {
			maxOpen = 4
		} else {
			maxOpen = 1
		}
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// Schema は既存 Python 実装と同一のテーブル定義です。
// 移行時にスキーマを変更しないでください。
const Schema = `
CREATE TABLE IF NOT EXISTS funding_rates (
    symbol TEXT NOT NULL,
    funding_rate REAL NOT NULL,
    funding_rate_timestamp INTEGER NOT NULL,
    next_funding_time INTEGER,
    funding_interval INTEGER,
    funding_rate_cap REAL,
    funding_rate_floor REAL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    PRIMARY KEY (symbol, funding_rate_timestamp)
)`

// EnsureSchema は funding_rates テーブルを作成します。
func EnsureSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, Schema); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	return nil
}

// SchemaExists は funding_rates テーブルが存在するかを返します。
func SchemaExists(ctx context.Context, db *sql.DB) (bool, error) {
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'funding_rates'`,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return name == "funding_rates", nil
}
