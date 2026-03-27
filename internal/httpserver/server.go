package httpserver

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/sandbox"
	"agent-container-hub/internal/store"
)

const authCookieName = "agent-container-hub_auth"

//go:embed ui/*
var uiFiles embed.FS

type SessionService interface {
	Create(context.Context, api.CreateSessionRequest) (*api.CreateSessionResponse, error)
	CreateTemplate(context.Context) (*api.SessionCreateTemplateResponse, error)
	Execute(context.Context, string, api.ExecuteSessionRequest) (*api.ExecuteSessionResponse, error)
	Stop(context.Context, string) (*api.StopSessionResponse, error)
	List(context.Context) ([]*api.SessionResponse, error)
	Query(context.Context, store.SessionQuery) (*api.SessionListResponse, error)
	Get(context.Context, string) (*api.SessionResponse, error)
	ListExecutions(context.Context, string, store.Pagination) (*api.SessionExecutionListResponse, error)
}

type EnvironmentService interface {
	Upsert(context.Context, api.UpsertEnvironmentRequest) (*api.EnvironmentResponse, error)
	Get(context.Context, string) (*api.EnvironmentResponse, error)
	GetAgentPrompt(context.Context, string) (*api.EnvironmentAgentPromptResponse, error)
	List(context.Context) ([]*api.EnvironmentResponse, error)
	ListFiles(context.Context, string) ([]*api.EnvironmentFileResponse, error)
	GetFile(context.Context, string, string) (*api.EnvironmentFileResponse, error)
	PutFile(context.Context, string, string, string) (*api.EnvironmentFileResponse, error)
}

type BuildService interface {
	StartBuildJob(context.Context, string) (*api.BuildJobResponse, error)
	GetBuildJob(context.Context, string) (*api.BuildJobResponse, error)
	SubscribeBuildJob(context.Context, string) (*api.BuildJobResponse, <-chan sandbox.BuildEvent, func(), error)
}

type Server struct {
	sessions     SessionService
	environments EnvironmentService
	builds       BuildService
	authToken    string
	uiFS         fs.FS
	logger       *slog.Logger
	accessLogs   bool
	errorLogs    bool
}

type Options struct {
	Logger           *slog.Logger
	AccessLogEnabled bool
	ErrorLogEnabled  bool
}

func New(sessions SessionService, environments EnvironmentService, builds BuildService, authToken string, options Options) http.Handler {
	uiFS, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		sessions:     sessions,
		environments: environments,
		builds:       builds,
		authToken:    strings.TrimSpace(authToken),
		uiFS:         uiFS,
		logger:       logger,
		accessLogs:   options.AccessLogEnabled,
		errorLogs:    options.ErrorLogEnabled,
	}
	mux := http.NewServeMux()
	apiMux := http.NewServeMux()

	apiMux.Handle("POST /api/auth/login", http.HandlerFunc(server.handleLogin))
	apiMux.Handle("POST /api/auth/logout", http.HandlerFunc(server.handleLogout))

	mux.Handle("GET /", server.requireAuth(http.HandlerFunc(server.handleSessionsPage)))
	mux.Handle("GET /app", server.requireAuth(http.HandlerFunc(server.handleSessionsPage)))
	mux.Handle("GET /sessions", server.requireAuth(http.HandlerFunc(server.handleSessionsPage)))
	mux.Handle("GET /environments", server.requireAuth(http.HandlerFunc(server.handleEnvironmentsPage)))
	mux.Handle("GET /ui/", server.requireAuth(http.StripPrefix("/ui/", http.FileServer(http.FS(server.uiFS)))))
	mux.HandleFunc("GET /login", server.handleLoginPage)

	apiMux.Handle("POST /api/sessions/create", server.requireAuth(http.HandlerFunc(server.handleCreateSession)))
	apiMux.Handle("GET /api/session-create/template", server.requireAuth(http.HandlerFunc(server.handleGetSessionCreateTemplate)))
	apiMux.Handle("GET /api/sessions", server.requireAuth(http.HandlerFunc(server.handleListSessions)))
	apiMux.Handle("GET /api/sessions/query", server.requireAuth(http.HandlerFunc(server.handleQuerySessions)))
	apiMux.Handle("GET /api/sessions/{id}", server.requireAuth(http.HandlerFunc(server.handleGetSession)))
	apiMux.Handle("POST /api/sessions/{id}/execute", server.requireAuth(http.HandlerFunc(server.handleExecuteSession)))
	apiMux.Handle("GET /api/sessions/{id}/executions", server.requireAuth(http.HandlerFunc(server.handleListSessionExecutions)))
	apiMux.Handle("POST /api/sessions/{id}/stop", server.requireAuth(http.HandlerFunc(server.handleStopSession)))
	apiMux.Handle("GET /api/environments", server.requireAuth(http.HandlerFunc(server.handleListEnvironments)))
	apiMux.Handle("POST /api/environments", server.requireAuth(http.HandlerFunc(server.handleUpsertEnvironment)))
	apiMux.Handle("GET /api/environments/{name}", server.requireAuth(http.HandlerFunc(server.handleGetEnvironment)))
	apiMux.Handle("GET /api/environments/{name}/agent-prompt", server.requireAuth(http.HandlerFunc(server.handleGetEnvironmentAgentPrompt)))
	apiMux.Handle("PUT /api/environments/{name}", server.requireAuth(http.HandlerFunc(server.handleUpsertEnvironment)))
	apiMux.Handle("GET /api/environments/{name}/files", server.requireAuth(http.HandlerFunc(server.handleListEnvironmentFiles)))
	apiMux.Handle("GET /api/environments/{name}/files/{path...}", server.requireAuth(http.HandlerFunc(server.handleGetEnvironmentFile)))
	apiMux.Handle("PUT /api/environments/{name}/files/{path...}", server.requireAuth(http.HandlerFunc(server.handlePutEnvironmentFile)))
	apiMux.Handle("POST /api/environments/{name}/build-jobs", server.requireAuth(http.HandlerFunc(server.handleStartBuildJob)))
	apiMux.Handle("GET /api/build-jobs/{id}", server.requireAuth(http.HandlerFunc(server.handleGetBuildJob)))
	apiMux.Handle("GET /api/build-jobs/{id}/events", server.requireAuth(http.HandlerFunc(server.handleBuildJobEvents)))
	apiMux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	apiHandler := server.wrapAPI(apiMux)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		mux.Handle(method+" /api/", apiHandler)
	}

	return mux
}

type apiResponseWriter struct {
	http.ResponseWriter
	status       int
	errorMessage string
}

func (w *apiResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *apiResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func (w *apiResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *apiResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *apiResponseWriter) setErrorMessage(message string) {
	w.errorMessage = message
}

func (s *Server) wrapAPI(next http.Handler) http.Handler {
	return s.observeAPI(s.recoverAPI(next))
}

func (s *Server) observeAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		tracker := &apiResponseWriter{ResponseWriter: w}
		next.ServeHTTP(tracker, r)

		durationMS := time.Since(startedAt).Milliseconds()
		logArgs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", tracker.Status(),
			"duration_ms", durationMS,
			"remote_addr", r.RemoteAddr,
		}
		if s.accessLogs {
			s.logger.Info("api request", logArgs...)
		}
		if s.errorLogs && tracker.Status() >= http.StatusBadRequest {
			logArgs = append(logArgs, "error", tracker.errorMessage)
			s.logger.Error("api request failed", logArgs...)
		}
	})
}

func (s *Server) recoverAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("api panic recovered",
					"method", r.Method,
					"path", r.URL.Path,
					"query", r.URL.RawQuery,
					"remote_addr", r.RemoteAddr,
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	if s.authToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isAuthorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.serveUI(w, "login.html")
}

func (s *Server) handleSessionsPage(w http.ResponseWriter, r *http.Request) {
	s.serveUI(w, "sessions.html")
}

func (s *Server) handleEnvironmentsPage(w http.ResponseWriter, r *http.Request) {
	s.serveUI(w, "environments.html")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authToken == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
		return
	}
	var req api.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Token) != s.authToken {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    s.authToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req api.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := s.sessions.Create(r.Context(), req)
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetSessionCreateTemplate(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.CreateTemplate(r.Context())
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleExecuteSession(w http.ResponseWriter, r *http.Request) {
	var req api.ExecuteSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := s.sessions.Execute(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.Stop(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.List(r.Context())
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleQuerySessions(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.Query(r.Context(), store.SessionQuery{
		Status:          r.URL.Query().Get("status"),
		SessionID:       r.URL.Query().Get("session_id"),
		EnvironmentName: r.URL.Query().Get("environment_name"),
		Pagination: store.Pagination{
			Page:     parsePositiveInt(r.URL.Query().Get("page"), 1),
			PageSize: parsePositiveInt(r.URL.Query().Get("page_size"), 20),
		},
	})
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListSessionExecutions(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.ListExecutions(r.Context(), r.PathValue("id"), store.Pagination{
		Page:     parsePositiveInt(r.URL.Query().Get("page"), 1),
		PageSize: parsePositiveInt(r.URL.Query().Get("page_size"), 20),
	})
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.List(r.Context())
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListEnvironmentFiles(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.ListFiles(r.Context(), r.PathValue("name"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetEnvironmentFile(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.GetFile(r.Context(), r.PathValue("name"), r.PathValue("path"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePutEnvironmentFile(w http.ResponseWriter, r *http.Request) {
	var req api.PutEnvironmentFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := s.environments.PutFile(r.Context(), r.PathValue("name"), r.PathValue("path"), req.Content)
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetEnvironmentAgentPrompt(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.GetAgentPrompt(r.Context(), r.PathValue("name"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleUpsertEnvironment(w http.ResponseWriter, r *http.Request) {
	var req api.UpsertEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if pathName := strings.TrimSpace(r.PathValue("name")); pathName != "" {
		req.Name = pathName
	}
	response, err := s.environments.Upsert(r.Context(), req)
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStartBuildJob(w http.ResponseWriter, r *http.Request) {
	response, err := s.builds.StartBuildJob(r.Context(), r.PathValue("name"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetBuildJob(w http.ResponseWriter, r *http.Request) {
	response, err := s.builds.GetBuildJob(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleBuildJobEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	snapshot, events, cancel, err := s.builds.SubscribeBuildJob(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMappedError(w, err)
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	writeSSEEvent(w, sandbox.BuildEventSnapshot, snapshot)
	flusher.Flush()

	if events == nil {
		writeSSEEvent(w, sandbox.BuildEventComplete, snapshot)
		flusher.Flush()
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.Type {
			case sandbox.BuildEventLog:
				writeSSEEvent(w, event.Type, map[string]string{
					"id":    snapshot.ID,
					"chunk": event.Chunk,
				})
			default:
				writeSSEEvent(w, event.Type, event.Job)
			}
			flusher.Flush()
		}
	}
}

func (s *Server) serveUI(w http.ResponseWriter, name string) {
	content, err := fs.ReadFile(s.uiFS, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ui asset missing")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) isAuthorized(r *http.Request) bool {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "Bearer "+s.authToken {
		return true
	}
	cookie, err := r.Cookie(authCookieName)
	return err == nil && cookie.Value == s.authToken
}

func (s *Server) writeMappedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sandbox.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sandbox.ErrBusy), errors.Is(err, sandbox.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound), errors.Is(err, runtime.ErrContainerNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case runtimePublicMessageAvailable(err):
		message, _ := runtime.PublicErrorMessage(err)
		writeError(w, http.StatusInternalServerError, message)
	default:
		s.logger.Error("internal api error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func runtimePublicMessageAvailable(err error) bool {
	_, ok := runtime.PublicErrorMessage(err)
	return ok
}

func writeError(w http.ResponseWriter, status int, message string) {
	if tracker, ok := w.(interface{ setErrorMessage(string) }); ok {
		tracker.setErrorMessage(message)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSEEvent(w http.ResponseWriter, event string, payload any) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	if payload != nil {
		data, _ := json.Marshal(payload)
		for _, line := range strings.Split(string(data), "\n") {
			_, _ = fmt.Fprintf(w, "data: %s\n", line)
		}
	}
	_, _ = fmt.Fprint(w, "\n")
}

func parsePositiveInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
