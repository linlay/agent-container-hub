package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentbox/internal/api"
	"agentbox/internal/config"
	"agentbox/internal/model"
	"agentbox/internal/runtime"
	"agentbox/internal/store"
)

const (
	buildStatusSucceeded = "succeeded"
	buildStatusFailed    = "failed"
)

type BuildService struct {
	cfg     config.Config
	store   store.Store
	builder runtime.Builder
	runtime runtime.Provider
	logger  *slog.Logger
}

func NewBuildService(cfg config.Config, st store.Store, builder runtime.Builder, provider runtime.Provider, logger *slog.Logger) *BuildService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BuildService{
		cfg:     cfg,
		store:   st,
		builder: builder,
		runtime: provider,
		logger:  logger,
	}
}

func (s *BuildService) BuildEnvironment(ctx context.Context, name string) (*api.BuildJobResponse, error) {
	environment, err := s.store.GetEnvironment(ctx, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(environment.Build.Dockerfile) == "" {
		return nil, fmt.Errorf("%w: build.dockerfile is required", ErrValidation)
	}

	jobID, err := generateID()
	if err != nil {
		return nil, err
	}
	job := &model.BuildJob{
		ID:              "build-" + jobID,
		EnvironmentName: environment.Name,
		ImageRef:        environment.ImageRef(),
		Status:          buildStatusFailed,
		StartedAt:       time.Now().UTC(),
	}

	buildDir := filepath.Join(s.cfg.BuildRoot, job.ID)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return nil, fmt.Errorf("create build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(environment.Build.Dockerfile), 0o644); err != nil {
		return nil, fmt.Errorf("write dockerfile: %w", err)
	}

	result, err := s.builder.Build(ctx, runtime.BuildOptions{
		ContextDir:     buildDir,
		DockerfilePath: dockerfilePath,
		Image:          environment.ImageRef(),
		BuildArgs:      cloneMap(environment.Build.BuildArgs),
	})
	job.Output = result.Output
	job.FinishedAt = result.FinishedAt
	if job.FinishedAt.IsZero() {
		job.FinishedAt = time.Now().UTC()
	}
	if err != nil {
		job.Error = err.Error()
		if saveErr := s.store.SaveBuildJob(ctx, job); saveErr != nil {
			return nil, saveErr
		}
		return buildJobToResponse(job), nil
	}

	if strings.TrimSpace(environment.Build.SmokeCommand) != "" {
		if smokeErr := s.runSmokeCheck(ctx, environment); smokeErr != nil {
			job.Error = smokeErr.Error()
			if saveErr := s.store.SaveBuildJob(ctx, job); saveErr != nil {
				return nil, saveErr
			}
			return buildJobToResponse(job), nil
		}
	}

	job.Status = buildStatusSucceeded
	if err := s.store.SaveBuildJob(ctx, job); err != nil {
		return nil, err
	}
	s.logger.Info("environment built", "environment", environment.Name, "image", environment.ImageRef())
	return buildJobToResponse(job), nil
}

func (s *BuildService) runSmokeCheck(ctx context.Context, environment *model.Environment) error {
	name, err := generateID()
	if err != nil {
		return err
	}
	workspace := filepath.Join(s.cfg.BuildRoot, "smoke-"+name)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(workspace)

	info, err := s.runtime.Create(ctx, runtime.CreateOptions{
		Name:  "smoke-" + name,
		Image: environment.ImageRef(),
		Cwd:   sessionDefaultCwd(environment.DefaultCwd),
		Env:   cloneMap(environment.DefaultEnv),
		Mounts: []model.Mount{{
			Source:      workspace,
			Destination: runtime.DefaultMountPath,
		}},
		Resources: environment.Resources,
		Labels: map[string]string{
			runtime.ManagedByLabel: "agentboxd",
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = s.runtime.Remove(context.Background(), info.ID) }()

	if _, err := s.runtime.Start(ctx, info.ID); err != nil {
		return err
	}
	result, err := s.runtime.Exec(ctx, info.ID, runtime.ExecOptions{
		Command: environment.Build.SmokeCommand,
		Args:    append([]string(nil), environment.Build.SmokeArgs...),
		Cwd:     sessionDefaultCwd(environment.DefaultCwd),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("smoke check failed with exit code %d", result.ExitCode)
	}
	return nil
}

func buildJobToResponse(job *model.BuildJob) *api.BuildJobResponse {
	return &api.BuildJobResponse{
		ID:              job.ID,
		EnvironmentName: job.EnvironmentName,
		ImageRef:        job.ImageRef,
		Status:          job.Status,
		Output:          job.Output,
		Error:           job.Error,
		StartedAt:       job.StartedAt,
		FinishedAt:      job.FinishedAt,
	}
}
