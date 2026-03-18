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

	payload, err := os.ReadFile(filepath.Join(root, "shell", environmentMetadataFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, "image_repository: busybox") {
		t.Fatalf("file content = %q, want image_repository", text)
	}
	if strings.Contains(text, "dockerfile:") {
		t.Fatalf("metadata unexpectedly contains dockerfile: %q", text)
	}

	dockerfile, err := os.ReadFile(filepath.Join(root, "shell", environmentDockerfile))
	if err != nil {
		t.Fatalf("ReadFile(Dockerfile) error = %v", err)
	}
	if string(dockerfile) != "FROM busybox:latest\n" {
		t.Fatalf("Dockerfile = %q", dockerfile)
	}

	stored, err := store.GetEnvironment(context.Background(), "shell")
	if err != nil {
		t.Fatalf("GetEnvironment() error = %v", err)
	}
	if stored.Name != "shell" || stored.ImageRepository != "busybox" {
		t.Fatalf("GetEnvironment() = %+v", stored)
	}
	if stored.Build.Dockerfile != "FROM busybox:latest\n" {
		t.Fatalf("GetEnvironment().Build.Dockerfile = %q", stored.Build.Dockerfile)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
		t.Fatalf("mtime-derived timestamps not populated: %+v", stored)
	}
}

func TestFileEnvironmentStoreListReturnsFilenameForInvalidYAML(t *testing.T) {
	t.Parallel()

	store, root := newTestFileEnvironmentStore(t)
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", environmentMetadataFile), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := store.ListEnvironments(context.Background())
	if err == nil || !strings.Contains(err.Error(), filepath.Join("broken", environmentMetadataFile)) {
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
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", environmentMetadataFile), []byte("name: [\n"), 0o644); err != nil {
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
	path := filepath.Join(root, "shell", environmentMetadataFile)
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

func TestFileEnvironmentStoreIgnoresLegacyFlatYAMLFiles(t *testing.T) {
	t.Parallel()

	store, root := newTestFileEnvironmentStore(t)
	if err := os.WriteFile(filepath.Join(root, "legacy.yaml"), []byte("name: legacy\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	environments, err := store.ListEnvironments(context.Background())
	if err != nil {
		t.Fatalf("ListEnvironments() error = %v", err)
	}
	if len(environments) != 0 {
		t.Fatalf("ListEnvironments() = %+v, want empty", environments)
	}
	if _, err := store.GetEnvironment(context.Background(), "legacy"); err != ErrNotFound {
		t.Fatalf("GetEnvironment() error = %v, want ErrNotFound", err)
	}
}

func TestFileEnvironmentStoreListReadAndWriteEnvironmentFiles(t *testing.T) {
	t.Parallel()

	store, _ := newTestFileEnvironmentStore(t)
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

	if err := store.WriteEnvironmentFile(context.Background(), "shell", "build.sh", []byte("#!/bin/sh\necho shell\n")); err != nil {
		t.Fatalf("WriteEnvironmentFile(build.sh) error = %v", err)
	}
	if err := store.WriteEnvironmentFile(context.Background(), "shell", "scripts/check.sh", []byte("#!/bin/sh\ntrue\n")); err != nil {
		t.Fatalf("WriteEnvironmentFile(scripts/check.sh) error = %v", err)
	}

	files, err := store.ListEnvironmentFiles(context.Background(), "shell")
	if err != nil {
		t.Fatalf("ListEnvironmentFiles() error = %v", err)
	}
	if len(files) != 4 {
		t.Fatalf("ListEnvironmentFiles() len = %d, want 4", len(files))
	}

	file, err := store.ReadEnvironmentFile(context.Background(), "shell", "scripts/check.sh")
	if err != nil {
		t.Fatalf("ReadEnvironmentFile() error = %v", err)
	}
	if string(file.Content) != "#!/bin/sh\ntrue\n" {
		t.Fatalf("ReadEnvironmentFile().Content = %q", file.Content)
	}
}

func TestFileEnvironmentStoreRejectsInvalidEnvironmentFilePaths(t *testing.T) {
	t.Parallel()

	store, _ := newTestFileEnvironmentStore(t)
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

	for _, relPath := range []string{"../outside.sh", "/abs/path", "tmp/file.txt"} {
		if err := store.WriteEnvironmentFile(context.Background(), "shell", relPath, []byte("x")); err == nil {
			t.Fatalf("WriteEnvironmentFile(%q) error = nil, want validation failure", relPath)
		}
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
