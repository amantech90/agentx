package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"agentx/internal/model"
)

type recordingRunner struct {
	mu       sync.Mutex
	requests []RunRequest
}

func (r *recordingRunner) Run(_ context.Context, request RunRequest, emit func(Event)) (RunResult, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	count := len(r.requests)
	r.mu.Unlock()

	emit(Event{ID: "assistant", Kind: "message", Role: "assistant", Content: "response"})
	return RunResult{ProviderSessionID: "session-" + string(rune('0'+count))}, nil
}

func TestManagerQueuesRunsAndPersistsAResumableSession(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	manager.now = func() time.Time { return time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC) }

	workspace := model.Workspace{
		ID:         "workspace-1",
		Name:       "Agent X",
		RootPath:   t.TempDir(),
		ProviderID: "codex",
	}
	if _, err := manager.Enqueue(workspace, "/tools/codex", "Run the tests"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, err := manager.Snapshot(workspace)
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		if snapshot.Status == "idle" && len(snapshot.Items) == 2 {
			if snapshot.ProviderSessionID != "session-1" {
				t.Fatalf("provider session id = %q", snapshot.ProviderSessionID)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not finish: %#v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}

	reopened := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(reopened.Close)
	snapshot, err := reopened.Snapshot(workspace)
	if err != nil {
		t.Fatalf("reopened Snapshot() error = %v", err)
	}
	if snapshot.ProviderSessionID != "session-1" || len(snapshot.Items) != 2 {
		t.Fatalf("reopened snapshot = %#v", snapshot)
	}
	if got := filepath.Join(workspace.RootPath, ".agentx", "session.json"); got == "" {
		t.Fatal("unreachable")
	}
}
