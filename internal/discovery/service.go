package discovery

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"agentx/internal/model"
)

const (
	serviceType      = "_agentx._tcp"
	servicePort      = 41937
	protocolVersion  = "1"
	deviceTimeout    = 15 * time.Second
	maximumAppLength = 32
)

var deviceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Entry struct {
	Text    []string
	Addrs   []netip.Addr
	Port    uint16
	Removed bool
}

type Peer struct {
	Device         model.Device
	Endpoint       string
	BridgeEndpoint string
	PublicKey      string
}

type registration interface {
	Shutdown()
}

type transport interface {
	Start(context.Context, model.Device, string, string, uint16, chan<- Entry) (registration, error)
}

type seenDevice struct {
	device         model.Device
	endpoint       string
	bridgeEndpoint string
	publicKey      string
}

type Service struct {
	mu           sync.RWMutex
	transport    transport
	localID      string
	devices      map[string]seenDevice
	emit         func([]model.Device)
	isTrusted    func(string, string) bool
	cancel       context.CancelFunc
	registration registration
	started      bool
	wg           sync.WaitGroup
}

func New() *Service {
	return &Service{
		transport: zeroconfTransport{},
		devices:   make(map[string]seenDevice),
		emit:      func([]model.Device) {},
		isTrusted: func(string, string) bool { return false },
	}
}

func newRegistry(localID string) *Service {
	return &Service{
		localID: localID, devices: make(map[string]seenDevice),
		emit:      func([]model.Device) {},
		isTrusted: func(string, string) bool { return false },
	}
}

func (s *Service) SetEmitter(emit func([]model.Device)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit == nil {
		s.emit = func([]model.Device) {}
		return
	}
	s.emit = emit
}

func (s *Service) SetTrustProvider(isTrusted func(string, string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if isTrusted == nil {
		s.isTrusted = func(string, string) bool { return false }
		return
	}
	s.isTrusted = isTrusted
}

func (s *Service) Start(parent context.Context, device model.Device, appVersion, publicKey string, bridgePort uint16) error {
	if parent == nil {
		return errors.New("discovery context is required")
	}
	if !validLocalDevice(device) {
		return errors.New("local device metadata is invalid")
	}
	appVersion = strings.TrimSpace(appVersion)
	if appVersion == "" || len(appVersion) > maximumAppLength {
		return errors.New("application version is invalid")
	}
	if publicKey != "" && !validPublicKey(publicKey) {
		return errors.New("pairing public key is invalid")
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.localID = device.ID
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(parent)
	entries := make(chan Entry, 32)
	registered, err := s.transport.Start(ctx, device, appVersion, publicKey, bridgePort, entries)
	if err != nil {
		cancel()
		return fmt.Errorf("start Agent X discovery: %w", err)
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		registered.Shutdown()
		cancel()
		return nil
	}
	s.started = true
	s.cancel = cancel
	s.registration = registered
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run(ctx, entries)
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	registered := s.registration
	s.started = false
	s.cancel = nil
	s.registration = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if registered != nil {
		registered.Shutdown()
	}
	s.wg.Wait()
}

func (s *Service) Devices() []model.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return devicesFromRegistry(s.devices)
}

func (s *Service) Lookup(deviceID string) (Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen, ok := s.devices[deviceID]
	if !ok || seen.endpoint == "" || seen.publicKey == "" {
		return Peer{}, false
	}
	return Peer{
		Device: seen.device, Endpoint: seen.endpoint,
		BridgeEndpoint: seen.bridgeEndpoint, PublicKey: seen.publicKey,
	}, true
}

func (s *Service) RefreshTrust() {
	s.mu.Lock()
	changed := false
	for id, seen := range s.devices {
		trusted := s.isTrusted(id, seen.publicKey)
		if seen.device.Trusted == trusted {
			continue
		}
		seen.device.Trusted = trusted
		s.devices[id] = seen
		changed = true
	}
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

func (s *Service) run(ctx context.Context, entries <-chan Entry) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}
			if s.observe(entry) {
				s.notify()
			}
		}
	}
}

func (s *Service) observe(entry Entry) bool {
	device, _, ok := parseDeviceTXT(entry.Text)
	if !ok || device.ID == s.localID {
		return false
	}
	publicKey := publicKeyFromTXT(entry.Text)
	endpoint := selectEndpoint(entry.Addrs, entry.Port)
	bridgeEndpoint := selectEndpoint(entry.Addrs, bridgePortFromTXT(entry.Text))
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Removed {
		if _, exists := s.devices[device.ID]; !exists {
			return false
		}
		delete(s.devices, device.ID)
		return true
	}
	device.Trusted = s.isTrusted(device.ID, publicKey)
	previous, exists := s.devices[device.ID]
	current := seenDevice{device: device, endpoint: endpoint, bridgeEndpoint: bridgeEndpoint, publicKey: publicKey}
	s.devices[device.ID] = current
	return !exists || previous != current
}

func (s *Service) notify() {
	s.mu.RLock()
	emit := s.emit
	devices := devicesFromRegistry(s.devices)
	s.mu.RUnlock()
	emit(devices)
}

func devicesFromRegistry(registry map[string]seenDevice) []model.Device {
	devices := make([]model.Device, 0, len(registry))
	for _, seen := range registry {
		devices = append(devices, seen.device)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Name == devices[j].Name {
			return devices[i].ID < devices[j].ID
		}
		return strings.ToLower(devices[i].Name) < strings.ToLower(devices[j].Name)
	})
	return devices
}

func parseDeviceTXT(records []string) (model.Device, string, bool) {
	values := make(map[string]string, len(records))
	for _, record := range records {
		key, value, found := strings.Cut(record, "=")
		if !found || key == "" {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	name := values["name"]
	version := values["app"]
	if values["v"] != protocolVersion || !deviceIDPattern.MatchString(values["id"]) {
		return model.Device{}, "", false
	}
	if !validDeviceName(name) || !validPlatform(values["os"], values["arch"]) {
		return model.Device{}, "", false
	}
	if version == "" || len(version) > maximumAppLength || !utf8.ValidString(version) {
		return model.Device{}, "", false
	}
	return model.Device{
		ID: values["id"], Name: name, OS: values["os"], Arch: values["arch"],
		Configured: true, Trusted: false,
	}, version, true
}

func publicKeyFromTXT(records []string) string {
	for _, record := range records {
		key, value, found := strings.Cut(record, "=")
		if found && key == "key" {
			value = strings.TrimSpace(value)
			if validPublicKey(value) {
				return value
			}
			return ""
		}
	}
	return ""
}

func validPublicKey(encoded string) bool {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && len(decoded) == ed25519.PublicKeySize
}

func bridgePortFromTXT(records []string) uint16 {
	for _, record := range records {
		key, value, found := strings.Cut(record, "=")
		if !found || key != "bridge" {
			continue
		}
		port, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
		if err == nil && port > 0 {
			return uint16(port)
		}
		return 0
	}
	return 0
}

// carrierNAT is the shared address space (RFC 6598) that Tailscale and
// carrier-grade NAT hand out. It looks routable but is almost never reachable
// between two machines on a plain LAN, so it must lose to real LAN addresses.
var carrierNAT = netip.MustParsePrefix("100.64.0.0/10")

// selectEndpoint chooses which advertised address a paired device should dial.
// A machine running VPNs advertises an address on every interface — Tailscale,
// corporate tunnels, link-local, IPv6 — and picking the wrong one leaves the
// device paired but permanently offline because the bridge dial never reaches
// it. Addresses are ranked so a real private-LAN IPv4 (192.168/16, 10/8,
// 172.16/12) always wins over tunnel and non-LAN addresses.
func selectEndpoint(addresses []netip.Addr, port uint16) string {
	if port == 0 {
		return ""
	}
	best := netip.Addr{}
	bestScore := -1
	for _, address := range addresses {
		score := endpointScore(address)
		if score < 0 {
			continue
		}
		if bestScore < 0 || score < bestScore {
			best = address.Unmap()
			bestScore = score
		}
	}
	if bestScore < 0 {
		return ""
	}
	return netip.AddrPortFrom(best, port).String()
}

// endpointScore ranks an advertised address by how likely it is to be
// reachable across a shared local network. Lower is better; a negative score
// means the address is unusable and must be skipped entirely.
func endpointScore(address netip.Addr) int {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() ||
		address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return -1
	}
	switch {
	case address.Is4() && address.IsPrivate():
		return 0 // real LAN: 192.168/16, 10/8, 172.16/12
	case address.Is4() && carrierNAT.Contains(address):
		return 3 // 100.64/10 — Tailscale / carrier-grade NAT tunnel
	case address.Is4():
		return 1 // other global IPv4
	case address.IsPrivate():
		return 4 // IPv6 unique-local (fc00::/7)
	default:
		return 5 // global IPv6
	}
}

func validLocalDevice(device model.Device) bool {
	return deviceIDPattern.MatchString(device.ID) && validDeviceName(device.Name) && validPlatform(device.OS, device.Arch)
}

func validDeviceName(name string) bool {
	if name == "" || len([]rune(name)) > 60 || !utf8.ValidString(name) {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validPlatform(osName, architecture string) bool {
	switch osName {
	case "darwin", "windows", "linux":
	default:
		return false
	}
	switch architecture {
	case "amd64", "arm64", "386":
		return true
	default:
		return false
	}
}
