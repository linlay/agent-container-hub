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

	"agent-container-hub/internal/util"
)

type CLIProvider struct {
	binary string
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
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
	if err := util.ValidateEnvMap(opts.Env, "environment variable"); err != nil {
		return ContainerInfo{}, err
	}
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
	result, err := p.runCommand(ctx, args...)
	if err != nil {
		if isAlreadyExists(result.stderr) {
			return ContainerInfo{}, ErrContainerExists
		}
		return ContainerInfo{}, p.commandError(args, result, err)
	}
	containerID := strings.TrimSpace(result.stdout)
	return ContainerInfo{
		ID:        containerID,
		Name:      opts.Name,
		Image:     opts.Image,
		State:     ContainerStopped,
		Labels:    util.CloneMap(opts.Labels),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (p *CLIProvider) Start(ctx context.Context, containerID string) (ContainerInfo, error) {
	resolvedID, err := p.resolveContainerReference(ctx, containerID)
	if err != nil {
		return ContainerInfo{}, err
	}
	result, err := p.runCommand(ctx, "start", resolvedID)
	if err != nil {
		return ContainerInfo{}, p.commandError([]string{"start", resolvedID}, result, err)
	}
	return ContainerInfo{
		ID:    resolvedID,
		State: ContainerRunning,
	}, nil
}

func (p *CLIProvider) Exec(ctx context.Context, containerID string, opts ExecOptions) (ExecResult, error) {
	if err := util.ValidateEnvMap(map[string]string{"COMMAND": opts.Command}, "exec command"); err != nil {
		return ExecResult{}, fmt.Errorf("invalid exec command: %w", err)
	}
	resolvedID, err := p.resolveContainerReference(ctx, containerID)
	if err != nil {
		return ExecResult{}, err
	}
	info, err := p.Inspect(ctx, resolvedID)
	if err != nil {
		return ExecResult{}, err
	}
	if info.State != ContainerRunning {
		return ExecResult{}, ErrContainerNotRunning
	}

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
	args[len(args)-len(opts.Args)-2] = resolvedID
	result, err := p.runCommand(execCtx, args...)
	finishedAt := time.Now().UTC()

	execResult := ExecResult{
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Stdout:     result.stdout,
		Stderr:     result.stderr,
	}

	if execCtx.Err() == context.DeadlineExceeded {
		execResult.TimedOut = true
		execResult.ExitCode = 124
		return execResult, nil
	}
	if err == nil {
		return execResult, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		current, inspectErr := p.Inspect(ctx, resolvedID)
		if inspectErr != nil {
			if errors.Is(inspectErr, ErrContainerNotFound) {
				return ExecResult{}, ErrContainerNotFound
			}
			return ExecResult{}, inspectErr
		}
		if current.State != ContainerRunning {
			return ExecResult{}, ErrContainerNotRunning
		}
		execResult.ExitCode = result.exitCode
		return execResult, nil
	}
	return ExecResult{}, p.commandError(args, result, err)
}

func (p *CLIProvider) Build(ctx context.Context, opts BuildOptions) (BuildResult, error) {
	startedAt := time.Now().UTC()
	args := []string{"build", "-t", opts.Image}
	if strings.TrimSpace(opts.DockerfilePath) != "" {
		args = append(args, "-f", opts.DockerfilePath)
	}
	if err := util.ValidateEnvMap(opts.BuildArgs, "build argument"); err != nil {
		return BuildResult{}, err
	}
	for key, value := range opts.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, opts.ContextDir)
	result, err := p.runCommand(ctx, args...)
	finishedAt := time.Now().UTC()

	buildResult := BuildResult{
		Output:     strings.TrimSpace(result.stdout + result.stderr),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if err != nil {
		return buildResult, p.commandError(args, result, err)
	}
	return buildResult, nil
}

func (p *CLIProvider) Stop(ctx context.Context, containerID string, timeout time.Duration) error {
	resolvedID, err := p.resolveContainerReference(ctx, containerID)
	if err != nil {
		return err
	}
	args := []string{"stop"}
	if timeout > 0 {
		args = append(args, "--time", strconv.Itoa(int(timeout.Seconds())))
	}
	args = append(args, resolvedID)
	result, err := p.runCommand(ctx, args...)
	if err != nil {
		return p.commandError(args, result, err)
	}
	return err
}

func (p *CLIProvider) Remove(ctx context.Context, containerID string) error {
	resolvedID, err := p.resolveContainerReference(ctx, containerID)
	if err != nil {
		return err
	}
	result, err := p.runCommand(ctx, "rm", "-f", resolvedID)
	if err != nil {
		return p.commandError([]string{"rm", "-f", resolvedID}, result, err)
	}
	return nil
}

func (p *CLIProvider) Inspect(ctx context.Context, containerID string) (ContainerInfo, error) {
	resolvedID, err := p.resolveContainerReference(ctx, containerID)
	if err != nil {
		return ContainerInfo{}, err
	}
	result, err := p.runCommand(ctx, "inspect", resolvedID)
	if err != nil {
		return ContainerInfo{}, p.commandError([]string{"inspect", resolvedID}, result, err)
	}
	infos, err := parseInspect(result.stdout)
	if err != nil {
		return ContainerInfo{}, err
	}
	if len(infos) == 0 {
		return ContainerInfo{}, ErrContainerNotFound
	}
	return infos[0], nil
}

func (p *CLIProvider) ListByLabel(ctx context.Context, key, value string) ([]ContainerInfo, error) {
	result, err := p.runCommand(ctx, "ps", "-a", "--filter", fmt.Sprintf("label=%s=%s", key, value), "--format", "{{.ID}}")
	if err != nil {
		return nil, p.commandError([]string{"ps", "-a", "--filter", fmt.Sprintf("label=%s=%s", key, value), "--format", "{{.ID}}"}, result, err)
	}
	ids := strings.Fields(strings.TrimSpace(result.stdout))
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect"}, ids...)
	inspectResult, err := p.runCommand(ctx, args...)
	if err != nil {
		return nil, p.commandError(args, inspectResult, err)
	}
	return parseInspect(inspectResult.stdout)
}

func (p *CLIProvider) runCommand(ctx context.Context, args ...string) (commandResult, error) {
	cmd := exec.CommandContext(ctx, p.binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := commandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		return result, err
	}
	result.exitCode = -1
	return result, err
}

func (p *CLIProvider) commandError(args []string, result commandResult, err error) error {
	detail := strings.TrimSpace(result.stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.stdout)
	}
	if detail == "" {
		return fmt.Errorf("%s %s: %w", p.binary, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", p.binary, strings.Join(args, " "), err, detail)
}

func (p *CLIProvider) resolveContainerReference(ctx context.Context, ref string) (string, error) {
	result, err := p.runCommand(ctx, "ps", "-a", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}")
	if err != nil {
		return "", p.commandError([]string{"ps", "-a", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}"}, result, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(result.stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		id := strings.TrimSpace(parts[0])
		if id == ref {
			return id, nil
		}
		if len(parts) < 2 {
			continue
		}
		for _, name := range strings.Split(parts[1], ",") {
			if strings.TrimSpace(name) == ref {
				return id, nil
			}
		}
	}
	return "", ErrContainerNotFound
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
			Labels:    util.CloneMap(item.Config.Labels),
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

func isAlreadyExists(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "already in use") ||
		strings.Contains(lower, "already exists")
}

func NormalizeMountSource(path string) string {
	clean := filepath.Clean(path)
	if abs, err := filepath.Abs(clean); err == nil {
		return abs
	}
	return clean
}
