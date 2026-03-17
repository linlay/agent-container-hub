package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CLIProvider struct {
	binary string
}

func NewAutoProvider(explicit string) (Provider, error) {
	if explicit != "" {
		if _, err := exec.LookPath(explicit); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrRuntimeUnavailable, explicit)
		}
		return &CLIProvider{binary: explicit}, nil
	}
	for _, candidate := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return &CLIProvider{binary: candidate}, nil
		}
	}
	return nil, ErrRuntimeUnavailable
}

func (p *CLIProvider) Name() string {
	return p.binary
}

func (p *CLIProvider) Create(ctx context.Context, opts CreateOptions) (ContainerInfo, error) {
	args := []string{"create", "--name", opts.Name}
	args = append(args, "--label", fmt.Sprintf("%s=agent-container-hub", ManagedByLabel))
	for key, value := range opts.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	if opts.Cwd != "" {
		args = append(args, "--workdir", opts.Cwd)
	}
	if opts.Resources.CPU > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(opts.Resources.CPU, 'f', -1, 64))
	}
	if opts.Resources.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", opts.Resources.MemoryMB))
	}
	if opts.Resources.PIDs > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(opts.Resources.PIDs))
	}
	for key, value := range opts.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", key, value))
	}
	for _, mount := range opts.Mounts {
		spec := fmt.Sprintf("type=bind,src=%s,dst=%s", mount.Source, mount.Destination)
		if mount.ReadOnly {
			spec += ",ro=true"
		}
		args = append(args, "--mount", spec)
	}
	args = append(args, opts.Image, "/bin/sh", "-lc", "trap exit TERM INT; while :; do sleep 3600; done")
	output, err := p.run(ctx, args...)
	if err != nil {
		return ContainerInfo{}, err
	}
	containerID := strings.TrimSpace(output)
	return ContainerInfo{
		ID:        containerID,
		Name:      opts.Name,
		Image:     opts.Image,
		State:     ContainerStopped,
		Labels:    cloneMap(opts.Labels),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (p *CLIProvider) Start(ctx context.Context, containerID string) (ContainerInfo, error) {
	if _, err := p.run(ctx, "start", containerID); err != nil {
		return ContainerInfo{}, err
	}
	return ContainerInfo{
		ID:    containerID,
		State: ContainerRunning,
	}, nil
}

func (p *CLIProvider) Exec(ctx context.Context, containerID string, opts ExecOptions) (ExecResult, error) {
	startedAt := time.Now().UTC()
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"exec"}
	if opts.Cwd != "" {
		args = append(args, "--workdir", opts.Cwd)
	}
	args = append(args, containerID, opts.Command)
	args = append(args, opts.Args...)

	cmd := exec.CommandContext(execCtx, p.binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	finishedAt := time.Now().UTC()

	result := ExecResult{
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if execCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = 124
		return result, nil
	}
	if err == nil {
		return result, nil
	}
	if isNotFound(stderr.String()) {
		return ExecResult{}, ErrContainerNotFound
	}
	if isNotRunning(stderr.String()) {
		return ExecResult{}, ErrContainerNotRunning
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return ExecResult{}, fmt.Errorf("exec command failed: %w", err)
}

func (p *CLIProvider) Build(ctx context.Context, opts BuildOptions) (BuildResult, error) {
	startedAt := time.Now().UTC()
	args := []string{"build", "-t", opts.Image}
	if strings.TrimSpace(opts.DockerfilePath) != "" {
		args = append(args, "-f", opts.DockerfilePath)
	}
	for key, value := range opts.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, opts.ContextDir)

	cmd := exec.CommandContext(ctx, p.binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	finishedAt := time.Now().UTC()

	result := BuildResult{
		Output:     strings.TrimSpace(stdout.String() + stderr.String()),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if err != nil {
		return result, fmt.Errorf("%s %s: %w", p.binary, strings.Join(args, " "), err)
	}
	return result, nil
}

func (p *CLIProvider) Stop(ctx context.Context, containerID string, timeout time.Duration) error {
	args := []string{"stop"}
	if timeout > 0 {
		args = append(args, "--time", strconv.Itoa(int(timeout.Seconds())))
	}
	args = append(args, containerID)
	_, err := p.run(ctx, args...)
	return err
}

func (p *CLIProvider) Remove(ctx context.Context, containerID string) error {
	_, err := p.run(ctx, "rm", "-f", containerID)
	return err
}

func (p *CLIProvider) Inspect(ctx context.Context, containerID string) (ContainerInfo, error) {
	output, err := p.run(ctx, "inspect", containerID)
	if err != nil {
		return ContainerInfo{}, err
	}
	infos, err := parseInspect(output)
	if err != nil {
		return ContainerInfo{}, err
	}
	if len(infos) == 0 {
		return ContainerInfo{}, ErrContainerNotFound
	}
	return infos[0], nil
}

func (p *CLIProvider) ListByLabel(ctx context.Context, key, value string) ([]ContainerInfo, error) {
	output, err := p.run(ctx, "ps", "-a", "--filter", fmt.Sprintf("label=%s=%s", key, value), "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(strings.TrimSpace(output))
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect"}, ids...)
	inspectOutput, err := p.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseInspect(inspectOutput)
}

func (p *CLIProvider) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, p.binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if isNotFound(stderr.String()) {
			return "", ErrContainerNotFound
		}
		if isAlreadyExists(stderr.String()) {
			return "", ErrContainerExists
		}
		return "", fmt.Errorf("%s %s: %w: %s", p.binary, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

type inspectResponse struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"ImageName"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	Created string `json:"Created"`
}

func parseInspect(raw string) ([]ContainerInfo, error) {
	var responses []inspectResponse
	if err := json.Unmarshal([]byte(raw), &responses); err != nil {
		return nil, fmt.Errorf("parse inspect: %w", err)
	}
	infos := make([]ContainerInfo, 0, len(responses))
	for _, item := range responses {
		createdAt, _ := time.Parse(time.RFC3339Nano, item.Created)
		image := item.Image
		if image == "" {
			image = item.Config.Image
		}
		infos = append(infos, ContainerInfo{
			ID:        item.ID,
			Name:      strings.TrimPrefix(item.Name, "/"),
			Image:     image,
			State:     parseContainerState(item.State.Status),
			Labels:    cloneMap(item.Config.Labels),
			CreatedAt: createdAt.UTC(),
		})
	}
	return infos, nil
}

func parseContainerState(raw string) ContainerState {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "running":
		return ContainerRunning
	case "stopped":
		return ContainerStopped
	case "exited":
		return ContainerExited
	default:
		return ContainerUnknown
	}
}

func isNotFound(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no such container") ||
		strings.Contains(lower, "no such object") ||
		strings.Contains(lower, "not found")
}

func isAlreadyExists(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "already in use") ||
		strings.Contains(lower, "already exists")
}

func isNotRunning(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "is not running") ||
		strings.Contains(lower, "can only create exec sessions on running containers") ||
		strings.Contains(lower, "container state improper")
}

func NormalizeMountSource(path string) string {
	clean := filepath.Clean(path)
	if abs, err := filepath.Abs(clean); err == nil {
		return abs
	}
	return clean
}

func cloneMap[V any](src map[string]V) map[string]V {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]V, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
