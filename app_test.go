package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentx/internal/bootstrap"
	"agentx/internal/config"
	"agentx/internal/model"
	"agentx/internal/provider"
	"agentx/internal/session"
	"agentx/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestOpenWorkspaceBrowsesOnlyAfterValidFormRequest(t *testing.T) {
	t.Parallel()

	app, deviceID := configuredTestApp(t)
	selectedFolder := t.TempDir()
	pickerCalled := false
	app.pickFolder = func(_ context.Context, options runtime.OpenDialogOptions) (string, error) {
		pickerCalled = true
		if options.Title != "Choose a folder for Booking API" {
			t.Fatalf("dialog title = %q", options.Title)
		}
		return selectedFolder, nil
	}

	state, err := app.OpenWorkspace(model.OpenWorkspaceRequest{
		Name:       "Booking API",
		ProviderID: "codex",
		DeviceID:   deviceID,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace() error = %v", err)
	}
	if !pickerCalled {
		t.Fatal("OpenWorkspace() did not open the folder picker")
	}
	if len(state.Workspaces) != 1 {
		t.Fatalf("workspace count = %d", len(state.Workspaces))
	}
	if state.Workspaces[0].Name != "Booking API" || state.Workspaces[0].RootPath != filepath.Clean(selectedFolder) {
		t.Fatalf("unexpected workspace: %#v", state.Workspaces[0])
	}
}

func TestOpenWorkspaceRejectsInvalidDeviceBeforeBrowsing(t *testing.T) {
	t.Parallel()

	app, _ := configuredTestApp(t)
	app.pickFolder = func(context.Context, runtime.OpenDialogOptions) (string, error) {
		t.Fatal("folder picker opened for an invalid device")
		return "", errors.New("unreachable")
	}

	_, err := app.OpenWorkspace(model.OpenWorkspaceRequest{
		Name:       "Booking API",
		ProviderID: "codex",
		DeviceID:   "another-device",
	})
	if err == nil {
		t.Fatal("OpenWorkspace() accepted an invalid device")
	}
}

type appTestRunner struct{}

func (appTestRunner) Run(_ context.Context, _ session.RunRequest, callbacks session.RunCallbacks) (session.RunResult, error) {
	callbacks.Emit(session.Event{ID: "answer", Kind: "message", Role: "assistant", Content: "Done"})
	return session.RunResult{ProviderSessionID: "provider-session"}, nil
}

func TestSendMessageQueuesARealWorkspaceSession(t *testing.T) {
	t.Parallel()

	app, deviceID := configuredTestApp(t)
	selectedFolder := t.TempDir()
	app.pickFolder = func(context.Context, runtime.OpenDialogOptions) (string, error) {
		return selectedFolder, nil
	}
	state, err := app.OpenWorkspace(model.OpenWorkspaceRequest{
		Name: "Booking API", ProviderID: "codex", DeviceID: deviceID,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace() error = %v", err)
	}
	workspaceID := state.Workspaces[0].ID
	if _, err := app.SendMessage(model.SendMessageRequest{WorkspaceID: workspaceID, Content: "Run the tests"}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, err := app.GetSession(workspaceID)
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}
		if snapshot.Status == "idle" && len(snapshot.Items) == 2 {
			if snapshot.ProviderSessionID != "provider-session" {
				t.Fatalf("provider session id = %q", snapshot.ProviderSessionID)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not finish: %#v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDeleteConversationClearsChatWithoutDeletingProject(t *testing.T) {
	t.Parallel()

	app, deviceID := configuredTestApp(t)
	selectedFolder := t.TempDir()
	projectFile := filepath.Join(selectedFolder, "main.go")
	if err := os.WriteFile(projectFile, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write project file: %v", err)
	}
	app.pickFolder = func(context.Context, runtime.OpenDialogOptions) (string, error) {
		return selectedFolder, nil
	}
	state, err := app.OpenWorkspace(model.OpenWorkspaceRequest{
		Name: "Booking API", ProviderID: "codex", DeviceID: deviceID,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace() error = %v", err)
	}
	workspaceID := state.Workspaces[0].ID
	if _, err := app.SendMessage(model.SendMessageRequest{WorkspaceID: workspaceID, Content: "Inspect the project"}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, snapshotErr := app.GetSession(workspaceID)
		if snapshotErr != nil {
			t.Fatalf("GetSession() error = %v", snapshotErr)
		}
		if snapshot.Status == "idle" && len(snapshot.Items) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not finish: %#v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}

	deleted, err := app.DeleteConversation(workspaceID)
	if err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}
	if len(deleted.Items) != 0 || deleted.ProviderSessionID != "" {
		t.Fatalf("deleted session = %#v", deleted)
	}
	if _, err := os.Stat(projectFile); err != nil {
		t.Fatalf("project file was deleted: %v", err)
	}
}

func TestLocalBridgeStateIncludesWorkspacesProvidersAndSessions(t *testing.T) {
	t.Parallel()

	app, deviceID := configuredTestApp(t)
	selectedFolder := t.TempDir()
	app.pickFolder = func(context.Context, runtime.OpenDialogOptions) (string, error) {
		return selectedFolder, nil
	}
	state, err := app.OpenWorkspace(model.OpenWorkspaceRequest{
		Name: "Shared Project", ProviderID: "codex", DeviceID: deviceID,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace() error = %v", err)
	}
	workspaceID := state.Workspaces[0].ID
	if _, err := app.SendMessage(model.SendMessageRequest{WorkspaceID: workspaceID, Content: "Inspect it"}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot, snapshotErr := app.GetSession(workspaceID)
		if snapshotErr != nil {
			t.Fatalf("GetSession() error = %v", snapshotErr)
		}
		if snapshot.Status == "idle" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not finish: %#v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}

	bridgeState, err := app.localBridgeState(context.Background())
	if err != nil {
		t.Fatalf("localBridgeState() error = %v", err)
	}
	if bridgeState.Device.ID != deviceID || !bridgeState.Online {
		t.Fatalf("bridge device = %#v", bridgeState.Device)
	}
	if len(bridgeState.Workspaces) != 1 || bridgeState.Workspaces[0].ID != workspaceID {
		t.Fatalf("bridge workspaces = %#v", bridgeState.Workspaces)
	}
	if len(bridgeState.Sessions) != 1 || bridgeState.Sessions[0].WorkspaceID != workspaceID {
		t.Fatalf("bridge sessions = %#v", bridgeState.Sessions)
	}
	if len(bridgeState.Providers) == 0 || len(bridgeState.SelectedProviderIDs) != 2 {
		t.Fatalf("bridge providers = %#v, selected = %#v", bridgeState.Providers, bridgeState.SelectedProviderIDs)
	}
}

func configuredTestApp(t *testing.T) (*App, string) {
	t.Helper()

	store := config.New(filepath.Join(t.TempDir(), "config.json"))
	data, err := store.LoadOrCreate("Test-Mac")
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if _, err := store.CompleteOnboarding("Test-Mac", model.OnboardingRequest{
		DeviceName:          "Test Mac",
		SelectedProviderIDs: []string{"claude", "codex"},
	}); err != nil {
		t.Fatalf("CompleteOnboarding() error = %v", err)
	}

	detector := provider.NewDetectorForTest(
		func(command string) (string, error) { return "/usr/local/bin/" + command, nil },
		func(context.Context, string) string { return "test-version" },
	)
	sessions := session.NewManager(map[string]session.Runner{
		"claude": appTestRunner{},
		"codex":  appTestRunner{},
	})
	t.Cleanup(sessions.Close)
	return &App{
		ctx:        context.Background(),
		bootstrap:  bootstrap.NewService(store, detector),
		workspaces: workspace.NewManager(),
		sessions:   sessions,
	}, data.DeviceID
}
