package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"agent-container-hub/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	environmentMetadataFile  = "environment.yml"
	environmentDockerfile    = "Dockerfile"
	environmentBuildMakefile = "Makefile"
)

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
	dir, err := s.environmentDir(environment.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir environment dir: %w", err)
	}
	if strings.TrimSpace(environment.Build.Dockerfile) != "" {
		if err := s.writeEnvironmentFile(dir, environmentDockerfile, []byte(environment.Build.Dockerfile), 0o644); err != nil {
			return err
		}
	}

	metadata := environment.Clone()
	metadata.Build.Dockerfile = ""
	payload, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal environment %q: %w", environment.Name, err)
	}
	if err := s.writeEnvironmentFile(dir, environmentMetadataFile, payload, 0o644); err != nil {
		return err
	}
	return nil
}

func (s *FileEnvironmentStore) GetEnvironment(_ context.Context, name string) (*model.Environment, error) {
	dir, err := s.environmentDir(name)
	if err != nil {
		return nil, err
	}
	environment, err := s.loadEnvironment(dir)
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
		if !entry.IsDir() {
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
	dir, err := s.environmentDir(name)
	if err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("stat environment dir: %w", err)
	}
	if !info.IsDir() {
		return ErrNotFound
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete environment dir: %w", err)
	}
	return nil
}

func (s *FileEnvironmentStore) ListEnvironmentFiles(_ context.Context, name string) ([]EnvironmentFile, error) {
	dir, err := s.environmentDir(name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, environmentMetadataFile)); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat environment metadata: %w", err)
	}

	paths := []string{environmentMetadataFile, environmentDockerfile, environmentBuildMakefile}
	files := make([]EnvironmentFile, 0, len(paths))
	for _, relPath := range paths {
		file, err := s.readEnvironmentFile(dir, relPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		files = append(files, file.EnvironmentFile)
	}
	for _, subdir := range []string{"scripts", "curl"} {
		collected, err := s.listFilesUnder(filepath.Join(dir, subdir), subdir)
		if err != nil {
			return nil, err
		}
		files = append(files, collected...)
	}
	slices.SortFunc(files, func(a, b EnvironmentFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	return files, nil
}

func (s *FileEnvironmentStore) ReadEnvironmentFile(_ context.Context, name, relPath string) (*EnvironmentFileContent, error) {
	dir, err := s.environmentDir(name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, environmentMetadataFile)); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat environment metadata: %w", err)
	}
	file, err := s.readEnvironmentFile(dir, relPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return file, nil
}

func (s *FileEnvironmentStore) WriteEnvironmentFile(_ context.Context, name, relPath string, content []byte) error {
	dir, err := s.environmentDir(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir environment dir: %w", err)
	}
	cleanPath, err := normalizeEnvironmentFilePath(relPath)
	if err != nil {
		return err
	}
	if cleanPath == environmentMetadataFile {
		var environment model.Environment
		if err := yaml.Unmarshal(content, &environment); err != nil {
			return fmt.Errorf("parse environment file %s: %w", environmentMetadataFile, err)
		}
		if strings.TrimSpace(environment.Name) == "" {
			environment.Name = filepath.Base(dir)
		}
		if !model.ValidEnvironmentName.MatchString(environment.Name) {
			return fmt.Errorf("parse environment file %s: invalid environment name %q", environmentMetadataFile, environment.Name)
		}
		if environment.Name != filepath.Base(dir) {
			return fmt.Errorf("parse environment file %s: name %q does not match directory name %q", environmentMetadataFile, environment.Name, filepath.Base(dir))
		}
	}
	return s.writeEnvironmentFile(dir, cleanPath, content, 0o644)
}

func (s *FileEnvironmentStore) environmentDir(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !model.ValidEnvironmentName.MatchString(name) {
		return "", fmt.Errorf("%w: invalid environment name %q", ErrNotFound, name)
	}
	return filepath.Join(s.root, name), nil
}

func (s *FileEnvironmentStore) loadEnvironment(dir string) (*model.Environment, error) {
	metadataPath := filepath.Join(dir, environmentMetadataFile)
	payload, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, err
	}
	var environment model.Environment
	if err := yaml.Unmarshal(payload, &environment); err != nil {
		return nil, fmt.Errorf("parse environment file %s: %w", filepath.Join(filepath.Base(dir), environmentMetadataFile), err)
	}
	baseName := filepath.Base(dir)
	if strings.TrimSpace(environment.Name) == "" {
		environment.Name = baseName
	}
	if !model.ValidEnvironmentName.MatchString(environment.Name) {
		return nil, fmt.Errorf("parse environment file %s: invalid environment name %q", filepath.Join(baseName, environmentMetadataFile), environment.Name)
	}
	if environment.Name != baseName {
		return nil, fmt.Errorf("parse environment file %s: name %q does not match directory name %q", filepath.Join(baseName, environmentMetadataFile), environment.Name, baseName)
	}
	dockerfile, err := os.ReadFile(filepath.Join(dir, environmentDockerfile))
	if err == nil {
		environment.Build.Dockerfile = string(dockerfile)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read environment Dockerfile %s: %w", filepath.Join(baseName, environmentDockerfile), err)
	}
	info, err := os.Stat(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("stat environment file %s: %w", filepath.Join(baseName, environmentMetadataFile), err)
	}
	environment.CreatedAt = info.ModTime().UTC()
	environment.UpdatedAt = info.ModTime().UTC()
	return environment.Clone(), nil
}

func (s *FileEnvironmentStore) listFilesUnder(absDir, relDir string) ([]EnvironmentFile, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read environment asset dir %s: %w", relDir, err)
	}
	files := make([]EnvironmentFile, 0, len(entries))
	for _, entry := range entries {
		relPath := filepath.Join(relDir, entry.Name())
		absPath := filepath.Join(absDir, entry.Name())
		if entry.IsDir() {
			nested, err := s.listFilesUnder(absPath, relPath)
			if err != nil {
				return nil, err
			}
			files = append(files, nested...)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat environment asset %s: %w", relPath, err)
		}
		files = append(files, EnvironmentFile{
			Path:       filepath.ToSlash(relPath),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC(),
			Type:       classifyEnvironmentFile(filepath.ToSlash(relPath)),
		})
	}
	return files, nil
}

func (s *FileEnvironmentStore) readEnvironmentFile(dir, relPath string) (*EnvironmentFileContent, error) {
	cleanPath, err := normalizeEnvironmentFilePath(relPath)
	if err != nil {
		return nil, err
	}
	absPath := filepath.Join(dir, filepath.FromSlash(cleanPath))
	payload, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	return &EnvironmentFileContent{
		EnvironmentFile: EnvironmentFile{
			Path:       cleanPath,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC(),
			Type:       classifyEnvironmentFile(cleanPath),
		},
		Content: payload,
	}, nil
}

func (s *FileEnvironmentStore) writeEnvironmentFile(dir, relPath string, content []byte, mode os.FileMode) error {
	tempDir := dir
	if nested := filepath.Dir(filepath.Join(dir, filepath.FromSlash(relPath))); nested != dir {
		if err := os.MkdirAll(nested, 0o755); err != nil {
			return fmt.Errorf("mkdir environment asset dir: %w", err)
		}
		tempDir = nested
	}
	targetPath := filepath.Join(dir, filepath.FromSlash(relPath))
	tempFile, err := os.CreateTemp(tempDir, filepath.Base(targetPath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp environment file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp environment file: %w", err)
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp environment file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp environment file: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("rename environment file: %w", err)
	}
	return nil
}

func normalizeEnvironmentFilePath(relPath string) (string, error) {
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if relPath == "" {
		return "", fmt.Errorf("%w: file path is required", ErrNotFound)
	}
	if strings.HasPrefix(relPath, "/") {
		return "", fmt.Errorf("%w: invalid environment file path %q", ErrNotFound, relPath)
	}
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relPath)))
	if cleanPath == "." {
		cleanPath = ""
	}
	if cleanPath == "." || cleanPath == "" || strings.HasPrefix(cleanPath, "../") || cleanPath == ".." {
		return "", fmt.Errorf("%w: invalid environment file path %q", ErrNotFound, relPath)
	}
	switch {
	case cleanPath == environmentMetadataFile, cleanPath == environmentDockerfile, cleanPath == environmentBuildMakefile:
		return cleanPath, nil
	case strings.HasPrefix(cleanPath, "scripts/"), strings.HasPrefix(cleanPath, "curl/"):
		return cleanPath, nil
	default:
		return "", fmt.Errorf("%w: unsupported environment file path %q", ErrNotFound, relPath)
	}
}

func classifyEnvironmentFile(relPath string) string {
	switch relPath {
	case environmentMetadataFile:
		return "metadata"
	case environmentDockerfile:
		return "dockerfile"
	case environmentBuildMakefile:
		return "script"
	}
	if strings.HasSuffix(relPath, ".sh") || strings.HasPrefix(relPath, "scripts/") || strings.HasPrefix(relPath, "curl/") {
		return "script"
	}
	return "other"
}
