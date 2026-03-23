package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var ValidEnvironmentName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Mount struct {
	Source      string `json:"source" yaml:"source"`
	Destination string `json:"destination" yaml:"destination"`
	ReadOnly    bool   `json:"read_only" yaml:"read_only"`
}

type ResourceSpec struct {
	CPU      float64 `json:"cpu" yaml:"cpu"`
	MemoryMB int64   `json:"memory_mb" yaml:"memory_mb"`
	PIDs     int     `json:"pids" yaml:"pids"`
}

type BuildSpec struct {
	Dockerfile   string            `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	BuildArgs    map[string]string `json:"build_args,omitempty" yaml:"build_args,omitempty"`
	Notes        string            `json:"notes,omitempty" yaml:"notes,omitempty"`
	SmokeCommand string            `json:"smoke_command,omitempty" yaml:"smoke_command,omitempty"`
	SmokeArgs    []string          `json:"smoke_args,omitempty" yaml:"smoke_args,omitempty"`
}

type ExecutePreset struct {
	Command   string   `json:"command,omitempty" yaml:"command,omitempty"`
	Args      []string `json:"args,omitempty" yaml:"args,omitempty"`
	Cwd       string   `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	TimeoutMS int64    `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
}

type Environment struct {
	Name            string            `json:"name" yaml:"name"`
	Description     string            `json:"description,omitempty" yaml:"description,omitempty"`
	ImageRepository string            `json:"image_repository" yaml:"image_repository"`
	ImageTag        string            `json:"image_tag" yaml:"image_tag"`
	DefaultCwd      string            `json:"default_cwd" yaml:"default_cwd"`
	DefaultEnv      map[string]string `json:"default_env,omitempty" yaml:"default_env,omitempty"`
	AgentPrompt     string            `json:"agent_prompt,omitempty" yaml:"agent_prompt,omitempty"`
	Mounts          []Mount           `json:"mounts,omitempty" yaml:"mounts,omitempty"`
	Resources       ResourceSpec      `json:"resources" yaml:"resources"`
	Enabled         bool              `json:"enabled" yaml:"enabled"`
	DefaultExecute  ExecutePreset     `json:"default_execute,omitempty" yaml:"default_execute,omitempty"`
	Build           BuildSpec         `json:"build" yaml:"build"`
	CreatedAt       time.Time         `json:"created_at" yaml:"-"`
	UpdatedAt       time.Time         `json:"updated_at" yaml:"-"`
}

func (e *Environment) Clone() *Environment {
	if e == nil {
		return nil
	}
	cp := *e
	cp.DefaultEnv = CloneMap(e.DefaultEnv)
	cp.Mounts = append([]Mount(nil), e.Mounts...)
	cp.DefaultExecute = e.DefaultExecute.Clone()
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
	cp.BuildArgs = CloneMap(b.BuildArgs)
	cp.SmokeArgs = append([]string(nil), b.SmokeArgs...)
	return cp
}

func (p ExecutePreset) Clone() ExecutePreset {
	cp := p
	cp.Args = append([]string(nil), p.Args...)
	return cp
}

type BuildJobStatus string

const (
	BuildJobStatusSucceeded BuildJobStatus = "succeeded"
	BuildJobStatusFailed    BuildJobStatus = "failed"
)

type BuildJob struct {
	ID              string         `json:"id"`
	EnvironmentName string         `json:"environment_name"`
	ImageRef        string         `json:"image_ref"`
	Status          BuildJobStatus `json:"status"`
	Output          string         `json:"output,omitempty"`
	Error           string         `json:"error,omitempty"`
	StartedAt       time.Time      `json:"started_at"`
	FinishedAt      time.Time      `json:"finished_at"`
}

func (j *BuildJob) Clone() *BuildJob {
	if j == nil {
		return nil
	}
	cp := *j
	return &cp
}

type SessionStatus string

const (
	SessionStatusActive  SessionStatus = "active"
	SessionStatusStopped SessionStatus = "stopped"
)

type Session struct {
	ID              string            `json:"session_id"`
	ContainerID     string            `json:"container_id,omitempty"`
	EnvironmentName string            `json:"environment_name"`
	Image           string            `json:"image"`
	DefaultCwd      string            `json:"cwd"`
	RootfsPath      string            `json:"rootfs_path"`
	Env             map[string]string `json:"env,omitempty"`
	Mounts          []Mount           `json:"mounts,omitempty"`
	Resources       ResourceSpec      `json:"resources"`
	Labels          map[string]string `json:"labels,omitempty"`
	Status          SessionStatus     `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
	StoppedAt       time.Time         `json:"stopped_at,omitempty"`
}

func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Env = CloneMap(s.Env)
	cp.Labels = CloneMap(s.Labels)
	cp.Mounts = append([]Mount(nil), s.Mounts...)
	return &cp
}

type SessionExecution struct {
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

func (e *SessionExecution) Clone() *SessionExecution {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Args = append([]string(nil), e.Args...)
	return &cp
}

func CloneMap[V any](src map[string]V) map[string]V {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]V, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func ValidateEnvMap(values map[string]string, kind string) error {
	for key, value := range values {
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("%s key %q must match %s", kind, key, envKeyPattern.String())
		}
		if containsControlChars(value) {
			return fmt.Errorf("%s value for %q contains control characters", kind, key)
		}
	}
	return nil
}

func containsControlChars(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
