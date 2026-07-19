package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"agentx/internal/fsx"
	"agentx/internal/model"
)

type Data struct {
	Version             int               `json:"version"`
	DeviceID            string            `json:"deviceId"`
	DeviceName          string            `json:"deviceName"`
	OnboardingComplete  bool              `json:"onboardingComplete"`
	SelectedProviderIDs []string          `json:"selectedProviderIds"`
	Workspaces          []model.Workspace `json:"workspaces"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "AgentX", "config.json"), nil
}

func New(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) LoadOrCreate(hostname string) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Data{}, err
	}

	deviceID, err := newID()
	if err != nil {
		return Data{}, fmt.Errorf("create device id: %w", err)
	}
	data = Data{
		Version:    model.ConfigVersion,
		DeviceID:   deviceID,
		DeviceName: defaultDeviceName(hostname),
		Workspaces: []model.Workspace{},
	}
	if err := s.save(data); err != nil {
		return Data{}, err
	}
	return data, nil
}

func (s *Store) CompleteOnboarding(hostname string, request model.OnboardingRequest) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return Data{}, err
	}

	name := strings.TrimSpace(request.DeviceName)
	if name == "" {
		name = defaultDeviceName(hostname)
	}
	if len([]rune(name)) > 60 {
		return Data{}, errors.New("device name must be 60 characters or fewer")
	}

	data.DeviceName = name
	data.OnboardingComplete = true
	data.SelectedProviderIDs = uniqueSorted(request.SelectedProviderIDs)
	if err := s.save(data); err != nil {
		return Data{}, err
	}
	return data, nil
}

func (s *Store) AddWorkspace(workspace model.Workspace) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return Data{}, err
	}
	workspace = normalizeWorkspaceIdentity(workspace)

	found := false
	for index, existing := range data.Workspaces {
		sameProviderFolder := existing.ProviderID == workspace.ProviderID && filepath.Clean(existing.RootPath) == filepath.Clean(workspace.RootPath)
		if existing.ID == workspace.ID || sameProviderFolder {
			data.Workspaces[index] = workspace
			found = true
			break
		}
	}
	if !found {
		data.Workspaces = append(data.Workspaces, workspace)
	}
	sort.Slice(data.Workspaces, func(i, j int) bool {
		return data.Workspaces[i].UpdatedAt > data.Workspaces[j].UpdatedAt
	})

	if err := s.save(data); err != nil {
		return Data{}, err
	}
	return data, nil
}

func (s *Store) load() (Data, error) {
	contents, err := os.ReadFile(s.path)
	if err != nil {
		return Data{}, fmt.Errorf("read Agent X config: %w", err)
	}

	var data Data
	if err := json.Unmarshal(contents, &data); err != nil {
		return Data{}, fmt.Errorf("decode Agent X config: %w", err)
	}
	if data.Version != model.ConfigVersion {
		return Data{}, fmt.Errorf("unsupported Agent X config version %d", data.Version)
	}
	if data.Workspaces == nil {
		data.Workspaces = []model.Workspace{}
	}
	data.Workspaces = normalizeWorkspaceIdentities(data.Workspaces)
	return data, nil
}

func normalizeWorkspaceIdentities(workspaces []model.Workspace) []model.Workspace {
	result := make([]model.Workspace, 0, len(workspaces)+1)
	for _, workspace := range workspaces {
		workspace = normalizeWorkspaceIdentity(workspace)
		result = upsertProviderWorkspace(result, workspace)
	}

	// V1 stored one shared session.json. If the folder was later opened with a
	// different provider, recover the original provider as a separate workspace
	// instead of allowing either history to be overwritten.
	for _, workspace := range append([]model.Workspace(nil), result...) {
		legacyProvider := legacySessionProvider(workspace.RootPath)
		if legacyProvider == "" || legacyProvider == workspace.ProviderID {
			continue
		}
		recovered := workspace
		recovered.ProviderID = legacyProvider
		recovered.ID = model.ProviderWorkspaceID(recovered.ProjectID, recovered.ProviderID)
		result = upsertProviderWorkspace(result, recovered)
	}
	return result
}

func normalizeWorkspaceIdentity(workspace model.Workspace) model.Workspace {
	workspace.RootPath = filepath.Clean(workspace.RootPath)
	if strings.TrimSpace(workspace.ProjectID) == "" {
		workspace.ProjectID = projectIDAt(workspace.RootPath)
	}
	if strings.TrimSpace(workspace.ProjectID) == "" {
		workspace.ProjectID = strings.TrimSpace(workspace.ID)
	}
	if workspace.ProjectID != "" && workspace.ProviderID != "" {
		workspace.ID = model.ProviderWorkspaceID(workspace.ProjectID, workspace.ProviderID)
	}
	return workspace
}

func upsertProviderWorkspace(workspaces []model.Workspace, workspace model.Workspace) []model.Workspace {
	for index, existing := range workspaces {
		if existing.ProviderID == workspace.ProviderID && filepath.Clean(existing.RootPath) == filepath.Clean(workspace.RootPath) {
			workspaces[index] = workspace
			return workspaces
		}
	}
	return append(workspaces, workspace)
}

func projectIDAt(rootPath string) string {
	contents, err := os.ReadFile(filepath.Join(rootPath, ".agentx", "project.json"))
	if err != nil {
		return ""
	}
	var project struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(contents, &project) != nil {
		return ""
	}
	return strings.TrimSpace(project.ID)
}

func legacySessionProvider(rootPath string) string {
	contents, err := os.ReadFile(filepath.Join(rootPath, ".agentx", "session.json"))
	if err != nil {
		return ""
	}
	var session struct {
		ProviderID string `json:"providerId"`
	}
	if json.Unmarshal(contents, &session) != nil {
		return ""
	}
	switch session.ProviderID {
	case "claude", "codex":
		return session.ProviderID
	default:
		return ""
	}
}

func (s *Store) save(data Data) error {
	data.Version = model.ConfigVersion
	contents, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Agent X config: %w", err)
	}
	contents = append(contents, '\n')

	if err := fsx.WriteFileAtomically(s.path, contents); err != nil {
		return fmt.Errorf("save Agent X config: %w", err)
	}
	return nil
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func defaultDeviceName(hostname string) string {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "This device"
	}
	return hostname
}

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
