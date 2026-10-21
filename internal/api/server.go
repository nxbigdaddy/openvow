package api

import (
	"encoding/json"
	"net/http"

	"openvow/internal/store"
)

// Server 承载 M0 的 HTTP 接口。
type Server struct {
	st      *store.Store
	repo    string
	version string
}

func New(st *store.Store, repo, version string) *Server {
	return &Server{st: st, repo: repo, version: version}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return mux
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Repo    string `json:"repo"`
	Tasks   int    `json:"tasks"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.CountTasks()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: s.version,
		Repo:    s.repo,
		Tasks:   n,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
