package sandbox

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/config"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/store"
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
	if created.DurationMS < 0 {
		t.Fatalf("Create() duration_ms = %d, want non-negative", created.DurationMS)
	}

	startedAt := time.Date(2026, time.March, 17, 12, 38, 34, 0, time.UTC)
	fake.execResult = runtime.ExecResult{
		ExitCode:   0,
		Stdout:     "ok",
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(95 * time.Millisecond),
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
	if executed.DurationMS != 95 {
		t.Fatalf("Execute() duration_ms = %d, want 95", executed.DurationMS)
	}
	if fake.lastExec.Cwd != "/workspace/project" {
		t.Fatalf("lastExec cwd = %q, want /workspace/project", fake.lastExec.Cwd)
	}

	stopped, err := services.sessions.Stop(context.Background(), created.SessionID)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if stopped.DurationMS < 0 {
		t.Fatalf("Stop() duration_ms = %d, want non-negative", stopped.DurationMS)
	}
	stored, err := services.sessions.Get(context.Background(), created.SessionID)
	if err != nil {
		t.Fatalf("Get() after Stop error = %v", err)
	}
	if stored.Status != string(model.SessionStatusStopped) {
		t.Fatalf("stored.Status = %q, want stopped", stored.Status)
	}
	if stored.StoppedAt.IsZero() {
		t.Fatal("expected stopped_at to be set")
	}
	active, err := services.sessions.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active sessions len = %d, want 0", len(active))
	}
}

func TestSessionExecuteCanReuseSameRunningSession(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\nCMD [\"/bin/sh\"]\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	created, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "reuse-session",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	fake.execResult = runtime.ExecResult{ExitCode: 0, Stdout: "first"}
	first, err := services.sessions.Execute(context.Background(), created.SessionID, api.ExecuteSessionRequest{
		Command: "echo",
		Args:    []string{"first"},
	})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if first.Stdout != "first" {
		t.Fatalf("first Execute() stdout = %q, want first", first.Stdout)
	}

	fake.execResult = runtime.ExecResult{ExitCode: 0, Stdout: "second"}
	second, err := services.sessions.Execute(context.Background(), created.SessionID, api.ExecuteSessionRequest{
		Command: "echo",
		Args:    []string{"second"},
	})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if second.Stdout != "second" {
		t.Fatalf("second Execute() stdout = %q, want second", second.Stdout)
	}
	if fake.startCalls != 1 {
		t.Fatalf("startCalls = %d, want 1", fake.startCalls)
	}
}

func TestDurationMilliseconds(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.March, 17, 12, 38, 34, 0, time.UTC)

	tests := []struct {
		name   string
		result runtime.ExecResult
		want   int64
	}{
		{
			name: "positive duration",
			result: runtime.ExecResult{
				StartedAt:  startedAt,
				FinishedAt: startedAt.Add(95 * time.Millisecond),
			},
			want: 95,
		},
		{
			name: "zero duration",
			result: runtime.ExecResult{
				StartedAt:  startedAt,
				FinishedAt: startedAt,
			},
			want: 0,
		},
		{
			name: "negative duration clamps to zero",
			result: runtime.ExecResult{
				StartedAt:  startedAt,
				FinishedAt: startedAt.Add(-95 * time.Millisecond),
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := durationMilliseconds(tc.result.StartedAt, tc.result.FinishedAt)
			if got != tc.want {
				t.Fatalf("durationMilliseconds() = %d, want %d", got, tc.want)
			}
		})
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

func TestCreateMergesEnvironmentAndSessionMounts(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	envSource := filepath.Join(filepath.Dir(services.sessions.cfg.BuildRoot), "builds", "skills")
	if err := os.MkdirAll(envSource, 0o755); err != nil {
		t.Fatalf("MkdirAll(envSource) error = %v", err)
	}
	sessionSource := filepath.Join(services.sessions.cfg.SessionMountTemplateRoot, "home")
	if err := os.MkdirAll(sessionSource, 0o755); err != nil {
		t.Fatalf("MkdirAll(sessionSource) error = %v", err)
	}

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Mounts: []model.Mount{{
			Source:      envSource,
			Destination: "/skills",
			ReadOnly:    true,
		}},
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	created, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "mount-session",
		EnvironmentName: "shell",
		Mounts: []model.Mount{{
			Source:      sessionSource,
			Destination: "/home",
		}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if len(created.Mounts) != 3 {
		t.Fatalf("created mounts len = %d, want 3", len(created.Mounts))
	}
	if fake.lastCreate.Mounts[0].Destination != "/skills" || !fake.lastCreate.Mounts[0].ReadOnly {
		t.Fatalf("env mount = %+v", fake.lastCreate.Mounts[0])
	}
	if fake.lastCreate.Mounts[1].Destination != "/home" || fake.lastCreate.Mounts[1].ReadOnly {
		t.Fatalf("session mount = %+v", fake.lastCreate.Mounts[1])
	}
	if fake.lastCreate.Mounts[2].Destination != "/workspace" {
		t.Fatalf("workspace mount = %+v", fake.lastCreate.Mounts[2])
	}
}

func TestCreateAcceptsMountOutsideFormerAllowedRoots(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	externalSource := filepath.Join(t.TempDir(), "external-mount")
	if err := os.MkdirAll(externalSource, 0o755); err != nil {
		t.Fatalf("MkdirAll(externalSource) error = %v", err)
	}

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
		SessionID:       "outside-former-roots",
		EnvironmentName: "shell",
		Mounts: []model.Mount{{
			Source:      externalSource,
			Destination: "/external",
		}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(created.Mounts) != 2 {
		t.Fatalf("created mounts len = %d, want 2", len(created.Mounts))
	}
	if fake.lastCreate.Mounts[0].Source != runtime.NormalizeMountSource(externalSource) {
		t.Fatalf("session mount source = %q, want %q", fake.lastCreate.Mounts[0].Source, runtime.NormalizeMountSource(externalSource))
	}
}

func TestCreateRejectsDuplicateMountDestinations(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
	defer cleanup()

	sourceA := filepath.Join(services.sessions.cfg.SessionMountTemplateRoot, "home")
	sourceB := filepath.Join(services.sessions.cfg.SessionMountTemplateRoot, "pan")
	if err := os.MkdirAll(sourceA, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceA) error = %v", err)
	}
	if err := os.MkdirAll(sourceB, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceB) error = %v", err)
	}

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

	_, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "dup-mounts",
		EnvironmentName: "shell",
		Mounts: []model.Mount{
			{Source: sourceA, Destination: "/tmp"},
			{Source: sourceB, Destination: "/tmp"},
		},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}
}

func TestCreateRejectsReservedWorkspaceMountDestination(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
	defer cleanup()

	source := filepath.Join(services.sessions.cfg.SessionMountTemplateRoot, "home")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("MkdirAll(source) error = %v", err)
	}

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

	_, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "reserved-workspace",
		EnvironmentName: "shell",
		Mounts: []model.Mount{{
			Source:      source,
			Destination: "/workspace",
		}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create() error = %v, want ErrValidation", err)
	}
}

func TestCreateTemplateListsMountDefaultsAndChats(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
	defer cleanup()

	for _, dir := range []string{"home", "pan", "skills", filepath.Join("chats", "chat-a"), filepath.Join("chats", "chat-b")} {
		if err := os.MkdirAll(filepath.Join(services.sessions.cfg.SessionMountTemplateRoot, dir), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	template, err := services.sessions.CreateTemplate(context.Background())
	if err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	if template.MountTemplateRoot != services.sessions.cfg.SessionMountTemplateRoot {
		t.Fatalf("MountTemplateRoot = %q, want %q", template.MountTemplateRoot, services.sessions.cfg.SessionMountTemplateRoot)
	}
	if len(template.DefaultMounts) != 3 {
		t.Fatalf("default mounts len = %d, want 3", len(template.DefaultMounts))
	}
	if template.DefaultMounts[0].Destination != "/home" || template.DefaultMounts[1].Destination != "/pan" || template.DefaultMounts[2].Destination != "/skills" {
		t.Fatalf("default mounts = %+v", template.DefaultMounts)
	}
	if len(template.ChatIDs) != 2 || template.ChatIDs[0] != "chat-a" || template.ChatIDs[1] != "chat-b" {
		t.Fatalf("chat IDs = %+v", template.ChatIDs)
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
		ImageRepository: "registry.example.com/agent-container-hub/python",
		ImageTag:        "3.11-v1",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile:   "FROM busybox:latest\nRUN echo ok\n",
			SmokeCommand: "/bin/sh",
			SmokeArgs:    []string{"-lc", "true"},
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
	if got := fake.lastCreate.Labels[runtime.ManagedByLabel]; got != "agent-container-hub" {
		t.Fatalf("build managed_by label = %q, want agent-container-hub", got)
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

func TestBuildEnvironmentUsesInlineDockerfileOnlyContext(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\nCMD [\"/bin/sh\"]\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if _, err := services.builds.BuildEnvironment(context.Background(), "shell"); err != nil {
		t.Fatalf("BuildEnvironment() error = %v", err)
	}
	if fake.lastBuild.Image != "busybox:latest" {
		t.Fatalf("lastBuild.Image = %q, want busybox:latest", fake.lastBuild.Image)
	}
	if len(fake.buildFiles) != 1 {
		t.Fatalf("buildFiles len = %d, want 1", len(fake.buildFiles))
	}
	if got := fake.buildFiles["Dockerfile"]; got != "FROM busybox:latest\nCMD [\"/bin/sh\"]\n" {
		t.Fatalf("Dockerfile = %q", got)
	}
}

func TestDailyOfficeBuildUsesInlineDockerfileAndPassesBuildArgs(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "daily-office",
		ImageRepository: "daily-office",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\nRUN echo daily-office\n",
			BuildArgs: map[string]string{
				"NPM_REGISTRY": "https://registry.npmjs.org",
			},
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if _, err := services.builds.BuildEnvironment(context.Background(), "daily-office"); err != nil {
		t.Fatalf("BuildEnvironment() error = %v", err)
	}
	if fake.lastBuild.Image != "daily-office:latest" {
		t.Fatalf("lastBuild.Image = %q, want daily-office:latest", fake.lastBuild.Image)
	}
	if got := fake.lastBuild.BuildArgs["NPM_REGISTRY"]; got != "https://registry.npmjs.org" {
		t.Fatalf("BuildArgs[NPM_REGISTRY] = %q", got)
	}
	if len(fake.buildFiles) != 1 {
		t.Fatalf("buildFiles len = %d, want 1", len(fake.buildFiles))
	}
	if got := fake.buildFiles["Dockerfile"]; got != "FROM busybox:latest\nRUN echo daily-office\n" {
		t.Fatalf("Dockerfile = %q", got)
	}
	if strings.Contains(fake.buildFiles["Dockerfile"], "COPY ") {
		t.Fatalf("Dockerfile unexpectedly contains COPY: %q", fake.buildFiles["Dockerfile"])
	}
}

func TestDailyOfficeSessionIncludesSkillsMount(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServices(t)
	defer cleanup()

	skillsRoot := filepath.Join(filepath.Dir(services.sessions.cfg.WorkspaceRoot), "skills-root")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(skillsRoot) error = %v", err)
	}

	expectedPath := "/opt/daily-office/node_modules/.bin:/skills/scripts:/skills/docx/scripts:/skills/pptx/scripts:/skills/pdf/scripts:/skills/xlsx/scripts:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	if _, err := services.environments.Upsert(context.Background(), api.UpsertEnvironmentRequest{
		Name:            "daily-office",
		ImageRepository: "daily-office",
		ImageTag:        "latest",
		DefaultCwd:      "/workspace",
		DefaultEnv: map[string]string{
			"NODE_PATH": "/opt/daily-office/node_modules",
			"PATH":      expectedPath,
		},
		Mounts: []model.Mount{{
			Source:      skillsRoot,
			Destination: "/skills",
			ReadOnly:    true,
		}},
		Enabled: true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	created, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "daily-office-session",
		EnvironmentName: "daily-office",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if fake.lastCreate.Env["NODE_PATH"] != "/opt/daily-office/node_modules" {
		t.Fatalf("NODE_PATH = %q", fake.lastCreate.Env["NODE_PATH"])
	}
	if fake.lastCreate.Env["PATH"] != expectedPath {
		t.Fatalf("PATH = %q", fake.lastCreate.Env["PATH"])
	}
	if len(created.Mounts) != 2 {
		t.Fatalf("created mounts len = %d, want 2", len(created.Mounts))
	}
	if created.Mounts[0].Destination != "/skills" || !created.Mounts[0].ReadOnly {
		t.Fatalf("skills mount = %+v", created.Mounts[0])
	}
	if created.Mounts[1].Destination != runtime.DefaultMountPath || created.Mounts[1].ReadOnly {
		t.Fatalf("workspace mount = %+v", created.Mounts[1])
	}
}

func TestCreateReturnsNotFoundWhenEnvironmentFileMissing(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
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
	if err := services.envs.DeleteEnvironment(context.Background(), "shell"); err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}

	_, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		EnvironmentName: "shell",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Create() error = %v, want ErrNotFound", err)
	}
}

func TestBuildReturnsNotFoundWhenEnvironmentFileMissing(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
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
	if err := services.envs.DeleteEnvironment(context.Background(), "shell"); err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}

	_, err := services.builds.BuildEnvironment(context.Background(), "shell")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("BuildEnvironment() error = %v, want ErrNotFound", err)
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

func TestQuerySessionsSeparatesActiveAndHistory(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
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

	activeSession, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "active-session",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create(active) error = %v", err)
	}
	historySession, err := services.sessions.Create(context.Background(), api.CreateSessionRequest{
		SessionID:       "history-session",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create(history) error = %v", err)
	}
	if _, err := services.sessions.Stop(context.Background(), historySession.SessionID); err != nil {
		t.Fatalf("Stop(history) error = %v", err)
	}

	activeResp, err := services.sessions.Query(context.Background(), store.SessionQuery{
		Status: "active",
		Pagination: store.Pagination{
			Page:     1,
			PageSize: 20,
		},
	})
	if err != nil {
		t.Fatalf("Query(active) error = %v", err)
	}
	if len(activeResp.Items) != 1 || activeResp.Items[0].SessionID != activeSession.SessionID {
		t.Fatalf("active items = %+v, want only %q", activeResp.Items, activeSession.SessionID)
	}

	historyResp, err := services.sessions.Query(context.Background(), store.SessionQuery{
		Status: "history",
		Pagination: store.Pagination{
			Page:     1,
			PageSize: 20,
		},
	})
	if err != nil {
		t.Fatalf("Query(history) error = %v", err)
	}
	if len(historyResp.Items) != 1 || historyResp.Items[0].SessionID != historySession.SessionID {
		t.Fatalf("history items = %+v, want only %q", historyResp.Items, historySession.SessionID)
	}
}

func TestExecuteLogPersistenceAndTruncation(t *testing.T) {
	t.Parallel()

	services, cleanup, fake := newTestServicesWithOptions(t, func(cfg *config.Config) {
		cfg.EnableExecLogPersist = true
		cfg.ExecLogMaxOutputBytes = 4
	})
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
		SessionID:       "log-session",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	fake.execResult = runtime.ExecResult{
		ExitCode:   0,
		Stdout:     "abcdef",
		Stderr:     "wxyz12",
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC().Add(5 * time.Millisecond),
	}
	if _, err := services.sessions.Execute(context.Background(), created.SessionID, api.ExecuteSessionRequest{
		Command: "echo",
		Args:    []string{"hello"},
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	logs, err := services.sessions.ListExecutions(context.Background(), created.SessionID, store.Pagination{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListExecutions() error = %v", err)
	}
	if len(logs.Items) != 1 {
		t.Fatalf("logs len = %d, want 1", len(logs.Items))
	}
	if logs.Items[0].Stdout != "abcd" || !logs.Items[0].StdoutTruncated {
		t.Fatalf("stdout log = %+v, want truncated abcd", logs.Items[0])
	}
	if logs.Items[0].Stderr != "wxyz" || !logs.Items[0].StderrTruncated {
		t.Fatalf("stderr log = %+v, want truncated wxyz", logs.Items[0])
	}
}

func TestExecuteLogPersistenceDisabledSkipsStorage(t *testing.T) {
	t.Parallel()

	services, cleanup, _ := newTestServices(t)
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
		SessionID:       "no-log-session",
		EnvironmentName: "shell",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := services.sessions.Execute(context.Background(), created.SessionID, api.ExecuteSessionRequest{
		Command: "pwd",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	logs, err := services.sessions.ListExecutions(context.Background(), created.SessionID, store.Pagination{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListExecutions() error = %v", err)
	}
	if len(logs.Items) != 0 {
		t.Fatalf("logs len = %d, want 0", len(logs.Items))
	}
}

type testServices struct {
	store        store.RuntimeStore
	envs         store.EnvironmentStore
	sessions     *SessionService
	environments *EnvironmentService
	builds       *BuildService
}

func newTestServices(t *testing.T) (*testServices, func(), *fakeRuntime) {
	t.Helper()

	return newTestServicesWithOptions(t, nil)
}

func newTestServicesWithOptions(t *testing.T, configure func(*config.Config)) (*testServices, func(), *fakeRuntime) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		BindAddr:                 "127.0.0.1:0",
		StateDBPath:              filepath.Join(tempDir, "agent-container-hub.db"),
		ConfigRoot:               filepath.Join(tempDir, "configs"),
		WorkspaceRoot:            filepath.Join(tempDir, "workspaces"),
		BuildRoot:                filepath.Join(tempDir, "builds"),
		SessionMountTemplateRoot: filepath.Join(tempDir, "zenmind-env"),
		DefaultCommandTimeout:    100 * time.Millisecond,
		ExecLogMaxOutputBytes:    65536,
	}
	if configure != nil {
		configure(&cfg)
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaces) error = %v", err)
	}
	if err := os.MkdirAll(cfg.BuildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(builds) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.SessionMountTemplateRoot, "chats"), 0o755); err != nil {
		t.Fatalf("MkdirAll(session mount template root) error = %v", err)
	}
	st, err := store.Open(cfg.StateDBPath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	envs, err := store.OpenFileEnvironmentStore(filepath.Join(cfg.ConfigRoot, "environments"))
	if err != nil {
		t.Fatalf("store.OpenFileEnvironmentStore() error = %v", err)
	}
	fake := &fakeRuntime{containers: make(map[string]runtime.ContainerInfo)}
	return &testServices{
		store:        st,
		envs:         envs,
		sessions:     NewSessionService(cfg, st, envs, fake, slog.New(slog.NewTextHandler(os.Stdout, nil))),
		environments: NewEnvironmentService(envs, st, slog.New(slog.NewTextHandler(os.Stdout, nil))),
		builds:       NewBuildService(cfg, st, envs, fake, fake, slog.New(slog.NewTextHandler(os.Stdout, nil))),
	}, func() { _ = st.Close() }, fake
}

type fakeRuntime struct {
	mu          sync.Mutex
	containers  map[string]runtime.ContainerInfo
	execResult  runtime.ExecResult
	lastCreate  runtime.CreateOptions
	lastExec    runtime.ExecOptions
	startCalls  int
	lastBuild   runtime.BuildOptions
	buildResult runtime.BuildResult
	buildFiles  map[string]string
	buildErr    error
}

func (f *fakeRuntime) Name() string { return "fake" }

func (f *fakeRuntime) Create(_ context.Context, opts runtime.CreateOptions) (runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCreate = opts
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

func (f *fakeRuntime) Build(_ context.Context, opts runtime.BuildOptions) (runtime.BuildResult, error) {
	f.mu.Lock()
	f.lastBuild = opts
	f.buildFiles = make(map[string]string)
	err := filepath.Walk(opts.ContextDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(opts.ContextDir, path)
		if relErr != nil {
			return relErr
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		f.buildFiles[rel] = string(payload)
		return nil
	})
	f.mu.Unlock()
	if err != nil {
		return runtime.BuildResult{}, err
	}
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
