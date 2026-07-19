package pairing

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"agentx/internal/model"
)

const protocolVersion = "1"

var deviceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type wireRequest struct {
	Version   string       `json:"version"`
	ID        string       `json:"id"`
	Device    model.Device `json:"device"`
	PublicKey string       `json:"publicKey"`
	Nonce     string       `json:"nonce"`
	IssuedAt  int64        `json:"issuedAt"`
	Signature string       `json:"signature"`
}

type wireChallenge struct {
	Version   string       `json:"version"`
	ID        string       `json:"id"`
	Device    model.Device `json:"device"`
	PublicKey string       `json:"publicKey"`
	Nonce     string       `json:"nonce"`
	ExpiresAt int64        `json:"expiresAt"`
}

type wireStatus struct {
	Version   string `json:"version"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	Signature string `json:"signature,omitempty"`
}

func signRequest(request wireRequest, privateKey ed25519.PrivateKey) (string, error) {
	payload, err := requestPayload(request)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)), nil
}

func verifyRequest(request wireRequest) bool {
	publicKey, ok := decodePublicKey(request.PublicKey)
	if !ok {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(request.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	payload, err := requestPayload(request)
	return err == nil && ed25519.Verify(publicKey, payload, signature)
}

func requestPayload(request wireRequest) ([]byte, error) {
	copy := request
	copy.Signature = ""
	return json.Marshal(copy)
}

func signApproval(request wireRequest, challenge wireChallenge, status string, privateKey ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, approvalPayload(request, challenge, status)))
}

func verifyApproval(request wireRequest, challenge wireChallenge, status, signature string) bool {
	publicKey, ok := decodePublicKey(challenge.PublicKey)
	if !ok {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	return err == nil && len(decoded) == ed25519.SignatureSize && ed25519.Verify(publicKey, approvalPayload(request, challenge, status), decoded)
}

func approvalPayload(request wireRequest, challenge wireChallenge, status string) []byte {
	value := struct {
		Purpose        string `json:"purpose"`
		RequestID      string `json:"requestId"`
		RequesterKey   string `json:"requesterKey"`
		RequesterNonce string `json:"requesterNonce"`
		ResponderKey   string `json:"responderKey"`
		ResponderNonce string `json:"responderNonce"`
		ApprovalStatus string `json:"status"`
	}{
		Purpose: "agentx-pair-approval-v1", RequestID: request.ID,
		RequesterKey: request.PublicKey, RequesterNonce: request.Nonce,
		ResponderKey: challenge.PublicKey, ResponderNonce: challenge.Nonce,
		ApprovalStatus: status,
	}
	payload, _ := json.Marshal(value)
	return payload
}

func verificationCode(request wireRequest, challenge wireChallenge) string {
	payload := approvalPayload(request, challenge, "verify")
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(sum[:4])%1_000_000)
}

func randomToken(bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validPublicKey(value string) bool {
	_, ok := decodePublicKey(value)
	return ok
}

func decodePublicKey(value string) (ed25519.PublicKey, bool) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(decoded), true
}

func validDevice(device model.Device) bool {
	if !deviceIDPattern.MatchString(device.ID) || device.Name == "" || len([]rune(device.Name)) > 60 || !utf8.ValidString(device.Name) {
		return false
	}
	for _, character := range device.Name {
		if unicode.IsControl(character) {
			return false
		}
	}
	if device.OS != "darwin" && device.OS != "windows" && device.OS != "linux" {
		return false
	}
	return strings.Contains(" amd64 arm64 386 ", " "+device.Arch+" ")
}
