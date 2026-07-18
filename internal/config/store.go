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

	found := false
	for index, existing := range data.Workspaces {
		if existing.ID == workspace.ID || filepath.Clean(existing.RootPath) == filepath.Clean(workspace.RootPath) {
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
	return data, nil
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
