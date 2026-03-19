package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"agent-container-hub/internal/model"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func Open(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &SQLiteStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) init() error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			container_id TEXT NOT NULL DEFAULT '',
			environment_name TEXT NOT NULL,
			image TEXT NOT NULL,
			default_cwd TEXT NOT NULL,
			workspace_path TEXT NOT NULL,
			env_json TEXT NOT NULL DEFAULT '{}',
			mounts_json TEXT NOT NULL DEFAULT '[]',
			resources_json TEXT NOT NULL DEFAULT '{}',
			labels_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			stopped_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_status_created_at ON sessions(status, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_environment_status ON sessions(environment_name, status, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS build_jobs (
			id TEXT PRIMARY KEY,
			environment_name TEXT NOT NULL,
			image_ref TEXT NOT NULL,
			status TEXT NOT NULL,
			output TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_build_jobs_environment_started_at ON build_jobs(environment_name, started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS session_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			command TEXT NOT NULL,
			args_json TEXT NOT NULL DEFAULT '[]',
			cwd TEXT NOT NULL DEFAULT '',
			timeout_ms INTEGER NOT NULL DEFAULT 0,
			exit_code INTEGER NOT NULL,
			stdout TEXT NOT NULL DEFAULT '',
			stderr TEXT NOT NULL DEFAULT '',
			stdout_truncated INTEGER NOT NULL DEFAULT 0,
			stderr_truncated INTEGER NOT NULL DEFAULT 0,
			timed_out INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(session_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_session_executions_session_started_at ON session_executions(session_id, started_at DESC, id DESC);`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) SaveSession(_ context.Context, session *model.Session) error {
	envJSON, err := marshalJSON(session.Env, "{}")
	if err != nil {
		return fmt.Errorf("marshal session env: %w", err)
	}
	mountsJSON, err := marshalJSON(session.Mounts, "[]")
	if err != nil {
		return fmt.Errorf("marshal session mounts: %w", err)
	}
	resourcesJSON, err := marshalJSON(session.Resources, "{}")
	if err != nil {
		return fmt.Errorf("marshal session resources: %w", err)
	}
	labelsJSON, err := marshalJSON(session.Labels, "{}")
	if err != nil {
		return fmt.Errorf("marshal session labels: %w", err)
	}

	var stoppedAt any
	if !session.StoppedAt.IsZero() {
		stoppedAt = session.StoppedAt.UTC().Format(time.RFC3339Nano)
	}

	_, err = s.db.Exec(`
		INSERT INTO sessions (
			session_id, container_id, environment_name, image, default_cwd, workspace_path,
			env_json, mounts_json, resources_json, labels_json, status, created_at, stopped_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			container_id=excluded.container_id,
			environment_name=excluded.environment_name,
			image=excluded.image,
			default_cwd=excluded.default_cwd,
			workspace_path=excluded.workspace_path,
			env_json=excluded.env_json,
			mounts_json=excluded.mounts_json,
			resources_json=excluded.resources_json,
			labels_json=excluded.labels_json,
			status=excluded.status,
			created_at=excluded.created_at,
			stopped_at=excluded.stopped_at
	`, session.ID, session.ContainerID, session.EnvironmentName, session.Image, session.DefaultCwd, session.WorkspacePath,
		envJSON, mountsJSON, resourcesJSON, labelsJSON, string(session.Status), session.CreatedAt.UTC().Format(time.RFC3339Nano), stoppedAt)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSession(_ context.Context, id string) (*model.Session, error) {
	row := s.db.QueryRow(`
		SELECT session_id, container_id, environment_name, image, default_cwd, workspace_path,
		       env_json, mounts_json, resources_json, labels_json, status, created_at, stopped_at
		FROM sessions
		WHERE session_id = ?
	`, id)
	session, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context) ([]*model.Session, error) {
	page := 1
	items := make([]*model.Session, 0, 32)
	for {
		batch, total, err := s.QuerySessions(ctx, SessionQuery{
			Status: "active",
			Pagination: Pagination{
				Page:     page,
				PageSize: 100,
			},
		})
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(batch) == 0 || len(items) >= total {
			break
		}
		page++
	}
	return items, nil
}

func (s *SQLiteStore) QuerySessions(_ context.Context, query SessionQuery) ([]*model.Session, int, error) {
	page, pageSize := NormalizePagination(query.Pagination)
	whereClauses := make([]string, 0, 3)
	args := make([]any, 0, 6)

	switch strings.ToLower(strings.TrimSpace(query.Status)) {
	case "", "active":
		whereClauses = append(whereClauses, "status = ?")
		args = append(args, string(model.SessionStatusActive))
	case "history":
		whereClauses = append(whereClauses, "status <> ?")
		args = append(args, string(model.SessionStatusActive))
	case "all":
	default:
		return nil, 0, fmt.Errorf("%w: invalid session status filter %q", ErrNotFound, query.Status)
	}
	if sessionID := strings.TrimSpace(query.SessionID); sessionID != "" {
		whereClauses = append(whereClauses, "session_id LIKE ?")
		args = append(args, "%"+sessionID+"%")
	}
	if environmentName := strings.TrimSpace(query.EnvironmentName); environmentName != "" {
		whereClauses = append(whereClauses, "environment_name LIKE ?")
		args = append(args, "%"+environmentName+"%")
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM sessions`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}

	queryArgs := append(append([]any(nil), args...), pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(`
		SELECT session_id, container_id, environment_name, image, default_cwd, workspace_path,
		       env_json, mounts_json, resources_json, labels_json, status, created_at, stopped_at
		FROM sessions`+whereSQL+`
		ORDER BY created_at DESC, session_id DESC
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, total, nil
}

func (s *SQLiteStore) DeleteSession(_ context.Context, id string) error {
	result, err := s.db.Exec(`DELETE FROM sessions WHERE session_id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) SaveSessionExecution(_ context.Context, execution *model.SessionExecution) error {
	argsJSON, err := marshalJSON(execution.Args, "[]")
	if err != nil {
		return fmt.Errorf("marshal execution args: %w", err)
	}
	result, err := s.db.Exec(`
		INSERT INTO session_executions (
			session_id, command, args_json, cwd, timeout_ms, exit_code, stdout, stderr,
			stdout_truncated, stderr_truncated, timed_out, duration_ms, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, execution.SessionID, execution.Command, argsJSON, execution.Cwd, execution.TimeoutMS, execution.ExitCode,
		execution.Stdout, execution.Stderr, boolToInt(execution.StdoutTruncated), boolToInt(execution.StderrTruncated),
		boolToInt(execution.TimedOut), execution.DurationMS, execution.StartedAt.UTC().Format(time.RFC3339Nano),
		execution.FinishedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save session execution: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		execution.ID = id
	}
	return nil
}

func (s *SQLiteStore) ListSessionExecutions(_ context.Context, sessionID string, pagination Pagination) ([]*model.SessionExecution, int, error) {
	page, pageSize := NormalizePagination(pagination)

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM session_executions WHERE session_id = ?`, sessionID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count session executions: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, session_id, command, args_json, cwd, timeout_ms, exit_code, stdout, stderr,
		       stdout_truncated, stderr_truncated, timed_out, duration_ms, started_at, finished_at
		FROM session_executions
		WHERE session_id = ?
		ORDER BY started_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, sessionID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("query session executions: %w", err)
	}
	defer rows.Close()

	var items []*model.SessionExecution
	for rows.Next() {
		item, err := scanSessionExecution(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate session executions: %w", err)
	}
	return items, total, nil
}

func (s *SQLiteStore) SaveBuildJob(_ context.Context, job *model.BuildJob) error {
	_, err := s.db.Exec(`
		INSERT INTO build_jobs (id, environment_name, image_ref, status, output, error, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			environment_name=excluded.environment_name,
			image_ref=excluded.image_ref,
			status=excluded.status,
			output=excluded.output,
			error=excluded.error,
			started_at=excluded.started_at,
			finished_at=excluded.finished_at
	`, job.ID, job.EnvironmentName, job.ImageRef, string(job.Status), job.Output, job.Error,
		job.StartedAt.UTC().Format(time.RFC3339Nano), job.FinishedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save build job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListBuildJobs(_ context.Context, environmentName string) ([]*model.BuildJob, error) {
	query := `
		SELECT id, environment_name, image_ref, status, output, error, started_at, finished_at
		FROM build_jobs`
	args := []any{}
	if strings.TrimSpace(environmentName) != "" {
		query += ` WHERE environment_name = ?`
		args = append(args, environmentName)
	}
	query += ` ORDER BY started_at DESC, id DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query build jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*model.BuildJob
	for rows.Next() {
		var job model.BuildJob
		var status string
		var startedAt, finishedAt string
		if err := rows.Scan(&job.ID, &job.EnvironmentName, &job.ImageRef, &status, &job.Output, &job.Error, &startedAt, &finishedAt); err != nil {
			return nil, fmt.Errorf("scan build job: %w", err)
		}
		job.Status = model.BuildJobStatus(status)
		job.StartedAt = parseTime(startedAt)
		job.FinishedAt = parseTime(finishedAt)
		jobs = append(jobs, &job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate build jobs: %w", err)
	}
	return jobs, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(scanner rowScanner) (*model.Session, error) {
	var session model.Session
	var envJSON, mountsJSON, resourcesJSON, labelsJSON string
	var status, createdAt string
	var stoppedAt sql.NullString

	if err := scanner.Scan(
		&session.ID,
		&session.ContainerID,
		&session.EnvironmentName,
		&session.Image,
		&session.DefaultCwd,
		&session.WorkspacePath,
		&envJSON,
		&mountsJSON,
		&resourcesJSON,
		&labelsJSON,
		&status,
		&createdAt,
		&stoppedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	if err := unmarshalJSON(envJSON, &session.Env); err != nil {
		return nil, fmt.Errorf("decode session env: %w", err)
	}
	if err := unmarshalJSON(mountsJSON, &session.Mounts); err != nil {
		return nil, fmt.Errorf("decode session mounts: %w", err)
	}
	if err := unmarshalJSON(resourcesJSON, &session.Resources); err != nil {
		return nil, fmt.Errorf("decode session resources: %w", err)
	}
	if err := unmarshalJSON(labelsJSON, &session.Labels); err != nil {
		return nil, fmt.Errorf("decode session labels: %w", err)
	}
	session.Status = model.SessionStatus(status)
	session.CreatedAt = parseTime(createdAt)
	if stoppedAt.Valid {
		session.StoppedAt = parseTime(stoppedAt.String)
	}
	return &session, nil
}

func scanSessionExecution(scanner rowScanner) (*model.SessionExecution, error) {
	var execution model.SessionExecution
	var argsJSON string
	var stdoutTruncated, stderrTruncated, timedOut int
	var startedAt, finishedAt string

	if err := scanner.Scan(
		&execution.ID,
		&execution.SessionID,
		&execution.Command,
		&argsJSON,
		&execution.Cwd,
		&execution.TimeoutMS,
		&execution.ExitCode,
		&execution.Stdout,
		&execution.Stderr,
		&stdoutTruncated,
		&stderrTruncated,
		&timedOut,
		&execution.DurationMS,
		&startedAt,
		&finishedAt,
	); err != nil {
		return nil, fmt.Errorf("scan session execution: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &execution.Args); err != nil {
		return nil, fmt.Errorf("decode session execution args: %w", err)
	}
	execution.StdoutTruncated = stdoutTruncated == 1
	execution.StderrTruncated = stderrTruncated == 1
	execution.TimedOut = timedOut == 1
	execution.StartedAt = parseTime(startedAt)
	execution.FinishedAt = parseTime(finishedAt)
	return &execution, nil
}

func marshalJSON(value any, fallback string) (string, error) {
	if isEmptyJSONValue(value) {
		return fallback, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func unmarshalJSON(payload string, target any) error {
	if strings.TrimSpace(payload) == "" {
		return nil
	}
	return json.Unmarshal([]byte(payload), target)
}

func NormalizePagination(p Pagination) (int, int) {
	page := p.Page
	if page <= 0 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isEmptyJSONValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map, reflect.Slice:
		return v.Len() == 0
	}
	return false
}
