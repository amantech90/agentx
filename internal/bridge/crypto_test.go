package bridge

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"agentx/internal/pairing"
)

func TestSignedAuthProofRejectsTamperingAndExpiredProofs(t *testing.T) {
	t.Parallel()
	store := pairing.NewStore(filepath.Join(t.TempDir(), "pairing.json"))
	identity, err := store.Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	now := time.Unix(1_750_000_000, 0)
	headers, err := signedAuthHeaders(bridgeDeviceAID, bridgeDeviceBID, identity, now)
	if err != nil {
		t.Fatalf("signedAuthHeaders() error = %v", err)
	}
	request := &http.Request{Header: headers}
	proof, signature, err := authProofFromRequest(request)
	if err != nil {
		t.Fatalf("authProofFromRequest() error = %v", err)
	}
	if !verifyAuthProof(proof, signature, bridgeDeviceBID, identity.PublicKey, now) {
		t.Fatal("valid signed bridge proof was rejected")
	}
	if verifyAuthProof(proof, signature, bridgeDeviceAID, identity.PublicKey, now) {
		t.Fatal("bridge proof was valid for the wrong target")
	}
	if verifyAuthProof(proof, signature, bridgeDeviceBID, identity.PublicKey, now.Add(authWindow+time.Second)) {
		t.Fatal("expired bridge proof was accepted")
	}
	proof.Nonce += "tampered"
	if verifyAuthProof(proof, signature, bridgeDeviceBID, identity.PublicKey, now) {
		t.Fatal("tampered bridge proof was accepted")
	}
}

func TestDecodePayloadRejectsTrailingJSON(t *testing.T) {
	t.Parallel()
	var value workspaceRequest
	if err := decodePayload([]byte(`{"workspaceId":"one"}{"workspaceId":"two"}`), &value); err == nil {
		t.Fatal("decodePayload() accepted multiple JSON values")
	}
}
