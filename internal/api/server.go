package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"openvow/internal/artifact"
	"openvow/internal/pipeline"
	"openvow/internal/store"
)

type Config struct {
	Repo          string
	MaxFileBytes  int64
	MaxTotalBytes int64
	LeaseTTL      time.Duration
	Version       string
}

type Server struct {
	cfg  Config
	st   *store.Store
	art  *artifact.Store
	pipe *pipeline.Service
}

func New(st *store.Store, art *artifact.Store, pipe *pipeline.Service, cfg Config) *Server {
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 30 * time.Minute
	}
	return &Server{cfg: cfg, st: st, art: art, pipe: pipe}
}

type ctxKey int

const terminalKey ctxKey = 0

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("POST /v1/claim", s.auth(http.HandlerFunc(s.handleClaim)))
	mux.Handle("POST /v1/heartbeat", s.auth(http.HandlerFunc(s.handleHeartbeat)))
	mux.Handle("POST /v1/submit", s.auth(http.HandlerFunc(s.handleSubmit)))
	mux.Handle("PUT /v1/artifacts/{sha256}", s.auth(http.HandlerFunc(s.handleUpload)))
	mux.Handle("GET /v1/artifacts/{sha256}", s.auth(http.HandlerFunc(s.handleDownload)))
	mux.Handle("GET /v1/tasks/peek", s.auth(http.HandlerFunc(s.handlePeek)))
	return mux
}

// ---------- 中间件 ----------

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		t, err := s.st.TerminalByToken(tok)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), terminalKey, t)))
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func terminalFrom(r *http.Request) *store.Terminal {
	t, _ := r.Context().Value(terminalKey).(*store.Terminal)
	return t
}

// ---------- handlers ----------

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Repo    string `json:"repo"`
	Tasks   int    `json:"tasks"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.CountTasks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: s.cfg.Version, Repo: s.cfg.Repo, Tasks: n})
}

type claimRequest struct {
	Kinds  []string `json:"kinds"`
	Labels []string `json:"labels"`
}

type claimResponse struct {
	TaskID         string               `json:"task_id"`
	Kind           string               `json:"kind"`
	Title          string               `json:"title"`
	Summary        string               `json:"summary"`
	Writes         []string             `json:"writes"`
	Reads          []string             `json:"reads"`
	Frozen         []store.FrozenFile   `json:"frozen"`
	Acceptance     json.RawMessage      `json:"acceptance"`
	LeaseID        string               `json:"lease_id"`
	LeaseExpiresAt string               `json:"lease_expires_at"`
	Attempt        int                  `json:"attempt"`
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var req claimRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	term := terminalFrom(r)
	task, err := s.st.Claim(term.ID, req.Kinds, s.cfg.LeaseTTL)
	if errors.Is(err, store.ErrNoTask) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, claimResponse{
		TaskID: task.ID, Kind: task.Kind, Title: task.Title, Summary: task.Summary,
		Writes: task.Writes, Reads: task.Reads, Frozen: task.Frozen,
		Acceptance: task.Acceptance, LeaseID: task.LeaseID,
		LeaseExpiresAt: task.LeaseUntil, Attempt: task.Attempts,
	})
}

type heartbeatRequest struct {
	TaskID  string `json:"task_id"`
	LeaseID string `json:"lease_id"`
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req heartbeatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	remaining, err := s.st.Heartbeat(req.TaskID, req.LeaseID, s.cfg.LeaseTTL)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "lease_expired", "lease is gone or already reclaimed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"remaining_seconds": remaining})
}

type submitFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type submitRequest struct {
	TaskID  string       `json:"task_id"`
	LeaseID string       `json:"lease_id"`
	Attempt int          `json:"attempt"`
	Files   []submitFile `json:"files"`
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	files := make([]pipeline.FileRef, len(req.Files))
	for i, f := range req.Files {
		files[i] = pipeline.FileRef{Path: f.Path, SHA256: f.SHA256}
	}
	res, err := s.pipe.Submit(r.Context(), pipeline.Request{
		TaskID: req.TaskID, LeaseID: req.LeaseID, Attempt: req.Attempt, Files: files,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	want := r.PathValue("sha256")
	limit := s.cfg.MaxFileBytes
	if limit <= 0 {
		limit = 2 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1)
	sha, size, err := s.art.Put(r.Body)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "artifact_too_large", "file exceeds limit")
			return
		}
		writeError(w, http.StatusBadRequest, "artifact_invalid", err.Error())
		return
	}
	if sha != want {
		writeError(w, http.StatusBadRequest, "artifact_hash_mismatch",
			"declared sha256 "+want+" does not match content "+sha)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sha256": sha, "size": size})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	f, err := s.art.Open(r.PathValue("sha256"))
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact_missing", "no such artifact")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	_, _ = io.Copy(w, f)
}

func (s *Server) handlePeek(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	tasks, err := s.st.ListTasks(state, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// ---------- 工具 ----------

func decodeJSON(r *http.Request, v any) error {
	defer io.Copy(io.Discard, r.Body)
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
