package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateCreatesTables(t *testing.T) {
	db := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 幂等：再跑一次不应报错
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate twice: %v", err)
	}

	want := []string{"terminals", "tasks", "artifacts", "task_files", "submissions", "events"}
	for _, name := range want {
		var got string
		err := st.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %q missing: %v", name, err)
		}
	}

	n, err := st.CountTasks()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("fresh db should have 0 tasks, got %d", n)
	}
}
