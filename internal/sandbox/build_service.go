package sandbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/config"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/store"
)

const (
	BuildEventSnapshot = "snapshot"
	BuildEventStatus   = "status"
	BuildEventLog      = "log"
	BuildEventComplete = "complete"
)

type BuildEvent struct {
	Type  string
	Job   *api.BuildJobResponse
	Chunk string
}

type BuildService struct {
	cfg             config.Config
	store           store.BuildJobStore
	envs            store.EnvironmentStore
	runtime         runtime.Provider
	logger          *slog.Logger
	locks           *namedLock
	mu              sync.RWMutex
	activeJobs      map[string]*activeBuildJob
	activeJobsByEnv map[string]string
}

type activeBuildJob struct {
	mu               sync.RWMutex
	job              *model.BuildJob
	subscribers      map[int]chan BuildEvent
	nextSubscriberID int
	releaseLock      func()
}

type buildOutputSink struct {
	write func(string)
}

func (s buildOutputSink) Write(payload []byte) (int, error) {
	if len(payload) > 0 && s.write != nil {
		s.write(string(payload))
	}
	return len(payload), nil
}

func NewBuildService(cfg config.Config, st store.BuildJobStore, envs store.EnvironmentStore, provider runtime.Provider, logger *slog.Logger) *BuildService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BuildService{
		cfg:             cfg,
		store:           st,
		envs:            envs,
		runtime:         provider,
		logger:          logger,
		locks:           newNamedLock(),
		activeJobs:      make(map[string]*activeBuildJob),
		activeJobsByEnv: make(map[string]string),
	}
}

func (s *BuildService) StartBuildJob(ctx context.Context, name string) (*api.BuildJobResponse, error) {
	environment, release, err := s.prepareBuild(ctx, name)
	if err != nil {
		return nil, err
	}

	jobID, err := generateID()
	if err != nil {
		release()
		return nil, err
	}
	job := &model.BuildJob{
		ID:              "build-" + jobID,
		EnvironmentName: environment.Name,
		ImageRef:        environment.ImageRef(),
		Status:          model.BuildJobStatusBuilding,
		StartedAt:       time.Now().UTC(),
	}

	buildDir := filepath.Join(s.cfg.BuildRoot, job.ID)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		release()
		return nil, fmt.Errorf("create build dir: %w", err)
	}

	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(environment.Build.Dockerfile), 0o644); err != nil {
		release()
		_ = os.RemoveAll(buildDir)
		return nil, fmt.Errorf("write dockerfile: %w", err)
	}

	active := &activeBuildJob{
		job:         job,
		subscribers: make(map[int]chan BuildEvent),
		releaseLock: release,
	}
	s.registerActiveJob(active)

	go s.runBuildJob(context.Background(), active, environment.Clone(), buildDir, dockerfilePath)
	return buildJobToResponse(job), nil
}

func (s *BuildService) BuildEnvironment(ctx context.Context, name string) (*api.BuildJobResponse, error) {
	started, err := s.StartBuildJob(ctx, name)
	if err != nil {
		return nil, err
	}
	snapshot, events, cancel, err := s.SubscribeBuildJob(ctx, started.ID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if isTerminalBuildStatus(snapshot.Status) || events == nil {
		return snapshot, nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-events:
			if !ok {
				return s.GetBuildJob(ctx, started.ID)
			}
			if event.Type == BuildEventComplete && event.Job != nil {
				return event.Job, nil
			}
		}
	}
}

func (s *BuildService) GetBuildJob(ctx context.Context, id string) (*api.BuildJobResponse, error) {
	if active := s.getActiveJob(id); active != nil {
		return active.snapshot(), nil
	}
	job, err := s.store.GetBuildJob(ctx, id)
	if err != nil {
		return nil, err
	}
	return buildJobToResponse(job), nil
}

func (s *BuildService) LatestBuildJob(ctx context.Context, environmentName string) (*api.BuildJobResponse, error) {
	environmentName = strings.TrimSpace(environmentName)
	if environmentName == "" {
		return nil, nil
	}
	if active := s.getActiveJobForEnvironment(environmentName); active != nil {
		return active.snapshot(), nil
	}
	jobs, err := s.store.ListBuildJobs(ctx, environmentName)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return buildJobToResponse(jobs[0]), nil
}

func (s *BuildService) SubscribeBuildJob(_ context.Context, id string) (*api.BuildJobResponse, <-chan BuildEvent, func(), error) {
	if active := s.getActiveJob(id); active != nil {
		return active.subscribe()
	}
	job, err := s.store.GetBuildJob(context.Background(), id)
	if err != nil {
		return nil, nil, nil, err
	}
	cancel := func() {}
	return buildJobToResponse(job), nil, cancel, nil
}

func (s *BuildService) prepareBuild(ctx context.Context, name string) (*model.Environment, func(), error) {
	name = strings.TrimSpace(name)
	if err := validateEnvironmentName(name); err != nil {
		return nil, nil, err
	}
	environment, err := s.envs.GetEnvironment(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(environment.Build.Dockerfile) == "" {
		return nil, nil, fmt.Errorf("%w: build.dockerfile is required", ErrValidation)
	}
	if err := model.ValidateEnvMap(environment.Build.BuildArgs, "build.build_args"); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrValidation, err)
	}
	release, acquired := s.locks.tryLock(environment.Name)
	if !acquired {
		return nil, nil, fmt.Errorf("%w: build already in progress for environment %q", ErrConflict, environment.Name)
	}
	return environment, release, nil
}

func (s *BuildService) runBuildJob(ctx context.Context, active *activeBuildJob, environment *model.Environment, buildDir string, dockerfilePath string) {
	defer os.RemoveAll(buildDir)

	result, err := s.runtime.Build(ctx, runtime.BuildOptions{
		ContextDir:     buildDir,
		DockerfilePath: dockerfilePath,
		Image:          environment.ImageRef(),
		BuildArgs:      model.CloneMap(environment.Build.BuildArgs),
		OutputSink: buildOutputSink{write: func(chunk string) {
			s.appendBuildOutput(active, chunk)
		}},
	})
	if result.Output != "" {
		s.setBuildOutput(active, result.Output)
	}
	if err != nil {
		s.logger.Error("environment build failed",
			"environment", environment.Name,
			"image", environment.ImageRef(),
			"build_job_id", active.job.ID,
			"error", err,
		)
		s.finishBuildJob(active, model.BuildJobStatusFailed, err.Error(), result.FinishedAt)
		return
	}

	if strings.TrimSpace(environment.Build.SmokeCommand) != "" {
		s.setBuildStatus(active, model.BuildJobStatusSmokeChecking)
		if smokeErr := s.runSmokeCheck(ctx, environment); smokeErr != nil {
			s.logger.Error("environment smoke check failed",
				"environment", environment.Name,
				"image", environment.ImageRef(),
				"build_job_id", active.job.ID,
				"error", smokeErr,
			)
			s.finishBuildJob(active, model.BuildJobStatusFailed, smokeErr.Error(), time.Now().UTC())
			return
		}
	}

	s.logger.Info("environment built", "environment", environment.Name, "image", environment.ImageRef())
	s.finishBuildJob(active, model.BuildJobStatusSucceeded, "", time.Now().UTC())
}

func (s *BuildService) registerActiveJob(active *activeBuildJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeJobs[active.job.ID] = active
	s.activeJobsByEnv[active.job.EnvironmentName] = active.job.ID
}

func (s *BuildService) getActiveJob(id string) *activeBuildJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeJobs[strings.TrimSpace(id)]
}

func (s *BuildService) getActiveJobForEnvironment(environmentName string) *activeBuildJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobID := s.activeJobsByEnv[strings.TrimSpace(environmentName)]
	if jobID == "" {
		return nil
	}
	return s.activeJobs[jobID]
}

func (s *BuildService) appendBuildOutput(active *activeBuildJob, chunk string) {
	if chunk == "" {
		return
	}
	subscribers := active.appendOutput(chunk)
	for _, subscriber := range subscribers {
		sendBuildEvent(subscriber, BuildEvent{Type: BuildEventLog, Chunk: chunk})
	}
}

func (s *BuildService) setBuildOutput(active *activeBuildJob, output string) {
	if output == "" {
		return
	}
	active.mu.Lock()
	active.job.Output = output
	active.mu.Unlock()
}

func (s *BuildService) setBuildStatus(active *activeBuildJob, status model.BuildJobStatus) {
	snapshot, subscribers := active.setStatus(status)
	for _, subscriber := range subscribers {
		sendBuildEvent(subscriber, BuildEvent{Type: BuildEventStatus, Job: snapshot})
	}
}

func (s *BuildService) finishBuildJob(active *activeBuildJob, status model.BuildJobStatus, errMessage string, finishedAt time.Time) {
	snapshot, subscribers, releaseLock := active.finish(status, errMessage, finishedAt)
	if saveErr := s.store.SaveBuildJob(context.Background(), responseToBuildJob(snapshot)); saveErr != nil {
		s.logger.Error("save build job failed", "build_job_id", snapshot.ID, "error", saveErr)
	}

	for _, subscriber := range subscribers {
		sendBuildEvent(subscriber, BuildEvent{Type: BuildEventComplete, Job: snapshot})
		close(subscriber)
	}

	s.mu.Lock()
	delete(s.activeJobs, snapshot.ID)
	if s.activeJobsByEnv[snapshot.EnvironmentName] == snapshot.ID {
		delete(s.activeJobsByEnv, snapshot.EnvironmentName)
	}
	s.mu.Unlock()

	if releaseLock != nil {
		releaseLock()
	}
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
		Cwd:   sessionDefaultCwd("", environment.DefaultCwd),
		Env:   model.CloneMap(environment.DefaultEnv),
		Mounts: []model.Mount{{
			Source:      workspace,
			Destination: runtime.DefaultMountPath,
		}},
		Resources: environment.Resources,
		Labels: map[string]string{
			runtime.ManagedByLabel: "agent-container-hub",
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
		Cwd:     sessionDefaultCwd("", environment.DefaultCwd),
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

func (j *activeBuildJob) snapshot() *api.BuildJobResponse {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return buildJobToResponse(j.job.Clone())
}

func (j *activeBuildJob) subscribe() (*api.BuildJobResponse, <-chan BuildEvent, func(), error) {
	ch := make(chan BuildEvent, 32)
	j.mu.Lock()
	id := j.nextSubscriberID
	j.nextSubscriberID++
	j.subscribers[id] = ch
	snapshot := buildJobToResponse(j.job.Clone())
	j.mu.Unlock()

	cancel := func() {
		j.mu.Lock()
		subscriber, ok := j.subscribers[id]
		if ok {
			delete(j.subscribers, id)
		}
		j.mu.Unlock()
		if ok {
			close(subscriber)
		}
	}
	return snapshot, ch, cancel, nil
}

func (j *activeBuildJob) appendOutput(chunk string) []chan BuildEvent {
	j.mu.Lock()
	j.job.Output += chunk
	subscribers := j.subscriberSnapshotLocked()
	j.mu.Unlock()
	return subscribers
}

func (j *activeBuildJob) setStatus(status model.BuildJobStatus) (*api.BuildJobResponse, []chan BuildEvent) {
	j.mu.Lock()
	j.job.Status = status
	snapshot := buildJobToResponse(j.job.Clone())
	subscribers := j.subscriberSnapshotLocked()
	j.mu.Unlock()
	return snapshot, subscribers
}

func (j *activeBuildJob) finish(status model.BuildJobStatus, errMessage string, finishedAt time.Time) (*api.BuildJobResponse, []chan BuildEvent, func()) {
	j.mu.Lock()
	j.job.Status = status
	j.job.Error = errMessage
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	j.job.FinishedAt = finishedAt
	snapshot := buildJobToResponse(j.job.Clone())
	subscribers := j.subscriberSnapshotLocked()
	j.subscribers = make(map[int]chan BuildEvent)
	releaseLock := j.releaseLock
	j.releaseLock = nil
	j.mu.Unlock()
	return snapshot, subscribers, releaseLock
}

func (j *activeBuildJob) subscriberSnapshotLocked() []chan BuildEvent {
	subscribers := make([]chan BuildEvent, 0, len(j.subscribers))
	for _, subscriber := range j.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	return subscribers
}

func buildJobToResponse(job *model.BuildJob) *api.BuildJobResponse {
	return &api.BuildJobResponse{
		ID:              job.ID,
		EnvironmentName: job.EnvironmentName,
		ImageRef:        job.ImageRef,
		Status:          string(job.Status),
		Output:          job.Output,
		Error:           job.Error,
		StartedAt:       job.StartedAt,
		FinishedAt:      job.FinishedAt,
	}
}

func responseToBuildJob(response *api.BuildJobResponse) *model.BuildJob {
	return &model.BuildJob{
		ID:              response.ID,
		EnvironmentName: response.EnvironmentName,
		ImageRef:        response.ImageRef,
		Status:          model.BuildJobStatus(response.Status),
		Output:          response.Output,
		Error:           response.Error,
		StartedAt:       response.StartedAt,
		FinishedAt:      response.FinishedAt,
	}
}

func isTerminalBuildStatus(status string) bool {
	switch status {
	case string(model.BuildJobStatusSucceeded), string(model.BuildJobStatusFailed):
		return true
	default:
		return false
	}
}

func sendBuildEvent(ch chan BuildEvent, event BuildEvent) {
	select {
	case ch <- event:
	default:
		// Slow subscribers are allowed to drop incremental updates; they will
		// resync from the next snapshot or terminal event.
		if event.Type != BuildEventLog {
			go func() {
				defer func() {
					_ = recover()
				}()
				ch <- event
			}()
		}
	}
}

var _ io.Writer = buildOutputSink{}
