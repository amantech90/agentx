package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agentx/internal/model"
)

func TestStoreKeepsSameFolderForClaudeAndCodex(t *testing.T) {
	t.Parallel()

	store := New(filepath.Join(t.TempDir(), "AgentX", "config.json"))
	if _, err := store.LoadOrCreate("test-mac"); err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if _, err := store.AddWorkspace(model.Workspace{
		ID: "project-codex", RootPath: root, Name: "Project", ProviderID: "codex",
	}); err != nil {
		t.Fatalf("AddWorkspace(codex) error = %v", err)
	}
	data, err := store.AddWorkspace(model.Workspace{
		ID: "project-claude", RootPath: root, Name: "Project", ProviderID: "claude",
	})
	if err != nil {
		t.Fatalf("AddWorkspace(claude) error = %v", err)
	}
	if len(data.Workspaces) != 2 {
		t.Fatalf("workspace count = %d, want 2: %#v", len(data.Workspaces), data.Workspaces)
	}
}

func TestStoreRecoversLegacyProviderWorkspaceWithoutReplacingCurrentProvider(t *testing.T) {
	t.Parallel()

	store := New(filepath.Join(t.TempDir(), "AgentX", "config.json"))
	if _, err := store.LoadOrCreate("test-mac"); err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "project")
	metadata := filepath.Join(root, ".agentx")
	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	project, _ := json.Marshal(model.ProjectFile{Version: model.ConfigVersion, ID: "base-project", Name: "Project"})
	if err := os.WriteFile(filepath.Join(metadata, "project.json"), project, 0o600); err != nil {
		t.Fatalf("write project metadata: %v", err)
	}
	legacySession := []byte(`{"version":1,"workspaceId":"base-project","providerId":"claude","items":[]}`)
	if err := os.WriteFile(filepath.Join(metadata, "session.json"), legacySession, 0o600); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}
	if _, err := store.AddWorkspace(model.Workspace{
		ID: "base-project", RootPath: root, Name: "Project", ProviderID: "codex",
	}); err != nil {
		t.Fatalf("AddWorkspace() error = %v", err)
	}

	data, err := store.LoadOrCreate("test-mac")
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if len(data.Workspaces) != 2 {
		t.Fatalf("workspace count = %d, want recovered Claude and Codex: %#v", len(data.Workspaces), data.Workspaces)
	}
	if data.Workspaces[0].ID == data.Workspaces[1].ID {
		t.Fatalf("recovered providers share id %q", data.Workspaces[0].ID)
	}
}

func TestStorePersistsOnboarding(t *testing.T) {
	t.Parallel()

	store := New(filepath.Join(t.TempDir(), "AgentX", "config.json"))
	created, err := store.LoadOrCreate("aman-mac")
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if created.DeviceID == "" {
		t.Fatal("LoadOrCreate() did not create a device id")
	}
	if created.OnboardingComplete {
		t.Fatal("new config should require onboarding")
	}

	completed, err := store.CompleteOnboarding("aman-mac", model.OnboardingRequest{
		DeviceName:          "Aman’s MacBook",
		SelectedProviderIDs: []string{"codex", "codex", "claude"},
	})
	if err != nil {
		t.Fatalf("CompleteOnboarding() error = %v", err)
	}
	if !completed.OnboardingComplete {
		t.Fatal("onboarding was not persisted")
	}
	if got := len(completed.SelectedProviderIDs); got != 2 {
		t.Fatalf("selected providers length = %d, want 2", got)
	}

	reloaded, err := store.LoadOrCreate("changed-hostname")
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if reloaded.DeviceID != created.DeviceID {
		t.Fatalf("device id changed: got %q want %q", reloaded.DeviceID, created.DeviceID)
	}
	if reloaded.DeviceName != "Aman’s MacBook" {
		t.Fatalf("device name = %q", reloaded.DeviceName)
	}
}

func TestStoreRejectsLongDeviceName(t *testing.T) {
	t.Parallel()

	store := New(filepath.Join(t.TempDir(), "config.json"))
	if _, err := store.LoadOrCreate("host"); err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	longName := "1234567890123456789012345678901234567890123456789012345678901"
	if _, err := store.CompleteOnboarding("host", model.OnboardingRequest{DeviceName: longName}); err == nil {
		t.Fatal("CompleteOnboarding() accepted an overlong device name")
	}
}
