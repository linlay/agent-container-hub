package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"agent-container-hub/internal/model"
	"gopkg.in/yaml.v3"
)

var validEnvironmentFilename = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

type FileEnvironmentStore struct {
	root string
}

func OpenFileEnvironmentStore(root string) (*FileEnvironmentStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil, fmt.Errorf("environment config root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir environment config root: %w", err)
	}
	return &FileEnvironmentStore{root: root}, nil
}

func (s *FileEnvironmentStore) SaveEnvironment(_ context.Context, environment *model.Environment) error {
	path, err := s.environmentPath(environment.Name)
	if err != nil {
		return err
	}
	payload, err := yaml.Marshal(environment)
	if err != nil {
		return fmt.Errorf("marshal environment %q: %w", environment.Name, err)
	}
	tempFile, err := os.CreateTemp(s.root, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp environment file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp environment file: %w", err)
	}
	if err := tempFile.Chmod(0o644); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp environment file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp environment file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename environment file: %w", err)
	}
	return nil
}

func (s *FileEnvironmentStore) GetEnvironment(_ context.Context, name string) (*model.Environment, error) {
	path, err := s.environmentPath(name)
	if err != nil {
		return nil, err
	}
	environment, err := s.loadEnvironment(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return environment, nil
}

func (s *FileEnvironmentStore) ListEnvironments(_ context.Context) ([]*model.Environment, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read environment config dir: %w", err)
	}
	environments := make([]*model.Environment, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		environment, err := s.loadEnvironment(filepath.Join(s.root, entry.Name()))
		if err != nil {
			return nil, err
		}
		environments = append(environments, environment)
	}
	slices.SortFunc(environments, func(a, b *model.Environment) int {
		return strings.Compare(a.Name, b.Name)
	})
	return environments, nil
}

func (s *FileEnvironmentStore) DeleteEnvironment(_ context.Context, name string) error {
	path, err := s.environmentPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("delete environment file: %w", err)
	}
	return nil
}

func (s *FileEnvironmentStore) environmentPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !validEnvironmentFilename.MatchString(name) {
		return "", fmt.Errorf("%w: invalid environment name %q", ErrNotFound, name)
	}
	return filepath.Join(s.root, name+".yaml"), nil
}

func (s *FileEnvironmentStore) loadEnvironment(path string) (*model.Environment, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var environment model.Environment
	if err := yaml.Unmarshal(payload, &environment); err != nil {
		return nil, fmt.Errorf("parse environment file %s: %w", filepath.Base(path), err)
	}
	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.TrimSpace(environment.Name) == "" {
		environment.Name = baseName
	}
	if !validEnvironmentFilename.MatchString(environment.Name) {
		return nil, fmt.Errorf("parse environment file %s: invalid environment name %q", filepath.Base(path), environment.Name)
	}
	if environment.Name != baseName {
		return nil, fmt.Errorf("parse environment file %s: name %q does not match file name %q", filepath.Base(path), environment.Name, baseName)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat environment file %s: %w", filepath.Base(path), err)
	}
	environment.CreatedAt = info.ModTime().UTC()
	environment.UpdatedAt = info.ModTime().UTC()
	return environment.Clone(), nil
}
