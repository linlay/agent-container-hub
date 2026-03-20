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
	t.Setenv("CONFIG_ROOT", "./configs")
	t.Setenv("ROOTFS_ROOT", "./data/rootfs")
	t.Setenv("BUILD_ROOT", "./data/builds")
	t.Setenv("SESSION_MOUNT_TEMPLATE_ROOT", "./zenmind-env")

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
	if want := filepath.Join(currentWD, "data", "rootfs"); cfg.RootfsRoot != want {
		t.Fatalf("RootfsRoot = %q, want %q", cfg.RootfsRoot, want)
	}
	if want := filepath.Join(currentWD, "configs"); cfg.ConfigRoot != want {
		t.Fatalf("ConfigRoot = %q, want %q", cfg.ConfigRoot, want)
	}
	if want := filepath.Join(currentWD, "data", "builds"); cfg.BuildRoot != want {
		t.Fatalf("BuildRoot = %q, want %q", cfg.BuildRoot, want)
	}
	if want := filepath.Join(currentWD, "zenmind-env"); cfg.SessionMountTemplateRoot != want {
		t.Fatalf("SessionMountTemplateRoot = %q, want %q", cfg.SessionMountTemplateRoot, want)
	}
}

func TestLoadUsesRenamedDefaultStateDBPath(t *testing.T) {
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

	t.Setenv("BIND_ADDR", "127.0.0.1:0")
	t.Setenv("STATE_DB_PATH", "")
	t.Setenv("CONFIG_ROOT", "")
	t.Setenv("ROOTFS_ROOT", "")
	t.Setenv("BUILD_ROOT", "")
	t.Setenv("SESSION_MOUNT_TEMPLATE_ROOT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	currentWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	want := filepath.Join(currentWD, "data", "agent-container-hub.db")
	if cfg.StateDBPath != want {
		t.Fatalf("StateDBPath = %q, want %q", cfg.StateDBPath, want)
	}
	if want := filepath.Join(currentWD, "configs"); cfg.ConfigRoot != want {
		t.Fatalf("ConfigRoot = %q, want %q", cfg.ConfigRoot, want)
	}
	if cfg.SessionMountTemplateRoot != "" {
		t.Fatalf("SessionMountTemplateRoot = %q, want empty", cfg.SessionMountTemplateRoot)
	}
	if !cfg.DeleteRootfsOnStop {
		t.Fatal("DeleteRootfsOnStop = false, want true")
	}
}

func TestLoadParsesHTTPLogFlags(t *testing.T) {
	t.Setenv("HTTP_ACCESS_LOG_ENABLED", "true")
	t.Setenv("HTTP_ERROR_LOG_ENABLED", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.HTTPAccessLogEnabled {
		t.Fatal("HTTPAccessLogEnabled = false, want true")
	}
	if !cfg.HTTPErrorLogEnabled {
		t.Fatal("HTTPErrorLogEnabled = false, want true")
	}
}

func TestLoadParsesDeleteRootfsOnStop(t *testing.T) {
	t.Setenv("DELETE_ROOTFS_ON_STOP", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DeleteRootfsOnStop {
		t.Fatal("DeleteRootfsOnStop = true, want false")
	}
}
