package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestCLIProviderCreateDoesNotInspect(t *testing.T) {
	t.Parallel()

	binary, logPath := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}

	info, err := provider.Create(context.Background(), CreateOptions{
		Name:   "demo",
		Image:  "busybox:latest",
		Cwd:    "/root",
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
	if !strings.Contains(logText, "--label sandbox.managed_by=agent-container-hub") {
		t.Fatalf("Create() log = %q, want managed_by label", logText)
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
	if !strings.Contains(logText, "ps -a --no-trunc") {
		t.Fatalf("Start() log = %q, want ps lookup", logText)
	}
}

func TestCLIProviderExecReturnsExitCodeForCommandFailure(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	t.Setenv("FAKE_RUNTIME_EXEC_EXIT", "7")
	t.Setenv("FAKE_RUNTIME_EXEC_STDOUT", "partial")
	t.Setenv("FAKE_RUNTIME_EXEC_STDERR", "warn")

	result, err := provider.Exec(context.Background(), "demo", ExecOptions{
		Command: "/bin/sh",
		Args:    []string{"-lc", "exit 7"},
		Cwd:     "/root",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("Exec() exit code = %d, want 7", result.ExitCode)
	}
	if result.Stdout != "partial" || result.Stderr != "warn" {
		t.Fatalf("Exec() output = (%q, %q), want (%q, %q)", result.Stdout, result.Stderr, "partial", "warn")
	}
}

func TestCLIProviderExecReturnsNotRunningFromInspectState(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	t.Setenv("FAKE_RUNTIME_INSPECT_STATUS", "stopped")

	_, err := provider.Exec(context.Background(), "demo", ExecOptions{
		Command: "pwd",
		Timeout: time.Second,
	})
	if !errors.Is(err, ErrContainerNotRunning) {
		t.Fatalf("Exec() error = %v, want ErrContainerNotRunning", err)
	}
}

func TestCLIProviderExecReturnsNotFoundWhenReferenceMissing(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	t.Setenv("FAKE_RUNTIME_PS_MODE", "empty")

	_, err := provider.Exec(context.Background(), "missing", ExecOptions{
		Command: "pwd",
		Timeout: time.Second,
	})
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("Exec() error = %v, want ErrContainerNotFound", err)
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
		"ps)\n" +
		"  if [ \"$FAKE_RUNTIME_PS_MODE\" = 'empty' ]; then\n" +
		"    exit 0\n" +
		"  fi\n" +
		"  if [ -n \"$FAKE_RUNTIME_PS_OUTPUT\" ]; then\n" +
		"    printf '%s\\n' \"$FAKE_RUNTIME_PS_OUTPUT\"\n" +
		"  else\n" +
		"    printf 'ctr-demo\\tdemo\\n'\n" +
		"  fi\n" +
		"  ;;\n" +
		"create)\n" +
		"  echo ctr-demo\n" +
		"  ;;\n" +
		"start)\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"inspect)\n" +
		"  if [ \"$FAKE_RUNTIME_INSPECT_MODE\" = 'missing' ]; then\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  status=\"${FAKE_RUNTIME_INSPECT_STATUS:-running}\"\n" +
		"  printf '[{\"Id\":\"ctr-demo\",\"Name\":\"/demo\",\"ImageName\":\"busybox:latest\",\"Config\":{\"Image\":\"busybox:latest\",\"Labels\":{}},\"State\":{\"Status\":\"%s\"},\"Created\":\"2026-03-17T12:38:34Z\"}]\\n' \"$status\"\n" +
		"  ;;\n" +
		"exec)\n" +
		"  printf '%s' \"$FAKE_RUNTIME_EXEC_STDOUT\"\n" +
		"  printf '%s' \"$FAKE_RUNTIME_EXEC_STDERR\" >&2\n" +
		"  exit \"${FAKE_RUNTIME_EXEC_EXIT:-0}\"\n" +
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
