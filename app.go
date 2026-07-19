package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"agentx/internal/bootstrap"
	workspacebridge "agentx/internal/bridge"
	"agentx/internal/config"
	devicediscovery "agentx/internal/discovery"
	"agentx/internal/model"
	"agentx/internal/pairing"
	"agentx/internal/provider"
	"agentx/internal/session"
	"agentx/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx        context.Context
	bootstrap  *bootstrap.Service
	workspaces *workspace.Manager
	sessions   *session.Manager
	discovery  *devicediscovery.Service
	pairing    *pairing.Service
	bridge     *workspacebridge.Service
	pickFolder func(context.Context, runtime.OpenDialogOptions) (string, error)
	emitState  func(model.BootstrapState)
}

func NewApp() (*App, error) {
	configPath, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	store := config.New(configPath)
	discoveryService := devicediscovery.New()
	pairingStore := pairing.NewStore(filepath.Join(filepath.Dir(configPath), "pairing.json"))
	pairingService := pairing.NewService(pairingStore)
	bridgeService := workspacebridge.NewService(pairingStore)
	bootstrapService := bootstrap.NewService(store, provider.NewDetector())
	bootstrapService.SetNearbyProvider(discoveryService.Devices)
	bootstrapService.SetPairedProvider(pairingService.TrustedDevices)
	discoveryService.SetTrustProvider(pairingService.IsTrusted)
	pairingService.SetTargetResolver(func(deviceID string) (pairing.Target, bool) {
		peer, ok := discoveryService.Lookup(deviceID)
		if !ok {
			return pairing.Target{}, false
		}
		return pairing.Target{Device: peer.Device, Endpoint: peer.Endpoint, PublicKey: peer.PublicKey}, true
	})
	sessions := session.NewManager(map[string]session.Runner{
		"claude": session.NewClaudeRunner(),
		"codex":  session.NewCodexRunner(),
	})
	application := &App{
		bootstrap:  bootstrapService,
		workspaces: workspace.NewManager(),
		sessions:   sessions,
		discovery:  discoveryService,
		pairing:    pairingService,
		bridge:     bridgeService,
		pickFolder: runtime.OpenDirectoryDialog,
		emitState:  func(model.BootstrapState) {},
	}
	bridgeService.SetHandlers(workspacebridge.Handlers{
		State: application.localBridgeState,
		OpenWorkspace: func(_ context.Context, request model.OpenWorkspaceRequest) (model.RemoteDeviceState, error) {
			if _, err := application.openLocalWorkspace(request); err != nil {
				return model.RemoteDeviceState{}, err
			}
			return application.localBridgeState(application.context())
		},
		GetSession: func(_ context.Context, workspaceID string) (model.SessionSnapshot, error) {
			return application.getLocalSession(workspaceID)
		},
		SendMessage: func(_ context.Context, request model.SendMessageRequest) (model.SessionSnapshot, error) {
			return application.sendLocalMessage(request)
		},
		ResolveApproval: func(_ context.Context, request model.ResolveApprovalRequest) (model.SessionSnapshot, error) {
			return application.resolveLocalApproval(request)
		},
		DeleteConversation: func(_ context.Context, workspaceID string) (model.SessionSnapshot, error) {
			return application.deleteLocalConversation(workspaceID)
		},
	})
	return application, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.emitState = func(state model.BootstrapState) {
		runtime.EventsEmit(ctx, "agentx:state", state)
	}
	a.sessions.SetEmitter(func(snapshot model.SessionSnapshot) {
		runtime.EventsEmit(ctx, "agentx:session", snapshot)
		if a.bridge != nil {
			a.bridge.PublishSession(snapshot)
		}
	})
	if a.bridge != nil {
		a.bridge.SetEmitter(func(snapshot model.BridgeSnapshot) {
			runtime.EventsEmit(ctx, "agentx:bridge", snapshot)
		})
	}
	a.discovery.SetEmitter(func(devices []model.Device) {
		runtime.EventsEmit(ctx, "agentx:devices", devices)
		a.syncBridgeTargets(devices)
	})
	a.pairing.SetEmitter(func(snapshot model.PairingSnapshot) {
		runtime.EventsEmit(ctx, "agentx:pairing", snapshot)
	})
	a.pairing.SetTrustChanged(func() {
		a.discovery.RefreshTrust()
		a.syncBridgeTargets(a.discovery.Devices())
	})
	state, err := a.bootstrap.State(ctx)
	if err == nil {
		a.startLocalServices(ctx, state)
	}
}

func (a *App) shutdown(context.Context) {
	if a.discovery != nil {
		a.discovery.Stop()
	}
	if a.bridge != nil {
		a.bridge.Stop()
	}
	if a.pairing != nil {
		a.pairing.Stop()
	}
	if a.sessions != nil {
		a.sessions.Close()
	}
}

func (a *App) Bootstrap() (model.BootstrapState, error) {
	return a.state(a.context())
}

func (a *App) CompleteOnboarding(request model.OnboardingRequest) (model.BootstrapState, error) {
	state, err := a.bootstrap.CompleteOnboarding(a.context(), request)
	if err == nil {
		a.startLocalServices(a.context(), state)
		state = a.withRemoteDevices(state)
	}
	return state, err
}

func (a *App) startLocalServices(ctx context.Context, state model.BootstrapState) {
	if state.NeedsOnboarding || !state.Device.Configured {
		return
	}
	publicKey := ""
	if a.pairing != nil {
		if err := a.pairing.Start(ctx, state.Device); err != nil {
			runtime.LogWarningf(ctx, "Agent X pairing is unavailable: %v", err)
		} else {
			publicKey = a.pairing.PublicKey()
		}
	}
	bridgePort := uint16(0)
	if a.bridge != nil {
		if err := a.bridge.Start(ctx, state.Device); err != nil {
			runtime.LogWarningf(ctx, "Agent X workspace bridge is unavailable: %v", err)
		} else {
			bridgePort = a.bridge.Port()
		}
	}
	if a.discovery != nil {
		if err := a.discovery.Start(ctx, state.Device, state.Version, publicKey, bridgePort); err != nil {
			runtime.LogWarningf(ctx, "Agent X device discovery is unavailable: %v", err)
		}
		a.syncBridgeTargets(a.discovery.Devices())
	}
}

func (a *App) syncBridgeTargets(devices []model.Device) {
	if a.bridge == nil || a.discovery == nil {
		return
	}
	targets := make([]workspacebridge.Target, 0, len(devices))
	for _, device := range devices {
		if !device.Trusted {
			continue
		}
		peer, ok := a.discovery.Lookup(device.ID)
		if !ok || peer.BridgeEndpoint == "" {
			continue
		}
		targets = append(targets, workspacebridge.Target{
			Device: peer.Device, Endpoint: peer.BridgeEndpoint, PublicKey: peer.PublicKey,
		})
	}
	a.bridge.UpdateTargets(targets)
}

func (a *App) PairingState() model.PairingSnapshot {
	if a.pairing == nil {
		return model.PairingSnapshot{}
	}
	return a.pairing.Snapshot()
}

func (a *App) RequestPairing(deviceID string) (model.PairingSnapshot, error) {
	if a.pairing == nil {
		return model.PairingSnapshot{}, errors.New("pairing is unavailable")
	}
	return a.pairing.RequestPairing(deviceID)
}

func (a *App) ApprovePairing(requestID string) (model.PairingSnapshot, error) {
	if a.pairing == nil {
		return model.PairingSnapshot{}, errors.New("pairing is unavailable")
	}
	return a.pairing.ApprovePairing(requestID)
}

func (a *App) RejectPairing(requestID string) (model.PairingSnapshot, error) {
	if a.pairing == nil {
		return model.PairingSnapshot{}, errors.New("pairing is unavailable")
	}
	return a.pairing.RejectPairing(requestID)
}

func (a *App) RemovePairedDevice(deviceID string) (model.PairingSnapshot, error) {
	if a.pairing == nil {
		return model.PairingSnapshot{}, errors.New("pairing is unavailable")
	}
	snapshot, err := a.pairing.RemovePairedDevice(deviceID)
	if err == nil && a.discovery != nil {
		a.syncBridgeTargets(a.discovery.Devices())
	}
	return snapshot, err
}

func (a *App) RefreshProviders() (model.BootstrapState, error) {
	state, err := a.state(a.context())
	if err == nil && a.bridge != nil {
		a.bridge.PublishState()
	}
	return state, err
}

func (a *App) GetSession(workspaceID string) (model.SessionSnapshot, error) {
	return a.getLocalSession(workspaceID)
}

func (a *App) GetWorkspaceSession(request model.WorkspaceCommandRequest) (model.SessionSnapshot, error) {
	local, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	if deviceID == "" || deviceID == local.Device.ID {
		return a.getLocalSession(request.WorkspaceID)
	}
	if a.bridge == nil {
		return model.SessionSnapshot{}, errors.New("workspace bridge is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 30*time.Second)
	defer cancel()
	return a.bridge.GetSession(ctx, deviceID, strings.TrimSpace(request.WorkspaceID))
}

func (a *App) getLocalSession(workspaceID string) (model.SessionSnapshot, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	workspace, ok := workspaceByID(state.Workspaces, strings.TrimSpace(workspaceID))
	if !ok {
		return model.SessionSnapshot{}, errors.New("workspace was not found")
	}
	return a.sessions.Snapshot(workspace)
}

func (a *App) DeleteConversation(workspaceID string) (model.SessionSnapshot, error) {
	return a.deleteLocalConversation(workspaceID)
}

func (a *App) DeleteWorkspaceConversation(request model.WorkspaceCommandRequest) (model.SessionSnapshot, error) {
	local, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	if deviceID == "" || deviceID == local.Device.ID {
		return a.deleteLocalConversation(request.WorkspaceID)
	}
	if a.bridge == nil {
		return model.SessionSnapshot{}, errors.New("workspace bridge is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.context(), 30*time.Second)
	defer cancel()
	return a.bridge.DeleteConversation(ctx, deviceID, strings.TrimSpace(request.WorkspaceID))
}

func (a *App) deleteLocalConversation(workspaceID string) (model.SessionSnapshot, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	workspace, ok := workspaceByID(state.Workspaces, strings.TrimSpace(workspaceID))
	if !ok {
		return model.SessionSnapshot{}, errors.New("workspace was not found")
	}
	return a.sessions.DeleteConversation(workspace)
}

func (a *App) SendMessage(request model.SendMessageRequest) (model.SessionSnapshot, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	if deviceID == "" || deviceID == state.Device.ID {
		return a.sendLocalMessage(request)
	}
	if a.bridge == nil {
		return model.SessionSnapshot{}, errors.New("workspace bridge is unavailable")
	}
	request.DeviceID = deviceID
	ctx, cancel := context.WithTimeout(a.context(), 30*time.Second)
	defer cancel()
	return a.bridge.SendMessage(ctx, deviceID, request)
}

func (a *App) sendLocalMessage(request model.SendMessageRequest) (model.SessionSnapshot, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	workspace, ok := workspaceByID(state.Workspaces, strings.TrimSpace(request.WorkspaceID))
	if !ok {
		return model.SessionSnapshot{}, errors.New("workspace was not found")
	}
	providerPath := ""
	for _, item := range state.Providers {
		if item.ID == workspace.ProviderID && item.Installed && item.Supported && !item.ComingSoon {
			providerPath = item.Path
			break
		}
	}
	if providerPath == "" {
		return model.SessionSnapshot{}, errors.New("the workspace provider is unavailable on this device")
	}
	return a.sessions.EnqueueMessage(workspace, providerPath, session.MessageInput{
		Prompt: request.Content, DisplayPrompt: request.DisplayContent,
		PermissionMode: request.PermissionMode, Screenshot: request.Screenshot,
	})
}

func (a *App) ResolveApproval(request model.ResolveApprovalRequest) (model.SessionSnapshot, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	if deviceID == "" || deviceID == state.Device.ID {
		return a.resolveLocalApproval(request)
	}
	if a.bridge == nil {
		return model.SessionSnapshot{}, errors.New("workspace bridge is unavailable")
	}
	request.DeviceID = deviceID
	ctx, cancel := context.WithTimeout(a.context(), 30*time.Second)
	defer cancel()
	return a.bridge.ResolveApproval(ctx, deviceID, request)
}

func (a *App) resolveLocalApproval(request model.ResolveApprovalRequest) (model.SessionSnapshot, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	workspace, ok := workspaceByID(state.Workspaces, strings.TrimSpace(request.WorkspaceID))
	if !ok {
		return model.SessionSnapshot{}, errors.New("workspace was not found")
	}
	return a.sessions.ResolveApproval(workspace, request.ApprovalID, request.Decision)
}

func (a *App) OpenWorkspace(request model.OpenWorkspaceRequest) (model.BootstrapState, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.BootstrapState{}, err
	}
	request.Name = strings.TrimSpace(request.Name)
	if err := validateWorkspaceName(request.Name); err != nil {
		return model.BootstrapState{}, err
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	if deviceID == "" || deviceID == state.Device.ID {
		return a.openLocalWorkspace(request)
	}
	if a.bridge == nil {
		return model.BootstrapState{}, errors.New("workspace bridge is unavailable")
	}
	remote, ok := remoteDeviceByID(a.bridge.Snapshot().Devices, deviceID)
	if !ok || !remote.Online {
		return model.BootstrapState{}, errors.New("paired device is offline")
	}
	request.DeviceID = deviceID
	if err := validateRemoteWorkspaceRequest(remote, request); err != nil {
		return model.BootstrapState{}, err
	}
	ctx, cancel := context.WithTimeout(a.context(), 10*time.Minute)
	defer cancel()
	if _, err := a.bridge.OpenWorkspace(ctx, deviceID, request); err != nil {
		return model.BootstrapState{}, err
	}
	return a.state(a.context())
}

func (a *App) openLocalWorkspace(request model.OpenWorkspaceRequest) (model.BootstrapState, error) {
	state, err := a.bootstrap.State(a.context())
	if err != nil {
		return model.BootstrapState{}, err
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.DeviceID == "" {
		request.DeviceID = state.Device.ID
	}
	if err := validateWorkspaceRequest(state, request); err != nil {
		return model.BootstrapState{}, err
	}

	rootPath, err := a.pickFolder(a.context(), runtime.OpenDialogOptions{
		Title:                fmt.Sprintf("Choose a folder for %s", request.Name),
		CanCreateDirectories: true,
	})
	if err != nil {
		return model.BootstrapState{}, fmt.Errorf("open workspace dialog: %w", err)
	}
	if rootPath == "" {
		return a.state(a.context())
	}

	opened, err := a.workspaces.Open(rootPath, request.Name, request.ProviderID)
	if err != nil {
		return model.BootstrapState{}, err
	}
	updated, err := a.bootstrap.AddWorkspace(a.context(), opened)
	if err != nil {
		return model.BootstrapState{}, err
	}
	updated = a.withRemoteDevices(updated)
	if a.emitState != nil {
		a.emitState(updated)
	}
	if a.bridge != nil {
		a.bridge.PublishState()
	}
	return updated, nil
}

func validateWorkspaceRequest(state model.BootstrapState, request model.OpenWorkspaceRequest) error {
	if err := validateWorkspaceName(request.Name); err != nil {
		return err
	}
	if request.DeviceID == "" || request.DeviceID != state.Device.ID {
		return errors.New("select this device to browse for a local workspace")
	}

	selected := false
	for _, id := range state.SelectedProviderIDs {
		if id == request.ProviderID {
			selected = true
			break
		}
	}
	if !selected {
		return errors.New("select a configured provider")
	}
	for _, item := range state.Providers {
		if item.ID == request.ProviderID && item.Installed && item.Supported && !item.ComingSoon {
			return nil
		}
	}
	return errors.New("the selected provider is no longer available on this device")
}

func validateWorkspaceName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("project name is required")
	}
	if len([]rune(name)) > 80 {
		return errors.New("project name must be 80 characters or fewer")
	}
	return nil
}

func validateRemoteWorkspaceRequest(state model.RemoteDeviceState, request model.OpenWorkspaceRequest) error {
	selected := false
	for _, id := range state.SelectedProviderIDs {
		if id == request.ProviderID {
			selected = true
			break
		}
	}
	if !selected {
		return errors.New("select a provider configured on the project device")
	}
	for _, item := range state.Providers {
		if item.ID == request.ProviderID && item.Installed && item.Supported && !item.ComingSoon {
			return nil
		}
	}
	return errors.New("the selected provider is unavailable on the project device")
}

func (a *App) BridgeState() model.BridgeSnapshot {
	if a.bridge == nil {
		return model.BridgeSnapshot{Devices: []model.RemoteDeviceState{}}
	}
	return a.bridge.Snapshot()
}

func (a *App) localBridgeState(ctx context.Context) (model.RemoteDeviceState, error) {
	state, err := a.bootstrap.State(ctx)
	if err != nil {
		return model.RemoteDeviceState{}, err
	}
	snapshots := make([]model.SessionSnapshot, 0, len(state.Workspaces))
	for _, item := range state.Workspaces {
		snapshot, snapshotErr := a.sessions.Snapshot(item)
		if snapshotErr == nil {
			snapshots = append(snapshots, snapshot)
		}
	}
	providers := append([]model.Provider(nil), state.Providers...)
	for index := range providers {
		// A paired client only needs availability metadata. Keep the absolute
		// executable location private to the device that runs the provider.
		providers[index].Path = ""
	}
	return model.RemoteDeviceState{
		Device: state.Device, Online: true, Providers: providers,
		SelectedProviderIDs: state.SelectedProviderIDs, Workspaces: state.Workspaces, Sessions: snapshots,
	}, nil
}

func (a *App) state(ctx context.Context) (model.BootstrapState, error) {
	state, err := a.bootstrap.State(ctx)
	if err != nil {
		return model.BootstrapState{}, err
	}
	return a.withRemoteDevices(state), nil
}

func (a *App) withRemoteDevices(state model.BootstrapState) model.BootstrapState {
	if a.bridge == nil {
		state.RemoteDevices = []model.RemoteDeviceState{}
		return state
	}
	state.RemoteDevices = a.bridge.Snapshot().Devices
	return state
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func workspaceByID(workspaces []model.Workspace, id string) (model.Workspace, bool) {
	for _, item := range workspaces {
		if item.ID == id {
			return item, true
		}
	}
	return model.Workspace{}, false
}

func remoteDeviceByID(devices []model.RemoteDeviceState, id string) (model.RemoteDeviceState, bool) {
	for _, item := range devices {
		if item.Device.ID == id {
			return item, true
		}
	}
	return model.RemoteDeviceState{}, false
}
