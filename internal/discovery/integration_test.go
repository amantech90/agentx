//go:build discovery_integration

package discovery

import (
	"context"
	"testing"
	"time"

	"agentx/internal/model"
)

const (
	firstIntegrationID  = "11111111111111111111111111111111"
	secondIntegrationID = "22222222222222222222222222222222"
)

func TestTwoLocalServicesDiscoverAndRemoveEachOther(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	first := New()
	second := New()
	t.Cleanup(first.Stop)
	t.Cleanup(second.Stop)
	if err := first.Start(ctx, model.Device{
		ID: firstIntegrationID, Name: "Discovery Mac", OS: "darwin", Arch: "arm64", Configured: true, Trusted: true,
	}, "test", "", 0); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := second.Start(ctx, model.Device{
		ID: secondIntegrationID, Name: "Discovery Windows", OS: "windows", Arch: "amd64", Configured: true, Trusted: true,
	}, "test", "", 0); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if hasDevice(first.Devices(), secondIntegrationID) && hasDevice(second.Devices(), firstIntegrationID) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("services did not discover each other: first=%#v second=%#v", first.Devices(), second.Devices())
		case <-ticker.C:
		}
	}

	second.Stop()
	removalDeadline := time.Now().Add(3 * time.Second)
	for hasDevice(first.Devices(), secondIntegrationID) {
		if time.Now().After(removalDeadline) {
			t.Fatalf("stopped service remained visible: first=%#v", first.Devices())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func hasDevice(devices []model.Device, id string) bool {
	for _, device := range devices {
		if device.ID == id {
			return true
		}
	}
	return false
}
