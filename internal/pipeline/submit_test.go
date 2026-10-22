package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openvow/internal/artifact"
	"openvow/internal/store"
)

type fixture struct {
	svc  *Service
	st   *store.Store
	repo string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	art, err := artifact.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatalf("artifact store: %v", err)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Store:         st,
		Artifacts:     art,
		Repo:          repo,
		MaxFileBytes:  2 << 20,
		MaxTotalBytes: 50 << 20,
	}
	return &fixture{svc: svc, st: st, repo: repo}
}

func (f *fixture) put(t *testing.T, content string) string {
	t.Helper()
	sha, _, err := f.svc.Artifacts.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return sha
}

func (f *fixture) task(t *testing.T, acceptance string, writes []string, frozen []store.FrozenFile) *store.Task {
	t.Helper()
	tk := &store.Task{
		ID:          "T-1",
		Kind:        "implement",
		Title:       "demo",
		Writes:      writes,
		Frozen:      frozen,
		Acceptance:  json.RawMessage(acceptance),
		State:       store.StateClaimed,
		Attempts:    1,
		MaxAttempts: 2,
		LeaseID:     "L-1",
		LeaseUntil:  time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
	}
	if err := f.st.InsertTask(tk); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return tk
}

func green() string { return `{"kind":"green_tests","build":"exit 0","run":"exit 0"}` }

func TestSubmit_Accepted(t *testing.T) {
	f := newFixture(t)
	f.task(t, green(), []string{"src/**"}, nil)

	sha := f.put(t, "package main\n")
	res, err := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/main.go", SHA256: sha}},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("want accepted, got %+v", res)
	}
	tk, _ := f.st.GetTask("T-1")
	if tk.State != store.StateAccepted {
		t.Errorf("state = %q", tk.State)
	}
}

func TestSubmit_Idempotent(t *testing.T) {
	f := newFixture(t)
	f.task(t, green(), []string{"src/**"}, nil)
	sha := f.put(t, "x")

	req := Request{TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/a.go", SHA256: sha}}}

	r1, _ := f.svc.Submit(context.Background(), req)
	// 第二次换一个不合法的租约：若幂等生效，应当返回上一次的结论而不是报错
	r2, err := f.svc.Submit(context.Background(), Request{TaskID: "T-1", LeaseID: "WRONG", Attempt: 1,
		Files: []FileRef{{Path: "src/a.go", SHA256: sha}}})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if r1.Accepted != r2.Accepted || r2.Code != "" {
		t.Fatalf("idempotency broken: r1=%+v r2=%+v", r1, r2)
	}
}

// 过期租约：任务必须带着已失效的 lease_until 被认领过。
func TestSubmit_LeaseExpired(t *testing.T) {
	f := newFixture(t)
	tk := &store.Task{
		ID: "T-1", Kind: "implement", Title: "demo", Writes: []string{"src/**"},
		Acceptance: json.RawMessage(green()), State: store.StateClaimed,
		Attempts: 1, MaxAttempts: 2, LeaseID: "L-1",
		LeaseUntil: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}
	if err := f.st.InsertTask(tk); err != nil {
		t.Fatal(err)
	}
	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/a.go", SHA256: strings.Repeat("a", 64)}},
	})
	if res.Code != CodeLeaseExpired {
		t.Fatalf("want %s, got %s", CodeLeaseExpired, res.Code)
	}
}

func TestSubmit_AttemptMismatch(t *testing.T) {
	f := newFixture(t)
	f.task(t, green(), []string{"src/**"}, nil)
	sha := f.put(t, "x")
	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 2,
		Files: []FileRef{{Path: "src/a.go", SHA256: sha}},
	})
	if res.Code != CodeAttemptMismatch {
		t.Fatalf("want %s, got %s", CodeAttemptMismatch, res.Code)
	}
}

func TestSubmit_OutsideWrites(t *testing.T) {
	f := newFixture(t)
	f.task(t, green(), []string{"src/**"}, nil)
	sha := f.put(t, "x")
	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "main.go", SHA256: sha}},
	})
	if res.Code != "path_outside_writes" {
		t.Fatalf("want path_outside_writes, got %s", res.Code)
	}
}

func TestSubmit_FrozenSubmitted(t *testing.T) {
	f := newFixture(t)
	sha := f.put(t, "func TestX(t *testing.T) {}")
	f.task(t, green(), []string{"src/**"}, []store.FrozenFile{{Path: "src/api.go", SHA256: sha}})

	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/api.go", SHA256: sha}},
	})
	if res.Code != "frozen_file_modified" {
		t.Fatalf("want frozen_file_modified, got %s", res.Code)
	}
}

func TestSubmit_ArtifactMissing(t *testing.T) {
	f := newFixture(t)
	f.task(t, green(), []string{"src/**"}, nil)
	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/a.go", SHA256: strings.Repeat("b", 64)}},
	})
	if res.Code != CodeArtifactMissing {
		t.Fatalf("want %s, got %s", CodeArtifactMissing, res.Code)
	}
}

func TestSubmit_RedTestsNotRed(t *testing.T) {
	f := newFixture(t)
	f.task(t, `{"kind":"red_tests","build":"exit 0","run":"exit 0"}`, []string{"src/**"}, nil)
	sha := f.put(t, "x")
	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/a_test.go", SHA256: sha}},
	})
	if res.Code != "tests_not_red" {
		t.Fatalf("want tests_not_red, got %s", res.Code)
	}
	tk, _ := f.st.GetTask("T-1")
	if tk.State != store.StateQueued {
		t.Errorf("state = %q, want queued", tk.State)
	}
}

func TestSubmit_GreenTestsFail(t *testing.T) {
	f := newFixture(t)
	f.task(t, `{"kind":"green_tests","build":"exit 0","run":"exit 3"}`, []string{"src/**"}, nil)
	sha := f.put(t, "x")
	res, _ := f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 1,
		Files: []FileRef{{Path: "src/a.go", SHA256: sha}},
	})
	if res.Code != "acceptance_failed" {
		t.Fatalf("want acceptance_failed, got %s", res.Code)
	}
}

func TestSubmit_ExhaustedAttemptsNeedsHuman(t *testing.T) {
	f := newFixture(t)
	tk := &store.Task{
		ID: "T-1", Kind: "implement", Title: "demo", Writes: []string{"src/**"},
		Acceptance: json.RawMessage(`{"kind":"green_tests","build":"exit 0","run":"exit 1"}`),
		State:      store.StateClaimed, Attempts: 2, MaxAttempts: 2,
		LeaseID: "L-1", LeaseUntil: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
	}
	if err := f.st.InsertTask(tk); err != nil {
		t.Fatal(err)
	}
	sha := f.put(t, "x")
	_, _ = f.svc.Submit(context.Background(), Request{
		TaskID: "T-1", LeaseID: "L-1", Attempt: 2,
		Files: []FileRef{{Path: "src/a.go", SHA256: sha}},
	})
	got, _ := f.st.GetTask("T-1")
	if got.State != store.StateNeedsHuman {
		t.Fatalf("state = %q, want needs_human", got.State)
	}
}
