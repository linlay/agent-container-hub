package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/config"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/sandbox"
	"agent-container-hub/internal/store"
)

func TestSessionEnvironmentAndUIEndpoints(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, "")

	envResp := doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")
	if envResp.Name != "shell" {
		t.Fatalf("envResp.Name = %q, want shell", envResp.Name)
	}

	createResp := doJSON[api.SessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "http-session",
		EnvironmentName: "shell",
	}, http.StatusOK, "")
	if createResp.SessionID != "http-session" {
		t.Fatalf("createResp.SessionID = %q, want http-session", createResp.SessionID)
	}

	executeResp := doJSON[api.ExecuteSessionResponse](t, handler, http.MethodPost, "/api/sessions/http-session/execute", api.ExecuteSessionRequest{
		Command: "pwd",
	}, http.StatusOK, "")
	if executeResp.Stdout != "ok" {
		t.Fatalf("executeResp.Stdout = %q, want ok", executeResp.Stdout)
	}

	sessions := doJSON[[]api.SessionResponse](t, handler, http.MethodGet, "/api/sessions", nil, http.StatusOK, "")
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}

	stopResp := doJSON[api.StopSessionResponse](t, handler, http.MethodPost, "/api/sessions/http-session/stop", nil, http.StatusOK, "")
	if stopResp.Status != "stopped" {
		t.Fatalf("stopResp.Status = %q, want stopped", stopResp.Status)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("Agent Container Hub Console")) {
		t.Fatalf("GET / body = %q, want console html", recorder.Body.String())
	}
}

func TestAuthProtectsAppAndAPI(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, "secret")

	apiReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	apiRecorder := httptest.NewRecorder()
	handler.ServeHTTP(apiRecorder, apiReq)
	if apiRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/sessions status = %d, want 401", apiRecorder.Code)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pageRecorder, pageReq)
	if pageRecorder.Code != http.StatusFound {
		t.Fatalf("GET / status = %d, want 302", pageRecorder.Code)
	}
	if location := pageRecorder.Header().Get("Location"); location != "/login" {
		t.Fatalf("Location = %q, want /login", location)
	}

	loginResp := doJSON[map[string]string](t, handler, http.MethodPost, "/api/auth/login", api.LoginRequest{Token: "secret"}, http.StatusOK, "")
	if loginResp["status"] != "ok" {
		t.Fatalf("loginResp = %+v, want ok", loginResp)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"token":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginReq)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want 200", loginRecorder.Code)
	}
	cookies := loginRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected login response to set cookie")
	}
	if cookies[0].Name != authCookieName {
		t.Fatalf("cookie name = %q, want %q", cookies[0].Name, authCookieName)
	}
}

func TestListEnvironmentsReturnsFilenameForInvalidYAML(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	if err := os.WriteFile(filepath.Join(cfg.ConfigRoot, "environments", "broken.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/environments", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/environments status = %d, want 500", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("broken.yaml")) {
		t.Fatalf("GET /api/environments body = %q, want filename", recorder.Body.String())
	}
}

func TestTargetedEnvironmentReadIgnoresUnrelatedInvalidYAML(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")
	if err := os.WriteFile(filepath.Join(cfg.ConfigRoot, "environments", "broken.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	envResp := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/shell", nil, http.StatusOK, "")
	if envResp.Name != "shell" {
		t.Fatalf("envResp.Name = %q, want shell", envResp.Name)
	}
	createResp := doJSON[api.SessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "targeted",
		EnvironmentName: "shell",
	}, http.StatusOK, "")
	if createResp.EnvironmentName != "shell" {
		t.Fatalf("createResp.EnvironmentName = %q, want shell", createResp.EnvironmentName)
	}
}

func doJSON[T any](t *testing.T, handler http.Handler, method, path string, payload any, wantStatus int, bearer string) T {
	t.Helper()

	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("json.Encode() error = %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	var result T
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("json.Decode() error = %v", err)
	}
	return result
}

func newTestHandler(t *testing.T, authToken string) http.Handler {
	t.Helper()

	handler, _ := newTestHandlerWithConfig(t, authToken)
	return handler
}

func newTestHandlerWithConfig(t *testing.T, authToken string) (http.Handler, config.Config) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		BindAddr:              "127.0.0.1:0",
		AuthToken:             authToken,
		StateDBPath:           filepath.Join(tempDir, "agent-container-hub.db"),
		ConfigRoot:            filepath.Join(tempDir, "configs"),
		WorkspaceRoot:         filepath.Join(tempDir, "workspaces"),
		BuildRoot:             filepath.Join(tempDir, "builds"),
		AllowedMountRoots:     []string{filepath.Join(tempDir, "workspaces"), filepath.Join(tempDir, "builds")},
		DefaultCommandTimeout: time.Second,
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaces) error = %v", err)
	}
	if err := os.MkdirAll(cfg.BuildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(builds) error = %v", err)
	}
	st, err := store.Open(cfg.StateDBPath)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	envs, err := store.OpenFileEnvironmentStore(filepath.Join(cfg.ConfigRoot, "environments"))
	if err != nil {
		t.Fatalf("store.OpenFileEnvironmentStore() error = %v", err)
	}

	fake := &httpFakeRuntime{
		containers: make(map[string]runtime.ContainerInfo),
		execResult: runtime.ExecResult{
			ExitCode:   0,
			Stdout:     "ok",
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		},
		buildResult: runtime.BuildResult{
			Output:     "built",
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		},
	}
	sessionService := sandbox.NewSessionService(cfg, st, envs, fake, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	environmentService := sandbox.NewEnvironmentService(envs, st, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	buildService := sandbox.NewBuildService(cfg, st, envs, fake, fake, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	return New(sessionService, environmentService, buildService, authToken), cfg
}

type httpFakeRuntime struct {
	containers  map[string]runtime.ContainerInfo
	execResult  runtime.ExecResult
	buildResult runtime.BuildResult
}

func (f *httpFakeRuntime) Name() string { return "fake" }

func (f *httpFakeRuntime) Create(_ context.Context, opts runtime.CreateOptions) (runtime.ContainerInfo, error) {
	id := "ctr-" + opts.Name
	info := runtime.ContainerInfo{
		ID:        id,
		Name:      opts.Name,
		Image:     opts.Image,
		State:     runtime.ContainerStopped,
		Labels:    cloneMap(opts.Labels),
		CreatedAt: time.Now().UTC(),
	}
	f.containers[id] = info
	return info, nil
}

func (f *httpFakeRuntime) Start(_ context.Context, containerID string) (runtime.ContainerInfo, error) {
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ContainerInfo{}, runtime.ErrContainerNotFound
	}
	info.State = runtime.ContainerRunning
	f.containers[info.ID] = info
	return info, nil
}

func (f *httpFakeRuntime) Exec(_ context.Context, _ string, _ runtime.ExecOptions) (runtime.ExecResult, error) {
	return f.execResult, nil
}

func (f *httpFakeRuntime) Build(_ context.Context, _ runtime.BuildOptions) (runtime.BuildResult, error) {
	return f.buildResult, nil
}

func (f *httpFakeRuntime) Stop(_ context.Context, containerID string, _ time.Duration) error {
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ErrContainerNotFound
	}
	info.State = runtime.ContainerStopped
	f.containers[info.ID] = info
	return nil
}

func (f *httpFakeRuntime) Remove(_ context.Context, containerID string) error {
	info, ok := f.lookup(containerID)
	if ok {
		delete(f.containers, info.ID)
	}
	return nil
}

func (f *httpFakeRuntime) Inspect(_ context.Context, containerID string) (runtime.ContainerInfo, error) {
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ContainerInfo{}, runtime.ErrContainerNotFound
	}
	return info, nil
}

func (f *httpFakeRuntime) ListByLabel(_ context.Context, key, value string) ([]runtime.ContainerInfo, error) {
	var infos []runtime.ContainerInfo
	for _, info := range f.containers {
		if info.Labels[key] == value {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

func (f *httpFakeRuntime) lookup(idOrName string) (runtime.ContainerInfo, bool) {
	if info, ok := f.containers[idOrName]; ok {
		return info, true
	}
	for _, info := range f.containers {
		if info.Name == idOrName {
			return info, true
		}
	}
	return runtime.ContainerInfo{}, false
}

func cloneMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
