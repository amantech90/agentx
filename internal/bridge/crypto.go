package bridge

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"agentx/internal/pairing"
)

const (
	authVersion       = "1"
	authWindow        = 30 * time.Second
	headerDeviceID    = "X-Agentx-Device-Id"
	headerTargetID    = "X-Agentx-Target-Id"
	headerTimestamp   = "X-Agentx-Timestamp"
	headerNonce       = "X-Agentx-Nonce"
	headerSignature   = "X-Agentx-Signature"
	bridgeSubprotocol = "agentx.bridge.v1"
)

var bridgeDeviceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type authProof struct {
	Purpose        string `json:"purpose"`
	Version        string `json:"version"`
	DeviceID       string `json:"deviceId"`
	TargetDeviceID string `json:"targetDeviceId"`
	Timestamp      int64  `json:"timestamp"`
	Nonce          string `json:"nonce"`
}

func signedAuthHeaders(localID, targetID string, identity pairing.Identity, now time.Time) (http.Header, error) {
	nonce, err := randomBridgeToken(24)
	if err != nil {
		return nil, err
	}
	proof := authProof{
		Purpose: "agentx-workspace-bridge", Version: authVersion,
		DeviceID: localID, TargetDeviceID: targetID, Timestamp: now.Unix(), Nonce: nonce,
	}
	payload, err := json.Marshal(proof)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(identity.PrivateKey, payload)
	header := make(http.Header)
	header.Set(headerDeviceID, proof.DeviceID)
	header.Set(headerTargetID, proof.TargetDeviceID)
	header.Set(headerTimestamp, strconv.FormatInt(proof.Timestamp, 10))
	header.Set(headerNonce, proof.Nonce)
	header.Set(headerSignature, base64.StdEncoding.EncodeToString(signature))
	return header, nil
}

func authProofFromRequest(request *http.Request) (authProof, []byte, error) {
	timestamp, err := strconv.ParseInt(request.Header.Get(headerTimestamp), 10, 64)
	if err != nil {
		return authProof{}, nil, errors.New("invalid bridge timestamp")
	}
	proof := authProof{
		Purpose: "agentx-workspace-bridge", Version: authVersion,
		DeviceID: request.Header.Get(headerDeviceID), TargetDeviceID: request.Header.Get(headerTargetID),
		Timestamp: timestamp, Nonce: request.Header.Get(headerNonce),
	}
	signature, err := base64.StdEncoding.DecodeString(request.Header.Get(headerSignature))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return authProof{}, nil, errors.New("invalid bridge signature")
	}
	return proof, signature, nil
}

func verifyAuthProof(proof authProof, signature []byte, targetID, encodedPublicKey string, now time.Time) bool {
	if proof.Purpose != "agentx-workspace-bridge" || proof.Version != authVersion ||
		!bridgeDeviceIDPattern.MatchString(proof.DeviceID) || proof.TargetDeviceID != targetID ||
		len(proof.Nonce) < 24 || len(proof.Nonce) > 64 {
		return false
	}
	issued := time.Unix(proof.Timestamp, 0)
	if now.Sub(issued) > authWindow || issued.Sub(now) > authWindow {
		return false
	}
	publicKey, err := base64.StdEncoding.DecodeString(encodedPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	payload, err := json.Marshal(proof)
	return err == nil && ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature)
}

func bridgeCertificate(identity pairing.Identity, deviceID string, now time.Time) (tls.Certificate, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "AgentX-" + deviceID},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, identity.PrivateKey.Public(), identity.PrivateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: identity.PrivateKey}, nil
}

func pinnedTLSConfig(encodedPublicKey string, now func() time.Time) (*tls.Config, error) {
	expected, err := base64.StdEncoding.DecodeString(encodedPublicKey)
	if err != nil || len(expected) != ed25519.PublicKeySize {
		return nil, errors.New("invalid pinned bridge key")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		// Certificate-chain verification is replaced with strict public-key
		// pinning to the Ed25519 identity verified during pairing.
		InsecureSkipVerify: true, //nolint:gosec
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 {
				return errors.New("bridge returned an invalid certificate chain")
			}
			certificate := state.PeerCertificates[0]
			publicKey, ok := certificate.PublicKey.(ed25519.PublicKey)
			if !ok || !bytes.Equal(publicKey, expected) {
				return errors.New("bridge certificate key does not match paired device")
			}
			current := now()
			if current.Before(certificate.NotBefore) || current.After(certificate.NotAfter) {
				return errors.New("bridge certificate is expired or not active")
			}
			return certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature)
		},
	}, nil
}

func randomBridgeToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
