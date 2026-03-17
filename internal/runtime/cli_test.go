package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseContainerState(t *testing.T) {
	t.Parallel()

	if got := parseContainerState("running"); got != ContainerRunning {
		t.Fatalf("parseContainerState() = %s, want %s", got, ContainerRunning)
	}
	if got := parseContainerState("exited"); got != ContainerExited {
		t.Fatalf("parseContainerState() = %s, want %s", got, ContainerExited)
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "docker missing container",
			output: "Error: No such container: demo",
			want:   true,
		},
		{
			name:   "podman missing object",
			output: "Error: no such object: \"demo\"",
			want:   true,
		},
		{
			name:   "generic not found",
			output: "resource not found",
			want:   true,
		},
		{
			name:   "other error",
			output: "permission denied",
			want:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isNotFound(tc.output); got != tc.want {
				t.Fatalf("isNotFound(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestIsAlreadyExists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "docker duplicate name",
			output: "Conflict. The container name \"/demo\" is already in use by container",
			want:   true,
		},
		{
			name:   "podman duplicate name",
			output: "Error: the container name \"demo\" is already in use by abc123",
			want:   true,
		},
		{
			name:   "generic exists",
			output: "resource already exists",
			want:   true,
		},
		{
			name:   "other error",
			output: "permission denied",
			want:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isAlreadyExists(tc.output); got != tc.want {
				t.Fatalf("isAlreadyExists(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestIsNotRunning(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "docker not running",
			output: "Error response from daemon: Container demo is not running",
			want:   true,
		},
		{
			name:   "podman state improper",
			output: "Error: can only create exec sessions on running containers: container state improper",
			want:   true,
		},
		{
			name:   "other error",
			output: "permission denied",
			want:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isNotRunning(tc.output); got != tc.want {
				t.Fatalf("isNotRunning(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestCLIProviderCreateDoesNotInspect(t *testing.T) {
	t.Parallel()

	binary, logPath := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}

	info, err := provider.Create(context.Background(), CreateOptions{
		Name:   "demo",
		Image:  "busybox:latest",
		Cwd:    "/workspace",
		Labels: map[string]string{"custom": "1"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if info.ID != "ctr-demo" {
		t.Fatalf("Create() ID = %q, want %q", info.ID, "ctr-demo")
	}
	if info.State != ContainerStopped {
		t.Fatalf("Create() state = %q, want %q", info.State, ContainerStopped)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	logText := string(logData)
	if strings.Contains(logText, "inspect") {
		t.Fatalf("Create() unexpectedly called inspect: %s", logText)
	}
}

func TestCLIProviderStartDoesNotInspect(t *testing.T) {
	t.Parallel()

	binary, logPath := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}

	info, err := provider.Start(context.Background(), "ctr-demo")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if info.ID != "ctr-demo" {
		t.Fatalf("Start() ID = %q, want %q", info.ID, "ctr-demo")
	}
	if info.State != ContainerRunning {
		t.Fatalf("Start() state = %q, want %q", info.State, ContainerRunning)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	logText := string(logData)
	if strings.Contains(logText, "inspect") {
		t.Fatalf("Start() unexpectedly called inspect: %s", logText)
	}
}

func writeFakeRuntimeBinary(t *testing.T) (string, string) {
	t.Helper()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "calls.log")
	scriptPath := filepath.Join(tempDir, "fake-runtime.sh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		"case \"$1\" in\n" +
		"create)\n" +
		"  echo ctr-demo\n" +
		"  ;;\n" +
		"start)\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"inspect)\n" +
		"  echo '[]'\n" +
		"  ;;\n" +
		"*)\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"esac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return scriptPath, logPath
}
