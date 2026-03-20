package store

import (
	"context"
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
		DefaultCwd:      "/workspace",
		RootfsPath:      "/tmp/active",
		Status:          model.SessionStatusActive,
		CreatedAt:       now,
	}
	history := &model.Session{
		ID:              "history-session",
		EnvironmentName: "python",
		Image:           "python:3.11",
		DefaultCwd:      "/workspace",
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
		Cwd:             "/workspace",
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
			DefaultCwd:      "/workspace",
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
