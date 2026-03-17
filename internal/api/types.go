package api

import (
	"time"

	"agent-container-hub/internal/model"
)

type CreateSessionRequest struct {
	SessionID       string            `json:"session_id"`
	EnvironmentName string            `json:"environment_name"`
	Labels          map[string]string `json:"labels"`
}

type ExecuteSessionRequest struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Cwd       string   `json:"cwd"`
	TimeoutMS int64    `json:"timeout_ms"`
}

type ExecuteSessionResponse struct {
	SessionID  string    `json:"session_id"`
	ExitCode   int       `json:"exit_code"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	TimedOut   bool      `json:"timed_out"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type StopSessionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type SessionResponse struct {
	SessionID       string             `json:"session_id"`
	EnvironmentName string             `json:"environment_name"`
	ContainerID     string             `json:"container_id,omitempty"`
	Image           string             `json:"image"`
	DefaultCwd      string             `json:"cwd"`
	WorkspacePath   string             `json:"workspace_path"`
	Labels          map[string]string  `json:"labels,omitempty"`
	Resources       model.ResourceSpec `json:"resources"`
	Mounts          []model.Mount      `json:"mounts,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	Status          string             `json:"status,omitempty"`
}

type UpsertEnvironmentRequest struct {
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	ImageRepository string             `json:"image_repository"`
	ImageTag        string             `json:"image_tag"`
	DefaultCwd      string             `json:"default_cwd"`
	DefaultEnv      map[string]string  `json:"default_env"`
	Mounts          []model.Mount      `json:"mounts"`
	Resources       model.ResourceSpec `json:"resources"`
	Enabled         bool               `json:"enabled"`
	Build           model.BuildSpec    `json:"build"`
}

type EnvironmentResponse struct {
	Name            string             `json:"name"`
	Description     string             `json:"description,omitempty"`
	ImageRepository string             `json:"image_repository"`
	ImageTag        string             `json:"image_tag"`
	ImageRef        string             `json:"image_ref"`
	DefaultCwd      string             `json:"default_cwd"`
	DefaultEnv      map[string]string  `json:"default_env,omitempty"`
	Mounts          []model.Mount      `json:"mounts,omitempty"`
	Resources       model.ResourceSpec `json:"resources"`
	Enabled         bool               `json:"enabled"`
	Build           model.BuildSpec    `json:"build"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	LastBuild       *BuildJobResponse  `json:"last_build,omitempty"`
}

type BuildEnvironmentRequest struct{}

type BuildJobResponse struct {
	ID              string    `json:"id"`
	EnvironmentName string    `json:"environment_name"`
	ImageRef        string    `json:"image_ref"`
	Status          string    `json:"status"`
	Output          string    `json:"output,omitempty"`
	Error           string    `json:"error,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

type LoginRequest struct {
	Token string `json:"token"`
}
