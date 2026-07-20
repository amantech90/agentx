package bridge

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentx/internal/model"
	"agentx/internal/pairing"
	"github.com/coder/websocket"
)

const (
	defaultListenAddress = ":41938"
	maximumMessageBytes  = 4 * 1024 * 1024
	maximumWorkspaces    = 1000
	maximumSessions      = 1000
	maximumProviders     = 50
)

// bridgePingInterval keeps the peer websocket warm. Consumer routers and Wi-Fi
// extenders drop idle TCP flows after as little as a minute, which would
// silently strand the connection (a half-open socket never surfaces an error
// to Read). A regular ping/pong keeps the NAT mapping alive and detects a dead
// peer within bridgePingTimeout so the dialer reconnects. They are vars so
// tests can shorten them.
var (
	bridgePingInterval = 15 * time.Second
	bridgePingTimeout  = 8 * time.Second
)

type pendingCall struct {
	deviceID string
	result   chan envelope
}

type peerConnection struct {
	device model.Device
	conn   *websocket.Conn
	done   chan struct{}
	once   sync.Once
}

func (peer *peerConnection) close(code websocket.StatusCode, reason string) {
	peer.once.Do(func() {
		_ = peer.conn.Close(code, reason)
		close(peer.done)
	})
}

type dialTask struct {
	target Target
	cancel context.CancelFunc
}

type Service struct {
	mu            sync.RWMutex
	store         *pairing.Store
	listenAddress string
	local         model.Device
	identity      pairing.Identity
	handlers      Handlers
	emit          func(model.BridgeSnapshot)
	listener      net.Listener
	server        *http.Server
	ctx           context.Context
	cancel        context.CancelFunc
	peers         map[string]*peerConnection
	remote        map[string]model.RemoteDeviceState
	pending       map[string]pendingCall
	targets       map[string]Target
	dialers       map[string]dialTask
	nonces        map[string]time.Time
	started       bool
	wg            sync.WaitGroup
}

func NewService(store *pairing.Store) *Service {
	return newService(store, defaultListenAddress)
}

func newService(store *pairing.Store, listenAddress string) *Service {
	return &Service{
		store: store, listenAddress: listenAddress,
		emit:  func(model.BridgeSnapshot) {},
		peers: make(map[string]*peerConnection), remote: make(map[string]model.RemoteDeviceState),
		pending: make(map[string]pendingCall), targets: make(map[string]Target),
		dialers: make(map[string]dialTask), nonces: make(map[string]time.Time),
	}
}

func (s *Service) SetHandlers(handlers Handlers) {
	s.mu.Lock()
	s.handlers = handlers
	s.mu.Unlock()
}

func (s *Service) SetEmitter(emit func(model.BridgeSnapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if emit == nil {
		s.emit = func(model.BridgeSnapshot) {}
		return
	}
	s.emit = emit
}

func (s *Service) Start(parent context.Context, device model.Device) error {
	if parent == nil || !bridgeDeviceIDPattern.MatchString(device.ID) || strings.TrimSpace(device.Name) == "" {
		return errors.New("bridge startup metadata is invalid")
	}
	if s.store == nil {
		return errors.New("bridge pairing store is required")
	}
	s.mu.RLock()
	alreadyStarted := s.started
	hasStateHandler := s.handlers.State != nil
	s.mu.RUnlock()
	if alreadyStarted {
		return nil
	}
	if !hasStateHandler {
		return errors.New("bridge state handler is required")
	}
	identity, err := s.store.Identity()
	if err != nil {
		return fmt.Errorf("load bridge identity: %w", err)
	}
	certificate, err := bridgeCertificate(identity, device.ID, time.Now())
	if err != nil {
		return fmt.Errorf("create bridge certificate: %w", err)
	}
	listener, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		return fmt.Errorf("listen for Agent X workspace bridge: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/bridge", s.handleBridge)
	server := &http.Server{
		Handler: mux, ReadHeaderTimeout: 4 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 45 * time.Second, MaxHeaderBytes: 16 * 1024,
	}
	tlsListener := tls.NewListener(listener, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
	})

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

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = server.Serve(tlsListener)
	}()
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	server := s.server
	peers := make([]*peerConnection, 0, len(s.peers))
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}
	for _, task := range s.dialers {
		task.cancel()
	}
	s.started = false
	s.cancel = nil
	s.server = nil
	s.listener = nil
	s.dialers = make(map[string]dialTask)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, peer := range peers {
		peer.close(websocket.StatusGoingAway, "Agent X is closing")
	}
	if server != nil {
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		_ = server.Shutdown(ctx)
		stop()
	}
	s.wg.Wait()
}

func (s *Service) Endpoint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Service) Port() uint16 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return 0
	}
	_, portText, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		return 0
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return 0
	}
	return uint16(port)
}

func (s *Service) Snapshot() model.BridgeSnapshot {
	s.mu.RLock()
	devices := make([]model.RemoteDeviceState, 0, len(s.remote))
	for _, state := range s.remote {
		devices = append(devices, copyRemoteState(state))
	}
	s.mu.RUnlock()
	sort.Slice(devices, func(i, j int) bool {
		if strings.EqualFold(devices[i].Device.Name, devices[j].Device.Name) {
			return devices[i].Device.ID < devices[j].Device.ID
		}
		return strings.ToLower(devices[i].Device.Name) < strings.ToLower(devices[j].Device.Name)
	})
	return model.BridgeSnapshot{Devices: devices}
}

func (s *Service) UpdateTargets(targets []Target) {
	trustedDevices := s.store.TrustedDevices()
	trustedIDs := make(map[string]model.Device, len(trustedDevices))
	for _, device := range trustedDevices {
		trustedIDs[device.ID] = device
	}
	validTargets := make(map[string]Target, len(targets))
	for _, target := range targets {
		if target.Device.ID == s.local.ID || !target.Device.Trusted || !validBridgeEndpoint(target.Endpoint) ||
			!s.store.IsTrusted(target.Device.ID, target.PublicKey) {
			continue
		}
		validTargets[target.Device.ID] = target
	}

	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	for id := range s.remote {
		if _, trusted := trustedIDs[id]; !trusted {
			delete(s.remote, id)
			if peer := s.peers[id]; peer != nil {
				go peer.close(websocket.StatusPolicyViolation, "device was forgotten")
			}
		}
	}
	for id, device := range trustedIDs {
		state := s.remote[id]
		state.Device = device
		s.remote[id] = state
	}
	for id, task := range s.dialers {
		target, found := validTargets[id]
		if !found || target.Endpoint != task.target.Endpoint || target.PublicKey != task.target.PublicKey {
			task.cancel()
			delete(s.dialers, id)
		}
	}
	s.targets = validTargets
	ctx := s.ctx
	for id, target := range validTargets {
		if s.local.ID >= id {
			continue
		}
		if _, running := s.dialers[id]; running {
			continue
		}
		dialCtx, cancel := context.WithCancel(ctx)
		s.dialers[id] = dialTask{target: target, cancel: cancel}
		s.wg.Add(1)
		go s.dialLoop(dialCtx, target)
	}
	s.mu.Unlock()
	s.notify()
}

func (s *Service) OpenWorkspace(ctx context.Context, deviceID string, request model.OpenWorkspaceRequest) (model.RemoteDeviceState, error) {
	var state model.RemoteDeviceState
	if err := s.call(ctx, deviceID, methodWorkspaceOpen, request, &state); err != nil {
		return model.RemoteDeviceState{}, err
	}
	if err := s.applyRemoteState(deviceID, state); err != nil {
		return model.RemoteDeviceState{}, err
	}
	return copyRemoteState(state), nil
}

func (s *Service) GetSession(ctx context.Context, deviceID, workspaceID string) (model.SessionSnapshot, error) {
	var snapshot model.SessionSnapshot
	if err := s.call(ctx, deviceID, methodSessionGet, workspaceRequest{WorkspaceID: workspaceID}, &snapshot); err != nil {
		return model.SessionSnapshot{}, err
	}
	s.applyRemoteSession(deviceID, snapshot)
	return snapshot, nil
}

func (s *Service) SendMessage(ctx context.Context, deviceID string, request model.SendMessageRequest) (model.SessionSnapshot, error) {
	var snapshot model.SessionSnapshot
	if err := s.call(ctx, deviceID, methodSessionSend, request, &snapshot); err != nil {
		return model.SessionSnapshot{}, err
	}
	s.applyRemoteSession(deviceID, snapshot)
	return snapshot, nil
}

func (s *Service) ResolveApproval(ctx context.Context, deviceID string, request model.ResolveApprovalRequest) (model.SessionSnapshot, error) {
	var snapshot model.SessionSnapshot
	if err := s.call(ctx, deviceID, methodApprovalResolve, request, &snapshot); err != nil {
		return model.SessionSnapshot{}, err
	}
	s.applyRemoteSession(deviceID, snapshot)
	return snapshot, nil
}

func (s *Service) DeleteConversation(ctx context.Context, deviceID, workspaceID string) (model.SessionSnapshot, error) {
	var snapshot model.SessionSnapshot
	if err := s.call(ctx, deviceID, methodSessionDelete, workspaceRequest{WorkspaceID: workspaceID}, &snapshot); err != nil {
		return model.SessionSnapshot{}, err
	}
	s.applyRemoteSession(deviceID, snapshot)
	return snapshot, nil
}

func (s *Service) PublishState() {
	s.mu.RLock()
	handler := s.handlers.State
	ctx := s.ctx
	s.mu.RUnlock()
	if handler == nil || ctx == nil {
		return
	}
	state, err := handler(ctx)
	if err != nil {
		return
	}
	s.broadcast(kindEvent, methodWorkspaceSnapshot, state)
}

func (s *Service) PublishSession(snapshot model.SessionSnapshot) {
	s.broadcast(kindEvent, methodSessionUpdated, snapshot)
}

func (s *Service) handleBridge(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	proof, signature, err := authProofFromRequest(request)
	if err != nil {
		http.Error(writer, "invalid bridge authentication", http.StatusUnauthorized)
		return
	}
	s.mu.RLock()
	localID := s.local.ID
	s.mu.RUnlock()
	publicKey, trusted := s.store.TrustedPublicKey(proof.DeviceID)
	if !trusted || proof.DeviceID >= localID || !verifyAuthProof(proof, signature, localID, publicKey, time.Now()) || !s.consumeNonce(proof.DeviceID, proof.Nonce) {
		http.Error(writer, "bridge authentication failed", http.StatusUnauthorized)
		return
	}
	device, ok := s.trustedDevice(proof.DeviceID)
	if !ok {
		http.Error(writer, "bridge device is not trusted", http.StatusUnauthorized)
		return
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols: []string{bridgeSubprotocol},
	})
	if err != nil {
		return
	}
	if connection.Subprotocol() != bridgeSubprotocol {
		_ = connection.Close(websocket.StatusPolicyViolation, "bridge protocol is required")
		return
	}
	connection.SetReadLimit(maximumMessageBytes)
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()
	s.servePeer(ctx, device, connection)
}

func (s *Service) consumeNonce(deviceID, nonce string) bool {
	now := time.Now()
	key := deviceID + "\x00" + nonce
	s.mu.Lock()
	defer s.mu.Unlock()
	for value, seenAt := range s.nonces {
		if now.Sub(seenAt) > 2*authWindow {
			delete(s.nonces, value)
		}
	}
	if _, exists := s.nonces[key]; exists {
		return false
	}
	s.nonces[key] = now
	return true
}

func (s *Service) trustedDevice(deviceID string) (model.Device, bool) {
	for _, device := range s.store.TrustedDevices() {
		if device.ID == deviceID {
			return device, true
		}
	}
	return model.Device{}, false
}

func (s *Service) dialLoop(ctx context.Context, target Target) {
	defer s.wg.Done()
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		connection, err := s.dial(ctx, target)
		if err != nil {
			if !waitBridgeRetry(ctx, backoff) {
				return
			}
			if backoff < 4*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
		s.servePeer(ctx, target.Device, connection)
		if !waitBridgeRetry(ctx, backoff) {
			return
		}
	}
}

func (s *Service) dial(ctx context.Context, target Target) (*websocket.Conn, error) {
	s.mu.RLock()
	local := s.local
	identity := s.identity
	s.mu.RUnlock()
	headers, err := signedAuthHeaders(local.ID, target.Device.ID, identity, time.Now())
	if err != nil {
		return nil, err
	}
	tlsConfig, err := pinnedTLSConfig(target.PublicKey, time.Now)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: false, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
	dialCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(dialCtx, "wss://"+target.Endpoint+"/v1/bridge", &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: headers, Subprotocols: []string{bridgeSubprotocol},
	})
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("bridge returned %s: %w", response.Status, err)
		}
		return nil, err
	}
	connection.SetReadLimit(maximumMessageBytes)
	if connection.Subprotocol() != bridgeSubprotocol {
		_ = connection.Close(websocket.StatusPolicyViolation, "bridge protocol is required")
		return nil, errors.New("peer did not negotiate the Agent X bridge protocol")
	}
	return connection, nil
}

func (s *Service) servePeer(ctx context.Context, device model.Device, connection *websocket.Conn) {
	if ctx == nil {
		_ = connection.Close(websocket.StatusGoingAway, "bridge is stopping")
		return
	}
	peer := &peerConnection{device: device, conn: connection, done: make(chan struct{})}
	if !s.registerPeer(peer) {
		peer.close(websocket.StatusPolicyViolation, "duplicate bridge connection")
		return
	}
	defer func() {
		peer.close(websocket.StatusNormalClosure, "bridge connection closed")
		s.unregisterPeer(peer)
	}()
	go s.heartbeat(ctx, peer)
	s.sendStateTo(ctx, peer)
	for {
		messageType, payload, err := connection.Read(ctx)
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			peer.close(websocket.StatusUnsupportedData, "text messages are required")
			return
		}
		var message envelope
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&message); err != nil || message.Version != wireVersion {
			peer.close(websocket.StatusPolicyViolation, "invalid bridge message")
			return
		}
		s.handleEnvelope(peer, message)
	}
}

// heartbeat pings the peer on an interval to keep the connection alive across
// NAT/router idle timeouts and to detect a dead peer promptly. A failed ping
// closes the connection, which unblocks servePeer's Read and — for the dialing
// side — lets dialLoop reconnect.
func (s *Service) heartbeat(ctx context.Context, peer *peerConnection) {
	ticker := time.NewTicker(bridgePingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.done:
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, bridgePingTimeout)
			err := peer.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				peer.close(websocket.StatusGoingAway, "bridge heartbeat timed out")
				return
			}
		}
	}
}

func (s *Service) registerPeer(peer *peerConnection) bool {
	s.mu.Lock()
	if !s.started || s.peers[peer.device.ID] != nil {
		s.mu.Unlock()
		return false
	}
	s.peers[peer.device.ID] = peer
	state := s.remote[peer.device.ID]
	state.Device = peer.device
	state.Online = true
	s.remote[peer.device.ID] = state
	s.mu.Unlock()
	s.notify()
	return true
}

func (s *Service) unregisterPeer(peer *peerConnection) {
	s.mu.Lock()
	if s.peers[peer.device.ID] != peer {
		s.mu.Unlock()
		return
	}
	delete(s.peers, peer.device.ID)
	state := s.remote[peer.device.ID]
	state.Online = false
	s.remote[peer.device.ID] = state
	for id, pending := range s.pending {
		if pending.deviceID == peer.device.ID {
			select {
			case pending.result <- envelope{Version: wireVersion, Kind: kindResponse, ID: id, Error: "device disconnected"}:
			default:
			}
			delete(s.pending, id)
		}
	}
	s.mu.Unlock()
	s.notify()
}

func (s *Service) handleEnvelope(peer *peerConnection, message envelope) {
	switch message.Kind {
	case kindRequest:
		if message.ID == "" || len(message.ID) > 64 {
			return
		}
		go s.handleRequest(peer, message)
	case kindResponse:
		s.mu.Lock()
		pending, ok := s.pending[message.ID]
		if ok && pending.deviceID == peer.device.ID {
			delete(s.pending, message.ID)
		}
		s.mu.Unlock()
		if ok && pending.deviceID == peer.device.ID {
			select {
			case pending.result <- message:
			default:
			}
		}
	case kindEvent:
		s.handleEvent(peer, message)
	}
}

func (s *Service) handleRequest(peer *peerConnection, message envelope) {
	s.mu.RLock()
	handlers := s.handlers
	ctx := s.ctx
	localID := s.local.ID
	s.mu.RUnlock()
	if ctx == nil {
		return
	}
	var output any
	var err error
	switch message.Method {
	case methodWorkspaceOpen:
		var request model.OpenWorkspaceRequest
		if decodePayload(message.Payload, &request) != nil || handlers.OpenWorkspace == nil {
			err = errors.New("workspace creation is unavailable")
			break
		}
		request.DeviceID = localID
		output, err = handlers.OpenWorkspace(ctx, request)
	case methodSessionGet:
		var request workspaceRequest
		if decodePayload(message.Payload, &request) != nil || handlers.GetSession == nil {
			err = errors.New("session loading is unavailable")
			break
		}
		output, err = handlers.GetSession(ctx, strings.TrimSpace(request.WorkspaceID))
	case methodSessionSend:
		var request model.SendMessageRequest
		if decodePayload(message.Payload, &request) != nil || handlers.SendMessage == nil {
			err = errors.New("session messaging is unavailable")
			break
		}
		request.DeviceID = localID
		output, err = handlers.SendMessage(ctx, request)
	case methodApprovalResolve:
		var request model.ResolveApprovalRequest
		if decodePayload(message.Payload, &request) != nil || handlers.ResolveApproval == nil {
			err = errors.New("approval handling is unavailable")
			break
		}
		request.DeviceID = localID
		output, err = handlers.ResolveApproval(ctx, request)
	case methodSessionDelete:
		var request workspaceRequest
		if decodePayload(message.Payload, &request) != nil || handlers.DeleteConversation == nil {
			err = errors.New("conversation deletion is unavailable")
			break
		}
		output, err = handlers.DeleteConversation(ctx, strings.TrimSpace(request.WorkspaceID))
	default:
		err = errors.New("bridge method is not allowed")
	}
	response := envelope{Version: wireVersion, Kind: kindResponse, ID: message.ID}
	if err != nil {
		response.Error = truncateBridgeError(err.Error())
	} else {
		response.Payload, err = json.Marshal(output)
		if err != nil {
			response.Error = "encode bridge response"
		}
	}
	_ = s.sendEnvelope(ctx, peer, response)
	if message.Method == methodWorkspaceOpen && response.Error == "" {
		s.PublishState()
	}
}

func (s *Service) handleEvent(peer *peerConnection, message envelope) {
	switch message.Method {
	case methodWorkspaceSnapshot:
		var state model.RemoteDeviceState
		if decodePayload(message.Payload, &state) == nil {
			_ = s.applyRemoteState(peer.device.ID, state)
		}
	case methodSessionUpdated:
		var snapshot model.SessionSnapshot
		if decodePayload(message.Payload, &snapshot) == nil {
			s.applyRemoteSession(peer.device.ID, snapshot)
		}
	}
}

func (s *Service) sendStateTo(ctx context.Context, peer *peerConnection) {
	s.mu.RLock()
	handler := s.handlers.State
	s.mu.RUnlock()
	if handler == nil {
		return
	}
	state, err := handler(ctx)
	if err != nil {
		return
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	_ = s.sendEnvelope(ctx, peer, envelope{
		Version: wireVersion, Kind: kindEvent, Method: methodWorkspaceSnapshot, Payload: payload,
	})
}

func (s *Service) broadcast(kind, method string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.mu.RLock()
	ctx := s.ctx
	peers := make([]*peerConnection, 0, len(s.peers))
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}
	s.mu.RUnlock()
	for _, peer := range peers {
		_ = s.sendEnvelope(ctx, peer, envelope{Version: wireVersion, Kind: kind, Method: method, Payload: payload})
	}
}

func (s *Service) sendEnvelope(ctx context.Context, peer *peerConnection, message envelope) error {
	if ctx == nil {
		return errors.New("bridge is not running")
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return peer.conn.Write(writeCtx, websocket.MessageText, payload)
}

func (s *Service) call(ctx context.Context, deviceID, method string, input, output any) error {
	deviceID = strings.TrimSpace(deviceID)
	s.mu.RLock()
	peer := s.peers[deviceID]
	started := s.started
	s.mu.RUnlock()
	if !started || peer == nil {
		return errors.New("paired device is offline")
	}
	id, err := randomBridgeToken(18)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	result := make(chan envelope, 1)
	s.mu.Lock()
	s.pending[id] = pendingCall{deviceID: deviceID, result: result}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	if err := s.sendEnvelope(ctx, peer, envelope{
		Version: wireVersion, Kind: kindRequest, ID: id, Method: method, Payload: payload,
	}); err != nil {
		return fmt.Errorf("send bridge request: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case response := <-result:
		if response.Error != "" {
			return errors.New(response.Error)
		}
		if output == nil {
			return nil
		}
		return decodePayload(response.Payload, output)
	}
}

func (s *Service) applyRemoteState(deviceID string, incoming model.RemoteDeviceState) error {
	if incoming.Device.ID != deviceID || len(incoming.Workspaces) > maximumWorkspaces ||
		len(incoming.Sessions) > maximumSessions || len(incoming.Providers) > maximumProviders {
		return errors.New("peer returned invalid workspace state")
	}
	trustedDevice, ok := s.trustedDevice(deviceID)
	if !ok {
		return errors.New("peer is no longer trusted")
	}
	incoming.Device = trustedDevice
	incoming.Online = true
	s.mu.Lock()
	s.remote[deviceID] = copyRemoteState(incoming)
	s.mu.Unlock()
	s.notify()
	return nil
}

func (s *Service) applyRemoteSession(deviceID string, snapshot model.SessionSnapshot) {
	if strings.TrimSpace(snapshot.WorkspaceID) == "" || len(snapshot.Items) > 10000 {
		return
	}
	s.mu.Lock()
	state, ok := s.remote[deviceID]
	if !ok {
		s.mu.Unlock()
		return
	}
	found := false
	for index := range state.Sessions {
		if state.Sessions[index].WorkspaceID == snapshot.WorkspaceID {
			state.Sessions[index] = copySession(snapshot)
			found = true
			break
		}
	}
	if !found && len(state.Sessions) < maximumSessions {
		state.Sessions = append(state.Sessions, copySession(snapshot))
	}
	state.Online = true
	s.remote[deviceID] = state
	s.mu.Unlock()
	s.notify()
}

func (s *Service) notify() {
	s.mu.RLock()
	emit := s.emit
	s.mu.RUnlock()
	emit(s.Snapshot())
}

func decodePayload(payload json.RawMessage, output any) error {
	// Bridge payloads are forward-compatible so paired devices can be one
	// release apart while optional request or snapshot fields are added. The
	// authenticated envelope remains strict and handlers validate used fields.
	decoder := json.NewDecoder(bytes.NewReader(payload))
	return decodeSinglePayload(decoder, output)
}

func decodeSinglePayload(decoder *json.Decoder, output any) error {
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple bridge payloads are not allowed")
	}
	return nil
}

func validBridgeEndpoint(endpoint string) bool {
	address, err := netip.ParseAddrPort(endpoint)
	return err == nil && address.Port() != 0 && address.Addr().IsValid() && !address.Addr().IsUnspecified() && !address.Addr().IsMulticast()
}

func waitBridgeRetry(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func truncateBridgeError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 512 {
		return message[:512]
	}
	return message
}

func copyRemoteState(state model.RemoteDeviceState) model.RemoteDeviceState {
	state.Providers = append([]model.Provider(nil), state.Providers...)
	state.SelectedProviderIDs = append([]string(nil), state.SelectedProviderIDs...)
	state.Workspaces = append([]model.Workspace(nil), state.Workspaces...)
	sessions := make([]model.SessionSnapshot, len(state.Sessions))
	for index, session := range state.Sessions {
		sessions[index] = copySession(session)
	}
	state.Sessions = sessions
	return state
}

func copySession(snapshot model.SessionSnapshot) model.SessionSnapshot {
	snapshot.Items = append([]model.ChatItem(nil), snapshot.Items...)
	for index := range snapshot.Items {
		if snapshot.Items[index].Approval != nil {
			approval := *snapshot.Items[index].Approval
			approval.Paths = append([]string(nil), approval.Paths...)
			snapshot.Items[index].Approval = &approval
		}
	}
	return snapshot
}
