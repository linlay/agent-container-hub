package httpserver

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"agentbox/internal/api"
	"agentbox/internal/runtime"
	"agentbox/internal/sandbox"
	"agentbox/internal/store"
)

const authCookieName = "agentbox_auth"

//go:embed ui/*
var uiFiles embed.FS

type SessionService interface {
	Create(context.Context, api.CreateSessionRequest) (*api.SessionResponse, error)
	Execute(context.Context, string, api.ExecuteSessionRequest) (*api.ExecuteSessionResponse, error)
	Stop(context.Context, string) (*api.StopSessionResponse, error)
	List(context.Context) ([]*api.SessionResponse, error)
	Get(context.Context, string) (*api.SessionResponse, error)
}

type EnvironmentService interface {
	Upsert(context.Context, api.UpsertEnvironmentRequest) (*api.EnvironmentResponse, error)
	Get(context.Context, string) (*api.EnvironmentResponse, error)
	List(context.Context) ([]*api.EnvironmentResponse, error)
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

	mux.Handle("GET /", server.requireAuth(http.HandlerFunc(server.handleApp)))
	mux.Handle("GET /app", server.requireAuth(http.HandlerFunc(server.handleApp)))
	mux.HandleFunc("GET /login", server.handleLoginPage)

	mux.Handle("POST /api/sessions/create", server.requireAuth(http.HandlerFunc(server.handleCreateSession)))
	mux.Handle("GET /api/sessions", server.requireAuth(http.HandlerFunc(server.handleListSessions)))
	mux.Handle("GET /api/sessions/{id}", server.requireAuth(http.HandlerFunc(server.handleGetSession)))
	mux.Handle("POST /api/sessions/{id}/execute", server.requireAuth(http.HandlerFunc(server.handleExecuteSession)))
	mux.Handle("POST /api/sessions/{id}/stop", server.requireAuth(http.HandlerFunc(server.handleStopSession)))
	mux.Handle("GET /api/environments", server.requireAuth(http.HandlerFunc(server.handleListEnvironments)))
	mux.Handle("POST /api/environments", server.requireAuth(http.HandlerFunc(server.handleUpsertEnvironment)))
	mux.Handle("GET /api/environments/{name}", server.requireAuth(http.HandlerFunc(server.handleGetEnvironment)))
	mux.Handle("PUT /api/environments/{name}", server.requireAuth(http.HandlerFunc(server.handleUpsertEnvironment)))
	mux.Handle("POST /api/environments/{name}/build", server.requireAuth(http.HandlerFunc(server.handleBuildEnvironment)))
	mux.Handle("POST /execute", server.requireAuth(http.HandlerFunc(server.handleDeprecatedExecute)))
	mux.Handle("POST /session/stop", server.requireAuth(http.HandlerFunc(server.handleDeprecatedStop)))

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

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	s.serveUI(w, "app.html")
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

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	response, err := s.environments.List(r.Context())
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

func (s *Server) handleDeprecatedExecute(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		SessionID string   `json:"session_id"`
		Command   string   `json:"command"`
		Args      []string `json:"args"`
		Cwd       string   `json:"cwd"`
		TimeoutMS int64    `json:"timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		writeError(w, http.StatusBadRequest, "deprecated /execute only supports existing session_id")
		return
	}
	response, err := s.sessions.Execute(r.Context(), payload.SessionID, api.ExecuteSessionRequest{
		Command:   payload.Command,
		Args:      payload.Args,
		Cwd:       payload.Cwd,
		TimeoutMS: payload.TimeoutMS,
	})
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDeprecatedStop(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := s.sessions.Stop(r.Context(), payload.SessionID)
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
