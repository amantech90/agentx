package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"agentx/internal/config"
	"agentx/internal/model"
	"agentx/internal/provider"
)

// Version is the current Agent X build, compared against the latest GitHub
// release to decide whether to prompt for an update. Keep it in sync with the
// release tag.
const Version = "1.0.0"

type Service struct {
	store    *config.Store
	detector *provider.Detector
	nearby   func() []model.Device
	paired   func() []model.Device
}

func NewService(store *config.Store, detector *provider.Detector) *Service {
	return &Service{
		store: store, detector: detector,
		nearby: func() []model.Device { return []model.Device{} },
		paired: func() []model.Device { return []model.Device{} },
	}
}

func (s *Service) SetNearbyProvider(provider func() []model.Device) {
	if provider == nil {
		s.nearby = func() []model.Device { return []model.Device{} }
		return
	}
	s.nearby = provider
}

func (s *Service) SetPairedProvider(provider func() []model.Device) {
	if provider == nil {
		s.paired = func() []model.Device { return []model.Device{} }
		return
	}
	s.paired = provider
}

func (s *Service) State(ctx context.Context) (model.BootstrapState, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	data, err := s.store.LoadOrCreate(hostname)
	if err != nil {
		return model.BootstrapState{}, err
	}
	return s.stateFromData(ctx, hostname, data), nil
}

func (s *Service) CompleteOnboarding(ctx context.Context, request model.OnboardingRequest) (model.BootstrapState, error) {
	hostname, _ := os.Hostname()
	providers := s.detector.Detect(ctx)
	valid := make(map[string]bool)
	for _, item := range providers {
		valid[item.ID] = item.Installed && item.Supported && !item.ComingSoon
	}
	for _, id := range request.SelectedProviderIDs {
		if !valid[id] {
			return model.BootstrapState{}, fmt.Errorf("provider %q is not installed or supported", id)
		}
	}
	if len(request.SelectedProviderIDs) == 0 {
		return model.BootstrapState{}, errors.New("select at least one installed provider")
	}

	data, err := s.store.CompleteOnboarding(hostname, request)
	if err != nil {
		return model.BootstrapState{}, err
	}
	return s.stateFromProviders(hostname, data, providers), nil
}

func (s *Service) AddWorkspace(ctx context.Context, workspace model.Workspace) (model.BootstrapState, error) {
	hostname, _ := os.Hostname()
	data, err := s.store.AddWorkspace(workspace)
	if err != nil {
		return model.BootstrapState{}, err
	}
	return s.stateFromData(ctx, hostname, data), nil
}

func (s *Service) stateFromData(ctx context.Context, hostname string, data config.Data) model.BootstrapState {
	return s.stateFromProviders(hostname, data, s.detector.Detect(ctx))
}

func (s *Service) stateFromProviders(hostname string, data config.Data, providers []model.Provider) model.BootstrapState {
	return model.BootstrapState{
		Version:         Version,
		NeedsOnboarding: !data.OnboardingComplete,
		Device: model.Device{
			ID:         data.DeviceID,
			Name:       data.DeviceName,
			Hostname:   hostname,
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			Configured: data.OnboardingComplete,
			Trusted:    true,
		},
		NearbyDevices:       s.nearby(),
		PairedDevices:       s.paired(),
		Providers:           providers,
		SelectedProviderIDs: data.SelectedProviderIDs,
		Workspaces:          data.Workspaces,
	}
}
