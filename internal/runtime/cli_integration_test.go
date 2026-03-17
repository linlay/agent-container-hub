package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentbox/internal/model"
)

func TestCLIProviderLifecycleIntegration(t *testing.T) {
	runtimeName := os.Getenv("SANDBOX_INTEGRATION_RUNTIME")
	testImage := os.Getenv("SANDBOX_TEST_IMAGE")
	if runtimeName == "" || testImage == "" {
		t.Skip("set SANDBOX_INTEGRATION_RUNTIME and SANDBOX_TEST_IMAGE to run integration test")
	}

	provider, err := NewAutoProvider(runtimeName)
	if err != nil {
		t.Fatalf("NewAutoProvider() error = %v", err)
	}

	workspace := t.TempDir()
	containerName := "sandbox-it-" + time.Now().UTC().Format("150405")
	info, err := provider.Create(context.Background(), CreateOptions{
		Name:  containerName,
		Image: testImage,
		Cwd:   DefaultMountPath,
		Mounts: []model.Mount{{
			Source:      filepath.Clean(workspace),
			Destination: DefaultMountPath,
		}},
		Labels: map[string]string{
			ManagedByLabel: "agentboxd",
			SessionIDLabel: "integration",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer func() { _ = provider.Remove(context.Background(), info.ID) }()

	info, err = provider.Start(context.Background(), info.ID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if info.State != ContainerRunning {
		t.Fatalf("Start() state = %s, want %s", info.State, ContainerRunning)
	}

	execResult, err := provider.Exec(context.Background(), info.ID, ExecOptions{
		Command: "/bin/sh",
		Args:    []string{"-lc", "printf ok"},
		Cwd:     DefaultMountPath,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if execResult.Stdout != "ok" {
		t.Fatalf("Exec() stdout = %q, want %q", execResult.Stdout, "ok")
	}

	if err := provider.Stop(context.Background(), info.ID, time.Second); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
