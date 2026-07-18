package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"agentx/internal/bootstrap"
	"agentx/internal/config"
	"agentx/internal/model"
	"agentx/internal/provider"
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
	return &App{
		ctx:        context.Background(),
		bootstrap:  bootstrap.NewService(store, detector),
		workspaces: workspace.NewManager(),
	}, data.DeviceID
}
