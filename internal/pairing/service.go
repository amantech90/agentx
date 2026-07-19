package pairing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"agentx/internal/model"
)

const (
	defaultListenAddress = ":41937"
	requestTTL           = 2 * time.Minute
	maximumPending       = 16
	maximumBodyBytes     = 32 * 1024
)

type Target struct {
	Device    model.Device
	Endpoint  string
	PublicKey string
}

type pairState struct {
	view      model.PairingRequest
	request   wireRequest
	challenge wireChallenge
	endpoint  string
	signature string
}

type Service struct {
	mu            sync.RWMutex
	store         *Store
	listenAddress string
	local         model.Device
	identity      Identity
	listener      net.Listener
	server        *http.Server
	client        *http.Client
	requests      map[string]*pairState
	lastRequest   map[string]time.Time
	resolveTarget func(string) (Target, bool)
	emit          func(model.PairingSnapshot)
	trustChanged  func()
	ctx           context.Context
	cancel        context.CancelFunc
	started       bool
}

func NewService(store *Store) *Service {
	return newService(store, defaultListenAddress)
}

func newService(store *Store, listenAddress string) *Service {
	return &Service{
		store: store, listenAddress: listenAddress,
		client:   &http.Client{Timeout: 4 * time.Second},
		requests: make(map[string]*pairState), lastRequest: make(map[string]time.Time),
		resolveTarget: func(string) (Target, bool) { return Target{}, false },
		emit:          func(model.PairingSnapshot) {}, trustChanged: func() {},
	}
}

func (s *Service) SetEmitter(emit func(model.PairingSnapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit == nil {
		s.emit = func(model.PairingSnapshot) {}
		return
	}
	s.emit = emit
}

func (s *Service) SetTrustChanged(callback func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if callback == nil {
		s.trustChanged = func() {}
		return
	}
	s.trustChanged = callback
}

func (s *Service) SetTargetResolver(resolver func(string) (Target, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resolver == nil {
		s.resolveTarget = func(string) (Target, bool) { return Target{}, false }
		return
	}
	s.resolveTarget = resolver
}

func (s *Service) Start(parent context.Context, device model.Device) error {
	if parent == nil || !validDevice(device) {
		return errors.New("pairing startup metadata is invalid")
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	identity, err := s.store.Identity()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		return fmt.Errorf("listen for Agent X pairing: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pair/requests", s.handleRequests)
	mux.HandleFunc("/v1/pair/requests/", s.handleStatus)
	server := &http.Server{
		Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024,
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		cancel()
		_ = listener.Close()
		return nil
	}
	s.local = device
	s.identity = identity
	s.listener = listener
	s.server = server
	s.ctx = ctx
	s.cancel = cancel
	s.started = true
	s.mu.Unlock()

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			// The Wails layer reports startup errors. Runtime listener failures are
			// intentionally contained; discovery will stop yielding a usable peer.
		}
	}()
	go s.expireLoop(ctx)
	go func() {
		<-ctx.Done()
		s.Stop()
	}()
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	server := s.server
	cancel := s.cancel
	s.started = false
	s.server = nil
	s.listener = nil
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server != nil {
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = server.Shutdown(ctx)
	}
}

func (s *Service) Endpoint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Service) PublicKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identity.PublicKey
}

func (s *Service) IsTrusted(deviceID, publicKey string) bool {
	return s.store.IsTrusted(deviceID, publicKey)
}

func (s *Service) TrustedDevices() []model.Device {
	return s.store.TrustedDevices()
}

func (s *Service) Snapshot() model.PairingSnapshot {
	s.mu.RLock()
	requests := make([]model.PairingRequest, 0, len(s.requests))
	for _, state := range s.requests {
		requests = append(requests, state.view)
	}
	s.mu.RUnlock()
	sort.Slice(requests, func(i, j int) bool { return requests[i].ExpiresAt < requests[j].ExpiresAt })
	return model.PairingSnapshot{Requests: requests, PairedDevices: s.store.TrustedDevices()}
}

func (s *Service) RequestPairing(deviceID string) (model.PairingSnapshot, error) {
	s.mu.RLock()
	resolver := s.resolveTarget
	local := s.local
	identity := s.identity
	started := s.started
	ctx := s.ctx
	s.mu.RUnlock()
	if !started {
		return model.PairingSnapshot{}, errors.New("pairing service is not running")
	}
	target, ok := resolver(strings.TrimSpace(deviceID))
	if !ok || !validDevice(target.Device) {
		return model.PairingSnapshot{}, errors.New("nearby device is no longer available")
	}
	if !validPublicKey(target.PublicKey) {
		return model.PairingSnapshot{}, errors.New("nearby device must update Agent X before pairing")
	}
	baseURL, err := endpointBaseURL(target.Endpoint)
	if err != nil {
		return model.PairingSnapshot{}, errors.New("nearby device endpoint is invalid")
	}
	requestID, err := randomToken(18)
	if err != nil {
		return model.PairingSnapshot{}, err
	}
	nonce, err := randomToken(24)
	if err != nil {
		return model.PairingSnapshot{}, err
	}
	request := wireRequest{
		Version: protocolVersion, ID: requestID, Device: publicDevice(local),
		PublicKey: identity.PublicKey, Nonce: nonce, IssuedAt: time.Now().Unix(),
	}
	request.Signature, err = signRequest(request, identity.PrivateKey)
	if err != nil {
		return model.PairingSnapshot{}, err
	}
	var challenge wireChallenge
	if err := s.postJSON(ctx, baseURL+"/v1/pair/requests", request, &challenge); err != nil {
		return model.PairingSnapshot{}, fmt.Errorf("request pairing: %w", err)
	}
	if err := validateChallenge(request, target, challenge); err != nil {
		return model.PairingSnapshot{}, err
	}
	state := &pairState{
		request: request, challenge: challenge, endpoint: baseURL,
		view: model.PairingRequest{
			ID: request.ID, Device: publicDevice(target.Device), Direction: "outgoing",
			Code: verificationCode(request, challenge), Status: "pending",
			ExpiresAt: time.Unix(challenge.ExpiresAt, 0).UTC().Format(time.RFC3339),
		},
	}
	s.mu.Lock()
	s.requests[request.ID] = state
	s.mu.Unlock()
	s.notify()
	go s.pollApproval(ctx, request.ID)
	return s.Snapshot(), nil
}

func (s *Service) ApprovePairing(requestID string) (model.PairingSnapshot, error) {
	s.mu.Lock()
	state := s.requests[strings.TrimSpace(requestID)]
	if state == nil || state.view.Direction != "incoming" || state.view.Status != "pending" {
		s.mu.Unlock()
		return model.PairingSnapshot{}, errors.New("pairing request is no longer pending")
	}
	if time.Now().Unix() > state.challenge.ExpiresAt {
		state.view.Status = "expired"
		s.mu.Unlock()
		return model.PairingSnapshot{}, errors.New("pairing request expired")
	}
	identity := s.identity
	request := state.request
	challenge := state.challenge
	s.mu.Unlock()

	if err := s.store.Trust(request.Device, request.PublicKey); err != nil {
		return model.PairingSnapshot{}, err
	}
	signature := signApproval(request, challenge, "approved", identity.PrivateKey)
	s.mu.Lock()
	state = s.requests[requestID]
	if state != nil {
		state.view.Status = "approved"
		state.signature = signature
	}
	trustChanged := s.trustChanged
	s.mu.Unlock()
	trustChanged()
	s.notify()
	return s.Snapshot(), nil
}

func (s *Service) RejectPairing(requestID string) (model.PairingSnapshot, error) {
	s.mu.Lock()
	state := s.requests[strings.TrimSpace(requestID)]
	if state == nil || state.view.Direction != "incoming" || state.view.Status != "pending" {
		s.mu.Unlock()
		return model.PairingSnapshot{}, errors.New("pairing request is no longer pending")
	}
	state.view.Status = "rejected"
	s.mu.Unlock()
	s.notify()
	return s.Snapshot(), nil
}

func (s *Service) RemovePairedDevice(deviceID string) (model.PairingSnapshot, error) {
	if err := s.store.RemoveTrust(strings.TrimSpace(deviceID)); err != nil {
		return model.PairingSnapshot{}, err
	}
	s.mu.RLock()
	trustChanged := s.trustChanged
	s.mu.RUnlock()
	trustChanged()
	s.notify()
	return s.Snapshot(), nil
}

func (s *Service) handleRequests(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		http.Error(writer, "invalid pairing request", http.StatusBadRequest)
		return
	}
	source, _, _ := net.SplitHostPort(request.RemoteAddr)
	now := time.Now()
	s.mu.Lock()
	if len(s.requests) >= maximumPending || now.Sub(s.lastRequest[source]) < time.Second {
		s.mu.Unlock()
		http.Error(writer, "pairing request rate limited", http.StatusTooManyRequests)
		return
	}
	s.lastRequest[source] = now
	s.mu.Unlock()

	request.Body = http.MaxBytesReader(writer, request.Body, maximumBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var incoming wireRequest
	if err := decoder.Decode(&incoming); err != nil || !validWireRequest(incoming, now) || !verifyRequest(incoming) {
		http.Error(writer, "invalid signed pairing request", http.StatusBadRequest)
		return
	}

	nonce, err := randomToken(24)
	if err != nil {
		http.Error(writer, "pairing unavailable", http.StatusInternalServerError)
		return
	}
	s.mu.RLock()
	local := s.local
	identity := s.identity
	s.mu.RUnlock()
	challenge := wireChallenge{
		Version: protocolVersion, ID: incoming.ID, Device: publicDevice(local),
		PublicKey: identity.PublicKey, Nonce: nonce, ExpiresAt: now.Add(requestTTL).Unix(),
	}
	state := &pairState{
		request: incoming, challenge: challenge,
		view: model.PairingRequest{
			ID: incoming.ID, Device: publicDevice(incoming.Device), Direction: "incoming",
			Code: verificationCode(incoming, challenge), Status: "pending",
			ExpiresAt: time.Unix(challenge.ExpiresAt, 0).UTC().Format(time.RFC3339),
		},
	}
	s.mu.Lock()
	if existing := s.requests[incoming.ID]; existing != nil {
		s.mu.Unlock()
		http.Error(writer, "pairing request already exists", http.StatusConflict)
		return
	}
	s.requests[incoming.ID] = state
	s.mu.Unlock()
	writeJSON(writer, http.StatusCreated, challenge)
	s.notify()
}

func (s *Service) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(request.URL.Path, "/v1/pair/requests/")
	if id == "" || strings.Contains(id, "/") || len(id) > 64 {
		http.NotFound(writer, request)
		return
	}
	s.mu.RLock()
	state := s.requests[id]
	if state == nil || state.view.Direction != "incoming" {
		s.mu.RUnlock()
		http.NotFound(writer, request)
		return
	}
	status := wireStatus{
		Version: protocolVersion, ID: id, Status: state.view.Status, Signature: state.signature,
	}
	s.mu.RUnlock()
	writeJSON(writer, http.StatusOK, status)
}

func (s *Service) pollApproval(ctx context.Context, requestID string) {
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.RLock()
		state := s.requests[requestID]
		if state == nil || state.view.Status != "pending" {
			s.mu.RUnlock()
			return
		}
		copy := *state
		s.mu.RUnlock()
		if time.Now().Unix() > copy.challenge.ExpiresAt {
			s.setStatus(requestID, "expired")
			return
		}
		var status wireStatus
		if err := s.getJSON(ctx, copy.endpoint+"/v1/pair/requests/"+requestID, &status); err != nil {
			continue
		}
		if status.ID != requestID || status.Version != protocolVersion {
			continue
		}
		switch status.Status {
		case "rejected", "expired":
			s.setStatus(requestID, status.Status)
			return
		case "approved":
			if !verifyApproval(copy.request, copy.challenge, "approved", status.Signature) {
				continue
			}
			if err := s.store.Trust(copy.challenge.Device, copy.challenge.PublicKey); err != nil {
				s.setStatus(requestID, "failed")
				return
			}
			s.setStatus(requestID, "approved")
			s.mu.RLock()
			trustChanged := s.trustChanged
			s.mu.RUnlock()
			trustChanged()
			return
		}
	}
}

func (s *Service) setStatus(requestID, status string) {
	s.mu.Lock()
	if state := s.requests[requestID]; state != nil {
		state.view.Status = status
	}
	s.mu.Unlock()
	s.notify()
}

func (s *Service) expireLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		changed := false
		s.mu.Lock()
		for id, state := range s.requests {
			expiresAt, _ := time.Parse(time.RFC3339, state.view.ExpiresAt)
			if state.view.Status == "pending" && now.After(expiresAt) {
				state.view.Status = "expired"
				changed = true
			}
			if now.After(expiresAt.Add(30 * time.Second)) {
				delete(s.requests, id)
				changed = true
			}
		}
		s.mu.Unlock()
		if changed {
			s.notify()
		}
	}
}

func (s *Service) notify() {
	s.mu.RLock()
	emit := s.emit
	s.mu.RUnlock()
	emit(s.Snapshot())
}

func (s *Service) postJSON(ctx context.Context, url string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	return s.doJSON(httpRequest, output)
}

func (s *Service) getJSON(ctx context.Context, url string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return s.doJSON(request, output)
}

func (s *Service) doJSON(request *http.Request, output any) error {
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("peer returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumBodyBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(output)
}

func endpointBaseURL(endpoint string) (string, error) {
	address, err := netip.ParseAddrPort(endpoint)
	if err != nil || !address.Addr().IsValid() || address.Addr().IsUnspecified() || address.Addr().IsMulticast() || address.Port() == 0 {
		return "", errors.New("invalid endpoint")
	}
	return "http://" + address.String(), nil
}

func validateChallenge(request wireRequest, target Target, challenge wireChallenge) error {
	if challenge.Version != protocolVersion || challenge.ID != request.ID || challenge.Device.ID != target.Device.ID ||
		challenge.PublicKey != target.PublicKey || !validPublicKey(challenge.PublicKey) || challenge.Nonce == "" ||
		challenge.ExpiresAt <= time.Now().Unix() || challenge.ExpiresAt > time.Now().Add(requestTTL+10*time.Second).Unix() {
		return errors.New("nearby device returned an invalid pairing challenge")
	}
	return nil
}

func validWireRequest(request wireRequest, now time.Time) bool {
	return request.Version == protocolVersion && len(request.ID) >= 20 && len(request.ID) <= 64 &&
		len(request.Nonce) >= 20 && len(request.Nonce) <= 64 && validDevice(request.Device) &&
		validPublicKey(request.PublicKey) && now.Sub(time.Unix(request.IssuedAt, 0)) < requestTTL &&
		time.Unix(request.IssuedAt, 0).Sub(now) < 30*time.Second
}

func publicDevice(device model.Device) model.Device {
	device.Hostname = ""
	device.Trusted = false
	device.Configured = true
	return device
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
