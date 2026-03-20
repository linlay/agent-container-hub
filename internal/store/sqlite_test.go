package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-container-hub/internal/model"
	_ "modernc.org/sqlite"
)

func TestOpenRejectsLegacyNonSQLiteFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, []byte("not-a-sqlite-file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("Open() error = empty, want non-empty error")
	}
	if strings.Contains(err.Error(), "bbolt") {
		t.Fatalf("Open() error = %q, want no legacy bbolt hint", err.Error())
	}
}

func TestNormalizePagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    Pagination
		wantPage int
		wantSize int
	}{
		{
			name:     "defaults",
			input:    Pagination{},
			wantPage: 1,
			wantSize: 20,
		},
		{
			name:     "clamps page size",
			input:    Pagination{Page: 2, PageSize: 1000},
			wantPage: 2,
			wantSize: 100,
		},
		{
			name:     "keeps valid values",
			input:    Pagination{Page: 3, PageSize: 10},
			wantPage: 3,
			wantSize: 10,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotPage, gotSize := NormalizePagination(tc.input)
			if gotPage != tc.wantPage || gotSize != tc.wantSize {
				t.Fatalf("NormalizePagination(%+v) = (%d, %d), want (%d, %d)", tc.input, gotPage, gotSize, tc.wantPage, tc.wantSize)
			}
		})
	}
}

func TestQuerySessionsAndExecutions(t *testing.T) {
	t.Parallel()

	st, cleanup := newSQLiteStore(t)
	defer cleanup()

	now := time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC)
	active := &model.Session{
		ID:              "active-session",
		EnvironmentName: "shell",
		Image:           "busybox:latest",
		DefaultCwd:      "/root",
		RootfsPath:      "/tmp/active",
		Status:          model.SessionStatusActive,
		CreatedAt:       now,
	}
	history := &model.Session{
		ID:              "history-session",
		EnvironmentName: "python",
		Image:           "python:3.11",
		DefaultCwd:      "/root",
		RootfsPath:      "/tmp/history",
		Status:          model.SessionStatusStopped,
		CreatedAt:       now.Add(time.Minute),
		StoppedAt:       now.Add(2 * time.Minute),
	}
	if err := st.SaveSession(context.Background(), active); err != nil {
		t.Fatalf("SaveSession(active) error = %v", err)
	}
	if err := st.SaveSession(context.Background(), history); err != nil {
		t.Fatalf("SaveSession(history) error = %v", err)
	}

	activeItems, total, err := st.QuerySessions(context.Background(), SessionQuery{
		Status: "active",
		Pagination: Pagination{
			Page:     1,
			PageSize: 20,
		},
	})
	if err != nil {
		t.Fatalf("QuerySessions(active) error = %v", err)
	}
	if total != 1 || len(activeItems) != 1 || activeItems[0].ID != active.ID {
		t.Fatalf("QuerySessions(active) = total %d items %+v, want one active", total, activeItems)
	}

	historyItems, total, err := st.QuerySessions(context.Background(), SessionQuery{
		Status:          "history",
		EnvironmentName: "py",
		Pagination: Pagination{
			Page:     1,
			PageSize: 20,
		},
	})
	if err != nil {
		t.Fatalf("QuerySessions(history) error = %v", err)
	}
	if total != 1 || len(historyItems) != 1 || historyItems[0].ID != history.ID {
		t.Fatalf("QuerySessions(history) = total %d items %+v, want one history", total, historyItems)
	}

	execution := &model.SessionExecution{
		SessionID:       history.ID,
		Command:         "echo",
		Args:            []string{"hello"},
		Cwd:             "/root",
		TimeoutMS:       30000,
		ExitCode:        0,
		Stdout:          "abcd",
		StdoutTruncated: true,
		DurationMS:      10,
		StartedAt:       now,
		FinishedAt:      now.Add(10 * time.Millisecond),
	}
	if err := st.SaveSessionExecution(context.Background(), execution); err != nil {
		t.Fatalf("SaveSessionExecution() error = %v", err)
	}

	logs, total, err := st.ListSessionExecutions(context.Background(), history.ID, Pagination{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListSessionExecutions() error = %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("ListSessionExecutions() = total %d len %d, want 1", total, len(logs))
	}
	if logs[0].Command != "echo" || !logs[0].StdoutTruncated {
		t.Fatalf("stored execution = %+v, want persisted execution log", logs[0])
	}
}

func TestListSessionsReturnsAllActivePages(t *testing.T) {
	t.Parallel()

	st, cleanup := newSQLiteStore(t)
	defer cleanup()

	now := time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 135; i++ {
		session := &model.Session{
			ID:              fmt.Sprintf("session-%03d", i),
			EnvironmentName: "shell",
			Image:           "busybox:latest",
			DefaultCwd:      "/root",
			RootfsPath:      filepath.Join("/tmp", fmt.Sprintf("session-%03d", i)),
			Status:          model.SessionStatusActive,
			CreatedAt:       now.Add(time.Duration(i) * time.Second),
		}
		if err := st.SaveSession(context.Background(), session); err != nil {
			t.Fatalf("SaveSession(%s) error = %v", session.ID, err)
		}
	}

	items, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(items) != 135 {
		t.Fatalf("ListSessions() len = %d, want 135", len(items))
	}
}

func newSQLiteStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return st, func() { _ = st.Close() }
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func TestOpenMigratesLegacyWorkspacePathColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE sessions (
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
	);`); err != nil {
		t.Fatalf("create legacy sessions table error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (session_id, container_id, environment_name, image, default_cwd, workspace_path, status, created_at) VALUES ('legacy', '', 'shell', 'busybox:latest', '/root', '/tmp/legacy', 'active', '2026-03-18T10:00:00Z')`); err != nil {
		t.Fatalf("insert legacy session error = %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()

	session, err := st.GetSession(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if session.RootfsPath != "/tmp/legacy" {
		t.Fatalf("RootfsPath = %q, want /tmp/legacy", session.RootfsPath)
	}
}
