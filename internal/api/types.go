package api

import (
	"time"

	"agent-container-hub/internal/model"
)

type CreateSessionRequest struct {
	SessionID       string            `json:"session_id"`
	EnvironmentName string            `json:"environment_name"`
	Labels          map[string]string `json:"labels"`
	Mounts          []model.Mount     `json:"mounts"`
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
	DurationMS int64     `json:"duration_ms"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type SessionExecutionResponse struct {
	ID              int64     `json:"id"`
	SessionID       string    `json:"session_id"`
	Command         string    `json:"command"`
	Args            []string  `json:"args,omitempty"`
	Cwd             string    `json:"cwd"`
	TimeoutMS       int64     `json:"timeout_ms"`
	ExitCode        int       `json:"exit_code"`
	Stdout          string    `json:"stdout,omitempty"`
	Stderr          string    `json:"stderr,omitempty"`
	StdoutTruncated bool      `json:"stdout_truncated,omitempty"`
	StderrTruncated bool      `json:"stderr_truncated,omitempty"`
	TimedOut        bool      `json:"timed_out"`
	DurationMS      int64     `json:"duration_ms"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

type SessionListResponse struct {
	Items    []*SessionResponse `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

type SessionExecutionListResponse struct {
	Items    []*SessionExecutionResponse `json:"items"`
	Total    int                         `json:"total"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
}

type SessionCreateTemplateResponse struct {
	MountTemplateRoot string        `json:"mount_template_root"`
	DefaultMounts     []model.Mount `json:"default_mounts"`
	ChatIDs           []string      `json:"chat_ids"`
}

type CreateSessionResponse struct {
	SessionResponse
	DurationMS int64 `json:"duration_ms"`
}

type StopSessionResponse struct {
	SessionID  string `json:"session_id"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
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
	StoppedAt       time.Time          `json:"stopped_at,omitempty"`
}

type UpsertEnvironmentRequest struct {
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	ImageRepository string              `json:"image_repository"`
	ImageTag        string              `json:"image_tag"`
	DefaultCwd      string              `json:"default_cwd"`
	DefaultEnv      map[string]string   `json:"default_env"`
	Mounts          []model.Mount       `json:"mounts"`
	Resources       model.ResourceSpec  `json:"resources"`
	Enabled         bool                `json:"enabled"`
	DefaultExecute  model.ExecutePreset `json:"default_execute"`
	Build           model.BuildSpec     `json:"build"`
}

type EnvironmentResponse struct {
	Name            string              `json:"name"`
	Description     string              `json:"description,omitempty"`
	ImageRepository string              `json:"image_repository"`
	ImageTag        string              `json:"image_tag"`
	ImageRef        string              `json:"image_ref"`
	DefaultCwd      string              `json:"default_cwd"`
	DefaultEnv      map[string]string   `json:"default_env,omitempty"`
	Mounts          []model.Mount       `json:"mounts,omitempty"`
	Resources       model.ResourceSpec  `json:"resources"`
	Enabled         bool                `json:"enabled"`
	DefaultExecute  model.ExecutePreset `json:"default_execute,omitempty"`
	Build           model.BuildSpec     `json:"build"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	LastBuild       *BuildJobResponse   `json:"last_build,omitempty"`
	YAML            string              `json:"yaml,omitempty"`
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
