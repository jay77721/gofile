package job

import (
	"context"
	"testing"
	"time"
)

func TestManagerLifecycleIsIdempotent(t *testing.T) {
	m := NewManager("", nil, nil, nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.Start(ctx)
	m.Start(ctx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestNilManagerIsSafe(t *testing.T) {
	var m *Manager
	m.Start(context.Background())
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("nil manager stop: %v", err)
	}
}
