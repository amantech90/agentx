package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentx/internal/fsx"
	"agentx/internal/model"
)

const metadataDirectory = ".agentx"
const metadataFilename = "project.json"

type Manager struct {
	now func() time.Time
}

func NewManager() *Manager {
	return &Manager{now: time.Now}
}

func (m *Manager) Open(rootPath, name, providerID string) (model.Workspace, error) {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	if rootPath == "." || rootPath == "" {
		return model.Workspace{}, errors.New("workspace path is required")
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return model.Workspace{}, fmt.Errorf("inspect workspace directory: %w", err)
	}
	if !info.IsDir() {
		return model.Workspace{}, errors.New("workspace path must be a directory")
	}
	if providerID != "claude" && providerID != "codex" {
		return model.Workspace{}, errors.New("select an available Claude or Codex provider")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Workspace{}, errors.New("project name is required")
	}
	if len([]rune(name)) > 80 {
		return model.Workspace{}, errors.New("project name must be 80 characters or fewer")
	}

	metadataPath := filepath.Join(rootPath, metadataDirectory, metadataFilename)
	project, err := readProject(metadataPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.Workspace{}, err
	}
	if errors.Is(err, os.ErrNotExist) {
		id, idErr := newID()
		if idErr != nil {
			return model.Workspace{}, fmt.Errorf("create workspace id: %w", idErr)
		}
		now := m.now().UTC()
		project = model.ProjectFile{
			Version:    model.ConfigVersion,
			ID:         id,
			Name:       name,
			ProviderID: providerID,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	} else {
		project.Name = name
		project.UpdatedAt = m.now().UTC()
	}

	if err := writeProject(metadataPath, project); err != nil {
		return model.Workspace{}, err
	}
	return model.Workspace{
		ID:         model.ProviderWorkspaceID(project.ID, providerID),
		ProjectID:  project.ID,
		Name:       project.Name,
		RootPath:   rootPath,
		ProviderID: providerID,
		CreatedAt:  project.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:  project.UpdatedAt.Format(time.RFC3339Nano),
	}, nil
}

func readProject(path string) (model.ProjectFile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	var project model.ProjectFile
	if err := json.Unmarshal(contents, &project); err != nil {
		return model.ProjectFile{}, fmt.Errorf("decode workspace metadata: %w", err)
	}
	if project.Version != model.ConfigVersion || project.ID == "" || project.Name == "" {
		return model.ProjectFile{}, errors.New("workspace metadata is invalid or unsupported")
	}
	return project, nil
}

func writeProject(path string, project model.ProjectFile) error {
	contents, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace metadata: %w", err)
	}
	contents = append(contents, '\n')

	if err := fsx.WriteFileAtomically(path, contents); err != nil {
		return fmt.Errorf("save workspace metadata: %w", err)
	}
	return nil
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
