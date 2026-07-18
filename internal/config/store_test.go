package config

import (
	"path/filepath"
	"testing"

	"agentx/internal/model"
)

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
