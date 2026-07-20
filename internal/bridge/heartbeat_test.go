package bridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agentx/internal/model"
	"agentx/internal/pairing"
)

// TestBridgeHeartbeatKeepsHealthyConnectionOnline shortens the ping interval so
// many heartbeat cycles elapse during the test, and asserts the connection
// stays online — proving the keep-alive ping/pong runs on a live connection
// without tearing it down.
func TestBridgeHeartbeatKeepsHealthyConnectionOnline(t *testing.T) {
	origInterval, origTimeout := bridgePingInterval, bridgePingTimeout
	bridgePingInterval, bridgePingTimeout = 40*time.Millisecond, 500*time.Millisecond
	t.Cleanup(func() { bridgePingInterval, bridgePingTimeout = origInterval, origTimeout })

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	deviceA := model.Device{ID: bridgeDeviceAID, Name: "Mac", OS: "darwin", Arch: "arm64", Configured: true, Trusted: true}
	deviceB := model.Device{ID: bridgeDeviceBID, Name: "Win", OS: "windows", Arch: "amd64", Configured: true, Trusted: true}
	storeA := pairing.NewStore(filepath.Join(t.TempDir(), "a.json"))
	storeB := pairing.NewStore(filepath.Join(t.TempDir(), "b.json"))
	idA, err := storeA.Identity()
	if err != nil {
		t.Fatalf("storeA.Identity() error = %v", err)
	}
	idB, err := storeB.Identity()
	if err != nil {
		t.Fatalf("storeB.Identity() error = %v", err)
	}
	if err := storeA.Trust(deviceB, idB.PublicKey); err != nil {
		t.Fatalf("storeA.Trust() error = %v", err)
	}
	if err := storeB.Trust(deviceA, idA.PublicKey); err != nil {
		t.Fatalf("storeB.Trust() error = %v", err)
	}

	serviceA := newService(storeA, "127.0.0.1:0")
	serviceB := newService(storeB, "127.0.0.1:0")
	t.Cleanup(serviceA.Stop)
	t.Cleanup(serviceB.Stop)
	online := func(device model.Device) Handlers {
		return Handlers{State: func(context.Context) (model.RemoteDeviceState, error) {
			return model.RemoteDeviceState{Device: device, Online: true, Workspaces: []model.Workspace{}}, nil
		}}
	}
	serviceA.SetHandlers(online(deviceA))
	serviceB.SetHandlers(online(deviceB))
	if err := serviceA.Start(ctx, deviceA); err != nil {
		t.Fatalf("serviceA.Start() error = %v", err)
	}
	if err := serviceB.Start(ctx, deviceB); err != nil {
		t.Fatalf("serviceB.Start() error = %v", err)
	}
	serviceA.UpdateTargets([]Target{{Device: deviceB, Endpoint: serviceB.Endpoint(), PublicKey: idB.PublicKey}})
	serviceB.UpdateTargets([]Target{{Device: deviceA, Endpoint: serviceA.Endpoint(), PublicKey: idA.PublicKey}})

	waitForBridge(t, 3*time.Second, func() bool {
		return remoteOnline(serviceA.Snapshot(), deviceB.ID) && remoteOnline(serviceB.Snapshot(), deviceA.ID)
	})

	// ~10 ping cycles: a healthy connection must survive all of them.
	time.Sleep(400 * time.Millisecond)

	if !remoteOnline(serviceA.Snapshot(), deviceB.ID) || !remoteOnline(serviceB.Snapshot(), deviceA.ID) {
		t.Fatalf("connection dropped despite heartbeat: A->B=%v B->A=%v",
			remoteOnline(serviceA.Snapshot(), deviceB.ID), remoteOnline(serviceB.Snapshot(), deviceA.ID))
	}
}
