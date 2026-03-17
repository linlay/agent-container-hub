package sandbox

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"agentbox/internal/api"
	"agentbox/internal/config"
	"agentbox/internal/model"
	"agentbox/internal/runtime"
	"agentbox/internal/store"
)

func TestSessionCreateExecuteAndStop(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace/project",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\nCMD [\"/bin/sh\"]\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	created, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "demo-session",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.EnvironmentName != "shell" {
		t.Fatalf("Create() environment = %q, want shell", created.EnvironmentName)
	}

	fake.execResult = runtime.ExecResult{
		ExitCode:   0,
		Stdout:     "ok",
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}
	executed, err := services.sessions.Execute(context.Background(), created.SessionID, api.ExecuteSessionRequest{
		Command: "pwd",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if executed.Stdout != "ok" {
		t.Fatalf("Execute() stdout = %q, want ok", executed.Stdout)
	}
	if fake.lastExec.Cwd != "/workspace/project" {
		t.Fatalf("lastExec cwd = %q, want /workspace/project", fake.lastExec.Cwd)
	}

	if _, err := services.sessions.Stop(context.Background(), created.SessionID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := services.sessions.Get(context.Background(), created.SessionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestCreateRejectsDisabledEnvironment(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "disabled",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         false,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	_, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		EnvironmentName: "disabled",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}
}

func TestExecuteReturnsNotFoundForMissingSession(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
	defer cleanup()

	_, err := services.sessions.Execute(context.Background(), "missing", api.ExecuteSessionRequest{
		Command: "pwd",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Execute() error = %v, want ErrNotFound", err)
	}
}

func TestExecuteStartsStoppedContainer(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	created, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "stopped",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	fake.mu.Lock()
	info := fake.containers[created.ContainerID]
	info.State = runtime.ContainerStopped
	fake.containers[created.ContainerID] = info
	fake.mu.Unlock()

	if _, err := services.sessions.Execute(context.Background(), created.SessionID, api.ExecuteSessionRequest{
		Command: "pwd",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fake.startCalls == 0 {
		t.Fatal("expected Start to be called")
	}
}

func TestBuildEnvironmentStoresSuccessfulJob(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "python",
		ImageRepository: "registry.example.com/agentbox/python",
		ImageTag:        "3.11-v1",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\nRUN echo ok\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	fake.buildResult = runtime.BuildResult{
		Output:     "built",
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}
	job, err := services.builds.BuildEnvironment(context.Background(), "python")
	if err != nil {
		t.Fatalf("BuildEnvironment() error = %v", err)
	}
	if job.Status != buildStatusSucceeded {
		t.Fatalf("BuildEnvironment() status = %q, want succeeded", job.Status)
	}

	storedJobs, err := services.store.ListBuildJobs(context.Background(), "python")
	if err != nil {
		t.Fatalf("ListBuildJobs() error = %v", err)
	}
	if len(storedJobs) != 1 {
		t.Fatalf("ListBuildJobs() len = %d, want 1", len(storedJobs))
	}
}

func TestBuildEnvironmentPreservesFailedBuild(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "broken",
		ImageRepository: "broken",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	fake.buildErr = errors.New("build failed")
	job, err := services.builds.BuildEnvironment(context.Background(), "broken")
	if err != nil {
		t.Fatalf("BuildEnvironment() error = %v", err)
	}
	if job.Status != buildStatusFailed || job.Error == "" {
		t.Fatalf("BuildEnvironment() = %+v, want failed job with error", job)
	}
}

func TestEnvironmentUpdateDoesNotRewriteExistingSessionSnapshot(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
	defer cleanup()

	initial := api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace/one",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}
	if _, err := services.environments.Upsert(context.Background(), initial); err != nil {
		t.Fatalf("Upsert(initial) error = %v", err)
	}
	session, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "frozen",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	initial.DefaultCwd = "/workspace/two"
	if _, err := services.environments.Upsert(context.Background(), initial); err != nil {
		t.Fatalf("Upsert(updated) error = %v", err)
	}
	stored, err := services.sessions.Get(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.DefaultCwd != "/workspace/one" {
		t.Fatalf("stored.DefaultCwd = %q, want original snapshot", stored.DefaultCwd)
	}
}

type testServices struct {
	store        store.Store
	sessions     *SessionService
	environments *EnvironmentService
	builds       *BuildService
}

func newTestServices(t *testing.T) (*testServices, func(), *fakeRuntime) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		BindAddr:              "127.0.0.1:0",
		StateDBPath:           filepath.Join(tempDir, "agentbox.db"),
		WorkspaceRoot:         filepath.Join(tempDir, "workspaces"),
		BuildRoot:             filepath.Join(tempDir, "builds"),
		AllowedMountRoots:     []string{filepath.Join(tempDir, "workspaces"), filepath.Join(tempDir, "builds")},
		DefaultCommandTimeout: 100 * time.Millisecond,
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaces) error = %v", err)
	}
	if err := os.MkdirAll(cfg.BuildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(builds) error = %v", err)
	}
	st, err := store.Open(cfg.StateDBPath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	fake := &fakeRuntime{containers: make(map[string]runtime.ContainerInfo)}
	return &testServices{
		store:        st,
		sessions:     NewSessionService(cfg, st, fake, slog.New(slog.NewTextHandler(os.Stdout, nil))),
		environments: NewEnvironmentService(st, slog.New(slog.NewTextHandler(os.Stdout, nil))),
		builds:       NewBuildService(cfg, st, fake, fake, slog.New(slog.NewTextHandler(os.Stdout, nil))),
	}, func() { _ = st.Close() }, fake
}

type fakeRuntime struct {
	mu          sync.Mutex
	containers  map[string]runtime.ContainerInfo
	execResult  runtime.ExecResult
	lastExec    runtime.ExecOptions
	startCalls  int
	buildResult runtime.BuildResult
	buildErr    error
}

func (f *fakeRuntime) Name() string { return "fake" }

func (f *fakeRuntime) Create(_ context.Context, opts runtime.CreateOptions) (runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "ctr-" + opts.Name
	info := runtime.ContainerInfo{
		ID:        id,
		Name:      opts.Name,
		Image:     opts.Image,
		State:     runtime.ContainerStopped,
		Labels:    cloneStringMap(opts.Labels),
		CreatedAt: time.Now().UTC(),
	}
	f.containers[id] = info
	return info, nil
}

func (f *fakeRuntime) Start(_ context.Context, containerID string) (runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ContainerInfo{}, runtime.ErrContainerNotFound
	}
	info.State = runtime.ContainerRunning
	f.containers[info.ID] = info
	f.startCalls++
	return info, nil
}

func (f *fakeRuntime) Exec(_ context.Context, containerID string, opts runtime.ExecOptions) (runtime.ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ExecResult{}, runtime.ErrContainerNotFound
	}
	if info.State != runtime.ContainerRunning {
		return runtime.ExecResult{}, runtime.ErrContainerNotRunning
	}
	f.lastExec = opts
	if f.execResult.StartedAt.IsZero() {
		now := time.Now().UTC()
		f.execResult.StartedAt = now
		f.execResult.FinishedAt = now
	}
	return f.execResult, nil
}

func (f *fakeRuntime) Build(_ context.Context, _ runtime.BuildOptions) (runtime.BuildResult, error) {
	if f.buildResult.StartedAt.IsZero() {
		now := time.Now().UTC()
		f.buildResult.StartedAt = now
		f.buildResult.FinishedAt = now
	}
	return f.buildResult, f.buildErr
}

func (f *fakeRuntime) Stop(_ context.Context, containerID string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ErrContainerNotFound
	}
	info.State = runtime.ContainerStopped
	f.containers[info.ID] = info
	return nil
}

func (f *fakeRuntime) Remove(_ context.Context, containerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.lookup(containerID)
	if ok {
		delete(f.containers, info.ID)
	}
	return nil
}

func (f *fakeRuntime) Inspect(_ context.Context, containerID string) (runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ContainerInfo{}, runtime.ErrContainerNotFound
	}
	return info, nil
}

func (f *fakeRuntime) ListByLabel(_ context.Context, key, value string) ([]runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var infos []runtime.ContainerInfo
	for _, info := range f.containers {
		if info.Labels[key] == value {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

func (f *fakeRuntime) lookup(idOrName string) (runtime.ContainerInfo, bool) {
	if info, ok := f.containers[idOrName]; ok {
		return info, true
	}
	for _, info := range f.containers {
		if info.Name == idOrName {
			return info, true
		}
	}
	return runtime.ContainerInfo{}, false
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
