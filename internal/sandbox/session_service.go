package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"agentbox/internal/api"
	"agentbox/internal/config"
	"agentbox/internal/model"
	"agentbox/internal/runtime"
	"agentbox/internal/store"
)

var (
	ErrValidation  = errors.New("validation failed")
	ErrBusy        = errors.New("session busy")
	ErrConflict    = errors.New("session configuration conflict")
	validSessionID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)
)

type SessionService struct {
	cfg     config.Config
	store   store.Store
	runtime runtime.Provider
	logger  *slog.Logger
	lockMu  sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewSessionService(cfg config.Config, st store.Store, provider runtime.Provider, logger *slog.Logger) *SessionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionService{
		cfg:     cfg,
		store:   st,
		runtime: provider,
		logger:  logger,
		locks:   make(map[string]*sync.Mutex),
	}
}

func (s *SessionService) Create(ctx context.Context, req api.CreateSessionRequest) (*api.SessionResponse, error) {
	environmentName := strings.TrimSpace(req.EnvironmentName)
	if environmentName == "" {
		return nil, fmt.Errorf("%w: environment_name is required", ErrValidation)
	}

	environment, err := s.store.GetEnvironment(ctx, environmentName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: environment not found", store.ErrNotFound)
		}
		return nil, err
	}
	if !environment.Enabled {
		return nil, fmt.Errorf("%w: environment is disabled", ErrValidation)
	}
	if err := s.validateMounts(environment.Mounts); err != nil {
		return nil, err
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		token, err := generateID()
		if err != nil {
			return nil, fmt.Errorf("generate session token: %w", err)
		}
		sessionID = "session-" + token
	}
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	release, acquired := s.tryLock(sessionID)
	if !acquired {
		return nil, ErrBusy
	}
	defer release()

	if _, err := s.store.GetSession(ctx, sessionID); err == nil {
		return nil, fmt.Errorf("%w: session already exists", ErrConflict)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	workspacePath := filepath.Join(s.cfg.WorkspaceRoot, sessionID)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	mounts := append([]model.Mount(nil), environment.Mounts...)
	mounts = append(mounts, model.Mount{
		Source:      workspacePath,
		Destination: runtime.DefaultMountPath,
	})

	containerLabels := cloneMap(req.Labels)
	if containerLabels == nil {
		containerLabels = make(map[string]string)
	}
	containerLabels[runtime.SessionIDLabel] = sessionID
	containerLabels[runtime.WorkspaceLabel] = workspacePath
	containerLabels[runtime.CreatedAtLabel] = time.Now().UTC().Format(time.RFC3339Nano)
	containerLabels["sandbox.environment"] = environment.Name

	info, err := s.runtime.Create(ctx, runtime.CreateOptions{
		Name:      sessionID,
		Image:     environment.ImageRef(),
		Cwd:       sessionDefaultCwd(environment.DefaultCwd),
		Env:       cloneMap(environment.DefaultEnv),
		Mounts:    mounts,
		Resources: environment.Resources,
		Labels:    containerLabels,
	})
	if err != nil {
		_ = os.RemoveAll(workspacePath)
		if errors.Is(err, runtime.ErrContainerExists) {
			return nil, fmt.Errorf("%w: session already exists", ErrConflict)
		}
		return nil, err
	}
	started, err := s.runtime.Start(ctx, info.ID)
	if err != nil {
		_ = s.runtime.Remove(ctx, info.ID)
		_ = os.RemoveAll(workspacePath)
		return nil, err
	}

	session := &model.Session{
		ID:              sessionID,
		ContainerID:     info.ID,
		EnvironmentName: environment.Name,
		Image:           environment.ImageRef(),
		DefaultCwd:      sessionDefaultCwd(environment.DefaultCwd),
		WorkspacePath:   workspacePath,
		Env:             cloneMap(environment.DefaultEnv),
		Mounts:          append([]model.Mount(nil), environment.Mounts...),
		Resources:       environment.Resources,
		Labels:          cloneMap(req.Labels),
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.store.SaveSession(ctx, session); err != nil {
		return nil, err
	}

	response := sessionToResponse(session)
	response.Status = string(started.State)
	s.logger.Info("session created", "session_id", session.ID, "environment", session.EnvironmentName, "image", session.Image)
	return response, nil
}

func (s *SessionService) Execute(ctx context.Context, sessionID string, req api.ExecuteSessionRequest) (*api.ExecuteSessionResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session_id is required", ErrValidation)
	}
	if strings.TrimSpace(req.Command) == "" {
		return nil, fmt.Errorf("%w: command is required", ErrValidation)
	}
	release, acquired := s.tryLock(sessionID)
	if !acquired {
		return nil, ErrBusy
	}
	defer release()

	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	execCwd := session.DefaultCwd
	if strings.TrimSpace(req.Cwd) != "" {
		execCwd = req.Cwd
	}
	execOpts := runtime.ExecOptions{
		Command: req.Command,
		Args:    append([]string(nil), req.Args...),
		Cwd:     execCwd,
		Timeout: timeoutFor(req.TimeoutMS, s.cfg.DefaultCommandTimeout),
	}
	target := session.ContainerID
	if target == "" {
		target = session.ID
	}
	result, err := s.runtime.Exec(ctx, target, execOpts)
	if err == nil {
		return executeResponse(sessionID, result), nil
	}
	if !errors.Is(err, runtime.ErrContainerNotFound) && !errors.Is(err, runtime.ErrContainerNotRunning) {
		return nil, err
	}

	info, err := s.inspectSession(ctx, session)
	if err != nil {
		if errors.Is(err, runtime.ErrContainerNotFound) {
			_ = s.store.DeleteSession(ctx, session.ID)
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if info.State != runtime.ContainerRunning {
		if _, err := s.runtime.Start(ctx, info.ID); err != nil {
			return nil, err
		}
	}
	if session.ContainerID != info.ID {
		session.ContainerID = info.ID
		if err := s.store.SaveSession(ctx, session); err != nil {
			return nil, err
		}
	}

	result, err = s.runtime.Exec(ctx, info.ID, execOpts)
	if err != nil {
		return nil, err
	}
	return executeResponse(session.ID, result), nil
}

func (s *SessionService) Stop(ctx context.Context, sessionID string) (*api.StopSessionResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session_id is required", ErrValidation)
	}
	release, acquired := s.tryLock(sessionID)
	if !acquired {
		return nil, ErrBusy
	}
	defer release()

	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	target := sessionID
	if session != nil && session.ContainerID != "" {
		target = session.ContainerID
	}
	if err := s.runtime.Stop(ctx, target, 5*time.Second); err != nil && !errors.Is(err, runtime.ErrContainerNotFound) {
		return nil, err
	}
	if err := s.runtime.Remove(ctx, target); err != nil && !errors.Is(err, runtime.ErrContainerNotFound) {
		return nil, err
	}
	if session != nil {
		if session.WorkspacePath != "" {
			if err := os.RemoveAll(session.WorkspacePath); err != nil {
				return nil, fmt.Errorf("delete workspace: %w", err)
			}
		}
		if err := s.store.DeleteSession(ctx, sessionID); err != nil {
			return nil, err
		}
	}

	return &api.StopSessionResponse{SessionID: sessionID, Status: "stopped"}, nil
}

func (s *SessionService) List(ctx context.Context) ([]*api.SessionResponse, error) {
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	responses := make([]*api.SessionResponse, 0, len(sessions))
	for _, session := range sessions {
		resp := sessionToResponse(session)
		info, inspectErr := s.inspectSession(ctx, session)
		switch {
		case inspectErr == nil:
			resp.Status = string(info.State)
		case errors.Is(inspectErr, runtime.ErrContainerNotFound):
			resp.Status = "missing"
		default:
			return nil, inspectErr
		}
		responses = append(responses, resp)
	}
	slices.SortFunc(responses, func(a, b *api.SessionResponse) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return responses, nil
}

func (s *SessionService) Get(ctx context.Context, sessionID string) (*api.SessionResponse, error) {
	session, err := s.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	response := sessionToResponse(session)
	info, inspectErr := s.inspectSession(ctx, session)
	switch {
	case inspectErr == nil:
		response.Status = string(info.State)
	case errors.Is(inspectErr, runtime.ErrContainerNotFound):
		response.Status = "missing"
	default:
		return nil, inspectErr
	}
	return response, nil
}

func (s *SessionService) Reconcile(ctx context.Context) error {
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		info, err := s.inspectSession(ctx, session)
		if err != nil {
			if errors.Is(err, runtime.ErrContainerNotFound) {
				if session.WorkspacePath != "" {
					_ = os.RemoveAll(session.WorkspacePath)
				}
				if delErr := s.store.DeleteSession(ctx, session.ID); delErr != nil {
					return delErr
				}
				continue
			}
			return err
		}
		if session.ContainerID != info.ID {
			session.ContainerID = info.ID
			if saveErr := s.store.SaveSession(ctx, session); saveErr != nil {
				return saveErr
			}
		}
	}
	return nil
}

func (s *SessionService) validateMounts(mounts []model.Mount) error {
	for _, mount := range mounts {
		source := runtime.NormalizeMountSource(mount.Source)
		allowed := false
		for _, root := range s.cfg.AllowedMountRoots {
			root = runtime.NormalizeMountSource(root)
			rel, err := filepath.Rel(root, source)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: mount %s is outside allowed roots", ErrValidation, mount.Source)
		}
	}
	return nil
}

func (s *SessionService) inspectSession(ctx context.Context, session *model.Session) (runtime.ContainerInfo, error) {
	info, err := s.runtime.Inspect(ctx, session.ID)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, runtime.ErrContainerNotFound) || session.ContainerID == "" {
		return runtime.ContainerInfo{}, err
	}
	return s.runtime.Inspect(ctx, session.ContainerID)
}

func (s *SessionService) tryLock(id string) (func(), bool) {
	s.lockMu.Lock()
	lock, ok := s.locks[id]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[id] = lock
	}
	s.lockMu.Unlock()
	if !lock.TryLock() {
		return nil, false
	}
	return lock.Unlock, true
}

func timeoutFor(timeoutMS int64, fallback time.Duration) time.Duration {
	if timeoutMS <= 0 {
		return fallback
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func sessionDefaultCwd(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return runtime.DefaultMountPath
	}
	return cwd
}

func validateSessionID(sessionID string) error {
	if !validSessionID.MatchString(strings.TrimSpace(sessionID)) {
		return fmt.Errorf("%w: session_id must match %s", ErrValidation, validSessionID.String())
	}
	return nil
}

func generateID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func executeResponse(sessionID string, result runtime.ExecResult) *api.ExecuteSessionResponse {
	return &api.ExecuteSessionResponse{
		SessionID:  sessionID,
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		TimedOut:   result.TimedOut,
		StartedAt:  result.StartedAt,
		FinishedAt: result.FinishedAt,
	}
}

func sessionToResponse(session *model.Session) *api.SessionResponse {
	return &api.SessionResponse{
		SessionID:       session.ID,
		EnvironmentName: session.EnvironmentName,
		ContainerID:     session.ContainerID,
		Image:           session.Image,
		DefaultCwd:      session.DefaultCwd,
		WorkspacePath:   session.WorkspacePath,
		Labels:          cloneMap(session.Labels),
		Resources:       session.Resources,
		Mounts:          append([]model.Mount(nil), session.Mounts...),
		CreatedAt:       session.CreatedAt,
	}
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
