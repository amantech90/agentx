package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"agentx/internal/model"
)

func TestManagerAcceptsScreenshotOnlyAndPersistsItForProvider(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{
		ID: "workspace-screenshot", RootPath: t.TempDir(), ProviderID: "codex",
	}
	png := []byte("\x89PNG\r\n\x1a\nagent-x-test")
	queued, err := manager.EnqueueMessage(workspace, "/tools/codex", MessageInput{
		Screenshot: &model.ScreenshotInput{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(png)},
	})
	if err != nil {
		t.Fatalf("EnqueueMessage() error = %v", err)
	}
	if len(queued.Items) != 1 || len(queued.Items[0].Screenshots) != 1 {
		t.Fatalf("queued items = %#v", queued.Items)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		runner.mu.Lock()
		if len(runner.requests) > 0 {
			request := runner.requests[0]
			runner.mu.Unlock()
			if request.Prompt != "" || len(request.ScreenshotPaths) != 1 {
				t.Fatalf("runner request = %#v", request)
			}
			contents, readErr := os.ReadFile(request.ScreenshotPaths[0])
			if readErr != nil {
				t.Fatalf("read persisted screenshot: %v", readErr)
			}
			if string(contents) != string(png) {
				t.Fatalf("persisted screenshot = %q", contents)
			}
			break
		}
		runner.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("runner did not receive screenshot")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitForManagerIdle(t, manager, workspace, deadline)
}

func TestManagerKeepsSlashCommandInChatWhileSendingExpandedPrompt(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{ID: "workspace-command", RootPath: t.TempDir(), ProviderID: "codex"}
	queued, err := manager.EnqueueMessage(workspace, "/tools/codex", MessageInput{
		Prompt:        "Review the current workspace changes for authentication bugs.",
		DisplayPrompt: "/review authentication",
	})
	if err != nil {
		t.Fatalf("EnqueueMessage() error = %v", err)
	}
	if len(queued.Items) != 1 || queued.Items[0].Content != "/review authentication" {
		t.Fatalf("queued items = %#v", queued.Items)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		runner.mu.Lock()
		if len(runner.requests) > 0 {
			request := runner.requests[0]
			runner.mu.Unlock()
			if request.Prompt != "Review the current workspace changes for authentication bugs." {
				t.Fatalf("runner prompt = %q", request.Prompt)
			}
			break
		}
		runner.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("runner did not receive command prompt")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitForManagerIdle(t, manager, workspace, deadline)
}

func waitForManagerIdle(t *testing.T, manager *Manager, workspace model.Workspace, deadline time.Time) {
	t.Helper()
	for {
		snapshot, err := manager.Snapshot(workspace)
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		if snapshot.Status == "idle" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not become idle: %#v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManagerRejectsInvalidScreenshotData(t *testing.T) {
	t.Parallel()

	manager := NewManager(map[string]Runner{"codex": &recordingRunner{}})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{ID: "workspace-invalid-image", RootPath: t.TempDir(), ProviderID: "codex"}
	_, err := manager.EnqueueMessage(workspace, "/tools/codex", MessageInput{
		Screenshot: &model.ScreenshotInput{MediaType: "image/png", Data: "not-base64"},
	})
	if err == nil {
		t.Fatal("EnqueueMessage() accepted invalid screenshot data")
	}
}

type recordingRunner struct {
	mu       sync.Mutex
	requests []RunRequest
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, RunRequest, RunCallbacks) (RunResult, error) {
	return RunResult{}, errors.New("authentication required")
}

func TestManagerKeepsFailedTurnVisible(t *testing.T) {
	t.Parallel()

	manager := NewManager(map[string]Runner{"claude": failingRunner{}})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{
		ID: "workspace-error", RootPath: t.TempDir(), ProviderID: "claude",
	}
	if _, err := manager.Enqueue(workspace, "/tools/claude", "Explain this project"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	snapshot, err := manager.Snapshot(workspace)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Status != "error" {
		t.Fatalf("status = %q, want error", snapshot.Status)
	}
	if len(snapshot.Items) != 2 || snapshot.Items[1].Kind != "error" {
		t.Fatalf("items = %#v", snapshot.Items)
	}
}

func (r *recordingRunner) Run(_ context.Context, request RunRequest, callbacks RunCallbacks) (RunResult, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	count := len(r.requests)
	r.mu.Unlock()

	callbacks.Emit(Event{ID: "assistant", Kind: "message", Role: "assistant", Content: "response"})
	return RunResult{ProviderSessionID: "session-" + string(rune('0'+count))}, nil
}

type approvalRunner struct {
	requested chan struct{}
	decisions chan ApprovalDecision
}

func (r *approvalRunner) Run(ctx context.Context, _ RunRequest, callbacks RunCallbacks) (RunResult, error) {
	decision, err := callbacks.RequestApproval(ctx, ApprovalRequest{
		Kind: "command", Tool: "Bash", Title: "Run tests", Command: "go test ./...",
		WorkingDirectory: "/workspace", Reason: "The test suite executes project code.",
	})
	if err != nil {
		return RunResult{}, err
	}
	r.decisions <- decision
	return RunResult{ProviderSessionID: "approval-session"}, nil
}

func TestManagerBlocksAProviderUntilApprovalIsResolved(t *testing.T) {
	t.Parallel()

	runner := &approvalRunner{requested: make(chan struct{}), decisions: make(chan ApprovalDecision, 1)}
	manager := NewManager(map[string]Runner{"claude": runner})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{ID: "workspace-approval", RootPath: t.TempDir(), ProviderID: "claude"}
	if _, err := manager.Enqueue(workspace, "/tools/claude", "Run the tests", ClaudePermissionDefault); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	var approval model.ChatItem
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, err := manager.Snapshot(workspace)
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		for _, item := range snapshot.Items {
			if item.Kind == "approval" {
				approval = item
			}
		}
		if approval.ID != "" {
			if snapshot.Status != "waiting" || approval.Status != "pending" || approval.Approval == nil {
				t.Fatalf("pending approval snapshot = %#v", snapshot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approval request did not become visible")
		}
		time.Sleep(5 * time.Millisecond)
	}

	resolved, err := manager.ResolveApproval(workspace, approval.ID, string(ApprovalAllow))
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if resolved.Status != "running" || approvalStatus(resolved.Items, approval.ID) != "approved" {
		t.Fatalf("resolved snapshot = %#v", resolved)
	}
	select {
	case decision := <-runner.decisions:
		if decision != ApprovalAllow {
			t.Fatalf("provider decision = %q, want allow", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("provider did not resume after approval")
	}
	waitForManagerIdle(t, manager, workspace, deadline)
}

func TestManagerRejectsWrongWorkspaceAndReusedApprovalDecision(t *testing.T) {
	t.Parallel()

	runner := &approvalRunner{requested: make(chan struct{}), decisions: make(chan ApprovalDecision, 1)}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{ID: "workspace-one-use", RootPath: t.TempDir(), ProviderID: "codex"}
	if _, err := manager.Enqueue(workspace, "/tools/codex", "Run tests"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	approvalID := waitForPendingApproval(t, manager, workspace)

	other := model.Workspace{ID: "other-workspace", RootPath: t.TempDir(), ProviderID: "codex"}
	if _, err := manager.ResolveApproval(other, approvalID, string(ApprovalAllow)); err == nil {
		t.Fatal("wrong workspace resolved an approval")
	}
	if _, err := manager.ResolveApproval(workspace, approvalID, string(ApprovalDeny)); err != nil {
		t.Fatalf("ResolveApproval(deny) error = %v", err)
	}
	if _, err := manager.ResolveApproval(workspace, approvalID, string(ApprovalAllow)); err == nil {
		t.Fatal("approval accepted a second decision")
	}
	select {
	case decision := <-runner.decisions:
		if decision != ApprovalDeny {
			t.Fatalf("provider decision = %q, want deny", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("provider did not receive denial")
	}
	waitForManagerIdle(t, manager, workspace, time.Now().Add(2*time.Second))
}

func waitForPendingApproval(t *testing.T, manager *Manager, workspace model.Workspace) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, err := manager.Snapshot(workspace)
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		for _, item := range snapshot.Items {
			if item.Kind == "approval" && item.Status == "pending" {
				return item.ID
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("approval did not become pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func approvalStatus(items []model.ChatItem, id string) string {
	for _, item := range items {
		if item.ID == id {
			return item.Status
		}
	}
	return ""
}

func TestLoadSessionCancelsApprovalLeftPendingByAStoppedProcess(t *testing.T) {
	t.Parallel()

	workspace := model.Workspace{ID: "workspace-stale-approval", RootPath: t.TempDir(), ProviderID: "claude"}
	if err := persistSession(workspace, model.SessionSnapshot{
		WorkspaceID: workspace.ID,
		ProviderID:  workspace.ProviderID,
		Items: []model.ChatItem{{
			ID: "stale", Kind: "approval", Role: "system", Status: "pending",
			Approval: &model.ToolApproval{Kind: "command", Command: "go test ./..."},
		}},
	}); err != nil {
		t.Fatalf("persistSession() error = %v", err)
	}
	snapshot, err := loadSession(workspace)
	if err != nil {
		t.Fatalf("loadSession() error = %v", err)
	}
	if approvalStatus(snapshot.Items, "stale") != "cancelled" {
		t.Fatalf("stale approval remained actionable: %#v", snapshot.Items)
	}
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
			if snapshot.Items[0].TurnID == "" || snapshot.Items[1].TurnID != snapshot.Items[0].TurnID {
				t.Fatalf("turn ids were not associated: %#v", snapshot.Items)
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
	if _, err := os.Stat(sessionPath(workspace)); err != nil {
		t.Fatalf("session file was not persisted: %v", err)
	}
	runner.mu.Lock()
	firstRequest := runner.requests[0]
	runner.mu.Unlock()
	if !firstRequest.ResumeLatest {
		t.Fatal("first request did not adopt the latest provider session")
	}
}

func TestManagerScopesReusedProviderEventIDsToEachTurn(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{
		ID: "workspace-reused-events", RootPath: t.TempDir(), ProviderID: "codex",
	}

	for _, prompt := range []string{"First turn", "Second turn"} {
		if _, err := manager.Enqueue(workspace, "/tools/codex", prompt); err != nil {
			t.Fatalf("Enqueue(%q) error = %v", prompt, err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			snapshot, err := manager.Snapshot(workspace)
			if err != nil {
				t.Fatalf("Snapshot() error = %v", err)
			}
			if snapshot.Status == "idle" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("turn %q did not finish: %#v", prompt, snapshot)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	snapshot, err := manager.Snapshot(workspace)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Items) != 4 {
		t.Fatalf("items = %#v, want two user messages and two assistant messages", snapshot.Items)
	}
	if snapshot.Items[1].TurnID == "" || snapshot.Items[3].TurnID == "" || snapshot.Items[1].TurnID == snapshot.Items[3].TurnID {
		t.Fatalf("assistant events were not scoped to separate turns: %#v", snapshot.Items)
	}
}

func TestDeleteConversationStartsFreshAndPreservesProjectFiles(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	root := t.TempDir()
	workspace := model.Workspace{ID: "workspace-delete", RootPath: root, ProviderID: "codex"}
	projectFile := filepath.Join(root, "important.go")
	if err := os.WriteFile(projectFile, []byte("package important\n"), 0o600); err != nil {
		t.Fatalf("write project file: %v", err)
	}

	snapshot, err := manager.DeleteConversation(workspace)
	if err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}
	if snapshot.ProviderSessionID != "" || len(snapshot.Items) != 0 {
		t.Fatalf("deleted snapshot = %#v", snapshot)
	}
	if _, err := os.Stat(projectFile); err != nil {
		t.Fatalf("project file was touched: %v", err)
	}
	if _, err := manager.Enqueue(workspace, "/tools/codex", "Start again"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		runner.mu.Lock()
		if len(runner.requests) > 0 {
			request := runner.requests[0]
			runner.mu.Unlock()
			if request.ResumeLatest {
				t.Fatal("request resumed after conversation deletion")
			}
			break
		}
		runner.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("runner did not receive request")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		snapshot, err := manager.Snapshot(workspace)
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		if snapshot.Status == "idle" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not finish: %#v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIdleSnapshotMeansConversationCanBeDeleted(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	manager := NewManager(map[string]Runner{"codex": runner})
	t.Cleanup(manager.Close)
	workspace := model.Workspace{
		ID: "workspace-idle-delete", RootPath: t.TempDir(), ProviderID: "codex",
	}
	deleteResult := make(chan error, 1)
	manager.SetEmitter(func(snapshot model.SessionSnapshot) {
		if snapshot.Status != "idle" || snapshot.ProviderSessionID == "" {
			return
		}
		_, err := manager.DeleteConversation(workspace)
		deleteResult <- err
	})
	if _, err := manager.Enqueue(workspace, "/tools/codex", "Finish the work"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	select {
	case err := <-deleteResult:
		if err != nil {
			t.Fatalf("DeleteConversation() after idle snapshot error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manager did not emit a completed idle snapshot")
	}
}

func TestProviderSessionsInSameProjectUseDifferentFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	claude := model.Workspace{ID: "project-claude", ProjectID: "project", RootPath: root, ProviderID: "claude"}
	codex := model.Workspace{ID: "project-codex", ProjectID: "project", RootPath: root, ProviderID: "codex"}
	if sessionPath(claude) == sessionPath(codex) {
		t.Fatalf("providers share session path %q", sessionPath(claude))
	}
	if err := persistSession(claude, model.SessionSnapshot{
		WorkspaceID: claude.ID, ProviderID: claude.ProviderID, Status: "idle", Items: []model.ChatItem{},
	}); err != nil {
		t.Fatalf("persistSession(claude) error = %v", err)
	}
	if err := persistSession(codex, model.SessionSnapshot{
		WorkspaceID: codex.ID, ProviderID: codex.ProviderID, Status: "idle", Items: []model.ChatItem{},
	}); err != nil {
		t.Fatalf("persistSession(codex) error = %v", err)
	}
	if _, err := os.Stat(sessionPath(claude)); err != nil {
		t.Fatalf("Claude session missing: %v", err)
	}
	if _, err := os.Stat(sessionPath(codex)); err != nil {
		t.Fatalf("Codex session missing: %v", err)
	}
}

func TestLegacySessionIsCopiedIntoMatchingProviderSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspace := model.Workspace{
		ID: model.ProviderWorkspaceID("base-project", "claude"), ProjectID: "base-project",
		RootPath: root, ProviderID: "claude",
	}
	stored := sessionFile{
		Version: sessionFileVersion, WorkspaceID: "base-project", ProviderID: "claude",
		ProviderSessionID: "claude-session", Items: []model.ChatItem{{ID: "answer", Kind: "message", Role: "assistant", Content: "Existing history"}},
	}
	contents, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacySessionPath(workspace)), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(legacySessionPath(workspace), contents, 0o600); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	snapshot, err := loadSession(workspace)
	if err != nil {
		t.Fatalf("loadSession() error = %v", err)
	}
	if snapshot.ProviderSessionID != "claude-session" || len(snapshot.Items) != 1 {
		t.Fatalf("migrated snapshot = %#v", snapshot)
	}
	if _, err := os.Stat(sessionPath(workspace)); err != nil {
		t.Fatalf("provider session was not created: %v", err)
	}
	if _, err := os.Stat(legacySessionPath(workspace)); err != nil {
		t.Fatalf("legacy session should remain recoverable: %v", err)
	}
}
