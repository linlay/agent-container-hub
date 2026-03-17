package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-container-hub/internal/model"
)

func TestFileEnvironmentStoreSaveAndGet(t *testing.T) {
	t.Parallel()

	store, root := newTestFileEnvironmentStore(t)
	environment := &model.Environment{
		Name:            "shell",
		Description:     "basic shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace",
		DefaultEnv:      map[string]string{"FOO": "bar"},
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}

	if err := store.SaveEnvironment(context.Background(), environment); err != nil {
		t.Fatalf("SaveEnvironment() error = %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "shell.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, "image_repository: busybox") {
		t.Fatalf("file content = %q, want image_repository", text)
	}

	stored, err := store.GetEnvironment(context.Background(), "shell")
	if err != nil {
		t.Fatalf("GetEnvironment() error = %v", err)
	}
	if stored.Name != "shell" || stored.ImageRepository != "busybox" {
		t.Fatalf("GetEnvironment() = %+v", stored)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
		t.Fatalf("mtime-derived timestamps not populated: %+v", stored)
	}
}

func TestFileEnvironmentStoreListReturnsFilenameForInvalidYAML(t *testing.T) {
	t.Parallel()

	store, root := newTestFileEnvironmentStore(t)
	if err := os.WriteFile(filepath.Join(root, "broken.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := store.ListEnvironments(context.Background())
	if err == nil || !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("ListEnvironments() error = %v, want filename", err)
	}
}

func TestFileEnvironmentStoreGetIgnoresUnrelatedInvalidYAML(t *testing.T) {
	t.Parallel()

	store, root := newTestFileEnvironmentStore(t)
	if err := store.SaveEnvironment(context.Background(), &model.Environment{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace",
		Enabled:         true,
		Build:           model.BuildSpec{Dockerfile: "FROM busybox:latest\n"},
	}); err != nil {
		t.Fatalf("SaveEnvironment() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	environment, err := store.GetEnvironment(context.Background(), "shell")
	if err != nil {
		t.Fatalf("GetEnvironment() error = %v", err)
	}
	if environment.Name != "shell" {
		t.Fatalf("GetEnvironment().Name = %q, want shell", environment.Name)
	}
}

func TestFileEnvironmentStoreUsesFileMTimeForTimestamps(t *testing.T) {
	t.Parallel()

	store, root := newTestFileEnvironmentStore(t)
	if err := store.SaveEnvironment(context.Background(), &model.Environment{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace",
		Enabled:         true,
		Build:           model.BuildSpec{Dockerfile: "FROM busybox:latest\n"},
	}); err != nil {
		t.Fatalf("SaveEnvironment() error = %v", err)
	}
	path := filepath.Join(root, "shell.yaml")
	wantTime := time.Now().UTC().Add(-2 * time.Minute).Round(time.Second)
	if err := os.Chtimes(path, wantTime, wantTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	environment, err := store.GetEnvironment(context.Background(), "shell")
	if err != nil {
		t.Fatalf("GetEnvironment() error = %v", err)
	}
	if !environment.CreatedAt.Equal(wantTime) || !environment.UpdatedAt.Equal(wantTime) {
		t.Fatalf("timestamps = (%s, %s), want %s", environment.CreatedAt, environment.UpdatedAt, wantTime)
	}
}

func newTestFileEnvironmentStore(t *testing.T) (*FileEnvironmentStore, string) {
	t.Helper()

	root := t.TempDir()
	store, err := OpenFileEnvironmentStore(root)
	if err != nil {
		t.Fatalf("OpenFileEnvironmentStore() error = %v", err)
	}
	return store, root
}
