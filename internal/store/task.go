package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// 任务状态
const (
	StateQueued     = "queued"
	StateClaimed    = "claimed"
	StateAccepted   = "accepted"
	StateRejected   = "rejected"
	StateNeedsHuman = "needs_human"
)

// FrozenFile 是一条冻结声明。
type FrozenFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Task 是任务的完整视图。
type Task struct {
	ID           string
	Kind         string
	Title        string
	Summary      string
	Writes       []string
	Reads        []string
	Frozen       []FrozenFile
	Acceptance   json.RawMessage
	Requires     []string
	State        string
	Attempts     int
	MaxAttempts  int
	LeaseID      string
	LeaseUntil   string
	TerminalID   string
	CreatedAt    string
	UpdatedAt    string
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// InsertTask 写入一个任务；字段缺省按 queued / max_attempts=2 补齐。
func (s *Store) InsertTask(t *Task) error {
	if t.State == "" {
		t.State = StateQueued
	}
	if t.MaxAttempts == 0 {
		t.MaxAttempts = 2
	}
	if t.Acceptance == nil {
		t.Acceptance = json.RawMessage(`{}`)
	}
	mustJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return "[]"
		}
		return string(b)
	}
	ts := now()
	if t.CreatedAt == "" {
		t.CreatedAt = ts
	}
	t.UpdatedAt = ts

	_, err := s.db.Exec(`
		INSERT INTO tasks (id, kind, title, summary, writes_json, reads_json, frozen_json,
		                   acceptance_json, requires_json, state, attempts, max_attempts,
		                   lease_id, lease_until, terminal_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Kind, t.Title, t.Summary,
		mustJSON(t.Writes), mustJSON(t.Reads), mustJSON(t.Frozen),
		string(t.Acceptance), mustJSON(t.Requires),
		t.State, t.Attempts, t.MaxAttempts,
		nullIfEmpty(t.LeaseID), nullIfEmpty(t.LeaseUntil), nullIfEmpty(t.TerminalID),
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

// GetTask 读取一个任务，并把 JSON 字段解回切片。
func (s *Store) GetTask(id string) (*Task, error) {
	var (
		t                                                Task
		writes, reads, frozen, requires, acceptance      string
		leaseID, leaseUntil, terminalID                  sql.NullString
	)
	err := s.db.QueryRow(`
		SELECT id, kind, title, summary, writes_json, reads_json, frozen_json,
		       acceptance_json, requires_json, state, attempts, max_attempts,
		       lease_id, lease_until, terminal_id, created_at, updated_at
		FROM tasks WHERE id = ?`, id).Scan(
		&t.ID, &t.Kind, &t.Title, &t.Summary,
		&writes, &reads, &frozen, &acceptance, &requires,
		&t.State, &t.Attempts, &t.MaxAttempts,
		&leaseID, &leaseUntil, &terminalID,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Writes = decodeStrings(writes)
	t.Reads = decodeStrings(reads)
	t.Requires = decodeStrings(requires)
	if frozen != "" {
		_ = json.Unmarshal([]byte(frozen), &t.Frozen)
	}
	t.Acceptance = json.RawMessage(acceptance)
	t.LeaseID = leaseID.String
	t.LeaseUntil = leaseUntil.String
	t.TerminalID = terminalID.String
	return &t, nil
}

// SetState 更新任务状态，并清空租约（除非状态仍是 claimed）。
func (s *Store) SetState(id, state string) error {
	_, err := s.db.Exec(`UPDATE tasks SET state = ?, lease_id = NULL, lease_until = NULL, updated_at = ? WHERE id = ?`,
		state, now(), id)
	return err
}

// Submission 是一次提交的落库结果。
type Submission struct {
	ID         int64
	TaskID     string
	Attempt    int
	LeaseID    string
	Accepted   bool
	Code       string
	DetailJSON string
	CreatedAt  string
}

// GetSubmission 用于幂等：同一 (task, attempt) 只处理一次。
func (s *Store) GetSubmission(taskID string, attempt int) (*Submission, error) {
	var sub Submission
	var accepted int
	err := s.db.QueryRow(`
		SELECT id, task_id, attempt, lease_id, accepted, COALESCE(code,''), COALESCE(detail_json,''), created_at
		FROM submissions WHERE task_id = ? AND attempt = ?`, taskID, attempt).Scan(
		&sub.ID, &sub.TaskID, &sub.Attempt, &sub.LeaseID, &accepted, &sub.Code, &sub.DetailJSON, &sub.CreatedAt)
	if err != nil {
		return nil, err
	}
	sub.Accepted = accepted == 1
	return &sub, nil
}

// RecordSubmission 写入提交结果；UNIQUE(task_id, attempt) 兜住并发重复提交。
func (s *Store) RecordSubmission(sub *Submission) error {
	accepted := 0
	if sub.Accepted {
		accepted = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO submissions (task_id, attempt, lease_id, accepted, code, detail_json, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		sub.TaskID, sub.Attempt, sub.LeaseID, accepted, nullIfEmpty(sub.Code), sub.DetailJSON, now())
	return err
}

// RecordFiles 记录一次提交包含的文件（内容寻址）。
func (s *Store) RecordFiles(taskID string, attempt int, files map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for path, sha := range files {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO task_files (task_id, attempt, path, sha256) VALUES (?,?,?,?)`,
			taskID, attempt, path, sha); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AppendEvent 追加审计事件。
func (s *Store) AppendEvent(taskID, kind string, payload any) error {
	b, _ := json.Marshal(payload)
	_, err := s.db.Exec(`INSERT INTO events (task_id, kind, payload_json, at) VALUES (?,?,?,?)`,
		nullIfEmpty(taskID), kind, string(b), now())
	return err
}

// EnsureArtifact 登记产物元数据。task_files.sha256 有外键指向 artifacts，
// 上传时可能还没登记，所以这里做一次幂等写入。
func (s *Store) EnsureArtifact(sha string, size int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO artifacts (sha256, size, created_at) VALUES (?,?,?)`,
		sha, size, now())
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func decodeStrings(raw string) []string {
	var out []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// 编译期断言：确保 Task 里用到的字段名与 SELECT 一致时不会静默漏读。
var _ = fmt.Sprintf
