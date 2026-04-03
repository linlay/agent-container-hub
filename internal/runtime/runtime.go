package runtime

import (
	"context"
	"errors"
	"io"
	"time"

	"agent-container-hub/internal/model"
)

var (
	ErrContainerNotFound   = errors.New("container not found")
	ErrContainerExists     = errors.New("container already exists")
	ErrContainerNotRunning = errors.New("container not running")
	ErrRuntimeUnavailable  = errors.New("runtime unavailable")
	ErrImageNotFound       = errors.New("image not found")
)

const (
	ManagedByLabel   = "sandbox.managed_by"
	SessionIDLabel   = "sandbox.session_id"
	RootfsLabel      = "sandbox.rootfs"
	CreatedAtLabel   = "sandbox.created_at"
	DefaultMountPath = "/workspace"
)

type ContainerState string

const (
	ContainerRunning ContainerState = "running"
	ContainerStopped ContainerState = "stopped"
	ContainerExited  ContainerState = "exited"
	ContainerUnknown ContainerState = "unknown"
)

type ContainerInfo struct {
	ID        string
	Name      string
	Image     string
	State     ContainerState
	Labels    map[string]string
	CreatedAt time.Time
}

type ImageInfo struct {
	ID        string
	Ref       string
	CreatedAt time.Time
}

type ImageMetadata struct {
	Ref             string
	CreatedAt       time.Time
	TotalSizeBytes  int64
	UniqueSizeBytes int64
}

type CreateOptions struct {
	Name      string
	Image     string
	Cwd       string
	Env       map[string]string
	Mounts    []model.Mount
	Resources model.ResourceSpec
	Labels    map[string]string
}

type ExecOptions struct {
	Command string
	Args    []string
	Cwd     string
	Timeout time.Duration
}

type ExecResult struct {
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
	TimedOut   bool
}

type BuildOptions struct {
	ContextDir     string
	DockerfilePath string
	Image          string
	BuildArgs      map[string]string
	OutputSink     io.Writer
}

type BuildResult struct {
	Output     string
	StartedAt  time.Time
	FinishedAt time.Time
}

type Provider interface {
	Name() string
	Create(context.Context, CreateOptions) (ContainerInfo, error)
	Start(context.Context, string) (ContainerInfo, error)
	Exec(context.Context, string, ExecOptions) (ExecResult, error)
	Build(context.Context, BuildOptions) (BuildResult, error)
	Stop(context.Context, string, time.Duration) error
	Remove(context.Context, string) error
	Inspect(context.Context, string) (ContainerInfo, error)
	InspectImage(context.Context, string) (ImageInfo, error)
	ListByLabel(context.Context, string, string) ([]ContainerInfo, error)
}
