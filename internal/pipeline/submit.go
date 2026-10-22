// Package pipeline 是提交流水线：幂等 → 租约 → L0/L1 → 落盘 → L3 → 落库。
package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"openvow/internal/accept"
	"openvow/internal/artifact"
	"openvow/internal/contract"
	"openvow/internal/store"
	"openvow/internal/workspace"
)

// 流水线自己的拒绝码（其余来自 contract / accept）。
const (
	CodeLeaseExpired     = "lease_expired"
	CodeAttemptMismatch  = "attempt_mismatch"
	CodeArtifactMissing  = "artifact_missing"
	CodeArtifactTooLarge = "artifact_too_large"
	CodeWorkspaceFailed  = "workspace_failed"
)

type Service struct {
	Store         *store.Store
	Artifacts     *artifact.Store
	Repo          string
	MaxFileBytes  int64
	MaxTotalBytes int64
	Now           func() time.Time
}

type FileRef struct {
	Path   string
	SHA256 string
}

type Request struct {
	TaskID  string
	LeaseID string
	Attempt int
	Files   []FileRef
}

type Result struct {
	Accepted   bool
	Code       string
	Violations []contract.Violation
	Detail     map[string]any
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Submit(ctx context.Context, req Request) (Result, error) {
	task, err := s.Store.GetTask(req.TaskID)
	if err != nil {
		return Result{}, fmt.Errorf("load task: %w", err)
	}

	// 1. 幂等：同一 (task, attempt) 直接返回上次结论，不重复跑命令。
	if sub, err := s.Store.GetSubmission(req.TaskID, req.Attempt); err == nil {
		return Result{Accepted: sub.Accepted, Code: sub.Code, Detail: decodeDetail(sub.DetailJSON)}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}

	// 2. 租约与 attempt。attempt 由服务端计数，客户端不能自报一个更小的值来绕过幂等。
	if task.LeaseID == "" || task.LeaseID != req.LeaseID || !leaseValid(task.LeaseUntil, s.now()) {
		return s.reject(task, req, CodeLeaseExpired, nil, map[string]any{"lease_until": task.LeaseUntil})
	}
	if req.Attempt != task.Attempts {
		return s.reject(task, req, CodeAttemptMismatch, nil,
			map[string]any{"want": task.Attempts, "got": req.Attempt})
	}

	// 3. L0/L1：路径合法、不重复、⊆ 写集、不碰冻结文件。
	submitted := make([]contract.SubmittedFile, len(req.Files))
	for i, f := range req.Files {
		submitted[i] = contract.SubmittedFile{Path: f.Path, SHA256: f.SHA256}
	}
	if vs := contract.Check(task.Writes, toFrozen(task.Frozen), submitted); len(vs) > 0 {
		return s.reject(task, req, vs[0].Code, vs, map[string]any{"violations": vs})
	}

	// 4. 产物存在性与大小。
	var total int64
	sizes := make(map[string]int64, len(req.Files))
	for _, f := range req.Files {
		size, err := s.Artifacts.Size(f.SHA256)
		if err != nil {
			return s.reject(task, req, CodeArtifactMissing, nil, map[string]any{"path": f.Path, "sha256": f.SHA256})
		}
		if s.MaxFileBytes > 0 && size > s.MaxFileBytes {
			return s.reject(task, req, CodeArtifactTooLarge, nil,
				map[string]any{"path": f.Path, "size": size, "limit": s.MaxFileBytes})
		}
		sizes[f.SHA256] = size
		total += size
	}
	if s.MaxTotalBytes > 0 && total > s.MaxTotalBytes {
		return s.reject(task, req, CodeArtifactTooLarge, nil, map[string]any{"total": total, "limit": s.MaxTotalBytes})
	}
	// task_files 有外键指向 artifacts，先把元数据登记掉（幂等）。
	for sha, size := range sizes {
		if err := s.Store.EnsureArtifact(sha, size); err != nil {
			return Result{}, err
		}
	}

	// 5. 落盘：复制仓库 → 冻结版本覆盖 → 应用提交内容。
	ws, err := workspace.Open(s.Repo)
	if err != nil {
		return s.reject(task, req, CodeWorkspaceFailed, nil, map[string]any{"error": err.Error()})
	}
	defer ws.Close()

	for _, fr := range task.Frozen {
		f, err := s.Artifacts.Open(fr.SHA256)
		if err != nil {
			return s.reject(task, req, CodeArtifactMissing, nil,
				map[string]any{"frozen": fr.Path, "sha256": fr.SHA256})
		}
		err = ws.WriteAt(fr.Path, f, 0o644)
		f.Close()
		if err != nil {
			return s.reject(task, req, CodeWorkspaceFailed, nil, map[string]any{"frozen": fr.Path, "error": err.Error()})
		}
	}
	for _, f := range req.Files {
		fh, err := s.Artifacts.Open(f.SHA256)
		if err != nil {
			return s.reject(task, req, CodeArtifactMissing, nil, map[string]any{"path": f.Path})
		}
		err = ws.WriteAt(f.Path, fh, 0o644)
		fh.Close()
		if err != nil {
			return s.reject(task, req, CodeWorkspaceFailed, nil, map[string]any{"path": f.Path, "error": err.Error()})
		}
	}

	// 6. L3：跑验收。
	out := accept.Evaluate(ctx, ws.Root(), task.Acceptance)
	if !out.Accepted {
		return s.reject(task, req, out.Code, nil, out.Detail)
	}

	// 7. 通过。
	if err := s.Store.RecordFiles(task.ID, req.Attempt, fileMap(req.Files)); err != nil {
		return Result{}, err
	}
	if err := s.Store.RecordSubmission(&store.Submission{
		TaskID: task.ID, Attempt: req.Attempt, LeaseID: req.LeaseID,
		Accepted: true, DetailJSON: encodeDetail(out.Detail),
	}); err != nil {
		return Result{}, err
	}
	if err := s.Store.SetState(task.ID, store.StateAccepted); err != nil {
		return Result{}, err
	}
	_ = s.Store.AppendEvent(task.ID, "accepted", map[string]any{"attempt": req.Attempt})

	return Result{Accepted: true, Detail: out.Detail}, nil
}

func (s *Service) reject(task *store.Task, req Request, code string, vs []contract.Violation, detail map[string]any) (Result, error) {
	// 拒绝时不写 task_files：路径可能本身就不合法，且外键要求产物已登记。
	// 提交内容改成记在 detail 里，审计照样查得到。
	if detail == nil {
		detail = map[string]any{}
	}
	files := make([]map[string]string, 0, len(req.Files))
	for _, f := range req.Files {
		files = append(files, map[string]string{"path": f.Path, "sha256": f.SHA256})
	}
	detail["files"] = files

	if err := s.Store.RecordSubmission(&store.Submission{
		TaskID: task.ID, Attempt: req.Attempt, LeaseID: req.LeaseID,
		Accepted: false, Code: code, DetailJSON: encodeDetail(detail),
	}); err != nil {
		return Result{}, err
	}

	next := store.StateQueued
	if task.Attempts >= task.MaxAttempts {
		next = store.StateNeedsHuman
	}
	if err := s.Store.SetState(task.ID, next); err != nil {
		return Result{}, err
	}
	_ = s.Store.AppendEvent(task.ID, "rejected", map[string]any{
		"attempt": req.Attempt, "code": code, "next": next,
	})

	return Result{Accepted: false, Code: code, Violations: vs, Detail: detail}, nil
}

func leaseValid(until string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return false
	}
	return now.Before(t)
}

func toFrozen(in []store.FrozenFile) []contract.Frozen {
	out := make([]contract.Frozen, len(in))
	for i, f := range in {
		out[i] = contract.Frozen{Path: f.Path, SHA256: f.SHA256}
	}
	return out
}

func fileMap(files []FileRef) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.SHA256
	}
	return m
}

func encodeDetail(d map[string]any) string {
	b, err := json.Marshal(d)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func decodeDetail(s string) map[string]any {
	var d map[string]any
	if s != "" {
		_ = json.Unmarshal([]byte(s), &d)
	}
	return d
}
