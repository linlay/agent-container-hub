package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNormalizesRelativePaths(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})

	t.Setenv("STATE_DB_PATH", "./data/state.db")
	t.Setenv("WORKSPACE_ROOT", "./data/workspaces")
	t.Setenv("BUILD_ROOT", "./data/builds")
	t.Setenv("ALLOWED_MOUNT_ROOTS", "./data/workspaces,./extra-mounts")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	currentWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() after Chdir error = %v", err)
	}

	if want := filepath.Join(currentWD, "data", "state.db"); cfg.StateDBPath != want {
		t.Fatalf("StateDBPath = %q, want %q", cfg.StateDBPath, want)
	}
	if want := filepath.Join(currentWD, "data", "workspaces"); cfg.WorkspaceRoot != want {
		t.Fatalf("WorkspaceRoot = %q, want %q", cfg.WorkspaceRoot, want)
	}
	if want := filepath.Join(currentWD, "data", "builds"); cfg.BuildRoot != want {
		t.Fatalf("BuildRoot = %q, want %q", cfg.BuildRoot, want)
	}
	wantRoots := []string{
		filepath.Join(currentWD, "data", "workspaces"),
		filepath.Join(currentWD, "extra-mounts"),
	}
	for i, want := range wantRoots {
		if cfg.AllowedMountRoots[i] != want {
			t.Fatalf("AllowedMountRoots[%d] = %q, want %q", i, cfg.AllowedMountRoots[i], want)
		}
	}
}
