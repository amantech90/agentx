package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentx/internal/model"
)

func TestManagerCreatesAndReopensWorkspaceMetadata(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "Booking API")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	manager := NewManager()
	manager.now = func() time.Time { return time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC) }
	first, err := manager.Open(root, "Booking API", "codex")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if first.Name != "Booking API" || first.ProviderID != "codex" || first.ID == "" {
		t.Fatalf("unexpected workspace: %#v", first)
	}

	metadataPath := filepath.Join(root, metadataDirectory, metadataFilename)
	contents, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var project model.ProjectFile
	if err := json.Unmarshal(contents, &project); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if project.ID != first.ID {
		t.Fatalf("project id = %q, want %q", project.ID, first.ID)
	}

	manager.now = func() time.Time { return time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC) }
	second, err := manager.Open(root, "Booking Platform", "claude")
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("reopen created a new id: got %q want %q", second.ID, first.ID)
	}
	if second.ProviderID != "claude" {
		t.Fatalf("provider id = %q", second.ProviderID)
	}
	if second.Name != "Booking Platform" {
		t.Fatalf("workspace name = %q", second.Name)
	}
}

func TestManagerRejectsUnsupportedProvider(t *testing.T) {
	t.Parallel()

	if _, err := NewManager().Open(t.TempDir(), "Project", "gemini"); err == nil {
		t.Fatal("Open() accepted unsupported provider")
	}
}

func TestManagerRejectsBlankOrLongProjectName(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	if _, err := manager.Open(t.TempDir(), "  ", "codex"); err == nil {
		t.Fatal("Open() accepted a blank project name")
	}
	if _, err := manager.Open(t.TempDir(), string(make([]rune, 81)), "codex"); err == nil {
		t.Fatal("Open() accepted a project name over 80 characters")
	}
}
