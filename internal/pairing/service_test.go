package pairing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agentx/internal/model"
)

const (
	deviceAID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	deviceBID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestStorePersistsIdentityAndRevocableTrust(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pairing.json")
	store := NewStore(path)
	first, err := store.Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	second, err := NewStore(path).Identity()
	if err != nil {
		t.Fatalf("reloaded Identity() error = %v", err)
	}
	if first.PublicKey != second.PublicKey || first.PublicKey == "" {
		t.Fatalf("identity changed: first=%q second=%q", first.PublicKey, second.PublicKey)
	}

	peer := model.Device{ID: deviceBID, Name: "Windows", OS: "windows", Arch: "amd64", Configured: true}
	if err := store.Trust(peer, second.PublicKey); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}
	if !store.IsTrusted(peer.ID, second.PublicKey) {
		t.Fatal("trusted key was not recognized")
	}
	trustedKey, ok := store.TrustedPublicKey(peer.ID)
	if !ok || trustedKey != second.PublicKey {
		t.Fatalf("TrustedPublicKey() = %q, %v", trustedKey, ok)
	}
	if err := store.RemoveTrust(peer.ID); err != nil {
		t.Fatalf("RemoveTrust() error = %v", err)
	}
	if store.IsTrusted(peer.ID, second.PublicKey) {
		t.Fatal("removed key remained trusted")
	}
}

func TestPairingTranscriptRejectsTampering(t *testing.T) {
	t.Parallel()

	identity, err := NewStore(filepath.Join(t.TempDir(), "pairing.json")).Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	request := wireRequest{
		Version: protocolVersion, ID: "request-id", Nonce: "request-nonce", IssuedAt: time.Now().Unix(),
		Device:    model.Device{ID: deviceAID, Name: "Mac", OS: "darwin", Arch: "arm64"},
		PublicKey: identity.PublicKey,
	}
	request.Signature, err = signRequest(request, identity.PrivateKey)
	if err != nil {
		t.Fatalf("signRequest() error = %v", err)
	}
	if !verifyRequest(request) {
		t.Fatal("valid request signature was rejected")
	}
	request.Device.Name = "Imposter"
	if verifyRequest(request) {
		t.Fatal("tampered request signature was accepted")
	}
}

func TestTwoServicesPairApprovePersistAndRemoveTrust(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	a := newService(NewStore(filepath.Join(t.TempDir(), "a.json")), "127.0.0.1:0")
	b := newService(NewStore(filepath.Join(t.TempDir(), "b.json")), "127.0.0.1:0")
	t.Cleanup(a.Stop)
	t.Cleanup(b.Stop)
	deviceA := model.Device{ID: deviceAID, Name: "Aman Mac", OS: "darwin", Arch: "arm64", Configured: true, Trusted: true}
	deviceB := model.Device{ID: deviceBID, Name: "Aman Windows", OS: "windows", Arch: "amd64", Configured: true, Trusted: true}
	if err := a.Start(ctx, deviceA); err != nil {
		t.Fatalf("a.Start() error = %v", err)
	}
	if err := b.Start(ctx, deviceB); err != nil {
		t.Fatalf("b.Start() error = %v", err)
	}
	a.SetTargetResolver(func(id string) (Target, bool) {
		return Target{Device: deviceB, Endpoint: b.Endpoint(), PublicKey: b.PublicKey()}, id == deviceB.ID
	})
	b.SetTargetResolver(func(id string) (Target, bool) {
		return Target{Device: deviceA, Endpoint: a.Endpoint(), PublicKey: a.PublicKey()}, id == deviceA.ID
	})

	outgoing, err := a.RequestPairing(deviceB.ID)
	if err != nil {
		t.Fatalf("RequestPairing() error = %v", err)
	}
	request := firstPending(outgoing, "outgoing")
	if request.ID == "" || len(request.Code) != 6 {
		t.Fatalf("outgoing request = %#v", request)
	}
	incoming := firstPending(b.Snapshot(), "incoming")
	if incoming.ID != request.ID || incoming.Code != request.Code {
		t.Fatalf("codes do not match: outgoing=%#v incoming=%#v", request, incoming)
	}
	if _, err := b.ApprovePairing(incoming.ID); err != nil {
		t.Fatalf("ApprovePairing() error = %v", err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for !a.IsTrusted(deviceB.ID, b.PublicKey()) || !b.IsTrusted(deviceA.ID, a.PublicKey()) {
		if time.Now().After(deadline) {
			t.Fatalf("trust was not persisted: a=%#v b=%#v", a.TrustedDevices(), b.TrustedDevices())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := a.RemovePairedDevice(deviceB.ID); err != nil {
		t.Fatalf("RemovePairedDevice() error = %v", err)
	}
	if a.IsTrusted(deviceB.ID, b.PublicKey()) {
		t.Fatal("removed device remained trusted")
	}
}

func firstPending(snapshot model.PairingSnapshot, direction string) model.PairingRequest {
	for _, request := range snapshot.Requests {
		if request.Direction == direction && request.Status == "pending" {
			return request
		}
	}
	return model.PairingRequest{}
}
