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
		Cwd:    DefaultMountPath,
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
		Cwd:     DefaultMountPath,
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

func TestCLIProviderInspectImageReturnsImageInfo(t *testing.T) {
	t.Parallel()

	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}

	info, err := provider.InspectImage(context.Background(), "daily-office:latest")
	if err != nil {
		t.Fatalf("InspectImage() error = %v", err)
	}
	if info.ID != "sha256:demo-image" {
		t.Fatalf("InspectImage() ID = %q, want %q", info.ID, "sha256:demo-image")
	}
	if info.Ref != "daily-office:latest" {
		t.Fatalf("InspectImage() Ref = %q, want %q", info.Ref, "daily-office:latest")
	}
	expectedCreatedAt := time.Date(2026, time.March, 17, 12, 38, 34, 0, time.UTC)
	if !info.CreatedAt.Equal(expectedCreatedAt) {
		t.Fatalf("InspectImage() CreatedAt = %s, want %s", info.CreatedAt, expectedCreatedAt)
	}
}

func TestCLIProviderInspectImageReturnsNotFoundWhenMissing(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	t.Setenv("FAKE_RUNTIME_IMAGE_INSPECT_MODE", "missing")
	t.Setenv("FAKE_RUNTIME_IMAGE_LS_MODE", "empty")

	_, err := provider.InspectImage(context.Background(), "daily-office:latest")
	if !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("InspectImage() error = %v, want ErrImageNotFound", err)
	}
}

func TestCLIProviderInspectImageFallbackOnContainerdStore(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	// image inspect reports "No such image" but image ls finds the image
	t.Setenv("FAKE_RUNTIME_IMAGE_INSPECT_MODE", "missing")

	info, err := provider.InspectImage(context.Background(), "daily-office:latest")
	if err != nil {
		t.Fatalf("InspectImage() error = %v, want nil (fallback should succeed)", err)
	}
	if info.ID != "sha256:demo-image" {
		t.Fatalf("InspectImage() ID = %q, want %q", info.ID, "sha256:demo-image")
	}
	if info.Ref != "daily-office:latest" {
		t.Fatalf("InspectImage() Ref = %q, want %q", info.Ref, "daily-office:latest")
	}
}

func TestCLIProviderListImageMetadata(t *testing.T) {
	t.Parallel()

	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}

	metadata, err := provider.ListImageMetadata(context.Background())
	if err != nil {
		t.Fatalf("ListImageMetadata() error = %v", err)
	}
	got, ok := metadata["daily-office:latest"]
	if !ok {
		t.Fatalf("metadata[daily-office:latest] missing from %+v", metadata)
	}
	if got.TotalSizeBytes != 3050000000 {
		t.Fatalf("TotalSizeBytes = %d, want %d", got.TotalSizeBytes, int64(3050000000))
	}
	if got.UniqueSizeBytes != 2881000000 {
		t.Fatalf("UniqueSizeBytes = %d, want %d", got.UniqueSizeBytes, int64(2881000000))
	}
	expectedCreatedAt := time.Date(2026, time.March, 29, 10, 43, 57, 0, time.UTC)
	if !got.CreatedAt.Equal(expectedCreatedAt) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, expectedCreatedAt)
	}
}

func TestParseDockerSystemDFVerboseJSON(t *testing.T) {
	t.Parallel()

	raw := `{"Images":[{"Repository":"daily-office","Tag":"latest","CreatedAt":"2026-03-29 18:43:57 +0800 CST","Size":"3.05GB","UniqueSize":"2.881GB"},{"Repository":"toolbox","Tag":"latest","CreatedAt":"2026-04-02 22:25:53 +0800 CST","Size":"252MB","UniqueSize":"85.28MB"},{"Repository":"busybox","Tag":"latest","CreatedAt":"2024-09-26 21:31:42 +0000 UTC","Size":"8.192kB","UniqueSize":"0B"},{"Repository":"<none>","Tag":"<none>","CreatedAt":"2026-03-29 19:39:20 +0800 CST","Size":"6.16MB","UniqueSize":"1.925MB"}]}`

	metadata, err := parseDockerSystemDFVerboseJSON(raw)
	if err != nil {
		t.Fatalf("parseDockerSystemDFVerboseJSON() error = %v", err)
	}
	if len(metadata) != 3 {
		t.Fatalf("len(metadata) = %d, want 3", len(metadata))
	}
	if _, ok := metadata["<none>:<none>"]; ok {
		t.Fatalf("metadata should filter dangling images: %+v", metadata)
	}
	if metadata["toolbox:latest"].TotalSizeBytes != 252000000 {
		t.Fatalf("toolbox TotalSizeBytes = %d", metadata["toolbox:latest"].TotalSizeBytes)
	}
	if metadata["toolbox:latest"].UniqueSizeBytes != 85280000 {
		t.Fatalf("toolbox UniqueSizeBytes = %d", metadata["toolbox:latest"].UniqueSizeBytes)
	}
	if metadata["busybox:latest"].TotalSizeBytes != 8192 {
		t.Fatalf("busybox TotalSizeBytes = %d", metadata["busybox:latest"].TotalSizeBytes)
	}
	if metadata["busybox:latest"].UniqueSizeBytes != 0 {
		t.Fatalf("busybox UniqueSizeBytes = %d", metadata["busybox:latest"].UniqueSizeBytes)
	}
}

func TestClassifyImageNotFoundMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		image  string
		detail string
		want   string
	}{
		{
			name:   "unable to find image locally",
			image:  "daily-office:latest",
			detail: "Unable to find image 'daily-office:latest' locally",
			want:   `image "daily-office:latest" not found`,
		},
		{
			name:   "pull access denied",
			image:  "daily-office:latest",
			detail: "Error response from daemon: pull access denied for daily-office, repository does not exist or may require 'docker login'",
			want:   `image "daily-office:latest" not found`,
		},
		{
			name:   "manifest unknown",
			image:  "daily-office:latest",
			detail: "manifest unknown",
			want:   `image "daily-office:latest" not found`,
		},
		{
			name:   "extract image from detail",
			detail: "Unable to find image 'busybox:missing' locally",
			want:   `image "busybox:missing" not found`,
		},
		{
			name:   "unclassified error",
			image:  "daily-office:latest",
			detail: "permission denied",
			want:   "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyImageNotFoundMessage(tc.image, tc.detail); got != tc.want {
				t.Fatalf("classifyImageNotFoundMessage(%q, %q) = %q, want %q", tc.image, tc.detail, got, tc.want)
			}
		})
	}
}

func TestCLIProviderCreateReturnsSanitizedImageNotFoundMessage(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	t.Setenv("FAKE_RUNTIME_CREATE_EXIT", "1")
	t.Setenv("FAKE_RUNTIME_CREATE_STDERR", "Unable to find image 'daily-office:latest' locally\nError response from daemon: pull access denied for daily-office, repository does not exist or may require 'docker login'")

	_, err := provider.Create(context.Background(), CreateOptions{
		Name:  "demo",
		Image: "daily-office:latest",
	})
	if err == nil {
		t.Fatal("Create() error = nil, want runtime failure")
	}
	message, ok := PublicErrorMessage(err)
	if !ok {
		t.Fatalf("PublicErrorMessage(%v) ok = false, want true", err)
	}
	if message != `image "daily-office:latest" not found` {
		t.Fatalf("PublicErrorMessage(%v) = %q, want %q", err, message, `image "daily-office:latest" not found`)
	}
	if !strings.Contains(err.Error(), "Unable to find image 'daily-office:latest' locally") {
		t.Fatalf("Create() error = %q, want raw runtime detail", err.Error())
	}
}

func TestCLIProviderBuildDoesNotExposeUnclassifiedRuntimeFailure(t *testing.T) {
	binary, _ := writeFakeRuntimeBinary(t)
	provider := &CLIProvider{binary: binary}
	t.Setenv("FAKE_RUNTIME_BUILD_EXIT", "1")
	t.Setenv("FAKE_RUNTIME_BUILD_STDERR", "permission denied")

	_, err := provider.Build(context.Background(), BuildOptions{
		ContextDir: ".",
		Image:      "daily-office:latest",
	})
	if err == nil {
		t.Fatal("Build() error = nil, want runtime failure")
	}
	if message, ok := PublicErrorMessage(err); ok {
		t.Fatalf("PublicErrorMessage(%v) = %q, want no public message", err, message)
	}
}

func writeFakeRuntimeBinary(t *testing.T) (string, string) {
	t.Helper()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "calls.log")
	scriptPath := filepath.Join(tempDir, "docker")
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
		"  printf '%s' \"$FAKE_RUNTIME_CREATE_STDOUT\"\n" +
		"  printf '%s' \"$FAKE_RUNTIME_CREATE_STDERR\" >&2\n" +
		"  if [ -n \"$FAKE_RUNTIME_CREATE_EXIT\" ]; then\n" +
		"    exit \"$FAKE_RUNTIME_CREATE_EXIT\"\n" +
		"  fi\n" +
		"  echo ctr-demo\n" +
		"  ;;\n" +
		"start)\n" +
		"  exit 0\n" +
		"  ;;\n" +
		"build)\n" +
		"  printf '%s' \"$FAKE_RUNTIME_BUILD_STDOUT\"\n" +
		"  printf '%s' \"$FAKE_RUNTIME_BUILD_STDERR\" >&2\n" +
		"  exit \"${FAKE_RUNTIME_BUILD_EXIT:-0}\"\n" +
		"  ;;\n" +
		"inspect)\n" +
		"  if [ \"$FAKE_RUNTIME_INSPECT_MODE\" = 'missing' ]; then\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  status=\"${FAKE_RUNTIME_INSPECT_STATUS:-running}\"\n" +
		"  printf '[{\"Id\":\"ctr-demo\",\"Name\":\"/demo\",\"ImageName\":\"busybox:latest\",\"Config\":{\"Image\":\"busybox:latest\",\"Labels\":{}},\"State\":{\"Status\":\"%s\"},\"Created\":\"2026-03-17T12:38:34Z\"}]\\n' \"$status\"\n" +
		"  ;;\n" +
		"image)\n" +
		"  case \"$2\" in\n" +
		"  inspect)\n" +
		"    if [ \"$FAKE_RUNTIME_IMAGE_INSPECT_MODE\" = 'missing' ]; then\n" +
		"      printf 'Error: No such image: %s\\n' \"$3\" >&2\n" +
		"      exit 1\n" +
		"    fi\n" +
		"    printf '[{\"Id\":\"sha256:demo-image\",\"RepoTags\":[\"%s\"],\"Created\":\"2026-03-17T12:38:34Z\"}]\\n' \"$3\"\n" +
		"    ;;\n" +
		"  ls)\n" +
		"    if [ \"$FAKE_RUNTIME_IMAGE_LS_MODE\" = 'empty' ]; then\n" +
		"      exit 0\n" +
		"    fi\n" +
		"    if [ -n \"$FAKE_RUNTIME_IMAGE_LS_OUTPUT\" ]; then\n" +
		"      printf '%s\\n' \"$FAKE_RUNTIME_IMAGE_LS_OUTPUT\"\n" +
		"    else\n" +
		"      ref=''\n" +
		"      for arg in \"$@\"; do\n" +
		"        case \"$arg\" in reference=*) ref=\"${arg#reference=}\";; esac\n" +
		"      done\n" +
		"      printf 'sha256:demo-image\\t%s\\n' \"$ref\"\n" +
		"    fi\n" +
		"    ;;\n" +
		"  esac\n" +
		"  ;;\n" +
		"system)\n" +
		"  if [ \"$2\" = 'df' ]; then\n" +
		"    if [ -n \"$FAKE_RUNTIME_SYSTEM_DF_OUTPUT\" ]; then\n" +
		"      printf '%s\\n' \"$FAKE_RUNTIME_SYSTEM_DF_OUTPUT\"\n" +
		"    else\n" +
		"      printf '{\"Images\":[{\"Repository\":\"daily-office\",\"Tag\":\"latest\",\"CreatedAt\":\"2026-03-29 18:43:57 +0800 CST\",\"Size\":\"3.05GB\",\"UniqueSize\":\"2.881GB\"},{\"Repository\":\"<none>\",\"Tag\":\"<none>\",\"CreatedAt\":\"2026-03-29 19:39:20 +0800 CST\",\"Size\":\"6.16MB\",\"UniqueSize\":\"1.925MB\"}]}'\n" +
		"    fi\n" +
		"  fi\n" +
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
