package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/store"
)

type imageMetadataProvider interface {
	ListImageMetadata(context.Context) (map[string]runtime.ImageMetadata, error)
}

type EnvironmentService struct {
	environments store.EnvironmentStore
	configRoot   string
	builds       interface {
		LatestBuildJob(context.Context, string) (*api.BuildJobResponse, error)
	}
	runtime imageInspector
	logger  *slog.Logger
}

func NewEnvironmentService(configRoot string, environments store.EnvironmentStore, builds interface {
	LatestBuildJob(context.Context, string) (*api.BuildJobResponse, error)
}, imageRuntime imageInspector, logger *slog.Logger) *EnvironmentService {
	if logger == nil {
		logger = slog.Default()
	}
	return &EnvironmentService{
		environments: environments,
		configRoot:   strings.TrimSpace(configRoot),
		builds:       builds,
		runtime:      imageRuntime,
		logger:       logger,
	}
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
	if err := model.ValidateEnvMap(req.DefaultEnv, "default_env"); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err)
	}
	if err := model.ValidateEnvMap(req.Build.BuildArgs, "build.build_args"); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err)
	}

	environment := &model.Environment{
		Name:            name,
		Description:     strings.TrimSpace(req.Description),
		ImageRepository: strings.TrimSpace(req.ImageRepository),
		ImageTag:        strings.TrimSpace(req.ImageTag),
		DefaultCwd:      sessionDefaultCwd("", req.DefaultCwd),
		DefaultEnv:      model.CloneMap(req.DefaultEnv),
		AgentPrompt:     req.AgentPrompt,
		Mounts:          append([]model.Mount(nil), req.Mounts...),
		Resources:       req.Resources,
		Enabled:         req.Enabled,
		DefaultExecute:  req.DefaultExecute.Clone(),
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
	imageMetadata, loaded := s.loadImageMetadata(ctx)
	return s.toResponse(ctx, stored, true, imageMetadata, loaded)
}

func (s *EnvironmentService) Get(ctx context.Context, name string) (*api.EnvironmentResponse, error) {
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	environment, err := s.environments.GetEnvironment(ctx, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	imageMetadata, loaded := s.loadImageMetadata(ctx)
	return s.toResponse(ctx, environment, true, imageMetadata, loaded)
}

func (s *EnvironmentService) List(ctx context.Context) ([]*api.EnvironmentResponse, error) {
	environments, err := s.environments.ListEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	imageMetadata, loaded := s.loadImageMetadata(ctx)
	responses := make([]*api.EnvironmentResponse, 0, len(environments))
	for _, environment := range environments {
		response, err := s.toResponse(ctx, environment, false, imageMetadata, loaded)
		if err != nil {
			s.logger.Warn("environment toResponse failed, using degraded response",
				"environment", environment.Name,
				"image", environment.ImageRef(),
				"error", err,
			)
			responses = append(responses, s.toResponseDegraded(environment))
			continue
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (s *EnvironmentService) GetAgentPrompt(ctx context.Context, name string) (*api.EnvironmentAgentPromptResponse, error) {
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	environment, err := s.environments.GetEnvironment(ctx, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	prompt := environment.AgentPrompt
	hasPrompt := strings.TrimSpace(prompt) != ""
	if !hasPrompt {
		prompt = ""
	}
	return &api.EnvironmentAgentPromptResponse{
		EnvironmentName: environment.Name,
		HasPrompt:       hasPrompt,
		Prompt:          prompt,
		UpdatedAt:       environment.UpdatedAt,
	}, nil
}

func (s *EnvironmentService) ListFiles(ctx context.Context, name string) ([]*api.EnvironmentFileResponse, error) {
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	files, err := s.environments.ListEnvironmentFiles(ctx, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	response := make([]*api.EnvironmentFileResponse, 0, len(files))
	for _, file := range files {
		response = append(response, &api.EnvironmentFileResponse{
			Path:       file.Path,
			Size:       file.Size,
			ModifiedAt: file.ModifiedAt,
			Type:       file.Type,
		})
	}
	return response, nil
}

func (s *EnvironmentService) GetFile(ctx context.Context, name, relPath string) (*api.EnvironmentFileResponse, error) {
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	file, err := s.environments.ReadEnvironmentFile(ctx, strings.TrimSpace(name), relPath)
	if err != nil {
		return nil, err
	}
	return &api.EnvironmentFileResponse{
		Path:       file.Path,
		Size:       file.Size,
		ModifiedAt: file.ModifiedAt,
		Type:       file.Type,
		Content:    string(file.Content),
	}, nil
}

func (s *EnvironmentService) PutFile(ctx context.Context, name, relPath, content string) (*api.EnvironmentFileResponse, error) {
	if err := validateEnvironmentName(name); err != nil {
		return nil, err
	}
	if err := s.environments.WriteEnvironmentFile(ctx, strings.TrimSpace(name), relPath, []byte(content)); err != nil {
		return nil, err
	}
	return s.GetFile(ctx, name, relPath)
}

func (s *EnvironmentService) toResponse(ctx context.Context, environment *model.Environment, includeYAML bool, imageMetadata map[string]runtime.ImageMetadata, imageMetadataLoaded bool) (*api.EnvironmentResponse, error) {
	response := &api.EnvironmentResponse{
		Name:            environment.Name,
		Description:     environment.Description,
		ImageRepository: environment.ImageRepository,
		ImageTag:        environment.ImageTag,
		ImageRef:        environment.ImageRef(),
		DefaultCwd:      environment.DefaultCwd,
		DefaultEnv:      model.CloneMap(environment.DefaultEnv),
		AgentPrompt:     environment.AgentPrompt,
		Mounts:          append([]model.Mount(nil), environment.Mounts...),
		Resources:       environment.Resources,
		Enabled:         environment.Enabled,
		DefaultExecute:  environment.DefaultExecute.Clone(),
		Build:           environment.Build.Clone(),
		CreatedAt:       environment.CreatedAt,
		UpdatedAt:       environment.UpdatedAt,
	}
	if imageMetadataLoaded {
		if metadata, ok := imageMetadata[response.ImageRef]; ok {
			response.Available = true
			response.ImageMetadata = imageMetadataToResponse(metadata)
		} else {
			response.Available = false
		}
	} else {
		info, available, err := inspectLocalImage(ctx, s.runtime, environment.ImageRef(), s.logger)
		if err != nil {
			return nil, err
		}
		response.Available = available
		if available {
			response.ImageMetadata = imageMetadataToResponse(runtime.ImageMetadata{
				Ref:       info.Ref,
				CreatedAt: info.CreatedAt,
			})
		}
	}
	availableTargets, err := AvailableBuildTargets(s.configRoot, environment.Name)
	if err != nil {
		return nil, err
	}
	response.AvailableBuildTargets = append([]string(nil), availableTargets...)
	latestBuild, err := s.builds.LatestBuildJob(ctx, environment.Name)
	if err != nil {
		return nil, err
	}
	if latestBuild != nil {
		response.LastBuild = latestBuild
	}
	if includeYAML {
		payload, err := s.environments.ReadEnvironmentFile(ctx, environment.Name, "environment.yml")
		if err != nil {
			return nil, fmt.Errorf("read environment yaml: %w", err)
		}
		response.YAML = string(payload.Content)
	}
	return response, nil
}

func (s *EnvironmentService) loadImageMetadata(ctx context.Context) (map[string]runtime.ImageMetadata, bool) {
	provider, ok := s.runtime.(imageMetadataProvider)
	if !ok {
		return nil, false
	}
	metadata, err := provider.ListImageMetadata(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("list image metadata failed, falling back to inspect", "error", err)
		}
		return nil, false
	}
	if metadata == nil {
		return nil, false
	}
	return metadata, true
}

func imageMetadataToResponse(metadata runtime.ImageMetadata) *api.ImageMetadataResponse {
	if metadata.CreatedAt.IsZero() && metadata.TotalSizeBytes == 0 && metadata.UniqueSizeBytes == 0 {
		return nil
	}
	response := &api.ImageMetadataResponse{
		CreatedAt: metadata.CreatedAt,
	}
	if metadata.TotalSizeBytes > 0 {
		response.TotalSizeBytes = &metadata.TotalSizeBytes
	}
	if metadata.UniqueSizeBytes > 0 {
		response.UniqueSizeBytes = &metadata.UniqueSizeBytes
	}
	return response
}

func (s *EnvironmentService) toResponseDegraded(environment *model.Environment) *api.EnvironmentResponse {
	return &api.EnvironmentResponse{
		Name:            environment.Name,
		Description:     environment.Description,
		ImageRepository: environment.ImageRepository,
		ImageTag:        environment.ImageTag,
		ImageRef:        environment.ImageRef(),
		DefaultCwd:      environment.DefaultCwd,
		DefaultEnv:      model.CloneMap(environment.DefaultEnv),
		AgentPrompt:     environment.AgentPrompt,
		Mounts:          append([]model.Mount(nil), environment.Mounts...),
		Resources:       environment.Resources,
		Enabled:         environment.Enabled,
		DefaultExecute:  environment.DefaultExecute.Clone(),
		Build:           environment.Build.Clone(),
		Available:       false,
		CreatedAt:       environment.CreatedAt,
		UpdatedAt:       environment.UpdatedAt,
	}
}
