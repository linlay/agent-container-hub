package model

import (
	"strings"
	"time"
)

type Mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
}

type ResourceSpec struct {
	CPU      float64 `json:"cpu"`
	MemoryMB int64   `json:"memory_mb"`
	PIDs     int     `json:"pids"`
}

type BuildSpec struct {
	Dockerfile   string            `json:"dockerfile"`
	BuildArgs    map[string]string `json:"build_args,omitempty"`
	Notes        string            `json:"notes,omitempty"`
	SmokeCommand string            `json:"smoke_command,omitempty"`
	SmokeArgs    []string          `json:"smoke_args,omitempty"`
}

type Environment struct {
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	ImageRepository string            `json:"image_repository"`
	ImageTag        string            `json:"image_tag"`
	DefaultCwd      string            `json:"default_cwd"`
	DefaultEnv      map[string]string `json:"default_env,omitempty"`
	Mounts          []Mount           `json:"mounts,omitempty"`
	Resources       ResourceSpec      `json:"resources"`
	Enabled         bool              `json:"enabled"`
	Build           BuildSpec         `json:"build"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func (e *Environment) Clone() *Environment {
	if e == nil {
		return nil
	}
	cp := *e
	cp.DefaultEnv = cloneMap(e.DefaultEnv)
	cp.Mounts = append([]Mount(nil), e.Mounts...)
	cp.Build = e.Build.Clone()
	return &cp
}

func (e *Environment) ImageRef() string {
	if e == nil {
		return ""
	}
	repository := strings.TrimSpace(e.ImageRepository)
	tag := strings.TrimSpace(e.ImageTag)
	if repository == "" {
		return ""
	}
	if tag == "" {
		return repository
	}
	return repository + ":" + tag
}

func (b BuildSpec) Clone() BuildSpec {
	cp := b
	cp.BuildArgs = cloneMap(b.BuildArgs)
	cp.SmokeArgs = append([]string(nil), b.SmokeArgs...)
	return cp
}

type BuildJob struct {
	ID              string    `json:"id"`
	EnvironmentName string    `json:"environment_name"`
	ImageRef        string    `json:"image_ref"`
	Status          string    `json:"status"`
	Output          string    `json:"output,omitempty"`
	Error           string    `json:"error,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

func (j *BuildJob) Clone() *BuildJob {
	if j == nil {
		return nil
	}
	cp := *j
	return &cp
}

type Session struct {
	ID              string            `json:"session_id"`
	ContainerID     string            `json:"container_id,omitempty"`
	EnvironmentName string            `json:"environment_name"`
	Image           string            `json:"image"`
	DefaultCwd      string            `json:"cwd"`
	WorkspacePath   string            `json:"workspace_path"`
	Env             map[string]string `json:"env,omitempty"`
	Mounts          []Mount           `json:"mounts,omitempty"`
	Resources       ResourceSpec      `json:"resources"`
	Labels          map[string]string `json:"labels,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Env = cloneMap(s.Env)
	cp.Labels = cloneMap(s.Labels)
	cp.Mounts = append([]Mount(nil), s.Mounts...)
	return &cp
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
