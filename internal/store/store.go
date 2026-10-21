package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store 是所有持久化的唯一入口。
// M0 使用 SQLite，M2 迁移 Postgres 时保持字段语义不变。
type Store struct {
	db *sql.DB
}

// Open 打开数据库并设置 PRAGMA。
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者：把连接数压到 1，避免 SQLITE_BUSY。
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接，供测试与后续仓储层使用。
func (s *Store) DB() *sql.DB { return s.db }

const schema = `
CREATE TABLE IF NOT EXISTS terminals (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  token_hash  BLOB NOT NULL,
  created_at  TEXT NOT NULL,
  banned_at   TEXT
);

CREATE TABLE IF NOT EXISTS tasks (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN ('interface','test','implement')),
  title           TEXT NOT NULL,
  summary         TEXT NOT NULL DEFAULT '',
  writes_json     TEXT NOT NULL,
  reads_json      TEXT NOT NULL DEFAULT '[]',
  frozen_json     TEXT NOT NULL DEFAULT '[]',
  acceptance_json TEXT NOT NULL,
  requires_json   TEXT NOT NULL DEFAULT '[]',
  state           TEXT NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  max_attempts    INTEGER NOT NULL DEFAULT 2,
  lease_id        TEXT,
  lease_until     TEXT,
  terminal_id     TEXT REFERENCES terminals(id),
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS tasks_state_idx ON tasks (state, created_at);

CREATE TABLE IF NOT EXISTS artifacts (
  sha256     TEXT PRIMARY KEY,
  size       INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS task_files (
  task_id  TEXT NOT NULL,
  attempt  INTEGER NOT NULL,
  path     TEXT NOT NULL,
  sha256   TEXT NOT NULL REFERENCES artifacts(sha256),
  PRIMARY KEY (task_id, attempt, path)
);

CREATE TABLE IF NOT EXISTS submissions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id     TEXT NOT NULL,
  attempt     INTEGER NOT NULL,
  lease_id    TEXT NOT NULL,
  accepted    INTEGER NOT NULL,
  code        TEXT,
  detail_json TEXT,
  created_at  TEXT NOT NULL,
  UNIQUE (task_id, attempt)
);

CREATE TABLE IF NOT EXISTS events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id      TEXT,
  kind         TEXT NOT NULL,
  payload_json TEXT,
  at           TEXT NOT NULL
);
`

// Migrate 建立 M0 的六张表，幂等。
func (s *Store) Migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return tx.Commit()
}

// CountTasks 返回任务总数，供 /healthz 使用。
func (s *Store) CountTasks() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n)
	return n, err
}
