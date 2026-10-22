package store

// TaskSummary 是看板用的轻量视图。
type TaskSummary struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Attempts int    `json:"attempts"`
}

// ListTasks 按状态过滤（空串表示全部），按创建时间倒序。
func (s *Store) ListTasks(state string, limit int) ([]TaskSummary, error) {
	q := `SELECT id, kind, title, state, attempts FROM tasks`
	args := []any{}
	if state != "" {
		q += ` WHERE state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TaskSummary{}
	for rows.Next() {
		var t TaskSummary
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.State, &t.Attempts); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
