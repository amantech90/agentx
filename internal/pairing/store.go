package pairing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"agentx/internal/fsx"
	"agentx/internal/model"
)

const storeVersion = 1

type Identity struct {
	PublicKey  string
	PrivateKey ed25519.PrivateKey
}

type trustedRecord struct {
	Device    model.Device `json:"device"`
	PublicKey string       `json:"publicKey"`
	PairedAt  string       `json:"pairedAt"`
}

type storedData struct {
	Version    int             `json:"version"`
	PrivateKey string          `json:"privateKey"`
	Trusted    []trustedRecord `json:"trustedDevices"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Identity() (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if errors.Is(err, os.ErrNotExist) {
		_, privateKey, generateErr := ed25519.GenerateKey(rand.Reader)
		if generateErr != nil {
			return Identity{}, fmt.Errorf("generate pairing identity: %w", generateErr)
		}
		data = storedData{
			Version: storeVersion, PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
			Trusted: []trustedRecord{},
		}
		if saveErr := s.save(data); saveErr != nil {
			return Identity{}, saveErr
		}
	} else if err != nil {
		return Identity{}, err
	}
	return identityFromData(data)
}

func (s *Store) Trust(device model.Device, publicKey string) error {
	if !validDevice(device) || !validPublicKey(publicKey) {
		return errors.New("trusted device identity is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	device.Configured = true
	device.Trusted = true
	record := trustedRecord{Device: device, PublicKey: publicKey, PairedAt: time.Now().UTC().Format(time.RFC3339)}
	found := false
	for index := range data.Trusted {
		if data.Trusted[index].Device.ID == device.ID {
			data.Trusted[index] = record
			found = true
			break
		}
	}
	if !found {
		data.Trusted = append(data.Trusted, record)
	}
	return s.save(data)
}

func (s *Store) RemoveTrust(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	filtered := data.Trusted[:0]
	for _, record := range data.Trusted {
		if record.Device.ID != strings.TrimSpace(deviceID) {
			filtered = append(filtered, record)
		}
	}
	data.Trusted = filtered
	return s.save(data)
}

func (s *Store) IsTrusted(deviceID, publicKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return false
	}
	for _, record := range data.Trusted {
		if record.Device.ID == deviceID && record.PublicKey == publicKey {
			return true
		}
	}
	return false
}

func (s *Store) TrustedPublicKey(deviceID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return "", false
	}
	deviceID = strings.TrimSpace(deviceID)
	for _, record := range data.Trusted {
		if record.Device.ID == deviceID && validPublicKey(record.PublicKey) {
			return record.PublicKey, true
		}
	}
	return "", false
}

func (s *Store) TrustedDevices() []model.Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return []model.Device{}
	}
	devices := make([]model.Device, 0, len(data.Trusted))
	for _, record := range data.Trusted {
		device := record.Device
		device.Trusted = true
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool {
		return strings.ToLower(devices[i].Name) < strings.ToLower(devices[j].Name)
	})
	return devices
}

func (s *Store) load() (storedData, error) {
	contents, err := os.ReadFile(s.path)
	if err != nil {
		return storedData{}, err
	}
	var data storedData
	if err := json.Unmarshal(contents, &data); err != nil {
		return storedData{}, fmt.Errorf("decode pairing store: %w", err)
	}
	if data.Version != storeVersion {
		return storedData{}, fmt.Errorf("unsupported pairing store version %d", data.Version)
	}
	if data.Trusted == nil {
		data.Trusted = []trustedRecord{}
	}
	if _, err := identityFromData(data); err != nil {
		return storedData{}, err
	}
	return data, nil
}

func (s *Store) save(data storedData) error {
	data.Version = storeVersion
	contents, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pairing store: %w", err)
	}
	contents = append(contents, '\n')
	if err := fsx.WriteFileAtomically(s.path, contents); err != nil {
		return fmt.Errorf("save pairing store: %w", err)
	}
	return nil
}

func identityFromData(data storedData) (Identity, error) {
	privateKey, err := base64.StdEncoding.DecodeString(data.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return Identity{}, errors.New("pairing private key is invalid")
	}
	private := ed25519.PrivateKey(append([]byte(nil), privateKey...))
	public := private.Public().(ed25519.PublicKey)
	return Identity{
		PublicKey: base64.StdEncoding.EncodeToString(public), PrivateKey: private,
	}, nil
}
