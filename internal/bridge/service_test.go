package bridge

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"agentx/internal/model"
	"agentx/internal/pairing"
)

const (
	bridgeDeviceAID = "11111111111111111111111111111111"
	bridgeDeviceBID = "22222222222222222222222222222222"
)

func TestTwoTrustedServicesExchangeStateAndRouteWorkspaceCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deviceA := model.Device{ID: bridgeDeviceAID, Name: "Aman Mac", OS: "darwin", Arch: "arm64", Configured: true, Trusted: true}
	deviceB := model.Device{ID: bridgeDeviceBID, Name: "Aman Windows", OS: "windows", Arch: "amd64", Configured: true, Trusted: true}
	storeA := pairing.NewStore(filepath.Join(t.TempDir(), "a.json"))
	storeB := pairing.NewStore(filepath.Join(t.TempDir(), "b.json"))
	identityA, err := storeA.Identity()
	if err != nil {
		t.Fatalf("storeA.Identity() error = %v", err)
	}
	identityB, err := storeB.Identity()
	if err != nil {
		t.Fatalf("storeB.Identity() error = %v", err)
	}
	if err := storeA.Trust(deviceB, identityB.PublicKey); err != nil {
		t.Fatalf("storeA.Trust() error = %v", err)
	}
	if err := storeB.Trust(deviceA, identityA.PublicKey); err != nil {
		t.Fatalf("storeB.Trust() error = %v", err)
	}

	var stateMu sync.Mutex
	stateA := model.RemoteDeviceState{Device: deviceA, Online: true, Workspaces: []model.Workspace{}}
	stateB := model.RemoteDeviceState{Device: deviceB, Online: true, Workspaces: []model.Workspace{}}
	serviceA := newService(storeA, "127.0.0.1:0")
	serviceB := newService(storeB, "127.0.0.1:0")
	t.Cleanup(serviceA.Stop)
	t.Cleanup(serviceB.Stop)
	serviceA.SetHandlers(Handlers{
		State: func(context.Context) (model.RemoteDeviceState, error) {
			stateMu.Lock()
			defer stateMu.Unlock()
			return cloneRemoteState(stateA), nil
		},
	})
	serviceB.SetHandlers(Handlers{
		State: func(context.Context) (model.RemoteDeviceState, error) {
			stateMu.Lock()
			defer stateMu.Unlock()
			return cloneRemoteState(stateB), nil
		},
		OpenWorkspace: func(_ context.Context, request model.OpenWorkspaceRequest) (model.RemoteDeviceState, error) {
			stateMu.Lock()
			defer stateMu.Unlock()
			stateB.Workspaces = append(stateB.Workspaces, model.Workspace{
				ID: "remote-workspace", ProjectID: "remote-project", Name: request.Name,
				ProviderID: request.ProviderID, RootPath: `C:\Code\BookingAPI`,
			})
			return cloneRemoteState(stateB), nil
		},
		SendMessage: func(_ context.Context, request model.SendMessageRequest) (model.SessionSnapshot, error) {
			return model.SessionSnapshot{
				WorkspaceID: request.WorkspaceID, ProviderID: "codex", Status: "queued", QueueDepth: 1,
			}, nil
		},
		ResolveApproval: func(_ context.Context, request model.ResolveApprovalRequest) (model.SessionSnapshot, error) {
			return model.SessionSnapshot{
				WorkspaceID: request.WorkspaceID, ProviderID: "codex", Status: "running",
				Items: []model.ChatItem{{ID: request.ApprovalID, Kind: "approval", Status: "approved"}},
			}, nil
		},
	})

	if err := serviceA.Start(ctx, deviceA); err != nil {
		t.Fatalf("serviceA.Start() error = %v", err)
	}
	if err := serviceB.Start(ctx, deviceB); err != nil {
		t.Fatalf("serviceB.Start() error = %v", err)
	}
	serviceA.UpdateTargets([]Target{{Device: deviceB, Endpoint: serviceB.Endpoint(), PublicKey: identityB.PublicKey}})
	serviceB.UpdateTargets([]Target{{Device: deviceA, Endpoint: serviceA.Endpoint(), PublicKey: identityA.PublicKey}})

	waitForBridge(t, 5*time.Second, func() bool {
		return remoteOnline(serviceA.Snapshot(), deviceB.ID) && remoteOnline(serviceB.Snapshot(), deviceA.ID)
	})

	opened, err := serviceA.OpenWorkspace(ctx, deviceB.ID, model.OpenWorkspaceRequest{
		Name: "Booking API", ProviderID: "codex", DeviceID: deviceB.ID,
	})
	if err != nil {
		t.Fatalf("OpenWorkspace() error = %v", err)
	}
	if len(opened.Workspaces) != 1 || opened.Workspaces[0].Name != "Booking API" {
		t.Fatalf("opened remote state = %#v", opened)
	}

	queued, err := serviceA.SendMessage(ctx, deviceB.ID, model.SendMessageRequest{
		DeviceID: deviceB.ID, WorkspaceID: "remote-workspace", Content: "Run tests",
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if queued.Status != "queued" || queued.QueueDepth != 1 {
		t.Fatalf("queued snapshot = %#v", queued)
	}

	approved, err := serviceA.ResolveApproval(ctx, deviceB.ID, model.ResolveApprovalRequest{
		DeviceID: deviceB.ID, WorkspaceID: "remote-workspace", ApprovalID: "approval-1", Decision: "allow",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if approved.Status != "running" || len(approved.Items) != 1 || approved.Items[0].Status != "approved" {
		t.Fatalf("approved snapshot = %#v", approved)
	}

	serviceB.PublishSession(model.SessionSnapshot{
		WorkspaceID: "remote-workspace", ProviderID: "codex", Status: "idle",
		Items: []model.ChatItem{{ID: "answer", Role: "assistant", Kind: "message", Content: "All tests pass"}},
	})
	waitForBridge(t, 3*time.Second, func() bool {
		state, ok := remoteState(serviceA.Snapshot(), deviceB.ID)
		return ok && len(state.Sessions) == 1 && len(state.Sessions[0].Items) == 1
	})
}

func TestDecodePayloadIgnoresFieldsAddedByANewerPeer(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"workspaceId":"workspace-1","content":"hi","futureOption":{"enabled":true}}`)
	var legacyRequest struct {
		WorkspaceID string `json:"workspaceId"`
		Content     string `json:"content"`
	}
	if err := decodePayload(payload, &legacyRequest); err != nil {
		t.Fatalf("decodePayload() rejected a newer optional field: %v", err)
	}
	if legacyRequest.WorkspaceID != "workspace-1" || legacyRequest.Content != "hi" {
		t.Fatalf("decoded request = %#v", legacyRequest)
	}
}

func waitForBridge(t *testing.T, timeout time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("bridge condition was not reached")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func remoteOnline(snapshot model.BridgeSnapshot, deviceID string) bool {
	state, ok := remoteState(snapshot, deviceID)
	return ok && state.Online
}

func remoteState(snapshot model.BridgeSnapshot, deviceID string) (model.RemoteDeviceState, bool) {
	for _, state := range snapshot.Devices {
		if state.Device.ID == deviceID {
			return state, true
		}
	}
	return model.RemoteDeviceState{}, false
}

func cloneRemoteState(state model.RemoteDeviceState) model.RemoteDeviceState {
	state.Workspaces = append([]model.Workspace(nil), state.Workspaces...)
	state.Sessions = append([]model.SessionSnapshot(nil), state.Sessions...)
	return state
}
