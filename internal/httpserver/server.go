package httpserver

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

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
	List(context.Context) ([]*api.EnvironmentResponse, error)
	ListFiles(context.Context, string) ([]*api.EnvironmentFileResponse, error)
	GetFile(context.Context, string, string) (*api.EnvironmentFileResponse, error)
	PutFile(context.Context, string, string, string) (*api.EnvironmentFileResponse, error)
}

type BuildService interface {
	BuildEnvironment(context.Context, string) (*api.BuildJobResponse, error)
}

type Server struct {
	sessions     SessionService
	environments EnvironmentService
	builds       BuildService
	authToken    string
	uiFS         fs.FS
}

func New(sessions SessionService, environments EnvironmentService, builds BuildService, authToken string) http.Handler {
	uiFS, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	server := &Server{
		sessions:     sessions,
		environments: environments,
		builds:       builds,
		authToken:    strings.TrimSpace(authToken),
		uiFS:         uiFS,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", server.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", server.handleLogout)

	mux.Handle("GET /", server.requireAuth(http.HandlerFunc(server.handleSessionsPage)))
	mux.Handle("GET /app", server.requireAuth(http.HandlerFunc(server.handleSessionsPage)))
	mux.Handle("GET /sessions", server.requireAuth(http.HandlerFunc(server.handleSessionsPage)))
	mux.Handle("GET /environments", server.requireAuth(http.HandlerFunc(server.handleEnvironmentsPage)))
	mux.Handle("GET /ui/", server.requireAuth(http.StripPrefix("/ui/", http.FileServer(http.FS(server.uiFS)))))
	mux.HandleFunc("GET /login", server.handleLoginPage)

	mux.Handle("POST /api/sessions/create", server.requireAuth(http.HandlerFunc(server.handleCreateSession)))
	mux.Handle("GET /api/session-create/template", server.requireAuth(http.HandlerFunc(server.handleGetSessionCreateTemplate)))
	mux.Handle("GET /api/sessions", server.requireAuth(http.HandlerFunc(server.handleListSessions)))
	mux.Handle("GET /api/sessions/query", server.requireAuth(http.HandlerFunc(server.handleQuerySessions)))
	mux.Handle("GET /api/sessions/{id}", server.requireAuth(http.HandlerFunc(server.handleGetSession)))
	mux.Handle("POST /api/sessions/{id}/execute", server.requireAuth(http.HandlerFunc(server.handleExecuteSession)))
	mux.Handle("GET /api/sessions/{id}/executions", server.requireAuth(http.HandlerFunc(server.handleListSessionExecutions)))
	mux.Handle("POST /api/sessions/{id}/stop", server.requireAuth(http.HandlerFunc(server.handleStopSession)))
	mux.Handle("GET /api/environments", server.requireAuth(http.HandlerFunc(server.handleListEnvironments)))
	mux.Handle("POST /api/environments", server.requireAuth(http.HandlerFunc(server.handleUpsertEnvironment)))
	mux.Handle("GET /api/environments/{name}", server.requireAuth(http.HandlerFunc(server.handleGetEnvironment)))
	mux.Handle("PUT /api/environments/{name}", server.requireAuth(http.HandlerFunc(server.handleUpsertEnvironment)))
	mux.Handle("GET /api/environments/{name}/files", server.requireAuth(http.HandlerFunc(server.handleListEnvironmentFiles)))
	mux.Handle("GET /api/environments/{name}/files/{path...}", server.requireAuth(http.HandlerFunc(server.handleGetEnvironmentFile)))
	mux.Handle("PUT /api/environments/{name}/files/{path...}", server.requireAuth(http.HandlerFunc(server.handlePutEnvironmentFile)))
	mux.Handle("POST /api/environments/{name}/build", server.requireAuth(http.HandlerFunc(server.handleBuildEnvironment)))

	return mux
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
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetSessionCreateTemplate(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.CreateTemplate(r.Context())
	if err != nil {
		writeMappedError(w, err)
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
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.Stop(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.List(r.Context())
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	response, err := s.sessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeMappedError(w, err)
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
		writeMappedError(w, err)
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
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.List(r.Context())
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListEnvironmentFiles(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.ListFiles(r.Context(), r.PathValue("name"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetEnvironmentFile(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.GetFile(r.Context(), r.PathValue("name"), r.PathValue("path"))
	if err != nil {
		writeMappedError(w, err)
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
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		writeMappedError(w, err)
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
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleBuildEnvironment(w http.ResponseWriter, r *http.Request) {
	response, err := s.builds.BuildEnvironment(r.Context(), r.PathValue("name"))
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
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

func writeMappedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sandbox.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sandbox.ErrBusy), errors.Is(err, sandbox.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound), errors.Is(err, runtime.ErrContainerNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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
