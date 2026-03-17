package store

import (
	"context"
	"errors"

	"agent-container-hub/internal/model"
)

var ErrNotFound = errors.New("record not found")

type SessionStore interface {
	SaveSession(context.Context, *model.Session) error
	GetSession(context.Context, string) (*model.Session, error)
	ListSessions(context.Context) ([]*model.Session, error)
	DeleteSession(context.Context, string) error
}

type BuildJobStore interface {
	SaveBuildJob(context.Context, *model.BuildJob) error
	ListBuildJobs(context.Context, string) ([]*model.BuildJob, error)
}

type RuntimeStore interface {
	SessionStore
	BuildJobStore
	Close() error
}

type EnvironmentStore interface {
	SaveEnvironment(context.Context, *model.Environment) error
	GetEnvironment(context.Context, string) (*model.Environment, error)
	ListEnvironments(context.Context) ([]*model.Environment, error)
	DeleteEnvironment(context.Context, string) error
}
