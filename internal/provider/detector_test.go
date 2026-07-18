package provider

import (
	"context"
	"errors"
	"testing"
)

func TestDetectorReportsInstalledAndComingSoonProviders(t *testing.T) {
	t.Parallel()

	detector := NewDetectorForTest(
		func(command string) (string, error) {
			if command == "codex" || command == "gemini" {
				return "/tools/" + command, nil
			}
			return "", errors.New("not found")
		},
		func(_ context.Context, path string) string { return path + " 1.0" },
	)

	providers := detector.Detect(context.Background())
	if len(providers) != 3 {
		t.Fatalf("providers length = %d, want 3", len(providers))
	}
	if providers[0].Installed {
		t.Fatal("Claude should be missing")
	}
	if !providers[1].Installed || !providers[1].Supported {
		t.Fatal("Codex should be installed and supported")
	}
	if !providers[2].Installed || !providers[2].ComingSoon || providers[2].Supported {
		t.Fatal("Gemini should be installed but marked coming soon")
	}
}
