package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/store"
	"gopkg.in/yaml.v3"
)

type EnvironmentService struct {
	environments store.EnvironmentStore
	builds       store.BuildJobStore
	logger       *slog.Logger
}

func NewEnvironmentService(environments store.EnvironmentStore, builds store.BuildJobStore, logger *slog.Logger) *EnvironmentService {
	if logger == nil {
		logger = slog.Default()
	}
	return &EnvironmentService{environments: environments, builds: builds, logger: logger}
}

func (s *EnvironmentService) Upsert(ctx context.Context, req api.UpsertEnvironmentRequest) (*api.EnvironmentResponse, error) {
	name := strings.TrimSpace(req.Name)
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ImageRepository) == "" {
		return nil, fmt.Errorf("%w: image_repository is required", ErrValidation)
	}
	if strings.TrimSpace(req.ImageTag) == "" {
		return nil, fmt.Errorf("%w: image_tag is required", ErrValidation)
	}
	if strings.TrimSpace(req.Build.Dockerfile) == "" {
		return nil, fmt.Errorf("%w: build.dockerfile is required", ErrValidation)
	}

	environment := &model.Environment{
		Name:            name,
		Description:     strings.TrimSpace(req.Description),
		ImageRepository: strings.TrimSpace(req.ImageRepository),
		ImageTag:        strings.TrimSpace(req.ImageTag),
		DefaultCwd:      sessionDefaultCwd(req.DefaultCwd),
		DefaultEnv:      cloneMap(req.DefaultEnv),
		Mounts:          append([]model.Mount(nil), req.Mounts...),
		Resources:       req.Resources,
		Enabled:         req.Enabled,
		Build:           req.Build.Clone(),
	}

	if err := s.environments.SaveEnvironment(ctx, environment); err != nil {
		return nil, err
	}
	stored, err := s.environments.GetEnvironment(ctx, name)
	if err != nil {
		return nil, err
	}
	s.logger.Info("environment upserted", "environment", environment.Name, "image", environment.ImageRef())
	return s.toResponse(ctx, stored, true)
}

func (s *EnvironmentService) Get(ctx context.Context, name string) (*api.EnvironmentResponse, error) {
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	environment, err := s.environments.GetEnvironment(ctx, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	return s.toResponse(ctx, environment, true)
}

func (s *EnvironmentService) List(ctx context.Context) ([]*api.EnvironmentResponse, error) {
	environments, err := s.environments.ListEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	responses := make([]*api.EnvironmentResponse, 0, len(environments))
	for _, environment := range environments {
		response, err := s.toResponse(ctx, environment, false)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (s *EnvironmentService) toResponse(ctx context.Context, environment *model.Environment, includeYAML bool) (*api.EnvironmentResponse, error) {
	response := &api.EnvironmentResponse{
		Name:            environment.Name,
		Description:     environment.Description,
		ImageRepository: environment.ImageRepository,
		ImageTag:        environment.ImageTag,
		ImageRef:        environment.ImageRef(),
		DefaultCwd:      environment.DefaultCwd,
		DefaultEnv:      cloneMap(environment.DefaultEnv),
		Mounts:          append([]model.Mount(nil), environment.Mounts...),
		Resources:       environment.Resources,
		Enabled:         environment.Enabled,
		Build:           environment.Build.Clone(),
		CreatedAt:       environment.CreatedAt,
		UpdatedAt:       environment.UpdatedAt,
	}
	jobs, err := s.builds.ListBuildJobs(ctx, environment.Name)
	if err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		latest := jobs[0]
		for _, job := range jobs[1:] {
			if job.StartedAt.After(latest.StartedAt) {
				latest = job
			}
		}
		response.LastBuild = buildJobToResponse(latest)
	}
	if includeYAML {
		payload, err := yaml.Marshal(environment)
		if err != nil {
			return nil, fmt.Errorf("marshal environment yaml: %w", err)
		}
		response.YAML = string(payload)
	}
	return response, nil
}
