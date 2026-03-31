package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agent-container-hub/internal/runtime"
)

type imageInspector interface {
	InspectImage(context.Context, string) (runtime.ImageInfo, error)
}

const (
	imageInspectMaxAttempts = 3
	imageInspectBackoff1    = 150 * time.Millisecond
	imageInspectBackoff2    = 300 * time.Millisecond
	imageInspectDetachTimeout = 5 * time.Second
)

func inspectLocalImageAvailability(ctx context.Context, inspector imageInspector, imageRef string, logger *slog.Logger) (bool, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return false, nil
	}
	if inspector == nil {
		return false, fmt.Errorf("image inspector is required")
	}

	backoffs := []time.Duration{imageInspectBackoff1, imageInspectBackoff2}
	var lastErr error

	for attempt := 0; attempt < imageInspectMaxAttempts; attempt++ {
		inspectCtx := ctx
		if ctx.Err() != nil {
			detached, cancel := context.WithTimeout(context.Background(), imageInspectDetachTimeout)
			defer cancel()
			inspectCtx = detached
		}

		_, err := inspector.InspectImage(inspectCtx, imageRef)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, runtime.ErrImageNotFound) {
			return false, nil
		}

		lastErr = err
		if attempt < imageInspectMaxAttempts-1 {
			if logger != nil {
				logger.Warn("image inspect transient failure, retrying",
					"image", imageRef,
					"attempt", attempt+1,
					"error", err,
				)
			}
			time.Sleep(backoffs[attempt])
		}
	}

	if logger != nil {
		logger.Error("image inspect failed after retries",
			"image", imageRef,
			"attempts", imageInspectMaxAttempts,
			"error", lastErr,
		)
	}
	return false, lastErr
}
