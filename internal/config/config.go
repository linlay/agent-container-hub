package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	BindAddr                 string
	AuthToken                string
	StateDBPath              string
	ConfigRoot               string
	WorkspaceRoot            string
	BuildRoot                string
	SessionMountTemplateRoot string
	Engine                   string
	DefaultCommandTimeout    time.Duration
	EnableExecLogPersist     bool
	ExecLogMaxOutputBytes    int
}

func Load() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("getwd: %w", err)
	}
	cfg := Config{
		BindAddr:                 getEnv("BIND_ADDR", "127.0.0.1:8080"),
		AuthToken:                strings.TrimSpace(os.Getenv("AUTH_TOKEN")),
		StateDBPath:              getEnv("STATE_DB_PATH", filepath.Join(cwd, "data", "agent-container-hub.db")),
		ConfigRoot:               getEnv("CONFIG_ROOT", filepath.Join(cwd, "configs")),
		WorkspaceRoot:            getEnv("WORKSPACE_ROOT", filepath.Join(cwd, "data", "workspaces")),
		BuildRoot:                getEnv("BUILD_ROOT", filepath.Join(cwd, "data", "builds")),
		SessionMountTemplateRoot: getEnv("SESSION_MOUNT_TEMPLATE_ROOT", "/Users/linlay/Project/zenmind-env"),
		Engine:                   firstNonEmpty(strings.TrimSpace(os.Getenv("ENGINE")), strings.TrimSpace(os.Getenv("RUNTIME"))),
		DefaultCommandTimeout:    getEnvDuration("DEFAULT_COMMAND_TIMEOUT", 30*time.Second),
		EnableExecLogPersist:     getEnvBool("ENABLE_EXEC_LOG_PERSIST", false),
		ExecLogMaxOutputBytes:    getEnvInt("EXEC_LOG_MAX_OUTPUT_BYTES", 65536),
	}
	if cfg.StateDBPath, err = absolutePath(cfg.StateDBPath); err != nil {
		return Config{}, fmt.Errorf("normalize state db path: %w", err)
	}
	if cfg.ConfigRoot, err = absolutePath(cfg.ConfigRoot); err != nil {
		return Config{}, fmt.Errorf("normalize config root: %w", err)
	}
	if cfg.WorkspaceRoot, err = absolutePath(cfg.WorkspaceRoot); err != nil {
		return Config{}, fmt.Errorf("normalize workspace root: %w", err)
	}
	if cfg.BuildRoot, err = absolutePath(cfg.BuildRoot); err != nil {
		return Config{}, fmt.Errorf("normalize build root: %w", err)
	}
	if cfg.SessionMountTemplateRoot, err = absolutePath(cfg.SessionMountTemplateRoot); err != nil {
		return Config{}, fmt.Errorf("normalize session mount template root: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.BindAddr == "" {
		return fmt.Errorf("bind address is required")
	}
	host, _, err := net.SplitHostPort(c.BindAddr)
	if err != nil {
		return fmt.Errorf("invalid bind address: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && c.AuthToken == "" {
		return fmt.Errorf("AUTH_TOKEN is required when binding to %q", host)
	}
	if c.StateDBPath == "" || c.ConfigRoot == "" || c.WorkspaceRoot == "" || c.BuildRoot == "" {
		return fmt.Errorf("state paths are required")
	}
	if c.ExecLogMaxOutputBytes < 0 {
		return fmt.Errorf("EXEC_LOG_MAX_OUTPUT_BYTES must be >= 0")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func absolutePath(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return "", nil
	}
	return filepath.Abs(path)
}
