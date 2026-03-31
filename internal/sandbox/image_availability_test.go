package sandbox

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"agent-container-hub/internal/runtime"
)

type countingInspector struct {
	calls   atomic.Int32
	results []inspectResult
}

type inspectResult struct {
	info runtime.ImageInfo
	err  error
}

func (m *countingInspector) InspectImage(_ context.Context, _ string) (runtime.ImageInfo, error) {
	idx := int(m.calls.Add(1)) - 1
	if idx >= len(m.results) {
		return runtime.ImageInfo{}, fmt.Errorf("unexpected call %d", idx)
	}
	return m.results[idx].info, m.results[idx].err
}

func TestInspectLocalImageAvailability_Success(t *testing.T) {
	t.Parallel()
	inspector := &countingInspector{
		results: []inspectResult{
			{info: runtime.ImageInfo{ID: "sha256:abc"}},
		},
	}
	available, err := inspectLocalImageAvailability(context.Background(), inspector, "test:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Fatal("expected available=true")
	}
	if inspector.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", inspector.calls.Load())
	}
}

func TestInspectLocalImageAvailability_ImageNotFoundNoRetry(t *testing.T) {
	t.Parallel()
	inspector := &countingInspector{
		results: []inspectResult{
			{err: runtime.ErrImageNotFound},
		},
	}
	available, err := inspectLocalImageAvailability(context.Background(), inspector, "test:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available {
		t.Fatal("expected available=false")
	}
	if inspector.calls.Load() != 1 {
		t.Fatalf("expected 1 call (no retry for ErrImageNotFound), got %d", inspector.calls.Load())
	}
}

func TestInspectLocalImageAvailability_TransientRetrySuccess(t *testing.T) {
	t.Parallel()
	transientErr := fmt.Errorf("connection refused")
	inspector := &countingInspector{
		results: []inspectResult{
			{err: transientErr},
			{err: transientErr},
			{info: runtime.ImageInfo{ID: "sha256:abc"}},
		},
	}
	available, err := inspectLocalImageAvailability(context.Background(), inspector, "test:latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Fatal("expected available=true after retry")
	}
	if inspector.calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", inspector.calls.Load())
	}
}

func TestInspectLocalImageAvailability_RetryExhausted(t *testing.T) {
	t.Parallel()
	transientErr := fmt.Errorf("connection refused")
	inspector := &countingInspector{
		results: []inspectResult{
			{err: transientErr},
			{err: transientErr},
			{err: transientErr},
		},
	}
	available, err := inspectLocalImageAvailability(context.Background(), inspector, "test:latest", nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if available {
		t.Fatal("expected available=false")
	}
	if inspector.calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", inspector.calls.Load())
	}
}

func TestInspectLocalImageAvailability_EmptyRef(t *testing.T) {
	t.Parallel()
	inspector := &countingInspector{}
	available, err := inspectLocalImageAvailability(context.Background(), inspector, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available {
		t.Fatal("expected available=false for empty ref")
	}
	if inspector.calls.Load() != 0 {
		t.Fatalf("expected 0 calls for empty ref, got %d", inspector.calls.Load())
	}
}

func TestInspectLocalImageAvailability_NilInspector(t *testing.T) {
	t.Parallel()
	_, err := inspectLocalImageAvailability(context.Background(), nil, "test:latest", nil)
	if err == nil {
		t.Fatal("expected error for nil inspector")
	}
}
