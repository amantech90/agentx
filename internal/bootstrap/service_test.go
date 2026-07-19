package bootstrap

import (
	"testing"

	"agentx/internal/config"
	"agentx/internal/model"
)

func TestStateIncludesLiveNearbyDevicesWithoutTrustingThem(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil)
	service.SetNearbyProvider(func() []model.Device {
		return []model.Device{{
			ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Aman Windows",
			OS: "windows", Arch: "amd64", Configured: true, Trusted: false,
		}}
	})
	state := service.stateFromProviders("test-mac", config.Data{
		DeviceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DeviceName: "Aman Mac", OnboardingComplete: true,
	}, nil)

	if !state.Device.Trusted {
		t.Fatal("the local device must be trusted")
	}
	if len(state.NearbyDevices) != 1 || state.NearbyDevices[0].Name != "Aman Windows" {
		t.Fatalf("nearby devices = %#v", state.NearbyDevices)
	}
	if state.NearbyDevices[0].Trusted {
		t.Fatal("discovery must not trust a nearby device")
	}
}

func TestStateIncludesPersistedPairedDevices(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil)
	service.SetPairedProvider(func() []model.Device {
		return []model.Device{{
			ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Aman Windows",
			OS: "windows", Arch: "amd64", Configured: true, Trusted: true,
		}}
	})
	state := service.stateFromProviders("test-mac", config.Data{
		DeviceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DeviceName: "Aman Mac", OnboardingComplete: true,
	}, nil)

	if len(state.PairedDevices) != 1 || !state.PairedDevices[0].Trusted {
		t.Fatalf("paired devices = %#v", state.PairedDevices)
	}
}
