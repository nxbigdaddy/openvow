package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openvow/internal/artifact"
	"openvow/internal/pipeline"
	"openvow/internal/store"
)

type testEnv struct {
	srv   *Server
	st    *store.Store
	token string
	repo  string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	art, err := artifact.New(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, token, err := st.CreateTerminal("term-1")
	if err != nil {
		t.Fatal(err)
	}
	pipe := &pipeline.Service{Store: st, Artifacts: art, Repo: repo, MaxFileBytes: 2 << 20, MaxTotalBytes: 50 << 20}
	srv := New(st, art, pipe, Config{Repo: repo, MaxFileBytes: 2 << 20, LeaseTTL: 30 * time.Minute, Version: "test"})
	return &testEnv{srv: srv, st: st, token: token, repo: repo}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if auth {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	rec := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) addTask(t *testing.T, acceptance string) {
	t.Helper()
	tk := &store.Task{
		ID: "T-1", Kind: "implement", Title: "demo", Writes: []string{"src/**"},
		Acceptance: json.RawMessage(acceptance),
	}
	if err := e.st.InsertTask(tk); err != nil {
		t.Fatal(err)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/healthz", nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var h healthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &h)
	if h.Status != "ok" || h.Version != "test" {
		t.Fatalf("body = %+v", h)
	}
}

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodPost, "/v1/claim", map[string]any{}, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/claim", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer nope")
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", w.Code)
	}
}

func TestClaimAndSubmit(t *testing.T) {
	e := newEnv(t)
	e.addTask(t, `{"kind":"green_tests","build":"exit 0","run":"exit 0"}`)

	rec := e.do(t, http.MethodPost, "/v1/claim", map[string]any{}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var c claimResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.TaskID != "T-1" || c.LeaseID == "" || c.Attempt != 1 {
		t.Fatalf("claim = %+v", c)
	}

	content := []byte("package src\n")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])

	req := httptest.NewRequest(http.MethodPut, "/v1/artifacts/"+sha, bytes.NewReader(content))
	req.Header.Set("Authorization", "Bearer "+e.token)
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", w.Code, w.Body.String())
	}

	sub := e.do(t, http.MethodPost, "/v1/submit", map[string]any{
		"task_id": "T-1", "lease_id": c.LeaseID, "attempt": 1,
		"files": []map[string]string{{"path": "src/main.go", "sha256": sha}},
	}, true)
	if sub.Code != http.StatusOK {
		t.Fatalf("submit = %d, body=%s", sub.Code, sub.Body.String())
	}
	var res pipeline.Result
	_ = json.Unmarshal(sub.Body.Bytes(), &res)
	if !res.Accepted {
		t.Fatalf("not accepted: %+v", res)
	}
	tk, _ := e.st.GetTask("T-1")
	if tk.State != store.StateAccepted {
		t.Fatalf("state = %s", tk.State)
	}
}

func TestClaimEmpty(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodPost, "/v1/claim", map[string]any{}, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestUploadHashMismatch(t *testing.T) {
	e := newEnv(t)
	wrong := strings.Repeat("a", 64)
	req := httptest.NewRequest(http.MethodPut, "/v1/artifacts/"+wrong, strings.NewReader("hello"))
	req.Header.Set("Authorization", "Bearer "+e.token)
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHeartbeat(t *testing.T) {
	e := newEnv(t)
	e.addTask(t, `{"kind":"green_tests","build":"exit 0","run":"exit 0"}`)
	rec := e.do(t, http.MethodPost, "/v1/claim", map[string]any{}, true)
	var c claimResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &c)

	ok := e.do(t, http.MethodPost, "/v1/heartbeat", map[string]any{"task_id": "T-1", "lease_id": c.LeaseID}, true)
	if ok.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d", ok.Code)
	}
	bad := e.do(t, http.MethodPost, "/v1/heartbeat", map[string]any{"task_id": "T-1", "lease_id": "WRONG"}, true)
	if bad.Code != http.StatusConflict {
		t.Fatalf("bad lease = %d, want 409", bad.Code)
	}
}

func TestPeek(t *testing.T) {
	e := newEnv(t)
	e.addTask(t, `{"kind":"green_tests","build":"exit 0","run":"exit 0"}`)
	rec := e.do(t, http.MethodGet, "/v1/tasks/peek", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "T-1") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
