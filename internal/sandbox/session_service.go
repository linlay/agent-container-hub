package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/config"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/store"
)

var (
	ErrValidation  = errors.New("validation failed")
	ErrBusy        = errors.New("session busy")
	ErrConflict    = errors.New("session configuration conflict")
	validSessionID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)
)

type SessionService struct {
	cfg     config.Config
	store   store.RuntimeStore
	envs    store.EnvironmentStore
	runtime runtime.Provider
	logger  *slog.Logger
	lockMu  sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewSessionService(cfg config.Config, st store.RuntimeStore, envs store.EnvironmentStore, provider runtime.Provider, logger *slog.Logger) *SessionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionService{
		cfg:     cfg,
		store:   st,
		envs:    envs,
		runtime: provider,
		logger:  logger,
		locks:   make(map[string]*sync.Mutex),
	}
}

func (s *SessionService) Create(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error) {
	startedAt := time.Now().UTC()
	environmentName := strings.TrimSpace(req.EnvironmentName)
	if err := validateEnvironmentName(environmentName); err != nil {
		return nil, err
	}

	environment, err := s.envs.GetEnvironment(ctx, environmentName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: environment not found", store.ErrNotFound)
		}
		return nil, err
	}
	if !environment.Enabled {
		return nil, fmt.Errorf("%w: environment is disabled", ErrValidation)
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

	mounts, err := s.buildSessionMounts(environment.Mounts, req.Mounts, workspacePath)
	if err != nil {
		_ = os.RemoveAll(workspacePath)
		return nil, err
	}

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
		Mounts:          append([]model.Mount(nil), mounts...),
		Resources:       environment.Resources,
		Labels:          cloneMap(req.Labels),
		Status:          model.SessionStatusActive,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.store.SaveSession(ctx, session); err != nil {
		return nil, err
	}

	response := sessionToCreateResponse(session, durationMilliseconds(startedAt, time.Now().UTC()))
	response.Status = string(model.SessionStatusActive)
	s.logger.Info("session created", "session_id", session.ID, "environment", session.EnvironmentName, "image", session.Image)
	if started.State != runtime.ContainerRunning {
		s.logger.Warn("session started with non-running state", "session_id", session.ID, "state", started.State)
	}
	return response, nil
}

func (s *SessionService) CreateTemplate(context.Context) (*api.SessionCreateTemplateResponse, error) {
	root := strings.TrimSpace(s.cfg.SessionMountTemplateRoot)
	response := &api.SessionCreateTemplateResponse{
		MountTemplateRoot: root,
		DefaultMounts:     []model.Mount{},
		ChatIDs:           []string{},
	}
	if root == "" {
		return response, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read session mount template root: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || name == "chats" {
			continue
		}
		response.DefaultMounts = append(response.DefaultMounts, model.Mount{
			Source:      filepath.Join(root, name),
			Destination: "/" + name,
		})
	}
	sort.Slice(response.DefaultMounts, func(i, j int) bool {
		return response.DefaultMounts[i].Destination < response.DefaultMounts[j].Destination
	})

	chatEntries, err := os.ReadDir(filepath.Join(root, "chats"))
	if err != nil {
		if os.IsNotExist(err) {
			return response, nil
		}
		return nil, fmt.Errorf("read session mount template chats: %w", err)
	}
	for _, entry := range chatEntries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		response.ChatIDs = append(response.ChatIDs, name)
	}
	sort.Strings(response.ChatIDs)
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
	if session.Status != model.SessionStatusActive {
		return nil, fmt.Errorf("%w: session is not active", ErrValidation)
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

	result, err := s.execOnSession(ctx, session, execOpts)
	if err != nil {
		return nil, err
	}

	response := executeResponse(sessionID, result)
	if s.cfg.EnableExecLogPersist {
		execution := executionFromResult(sessionID, req, execCwd, result, s.cfg.ExecLogMaxOutputBytes)
		if err := s.store.SaveSessionExecution(ctx, execution); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (s *SessionService) execOnSession(ctx context.Context, session *model.Session, execOpts runtime.ExecOptions) (runtime.ExecResult, error) {
	target := session.ContainerID
	if target == "" {
		target = session.ID
	}

	result, err := s.runtime.Exec(ctx, target, execOpts)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, runtime.ErrContainerNotFound) && !errors.Is(err, runtime.ErrContainerNotRunning) {
		return runtime.ExecResult{}, err
	}

	info, inspectErr := s.inspectSession(ctx, session)
	if inspectErr != nil {
		if errors.Is(inspectErr, runtime.ErrContainerNotFound) {
			if markErr := s.markSessionStopped(ctx, session, time.Now().UTC(), true); markErr != nil {
				return runtime.ExecResult{}, markErr
			}
			return runtime.ExecResult{}, store.ErrNotFound
		}
		return runtime.ExecResult{}, inspectErr
	}
	if info.State != runtime.ContainerRunning {
		if _, err := s.runtime.Start(ctx, info.ID); err != nil {
			return runtime.ExecResult{}, err
		}
	}
	if session.ContainerID != info.ID {
		session.ContainerID = info.ID
		if err := s.store.SaveSession(ctx, session); err != nil {
			return runtime.ExecResult{}, err
		}
	}
	return s.runtime.Exec(ctx, info.ID, execOpts)
}

func (s *SessionService) Stop(ctx context.Context, sessionID string) (*api.StopSessionResponse, error) {
	startedAt := time.Now().UTC()
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
	if err != nil {
		return nil, err
	}
	if session.Status != model.SessionStatusActive {
		return &api.StopSessionResponse{
			SessionID:  sessionID,
			Status:     string(session.Status),
			DurationMS: durationMilliseconds(startedAt, time.Now().UTC()),
		}, nil
	}

	target := sessionID
	if session.ContainerID != "" {
		target = session.ContainerID
	}
	if err := s.runtime.Stop(ctx, target, 5*time.Second); err != nil && !errors.Is(err, runtime.ErrContainerNotFound) {
		return nil, err
	}
	if err := s.runtime.Remove(ctx, target); err != nil && !errors.Is(err, runtime.ErrContainerNotFound) {
		return nil, err
	}
	if err := s.markSessionStopped(ctx, session, time.Now().UTC(), true); err != nil {
		return nil, err
	}

	return &api.StopSessionResponse{
		SessionID:  sessionID,
		Status:     string(model.SessionStatusStopped),
		DurationMS: durationMilliseconds(startedAt, time.Now().UTC()),
	}, nil
}

func (s *SessionService) List(ctx context.Context) ([]*api.SessionResponse, error) {
	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	responses := make([]*api.SessionResponse, 0, len(sessions))
	for _, session := range sessions {
		responses = append(responses, sessionToResponse(session))
	}
	return responses, nil
}

func (s *SessionService) Query(ctx context.Context, query store.SessionQuery) (*api.SessionListResponse, error) {
	switch strings.ToLower(strings.TrimSpace(query.Status)) {
	case "", "active", "history", "all":
	default:
		return nil, fmt.Errorf("%w: status must be one of active, history, all", ErrValidation)
	}
	items, total, err := s.store.QuerySessions(ctx, query)
	if err != nil {
		return nil, err
	}
	page, pageSize := normalizePagination(query.Pagination)
	responses := make([]*api.SessionResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, sessionToResponse(item))
	}
	return &api.SessionListResponse{
		Items:    responses,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *SessionService) Get(ctx context.Context, sessionID string) (*api.SessionResponse, error) {
	session, err := s.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	return sessionToResponse(session), nil
}

func (s *SessionService) ListExecutions(ctx context.Context, sessionID string, pagination store.Pagination) (*api.SessionExecutionListResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session_id is required", ErrValidation)
	}
	if _, err := s.store.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	items, total, err := s.store.ListSessionExecutions(ctx, sessionID, pagination)
	if err != nil {
		return nil, err
	}
	page, pageSize := normalizePagination(pagination)
	responses := make([]*api.SessionExecutionResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, &api.SessionExecutionResponse{
			ID:              item.ID,
			SessionID:       item.SessionID,
			Command:         item.Command,
			Args:            append([]string(nil), item.Args...),
			Cwd:             item.Cwd,
			TimeoutMS:       item.TimeoutMS,
			ExitCode:        item.ExitCode,
			Stdout:          item.Stdout,
			Stderr:          item.Stderr,
			StdoutTruncated: item.StdoutTruncated,
			StderrTruncated: item.StderrTruncated,
			TimedOut:        item.TimedOut,
			DurationMS:      item.DurationMS,
			StartedAt:       item.StartedAt,
			FinishedAt:      item.FinishedAt,
		})
	}
	return &api.SessionExecutionListResponse{
		Items:    responses,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *SessionService) Reconcile(ctx context.Context) error {
	sessions, _, err := s.store.QuerySessions(ctx, store.SessionQuery{
		Status: "active",
		Pagination: store.Pagination{
			Page:     1,
			PageSize: 1000,
		},
	})
	if err != nil {
		return err
	}
	for _, session := range sessions {
		info, err := s.inspectSession(ctx, session)
		if err != nil {
			if errors.Is(err, runtime.ErrContainerNotFound) {
				if markErr := s.markSessionStopped(ctx, session, time.Now().UTC(), true); markErr != nil {
					return markErr
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

func (s *SessionService) markSessionStopped(ctx context.Context, session *model.Session, stoppedAt time.Time, removeWorkspace bool) error {
	session.Status = model.SessionStatusStopped
	session.StoppedAt = stoppedAt.UTC()
	if err := s.store.SaveSession(ctx, session); err != nil {
		return err
	}
	if removeWorkspace && session.WorkspacePath != "" {
		if err := os.RemoveAll(session.WorkspacePath); err != nil {
			return fmt.Errorf("delete workspace: %w", err)
		}
	}
	return nil
}

func (s *SessionService) buildSessionMounts(environmentMounts, requestMounts []model.Mount, workspacePath string) ([]model.Mount, error) {
	normalizedEnvMounts, err := s.normalizeMountList(environmentMounts)
	if err != nil {
		return nil, err
	}
	normalizedRequestMounts, err := s.normalizeMountList(requestMounts)
	if err != nil {
		return nil, err
	}

	workspaceMount := model.Mount{
		Source:      runtime.NormalizeMountSource(workspacePath),
		Destination: runtime.DefaultMountPath,
	}

	destinations := map[string]struct{}{
		workspaceMount.Destination: {},
	}
	for _, mount := range normalizedEnvMounts {
		if err := validateMountDestination(mount.Destination, destinations); err != nil {
			return nil, err
		}
	}
	for _, mount := range normalizedRequestMounts {
		if mount.Destination == runtime.DefaultMountPath {
			return nil, fmt.Errorf("%w: mount destination %s is reserved for the workspace", ErrValidation, runtime.DefaultMountPath)
		}
		if err := validateMountDestination(mount.Destination, destinations); err != nil {
			return nil, err
		}
	}

	mounts := append([]model.Mount(nil), normalizedEnvMounts...)
	mounts = append(mounts, normalizedRequestMounts...)
	mounts = append(mounts, workspaceMount)
	return mounts, nil
}

func (s *SessionService) normalizeMountList(mounts []model.Mount) ([]model.Mount, error) {
	normalized := make([]model.Mount, 0, len(mounts))
	for _, mount := range mounts {
		source := strings.TrimSpace(mount.Source)
		if source == "" {
			return nil, fmt.Errorf("%w: mount source is required", ErrValidation)
		}
		destination := normalizeContainerPath(mount.Destination)
		if destination == "" {
			return nil, fmt.Errorf("%w: mount destination is required", ErrValidation)
		}
		normalized = append(normalized, model.Mount{
			Source:      runtime.NormalizeMountSource(source),
			Destination: destination,
			ReadOnly:    mount.ReadOnly,
		})
	}
	return normalized, nil
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

func validateMountDestination(destination string, seen map[string]struct{}) error {
	if _, exists := seen[destination]; exists {
		return fmt.Errorf("%w: mount destination %s is duplicated", ErrValidation, destination)
	}
	seen[destination] = struct{}{}
	return nil
}

func normalizeContainerPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	clean := path.Clean(value)
	if clean == "." {
		return ""
	}
	return clean
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
		DurationMS: durationMilliseconds(result.StartedAt, result.FinishedAt),
		StartedAt:  result.StartedAt,
		FinishedAt: result.FinishedAt,
	}
}

func executionFromResult(sessionID string, req api.ExecuteSessionRequest, execCwd string, result runtime.ExecResult, maxOutputBytes int) *model.SessionExecution {
	stdout, stdoutTruncated := truncateLogOutput(result.Stdout, maxOutputBytes)
	stderr, stderrTruncated := truncateLogOutput(result.Stderr, maxOutputBytes)
	return &model.SessionExecution{
		SessionID:       sessionID,
		Command:         req.Command,
		Args:            append([]string(nil), req.Args...),
		Cwd:             execCwd,
		TimeoutMS:       req.TimeoutMS,
		ExitCode:        result.ExitCode,
		Stdout:          stdout,
		Stderr:          stderr,
		StdoutTruncated: stdoutTruncated,
		StderrTruncated: stderrTruncated,
		TimedOut:        result.TimedOut,
		DurationMS:      durationMilliseconds(result.StartedAt, result.FinishedAt),
		StartedAt:       result.StartedAt,
		FinishedAt:      result.FinishedAt,
	}
}

func truncateLogOutput(output string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output, false
	}
	truncated := output[:maxBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated, true
}

func sessionToCreateResponse(session *model.Session, durationMS int64) *api.CreateSessionResponse {
	response := sessionToResponse(session)
	return &api.CreateSessionResponse{
		SessionResponse: *response,
		DurationMS:      durationMS,
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
		Status:          string(session.Status),
		StoppedAt:       session.StoppedAt,
	}
}

func durationMilliseconds(startedAt, finishedAt time.Time) int64 {
	durationMS := finishedAt.Sub(startedAt).Milliseconds()
	if durationMS < 0 {
		return 0
	}
	return durationMS
}

func normalizePagination(p store.Pagination) (int, int) {
	page := p.Page
	if page <= 0 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
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
