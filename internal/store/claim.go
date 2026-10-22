package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrNoTask 表示当前没有可认领的任务。
var ErrNoTask = errors.New("no claimable task")

// ExpireLeases 回收过期租约：还有重试机会的回 queued，否则进 needs_human。
func (s *Store) ExpireLeases() (int, error) {
	res, err := s.db.Exec(`
		UPDATE tasks
		SET state = CASE WHEN attempts >= max_attempts THEN 'needs_human' ELSE 'queued' END,
		    lease_id = NULL, lease_until = NULL, terminal_id = NULL, updated_at = ?
		WHERE state = 'claimed' AND lease_until IS NOT NULL AND lease_until < ?`, now(), now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Claim 原子地认领一个任务：在事务里挑候选、查依赖、再带条件更新。
// 条件更新（WHERE state='queued'）保证就算有并发也不会重复派发。
func (s *Store) Claim(terminalID string, kinds []string, lease time.Duration) (*Task, error) {
	if _, err := s.ExpireLeases(); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query := `SELECT id FROM tasks WHERE state = 'queued'`
	args := []any{}
	if len(kinds) > 0 {
		query += ` AND kind IN (` + placeholders(len(kinds)) + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	query += ` ORDER BY created_at LIMIT 50`

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, id := range ids {
		ready, err := requiresMet(tx, id)
		if err != nil {
			return nil, err
		}
		if !ready {
			continue
		}
		leaseID, err := randomID("L-")
		if err != nil {
			return nil, err
		}
		until := time.Now().Add(lease).UTC().Format(time.RFC3339)
		res, err := tx.Exec(`
			UPDATE tasks
			SET state = 'claimed', lease_id = ?, lease_until = ?, terminal_id = ?,
			    attempts = attempts + 1, updated_at = ?
			WHERE id = ? AND state = 'queued'`,
			leaseID, until, terminalID, now(), id)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			continue // 被别的连接抢走了
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetTask(id)
	}
	return nil, ErrNoTask
}

// Heartbeat 续租，返回剩余秒数。
func (s *Store) Heartbeat(taskID, leaseID string, extend time.Duration) (int64, error) {
	until := time.Now().Add(extend).UTC()
	res, err := s.db.Exec(`UPDATE tasks SET lease_until = ?, updated_at = ? WHERE id = ? AND lease_id = ? AND state = 'claimed'`,
		until.Format(time.RFC3339), now(), taskID, leaseID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, sql.ErrNoRows
	}
	return int64(extend.Seconds()), nil
}

func requiresMet(tx *sql.Tx, taskID string) (bool, error) {
	var raw string
	if err := tx.QueryRow(`SELECT requires_json FROM tasks WHERE id = ?`, taskID).Scan(&raw); err != nil {
		return false, err
	}
	var requires []string
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &requires); err != nil {
			return false, nil
		}
	}
	for _, id := range requires {
		var state string
		if err := tx.QueryRow(`SELECT state FROM tasks WHERE id = ?`, id).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		if state != StateAccepted {
			return false, nil
		}
	}
	return true, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ",?"
	}
	return out
}
