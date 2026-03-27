package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	createResp := doJSON[api.CreateSessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "http-session",
		EnvironmentName: "shell",
	}, http.StatusOK, "")
	if createResp.SessionID != "http-session" {
		t.Fatalf("createResp.SessionID = %q, want http-session", createResp.SessionID)
	}
	if createResp.DefaultCwd != runtime.DefaultMountPath {
		t.Fatalf("createResp.DefaultCwd = %q, want %q", createResp.DefaultCwd, runtime.DefaultMountPath)
	}
	if len(createResp.Mounts) != 1 || createResp.Mounts[0].Destination != runtime.DefaultMountPath {
		t.Fatalf("createResp.Mounts = %+v, want auto rootfs mount at %s", createResp.Mounts, runtime.DefaultMountPath)
	}
	if createResp.DurationMS < 0 {
		t.Fatalf("createResp.DurationMS = %d, want non-negative", createResp.DurationMS)
	}

	executeResp := doJSON[api.ExecuteSessionResponse](t, handler, http.MethodPost, "/api/sessions/http-session/execute", api.ExecuteSessionRequest{
		Command: "pwd",
	}, http.StatusOK, "")
	if executeResp.Stdout != "ok" {
		t.Fatalf("executeResp.Stdout = %q, want ok", executeResp.Stdout)
	}
	if executeResp.DurationMS != 95 {
		t.Fatalf("executeResp.DurationMS = %d, want 95", executeResp.DurationMS)
	}

	sessions := doJSON[[]api.SessionResponse](t, handler, http.MethodGet, "/api/sessions", nil, http.StatusOK, "")
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}

	executions := doJSON[api.SessionExecutionListResponse](t, handler, http.MethodGet, "/api/sessions/http-session/executions?page=1&page_size=10", nil, http.StatusOK, "")
	if len(executions.Items) != 1 {
		t.Fatalf("executions len = %d, want 1", len(executions.Items))
	}

	stopResp := doJSON[api.StopSessionResponse](t, handler, http.MethodPost, "/api/sessions/http-session/stop", nil, http.StatusOK, "")
	if stopResp.Status != "stopped" {
		t.Fatalf("stopResp.Status = %q, want stopped", stopResp.Status)
	}
	if stopResp.DurationMS < 0 {
		t.Fatalf("stopResp.DurationMS = %d, want non-negative", stopResp.DurationMS)
	}

	activeAfterStop := doJSON[[]api.SessionResponse](t, handler, http.MethodGet, "/api/sessions", nil, http.StatusOK, "")
	if len(activeAfterStop) != 0 {
		t.Fatalf("activeAfterStop len = %d, want 0", len(activeAfterStop))
	}

	history := doJSON[api.SessionListResponse](t, handler, http.MethodGet, "/api/sessions/query?status=history&page=1&page_size=10", nil, http.StatusOK, "")
	if len(history.Items) != 1 || history.Items[0].SessionID != "http-session" {
		t.Fatalf("history items = %+v, want stopped session", history.Items)
	}
	if history.Items[0].StoppedAt.IsZero() {
		t.Fatal("expected stopped history item to include stopped_at")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("Session Console")) {
		t.Fatalf("GET / body = %q, want session console html", recorder.Body.String())
	}

	sessionsPageReq := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	sessionsPageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sessionsPageRecorder, sessionsPageReq)
	if sessionsPageRecorder.Code != http.StatusOK {
		t.Fatalf("GET /sessions status = %d, want 200", sessionsPageRecorder.Code)
	}
	if !bytes.Contains(sessionsPageRecorder.Body.Bytes(), []byte("/ui/sessions.js")) {
		t.Fatalf("GET /sessions body = %q, want sessions asset reference", sessionsPageRecorder.Body.String())
	}

	environmentsPageReq := httptest.NewRequest(http.MethodGet, "/environments", nil)
	environmentsPageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(environmentsPageRecorder, environmentsPageReq)
	if environmentsPageRecorder.Code != http.StatusOK {
		t.Fatalf("GET /environments status = %d, want 200", environmentsPageRecorder.Code)
	}
	if !bytes.Contains(environmentsPageRecorder.Body.Bytes(), []byte("/ui/environments.js")) {
		t.Fatalf("GET /environments body = %q, want environments asset reference", environmentsPageRecorder.Body.String())
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/ui/styles.css", nil)
	assetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(assetRecorder, assetReq)
	if assetRecorder.Code != http.StatusOK {
		t.Fatalf("GET /ui/styles.css status = %d, want 200", assetRecorder.Code)
	}
	if contentType := assetRecorder.Header().Get("Content-Type"); contentType == "" || !bytes.Contains([]byte(contentType), []byte("text/css")) {
		t.Fatalf("GET /ui/styles.css content-type = %q, want text/css", contentType)
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

	for _, path := range []string{"/sessions", "/environments"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusFound {
			t.Fatalf("GET %s status = %d, want 302", path, recorder.Code)
		}
		if location := recorder.Header().Get("Location"); location != "/login" {
			t.Fatalf("GET %s location = %q, want /login", path, location)
		}
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

	authenticatedAssetReq := httptest.NewRequest(http.MethodGet, "/ui/common.js", nil)
	authenticatedAssetReq.Header.Set("Authorization", "Bearer secret")
	authenticatedAssetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authenticatedAssetRecorder, authenticatedAssetReq)
	if authenticatedAssetRecorder.Code != http.StatusOK {
		t.Fatalf("GET /ui/common.js status = %d, want 200", authenticatedAssetRecorder.Code)
	}
}

func TestAPIAccessLoggingCapturesAPIRequestsOnly(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler, _ := newTestHandlerWithServerOptions(t, "", Options{
		Logger:           slog.New(slog.NewJSONHandler(&logs, nil)),
		AccessLogEnabled: true,
	})

	_ = doJSON[[]api.SessionResponse](t, handler, http.MethodGet, "/api/sessions", nil, http.StatusOK, "")
	if !bytes.Contains(logs.Bytes(), []byte(`"msg":"api request"`)) {
		t.Fatalf("logs = %s, want api request entry", logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"path":"/api/sessions"`)) {
		t.Fatalf("logs = %s, want /api/sessions path", logs.String())
	}

	logLen := logs.Len()
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /sessions status = %d, want 200", recorder.Code)
	}
	if logs.Len() != logLen {
		t.Fatalf("non-api request unexpectedly logged: %s", logs.String()[logLen:])
	}
}

func TestAPIErrorLoggingIncludesErrorMessages(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler, _ := newTestHandlerWithServerOptions(t, "secret", Options{
		Logger:          slog.New(slog.NewJSONHandler(&logs, nil)),
		ErrorLogEnabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/sessions status = %d, want 401", recorder.Code)
	}

	badJSONReq := httptest.NewRequest(http.MethodPost, "/api/sessions/create", bytes.NewBufferString(`{`))
	badJSONReq.Header.Set("Authorization", "Bearer secret")
	badJSONReq.Header.Set("Content-Type", "application/json")
	badJSONRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badJSONRecorder, badJSONReq)
	if badJSONRecorder.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/sessions/create status = %d, want 400", badJSONRecorder.Code)
	}

	notFoundReq := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	notFoundReq.Header.Set("Authorization", "Bearer secret")
	notFoundRecorder := httptest.NewRecorder()
	handler.ServeHTTP(notFoundRecorder, notFoundReq)
	if notFoundRecorder.Code != http.StatusNotFound {
		t.Fatalf("GET /api/does-not-exist status = %d, want 404", notFoundRecorder.Code)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"api request failed"`,
		`"error":"unauthorized"`,
		`"error":"invalid JSON body"`,
		`"error":"not found"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs = %s, want %s", logText, want)
		}
	}
}

func TestAPIPanicRecoveryLogsAndReturnsJSON(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	server := &Server{
		logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
		errorLogs: true,
	}
	handler := server.wrapAPI(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/panic", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/panic status = %d, want 500", recorder.Code)
	}

	var payload map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("json.Decode() error = %v", err)
	}
	if payload["error"] != "internal server error" {
		t.Fatalf("panic payload = %+v, want internal server error", payload)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"api panic recovered"`,
		`"panic":"boom"`,
		`"msg":"api request failed"`,
		`"error":"internal server error"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs = %s, want %s", logText, want)
		}
	}
}

func TestListEnvironmentsReturnsFilenameForInvalidYAML(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	if err := os.MkdirAll(filepath.Join(cfg.ConfigRoot, "environments", "broken"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ConfigRoot, "environments", "broken", "environment.yml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/environments", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/environments status = %d, want 500", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"internal server error"`)) {
		t.Fatalf("GET /api/environments body = %q, want internal server error", recorder.Body.String())
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
	if err := os.MkdirAll(filepath.Join(cfg.ConfigRoot, "environments", "broken"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ConfigRoot, "environments", "broken", "environment.yml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	envResp := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/shell", nil, http.StatusOK, "")
	if envResp.Name != "shell" {
		t.Fatalf("envResp.Name = %q, want shell", envResp.Name)
	}
	if envResp.YAML == "" {
		t.Fatal("expected GET /api/environments/{name} to include yaml")
	}
	createResp := doJSON[api.CreateSessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "targeted",
		EnvironmentName: "shell",
	}, http.StatusOK, "")
	if createResp.EnvironmentName != "shell" {
		t.Fatalf("createResp.EnvironmentName = %q, want shell", createResp.EnvironmentName)
	}
}

func TestEnvironmentAPIResponsePreservesBuildMetadata(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	saved := doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "daily-office",
		ImageRepository: "daily-office",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
			BuildArgs: map[string]string{
				"NPM_REGISTRY": "https://registry.npmjs.org",
			},
			Notes:        "managed by repo context",
			SmokeCommand: "/bin/sh",
			SmokeArgs:    []string{"-lc", "command -v bash"},
		},
	}, http.StatusOK, "")
	if saved.Build.BuildArgs["NPM_REGISTRY"] != "https://registry.npmjs.org" {
		t.Fatalf("saved.Build.BuildArgs = %+v", saved.Build.BuildArgs)
	}
	if saved.Build.Notes != "managed by repo context" {
		t.Fatalf("saved.Build.Notes = %q", saved.Build.Notes)
	}
	if saved.Build.SmokeCommand != "/bin/sh" {
		t.Fatalf("saved.Build.SmokeCommand = %q", saved.Build.SmokeCommand)
	}

	got := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/daily-office", nil, http.StatusOK, "")
	if got.Build.BuildArgs["NPM_REGISTRY"] != "https://registry.npmjs.org" {
		t.Fatalf("GET Build.BuildArgs = %+v", got.Build.BuildArgs)
	}
	if got.Build.Notes != "managed by repo context" {
		t.Fatalf("GET Build.Notes = %q", got.Build.Notes)
	}
	if got.Build.SmokeCommand != "/bin/sh" || len(got.Build.SmokeArgs) != 2 {
		t.Fatalf("GET Build smoke config = %+v", got.Build)
	}
}

func TestEnvironmentAPIRoundTripsDefaultExecute(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	saved := doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		DefaultExecute: model.ExecutePreset{
			Command:   "pwd",
			Args:      []string{"-L"},
			Cwd:       "/root",
			TimeoutMS: 1234,
		},
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")
	if saved.DefaultExecute.Command != "pwd" || len(saved.DefaultExecute.Args) != 1 || saved.DefaultExecute.TimeoutMS != 1234 {
		t.Fatalf("saved.DefaultExecute = %+v", saved.DefaultExecute)
	}

	listed := doJSON[[]api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments", nil, http.StatusOK, "")
	if len(listed) != 1 || listed[0].DefaultExecute.Command != "pwd" {
		t.Fatalf("listed default execute = %+v", listed)
	}
}

func TestEnvironmentAPIRoundTripsAgentPrompt(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	wantPrompt := "You are running inside the shell environment.\nCheck bundled tools before installing anything.\n"
	saved := doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		AgentPrompt:     wantPrompt,
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")
	if saved.AgentPrompt != wantPrompt {
		t.Fatalf("saved.AgentPrompt = %q, want %q", saved.AgentPrompt, wantPrompt)
	}

	got := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/shell", nil, http.StatusOK, "")
	if got.AgentPrompt != wantPrompt {
		t.Fatalf("GET AgentPrompt = %q, want %q", got.AgentPrompt, wantPrompt)
	}

	promptResp := doJSON[api.EnvironmentAgentPromptResponse](t, handler, http.MethodGet, "/api/environments/shell/agent-prompt", nil, http.StatusOK, "")
	if !promptResp.HasPrompt {
		t.Fatal("promptResp.HasPrompt = false, want true")
	}
	if promptResp.Prompt != wantPrompt {
		t.Fatalf("promptResp.Prompt = %q, want %q", promptResp.Prompt, wantPrompt)
	}
	if promptResp.EnvironmentName != "shell" {
		t.Fatalf("promptResp.EnvironmentName = %q, want shell", promptResp.EnvironmentName)
	}
	if promptResp.UpdatedAt.IsZero() {
		t.Fatal("promptResp.UpdatedAt = zero, want non-zero")
	}
}

func TestEnvironmentAgentPromptEndpointReturnsEmptyPromptWhenUnset(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	promptResp := doJSON[api.EnvironmentAgentPromptResponse](t, handler, http.MethodGet, "/api/environments/shell/agent-prompt", nil, http.StatusOK, "")
	if promptResp.HasPrompt {
		t.Fatal("promptResp.HasPrompt = true, want false")
	}
	if promptResp.Prompt != "" {
		t.Fatalf("promptResp.Prompt = %q, want empty", promptResp.Prompt)
	}
}

func TestEnvironmentAgentPromptEndpointReturnsNotFoundForMissingEnvironment(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	req := httptest.NewRequest(http.MethodGet, "/api/environments/missing/agent-prompt", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("GET /api/environments/{name}/agent-prompt status = %d, want 404", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("record not found")) {
		t.Fatalf("GET /api/environments/{name}/agent-prompt body = %q, want not found", recorder.Body.String())
	}
}

func TestEnvironmentFileAPIListsGetsAndSavesFiles(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	files := doJSON[[]api.EnvironmentFileResponse](t, handler, http.MethodGet, "/api/environments/shell/files", nil, http.StatusOK, "")
	if len(files) != 2 {
		t.Fatalf("files len = %d, want 2", len(files))
	}

	dockerfile := doJSON[api.EnvironmentFileResponse](t, handler, http.MethodGet, "/api/environments/shell/files/Dockerfile", nil, http.StatusOK, "")
	if dockerfile.Type != "dockerfile" || !bytes.Contains([]byte(dockerfile.Content), []byte("FROM busybox")) {
		t.Fatalf("Dockerfile response = %+v", dockerfile)
	}

	saved := doJSON[api.EnvironmentFileResponse](t, handler, http.MethodPut, "/api/environments/shell/files/Makefile", api.PutEnvironmentFileRequest{
		Content: "build:\n\t@echo shell\n",
	}, http.StatusOK, "")
	if saved.Path != "Makefile" || saved.Type != "script" {
		t.Fatalf("saved file = %+v", saved)
	}

	updatedFiles := doJSON[[]api.EnvironmentFileResponse](t, handler, http.MethodGet, "/api/environments/shell/files", nil, http.StatusOK, "")
	if len(updatedFiles) != 3 {
		t.Fatalf("updated files len = %d, want 3", len(updatedFiles))
	}
}

func TestEnvironmentFileAPIRejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandlerWithConfig(t, "")

	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	req := httptest.NewRequest(http.MethodPut, "/api/environments/shell/files/tmp/file.txt", bytes.NewBufferString(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("PUT invalid file path status = %d, want 404", recorder.Code)
	}
}

func TestSessionCreateTemplateEndpoint(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	for _, dir := range []string{"home", "pan", "skills", "workspace", filepath.Join("chats", "chat-1"), filepath.Join("chats", "chat-2")} {
		if err := os.MkdirAll(filepath.Join(cfg.SessionMountTemplateRoot, dir), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	template := doJSON[api.SessionCreateTemplateResponse](t, handler, http.MethodGet, "/api/session-create/template", nil, http.StatusOK, "")
	if template.MountTemplateRoot != cfg.SessionMountTemplateRoot {
		t.Fatalf("MountTemplateRoot = %q, want %q", template.MountTemplateRoot, cfg.SessionMountTemplateRoot)
	}
	if len(template.DefaultMounts) != 4 {
		t.Fatalf("default mounts len = %d, want 4", len(template.DefaultMounts))
	}
}

func TestExecuteEndpointReturnsConflictWhenContainerStopped(t *testing.T) {
	t.Parallel()

	handler, _, fake := newTestHandlerWithRuntime(t, "")
	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")
	created := doJSON[api.CreateSessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "stopped-http-session",
		EnvironmentName: "shell",
	}, http.StatusOK, "")

	info := fake.containers[created.ContainerID]
	info.State = runtime.ContainerStopped
	fake.containers[created.ContainerID] = info

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/stopped-http-session/execute", bytes.NewBufferString(`{"command":"pwd"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("POST /api/sessions/{id}/execute status = %d, want 409", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("recreate the session")) {
		t.Fatalf("POST /api/sessions/{id}/execute body = %q, want recreate message", recorder.Body.String())
	}
}

func TestCreateSessionEndpointAcceptsMounts(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	if err := os.MkdirAll(filepath.Join(cfg.SessionMountTemplateRoot, "home"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home) error = %v", err)
	}

	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	created := doJSON[api.CreateSessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "with-mounts",
		EnvironmentName: "shell",
		Mounts: []model.Mount{{
			Source:      filepath.Join(cfg.SessionMountTemplateRoot, "home"),
			Destination: "/home",
		}},
	}, http.StatusOK, "")
	if len(created.Mounts) != 2 {
		t.Fatalf("created mounts len = %d, want 2", len(created.Mounts))
	}
	if created.Mounts[0].Destination != "/home" {
		t.Fatalf("created mount = %+v, want /home first", created.Mounts[0])
	}
	if created.Mounts[1].Destination != runtime.DefaultMountPath {
		t.Fatalf("created mount = %+v, want rootfs mount at %s", created.Mounts[1], runtime.DefaultMountPath)
	}
}

func TestCreateSessionEndpointAcceptsCwdAndCallerWorkspaceMount(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	if err := os.MkdirAll(filepath.Join(cfg.SessionMountTemplateRoot, "home"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home) error = %v", err)
	}

	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	created := doJSON[api.CreateSessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "with-cwd-and-workspace",
		EnvironmentName: "shell",
		Cwd:             "/workspace/chat-1",
		Mounts: []model.Mount{{
			Source:      filepath.Join(cfg.SessionMountTemplateRoot, "home"),
			Destination: runtime.DefaultMountPath,
		}},
	}, http.StatusOK, "")
	if created.DefaultCwd != "/workspace/chat-1" {
		t.Fatalf("created.DefaultCwd = %q, want /workspace/chat-1", created.DefaultCwd)
	}
	if created.RootfsPath != "" {
		t.Fatalf("created.RootfsPath = %q, want empty", created.RootfsPath)
	}
	if len(created.Mounts) != 1 || created.Mounts[0].Destination != runtime.DefaultMountPath {
		t.Fatalf("created mounts = %+v, want caller-provided workspace mount only", created.Mounts)
	}
}

func TestCreateSessionEndpointAcceptsRootMount(t *testing.T) {
	t.Parallel()

	handler, cfg := newTestHandlerWithConfig(t, "")
	if err := os.MkdirAll(filepath.Join(cfg.SessionMountTemplateRoot, "root"), 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}

	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	created := doJSON[api.CreateSessionResponse](t, handler, http.MethodPost, "/api/sessions/create", api.CreateSessionRequest{
		SessionID:       "with-root-mount",
		EnvironmentName: "shell",
		Mounts: []model.Mount{{
			Source:      filepath.Join(cfg.SessionMountTemplateRoot, "root"),
			Destination: "/root",
		}},
	}, http.StatusOK, "")
	if len(created.Mounts) != 2 {
		t.Fatalf("created mounts len = %d, want 2", len(created.Mounts))
	}
	if created.Mounts[0].Destination != "/root" {
		t.Fatalf("created mount = %+v, want /root first", created.Mounts[0])
	}
	if created.Mounts[1].Destination != runtime.DefaultMountPath {
		t.Fatalf("created mount = %+v, want rootfs mount at %s", created.Mounts[1], runtime.DefaultMountPath)
	}
}

func TestBuildJobEndpointsExposeActiveBuildAndSSE(t *testing.T) {
	t.Parallel()

	handler, _, fake := newTestHandlerWithRuntime(t, "")
	server := httptest.NewServer(handler)
	defer server.Close()
	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	fake.buildStarted = make(chan struct{}, 1)
	fake.buildContinue = make(chan struct{})
	fake.buildResult.Output = "step 1\nstep 2\n"

	started := doJSON[api.BuildJobResponse](t, handler, http.MethodPost, "/api/environments/shell/build-jobs", map[string]string{}, http.StatusOK, "")
	if started.Status != string(model.BuildJobStatusBuilding) {
		t.Fatalf("started.Status = %q, want building", started.Status)
	}

	select {
	case <-fake.buildStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for build to start")
	}

	activeEnvironment := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/shell", nil, http.StatusOK, "")
	if activeEnvironment.LastBuild == nil || activeEnvironment.LastBuild.Status != string(model.BuildJobStatusBuilding) {
		t.Fatalf("activeEnvironment.LastBuild = %+v, want building", activeEnvironment.LastBuild)
	}

	streamReady := make(chan struct{}, 1)
	eventsDone := make(chan string, 1)
	go func() {
		resp, err := http.Get(server.URL + "/api/build-jobs/" + started.ID + "/events")
		if err != nil {
			eventsDone <- err.Error()
			return
		}
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		var body bytes.Buffer
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				body.WriteString(line)
				if strings.Contains(body.String(), "event: snapshot") {
					select {
					case streamReady <- struct{}{}:
					default:
					}
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					eventsDone <- readErr.Error()
					return
				}
				break
			}
		}
		eventsDone <- body.String()
	}()

	select {
	case <-streamReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for snapshot event")
	}

	close(fake.buildContinue)

	select {
	case body := <-eventsDone:
		for _, want := range []string{
			"event: snapshot",
			"event: log",
			"event: complete",
			`"status":"succeeded"`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("event stream = %q, want %s", body, want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event stream to complete")
	}

	persisted := doJSON[api.BuildJobResponse](t, handler, http.MethodGet, "/api/build-jobs/"+started.ID, nil, http.StatusOK, "")
	if persisted.Status != string(model.BuildJobStatusSucceeded) {
		t.Fatalf("persisted.Status = %q, want succeeded", persisted.Status)
	}
	if persisted.Output != "step 1\nstep 2\n" {
		t.Fatalf("persisted.Output = %q, want build output", persisted.Output)
	}
}

func TestStartBuildJobReturnsConflictWhenEnvironmentAlreadyBuilding(t *testing.T) {
	t.Parallel()

	handler, _, fake := newTestHandlerWithRuntime(t, "")
	_ = doJSON[api.EnvironmentResponse](t, handler, http.MethodPost, "/api/environments", api.UpsertEnvironmentRequest{
		Name:            "shell",
		ImageRepository: "busybox",
		ImageTag:        "latest",
		Enabled:         true,
		Build: model.BuildSpec{
			Dockerfile: "FROM busybox:latest\n",
		},
	}, http.StatusOK, "")

	fake.buildStarted = make(chan struct{}, 1)
	fake.buildContinue = make(chan struct{})
	_ = doJSON[api.BuildJobResponse](t, handler, http.MethodPost, "/api/environments/shell/build-jobs", map[string]string{}, http.StatusOK, "")
	select {
	case <-fake.buildStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for build to start")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/environments/shell/build-jobs", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	close(fake.buildContinue)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("POST /api/environments/{name}/build-jobs status = %d, want 409", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "build already in progress") {
		t.Fatalf("conflict body = %q, want conflict message", recorder.Body.String())
	}
}

func TestBuiltinDailyOfficeEnvironmentIsListed(t *testing.T) {
	t.Parallel()

	repoRoot, err := repoRootFromPackageDir()
	if err != nil {
		t.Fatalf("repoRootFromPackageDir() error = %v", err)
	}
	handler := newHandlerForConfigRoot(t, "", filepath.Join(repoRoot, "configs"))

	envs := doJSON[[]api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments", nil, http.StatusOK, "")
	found := false
	for _, env := range envs {
		if env.Name == "daily-office" {
			found = true
			if env.ImageRef != "daily-office:latest" {
				t.Fatalf("daily-office image_ref = %q, want daily-office:latest", env.ImageRef)
			}
		}
	}
	if !found {
		t.Fatalf("daily-office not found in %+v", envs)
	}

	dailyOffice := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/daily-office", nil, http.StatusOK, "")
	if dailyOffice.DefaultExecute.Command != "/bin/bash" || len(dailyOffice.DefaultExecute.Args) != 2 || dailyOffice.DefaultExecute.TimeoutMS != 30000 {
		t.Fatalf("daily-office default execute = %+v, want health-check preset", dailyOffice.DefaultExecute)
	}
	if len(dailyOffice.Mounts) != 0 {
		t.Fatalf("daily-office mounts len = %d, want 0", len(dailyOffice.Mounts))
	}
	if dailyOffice.DefaultEnv["NODE_PATH"] != "/opt/daily-office/node_modules" {
		t.Fatalf("daily-office NODE_PATH = %q", dailyOffice.DefaultEnv["NODE_PATH"])
	}
	if !bytes.Contains([]byte(dailyOffice.DefaultEnv["PATH"]), []byte("/skills/scripts")) {
		t.Fatalf("daily-office PATH = %q", dailyOffice.DefaultEnv["PATH"])
	}
	if bytes.Contains([]byte(dailyOffice.Build.Dockerfile), []byte("COPY ")) {
		t.Fatalf("daily-office Dockerfile unexpectedly contains COPY")
	}
	if bytes.Contains([]byte(dailyOffice.Build.Dockerfile), []byte("ENTRYPOINT")) {
		t.Fatalf("daily-office Dockerfile unexpectedly contains ENTRYPOINT")
	}
	if len(dailyOffice.Build.SmokeArgs) == 0 {
		t.Fatalf("daily-office smoke args = %+v, want cli/python/node checks", dailyOffice.Build.SmokeArgs)
	}
	smokeScript := []byte(dailyOffice.Build.SmokeArgs[len(dailyOffice.Build.SmokeArgs)-1])
	if !bytes.Contains(smokeScript, []byte("command -v himalaya")) || !bytes.Contains(smokeScript, []byte("command -v dbx")) || !bytes.Contains(smokeScript, []byte("command -v httpx")) || !bytes.Contains(smokeScript, []byte("python -c")) || !bytes.Contains(smokeScript, []byte("node -e")) {
		t.Fatalf("daily-office smoke args = %+v, want cli/python/node checks", dailyOffice.Build.SmokeArgs)
	}
	if !strings.Contains(dailyOffice.AgentPrompt, "daily-office") {
		t.Fatalf("daily-office AgentPrompt = %q, want daily-office guidance", dailyOffice.AgentPrompt)
	}

	promptResp := doJSON[api.EnvironmentAgentPromptResponse](t, handler, http.MethodGet, "/api/environments/daily-office/agent-prompt", nil, http.StatusOK, "")
	if !promptResp.HasPrompt {
		t.Fatal("daily-office prompt HasPrompt = false, want true")
	}
	if !strings.Contains(promptResp.Prompt, "npm list -g") {
		t.Fatalf("daily-office prompt = %q, want npm list -g guidance", promptResp.Prompt)
	}
}

func TestBuiltinDailyOfficeProEnvironmentIsListed(t *testing.T) {
	t.Parallel()

	repoRoot, err := repoRootFromPackageDir()
	if err != nil {
		t.Fatalf("repoRootFromPackageDir() error = %v", err)
	}
	handler := newHandlerForConfigRoot(t, "", filepath.Join(repoRoot, "configs"))

	envs := doJSON[[]api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments", nil, http.StatusOK, "")
	found := false
	for _, env := range envs {
		if env.Name == "daily-office-pro" {
			found = true
			if env.ImageRef != "daily-office-pro:latest" {
				t.Fatalf("daily-office-pro image_ref = %q, want daily-office-pro:latest", env.ImageRef)
			}
		}
	}
	if !found {
		t.Fatalf("daily-office-pro not found in %+v", envs)
	}

	dailyOfficePro := doJSON[api.EnvironmentResponse](t, handler, http.MethodGet, "/api/environments/daily-office-pro", nil, http.StatusOK, "")
	if dailyOfficePro.DefaultExecute.Command != "/bin/bash" || len(dailyOfficePro.DefaultExecute.Args) != 2 || dailyOfficePro.DefaultExecute.TimeoutMS != 30000 {
		t.Fatalf("daily-office-pro default execute = %+v, want health-check preset", dailyOfficePro.DefaultExecute)
	}
	if len(dailyOfficePro.Mounts) != 0 {
		t.Fatalf("daily-office-pro mounts len = %d, want 0", len(dailyOfficePro.Mounts))
	}
	if dailyOfficePro.DefaultEnv["NODE_PATH"] != "/opt/daily-office-pro/node_modules" {
		t.Fatalf("daily-office-pro NODE_PATH = %q", dailyOfficePro.DefaultEnv["NODE_PATH"])
	}
	for _, expected := range []string{
		"/skills/skills/minimax-pdf/scripts",
		"/skills/skills/minimax-xlsx/scripts",
		"/skills/skills/minimax-docx/scripts",
	} {
		if !bytes.Contains([]byte(dailyOfficePro.DefaultEnv["PATH"]), []byte(expected)) {
			t.Fatalf("daily-office-pro PATH = %q, want %s", dailyOfficePro.DefaultEnv["PATH"], expected)
		}
	}
	if bytes.Contains([]byte(dailyOfficePro.Build.Dockerfile), []byte("COPY ")) {
		t.Fatalf("daily-office-pro Dockerfile unexpectedly contains COPY")
	}
	if bytes.Contains([]byte(dailyOfficePro.Build.Dockerfile), []byte("ENTRYPOINT")) {
		t.Fatalf("daily-office-pro Dockerfile unexpectedly contains ENTRYPOINT")
	}
	if len(dailyOfficePro.Build.SmokeArgs) == 0 {
		t.Fatalf("daily-office-pro smoke args = %+v, want cli/python/node checks", dailyOfficePro.Build.SmokeArgs)
	}
	smokeScript := []byte(dailyOfficePro.Build.SmokeArgs[len(dailyOfficePro.Build.SmokeArgs)-1])
	for _, expected := range [][]byte{
		[]byte("command -v dotnet"),
		[]byte("command -v chromium"),
		[]byte("command -v pandoc"),
		[]byte("command -v himalaya"),
		[]byte("command -v dbx"),
		[]byte("command -v httpx"),
		[]byte("python -c"),
		[]byte("node -e"),
	} {
		if !bytes.Contains(smokeScript, expected) {
			t.Fatalf("daily-office-pro smoke args = %+v, want %q", dailyOfficePro.Build.SmokeArgs, expected)
		}
	}
	if !strings.Contains(dailyOfficePro.AgentPrompt, "daily-office-pro") || !strings.Contains(dailyOfficePro.AgentPrompt, "/skills/skills/minimax-pdf") {
		t.Fatalf("daily-office-pro AgentPrompt = %q, want MiniMax guidance", dailyOfficePro.AgentPrompt)
	}

	promptResp := doJSON[api.EnvironmentAgentPromptResponse](t, handler, http.MethodGet, "/api/environments/daily-office-pro/agent-prompt", nil, http.StatusOK, "")
	if !promptResp.HasPrompt {
		t.Fatal("daily-office-pro prompt HasPrompt = false, want true")
	}
	if !strings.Contains(promptResp.Prompt, "npm install -g") || !strings.Contains(promptResp.Prompt, "pptx-generator") {
		t.Fatalf("daily-office-pro prompt = %q, want MiniMax package guidance", promptResp.Prompt)
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
	handler, cfg, _ := newTestHandlerWithRuntimeAndOptions(t, authToken, Options{})
	return handler, cfg
}

func newTestHandlerWithServerOptions(t *testing.T, authToken string, options Options) (http.Handler, config.Config) {
	handler, cfg, _ := newTestHandlerWithRuntimeAndOptions(t, authToken, options)
	return handler, cfg
}

func newTestHandlerWithRuntime(t *testing.T, authToken string) (http.Handler, config.Config, *httpFakeRuntime) {
	return newTestHandlerWithRuntimeAndOptions(t, authToken, Options{})
}

func newTestHandlerWithRuntimeAndOptions(t *testing.T, authToken string, options Options) (http.Handler, config.Config, *httpFakeRuntime) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		BindAddr:                 "127.0.0.1:0",
		AuthToken:                authToken,
		StateDBPath:              filepath.Join(tempDir, "agent-container-hub.db"),
		ConfigRoot:               filepath.Join(tempDir, "configs"),
		RootfsRoot:               filepath.Join(tempDir, "rootfs"),
		BuildRoot:                filepath.Join(tempDir, "builds"),
		SessionMountTemplateRoot: filepath.Join(tempDir, "zenmind-env"),
		DefaultCommandTimeout:    time.Second,
		DeleteRootfsOnStop:       true,
		EnableExecLogPersist:     true,
		ExecLogMaxOutputBytes:    65536,
	}
	handler, returnedCfg, fake := newHandlerForConfigWithRuntimeAndOptions(t, cfg, options)
	return handler, returnedCfg, fake
}

func newHandlerForConfigRoot(t *testing.T, authToken, configRoot string) http.Handler {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		BindAddr:                 "127.0.0.1:0",
		AuthToken:                authToken,
		StateDBPath:              filepath.Join(tempDir, "agent-container-hub.db"),
		ConfigRoot:               configRoot,
		RootfsRoot:               filepath.Join(tempDir, "rootfs"),
		BuildRoot:                filepath.Join(tempDir, "builds"),
		SessionMountTemplateRoot: filepath.Join(tempDir, "zenmind-env"),
		DefaultCommandTimeout:    time.Second,
		DeleteRootfsOnStop:       true,
		EnableExecLogPersist:     true,
		ExecLogMaxOutputBytes:    65536,
	}
	handler, _ := newHandlerForConfig(t, cfg)
	return handler
}

func newHandlerForConfig(t *testing.T, cfg config.Config) (http.Handler, config.Config) {
	handler, returnedCfg, _ := newHandlerForConfigWithRuntimeAndOptions(t, cfg, Options{})
	return handler, returnedCfg
}

func newHandlerForConfigWithOptions(t *testing.T, cfg config.Config, options Options) (http.Handler, config.Config) {
	handler, returnedCfg, _ := newHandlerForConfigWithRuntimeAndOptions(t, cfg, options)
	return handler, returnedCfg
}

func newHandlerForConfigWithRuntimeAndOptions(t *testing.T, cfg config.Config, options Options) (http.Handler, config.Config, *httpFakeRuntime) {
	t.Helper()

	if err := os.MkdirAll(cfg.RootfsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootfs) error = %v", err)
	}
	if err := os.MkdirAll(cfg.BuildRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(builds) error = %v", err)
	}
	if err := os.MkdirAll(cfg.SessionMountTemplateRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(session mount template root) error = %v", err)
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
			StartedAt:  time.Date(2026, time.March, 17, 12, 38, 34, 0, time.UTC),
			FinishedAt: time.Date(2026, time.March, 17, 12, 38, 34, 95*int(time.Millisecond), time.UTC),
		},
		buildResult: runtime.BuildResult{
			Output:     "built",
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		},
	}
	serviceLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionService := sandbox.NewSessionService(cfg, st, envs, fake, serviceLogger)
	buildService := sandbox.NewBuildService(cfg, st, envs, fake, serviceLogger)
	environmentService := sandbox.NewEnvironmentService(envs, buildService, serviceLogger)
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return New(sessionService, environmentService, buildService, cfg.AuthToken, options), cfg, fake
}

func repoRootFromPackageDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, "..", "..")), nil
}

type httpFakeRuntime struct {
	containers    map[string]runtime.ContainerInfo
	execResult    runtime.ExecResult
	buildResult   runtime.BuildResult
	buildStarted  chan struct{}
	buildContinue chan struct{}
}

func (f *httpFakeRuntime) Name() string { return "fake" }

func (f *httpFakeRuntime) Create(_ context.Context, opts runtime.CreateOptions) (runtime.ContainerInfo, error) {
	id := "ctr-" + opts.Name
	info := runtime.ContainerInfo{
		ID:        id,
		Name:      opts.Name,
		Image:     opts.Image,
		State:     runtime.ContainerStopped,
		Labels:    model.CloneMap(opts.Labels),
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

func (f *httpFakeRuntime) Exec(_ context.Context, containerID string, _ runtime.ExecOptions) (runtime.ExecResult, error) {
	info, ok := f.lookup(containerID)
	if !ok {
		return runtime.ExecResult{}, runtime.ErrContainerNotFound
	}
	if info.State != runtime.ContainerRunning {
		return runtime.ExecResult{}, runtime.ErrContainerNotRunning
	}
	return f.execResult, nil
}

func (f *httpFakeRuntime) Build(_ context.Context, opts runtime.BuildOptions) (runtime.BuildResult, error) {
	if f.buildStarted != nil {
		select {
		case f.buildStarted <- struct{}{}:
		default:
		}
	}
	if f.buildContinue != nil {
		<-f.buildContinue
	}
	if opts.OutputSink != nil && f.buildResult.Output != "" {
		_, _ = io.WriteString(opts.OutputSink, f.buildResult.Output)
	}
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
