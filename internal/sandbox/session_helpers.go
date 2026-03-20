package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"agent-container-hub/internal/api"
	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/util"
)

var validSessionID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

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
		RootfsPath:      session.RootfsPath,
		Labels:          util.CloneMap(session.Labels),
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
