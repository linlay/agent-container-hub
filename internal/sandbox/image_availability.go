package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agent-container-hub/internal/runtime"
)

type imageInspector interface {
	InspectImage(context.Context, string) (runtime.ImageInfo, error)
}

func inspectLocalImageAvailability(ctx context.Context, inspector imageInspector, imageRef string) (bool, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return false, nil
	}
	if inspector == nil {
		return false, fmt.Errorf("image inspector is required")
	}
	_, err := inspector.InspectImage(ctx, imageRef)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, runtime.ErrImageNotFound) {
		return false, nil
	}
	return false, err
}
