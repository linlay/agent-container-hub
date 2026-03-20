package sandbox

import (
	"fmt"
	"os"
	"path"
	"strings"

	"agent-container-hub/internal/model"
	"agent-container-hub/internal/runtime"
)

func (s *SessionService) buildSessionMounts(environmentMounts, requestMounts []model.Mount, rootfsPath string) ([]model.Mount, bool, error) {
	normalizedEnvMounts, err := s.normalizeMountList(environmentMounts)
	if err != nil {
		return nil, false, err
	}
	normalizedRequestMounts, err := s.normalizeMountList(requestMounts)
	if err != nil {
		return nil, false, err
	}

	callerProvidesWorkspace := false
	for _, mount := range normalizedRequestMounts {
		if mount.Destination == runtime.DefaultMountPath {
			callerProvidesWorkspace = true
			break
		}
	}

	rootfsMount := model.Mount{
		Source:      runtime.NormalizeMountSource(rootfsPath),
		Destination: runtime.DefaultMountPath,
	}

	destinations := make(map[string]struct{})
	for _, mount := range normalizedEnvMounts {
		if mount.Destination == runtime.DefaultMountPath {
			return nil, false, fmt.Errorf("%w: mount destination %s is reserved for the rootfs", ErrValidation, runtime.DefaultMountPath)
		}
		if err := validateMountDestination(mount.Destination, destinations); err != nil {
			return nil, false, err
		}
	}
	if !callerProvidesWorkspace {
		destinations[rootfsMount.Destination] = struct{}{}
	}
	for _, mount := range normalizedRequestMounts {
		if err := validateMountDestination(mount.Destination, destinations); err != nil {
			return nil, false, err
		}
	}

	mounts := append([]model.Mount(nil), normalizedEnvMounts...)
	mounts = append(mounts, normalizedRequestMounts...)
	if !callerProvidesWorkspace {
		mounts = append(mounts, rootfsMount)
	}
	return mounts, callerProvidesWorkspace, nil
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
		normalizedSource := runtime.NormalizeMountSource(source)
		if _, err := os.Stat(normalizedSource); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w: mount source does not exist: %s", ErrValidation, normalizedSource)
			}
			return nil, fmt.Errorf("stat mount source %s: %w", normalizedSource, err)
		}
		normalized = append(normalized, model.Mount{
			Source:      normalizedSource,
			Destination: destination,
			ReadOnly:    mount.ReadOnly,
		})
	}
	return normalized, nil
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
