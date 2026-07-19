package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agentx/internal/fsx"
	"agentx/internal/model"
)

const sessionFileVersion = 1

const (
	maximumScreenshotBytes = 2 * 1024 * 1024
	maximumPreviewBytes    = 256 * 1024
)

type MessageInput struct {
	Prompt         string
	DisplayPrompt  string
	PermissionMode string
	Screenshot     *model.ScreenshotInput
}

type queuedTurn struct {
	prompt          string
	providerPath    string
	permissionMode  string
	screenshotPaths []string
	turnID          string
}

type activeSession struct {
	snapshot     model.SessionSnapshot
	workspace    model.Workspace
	queue        []queuedTurn
	running      bool
	resumeLatest bool
}

type pendingApproval struct {
	workspaceID string
	itemID      string
	decision    chan ApprovalDecision
}

type sessionFile struct {
	Version           int              `json:"version"`
	WorkspaceID       string           `json:"workspaceId"`
	ProviderID        string           `json:"providerId"`
	ProviderSessionID string           `json:"providerSessionId,omitempty"`
	Items             []model.ChatItem `json:"items"`
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*activeSession
	pending  map[string]pendingApproval
	runners  map[string]Runner
	now      func() time.Time
	emit     func(model.SessionSnapshot)
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewManager(runners map[string]Runner) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		sessions: make(map[string]*activeSession),
		pending:  make(map[string]pendingApproval),
		runners:  runners,
		now:      time.Now,
		emit:     func(model.SessionSnapshot) {},
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (m *Manager) SetEmitter(emit func(model.SessionSnapshot)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if emit == nil {
		m.emit = func(model.SessionSnapshot) {}
		return
	}
	m.emit = emit
}

func (m *Manager) Snapshot(workspace model.Workspace) (model.SessionSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, err := m.ensureSessionLocked(workspace)
	if err != nil {
		return model.SessionSnapshot{}, err
	}
	return cloneSnapshot(session.snapshot), nil
}

func (m *Manager) Enqueue(workspace model.Workspace, providerPath, prompt string, requestedPermissionMode ...string) (model.SessionSnapshot, error) {
	permissionMode := ""
	if len(requestedPermissionMode) > 0 {
		permissionMode = requestedPermissionMode[0]
	}
	return m.EnqueueMessage(workspace, providerPath, MessageInput{Prompt: prompt, PermissionMode: permissionMode})
}

func (m *Manager) EnqueueMessage(workspace model.Workspace, providerPath string, input MessageInput) (model.SessionSnapshot, error) {
	prompt := strings.TrimSpace(input.Prompt)
	displayPrompt := strings.TrimSpace(input.DisplayPrompt)
	if prompt == "" && input.Screenshot == nil {
		return model.SessionSnapshot{}, errors.New("message is required")
	}
	if len([]rune(prompt)) > 100_000 {
		return model.SessionSnapshot{}, errors.New("message must be 100,000 characters or fewer")
	}
	if len([]rune(displayPrompt)) > 100_000 {
		return model.SessionSnapshot{}, errors.New("display message must be 100,000 characters or fewer")
	}
	if displayPrompt == "" {
		displayPrompt = prompt
	}
	if strings.TrimSpace(providerPath) == "" {
		return model.SessionSnapshot{}, errors.New("provider executable is unavailable")
	}

	var screenshots []model.Screenshot
	var screenshotPaths []string
	if input.Screenshot != nil {
		screenshot, screenshotPath, err := persistScreenshot(workspace, *input.Screenshot)
		if err != nil {
			return model.SessionSnapshot{}, err
		}
		screenshots = []model.Screenshot{screenshot}
		screenshotPaths = []string{screenshotPath}
	}

	m.mu.Lock()
	session, err := m.ensureSessionLocked(workspace)
	if err != nil {
		m.mu.Unlock()
		return model.SessionSnapshot{}, err
	}
	messageID, err := randomID()
	if err != nil {
		m.mu.Unlock()
		return model.SessionSnapshot{}, fmt.Errorf("create message id: %w", err)
	}
	session.snapshot.Items = append(session.snapshot.Items, model.ChatItem{
		ID: messageID, TurnID: messageID, Kind: "message", Role: "user", Content: displayPrompt,
		Screenshots: screenshots, Status: "queued", CreatedAt: m.now().UTC().Format(time.RFC3339Nano),
	})
	session.queue = append(session.queue, queuedTurn{
		prompt: prompt, providerPath: providerPath,
		permissionMode:  NormalizePermissionMode(workspace.ProviderID, input.PermissionMode),
		screenshotPaths: screenshotPaths,
		turnID:          messageID,
	})
	session.snapshot.QueueDepth = len(session.queue)
	shouldStart := !session.running
	if shouldStart {
		session.running = true
		session.snapshot.Status = "queued"
	}
	if err := persistSession(session.workspace, session.snapshot); err != nil {
		m.mu.Unlock()
		return model.SessionSnapshot{}, err
	}
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()

	emit(snapshot)
	if shouldStart {
		go m.runQueue(workspace.ID)
	}
	return snapshot, nil
}

func (m *Manager) Close() {
	m.cancel()
}

// DeleteConversation clears only Agent X's local conversation metadata. It
// deliberately preserves the workspace, project files, and provider-owned
// history. The next message starts a new provider session.
func (m *Manager) DeleteConversation(workspace model.Workspace) (model.SessionSnapshot, error) {
	m.mu.Lock()
	session, err := m.ensureSessionLocked(workspace)
	if err != nil {
		m.mu.Unlock()
		return model.SessionSnapshot{}, err
	}
	if session.running || len(session.queue) > 0 {
		m.mu.Unlock()
		return model.SessionSnapshot{}, errors.New("wait for the current agent run to finish before deleting the conversation")
	}
	session.snapshot = model.SessionSnapshot{
		WorkspaceID: workspace.ID,
		ProviderID:  workspace.ProviderID,
		Status:      "idle",
		Items:       []model.ChatItem{},
	}
	session.resumeLatest = false
	if err := persistSession(session.workspace, session.snapshot); err != nil {
		m.mu.Unlock()
		return model.SessionSnapshot{}, err
	}
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()
	emit(snapshot)
	return snapshot, nil
}

func (m *Manager) runQueue(workspaceID string) {
	for {
		m.mu.Lock()
		session := m.sessions[workspaceID]
		if session == nil || len(session.queue) == 0 {
			if session != nil {
				session.running = false
				if session.snapshot.Status != "error" {
					session.snapshot.Status = "idle"
				}
				session.snapshot.QueueDepth = 0
				_ = persistSession(session.workspace, session.snapshot)
			}
			var snapshot model.SessionSnapshot
			if session != nil {
				snapshot = cloneSnapshot(session.snapshot)
			}
			emit := m.emit
			m.mu.Unlock()
			if session != nil {
				emit(snapshot)
			}
			return
		}

		turn := session.queue[0]
		session.queue = session.queue[1:]
		session.snapshot.Status = "running"
		session.snapshot.QueueDepth = len(session.queue)
		markOldestQueuedUserRunning(&session.snapshot)
		request := RunRequest{
			Workspace:    session.workspace,
			ProviderPath: turn.providerPath, ProviderSessionID: session.snapshot.ProviderSessionID,
			Prompt: turn.prompt, PermissionMode: turn.permissionMode, ScreenshotPaths: turn.screenshotPaths,
			ResumeLatest: session.resumeLatest && session.snapshot.ProviderSessionID == "",
		}
		runner := m.runners[session.workspace.ProviderID]
		runningSnapshot := cloneSnapshot(session.snapshot)
		emit := m.emit
		m.mu.Unlock()
		emit(runningSnapshot)

		if runner == nil {
			m.finishTurn(workspaceID, RunResult{}, fmt.Errorf("provider %q has no session adapter", request.Workspace.ProviderID))
			continue
		}
		result, runErr := runner.Run(m.ctx, request, RunCallbacks{
			Emit: func(event Event) {
				m.applyEvent(workspaceID, turn.turnID, event)
			},
			RequestApproval: func(ctx context.Context, approval ApprovalRequest) (ApprovalDecision, error) {
				return m.requestApproval(ctx, workspaceID, turn.turnID, approval)
			},
		})
		m.finishTurn(workspaceID, result, runErr)
	}
}

func (m *Manager) requestApproval(ctx context.Context, workspaceID, turnID string, request ApprovalRequest) (ApprovalDecision, error) {
	approvalID, err := randomID()
	if err != nil {
		return ApprovalDeny, fmt.Errorf("create approval id: %w", err)
	}
	request = normalizeApprovalRequest(request)
	decision := make(chan ApprovalDecision, 1)

	m.mu.Lock()
	session := m.sessions[workspaceID]
	if session == nil || !session.running {
		m.mu.Unlock()
		return ApprovalDeny, errors.New("the provider run is no longer active")
	}
	m.pending[approvalID] = pendingApproval{workspaceID: workspaceID, itemID: approvalID, decision: decision}
	session.snapshot.Items = append(session.snapshot.Items, model.ChatItem{
		ID: approvalID, TurnID: turnID, Kind: "approval", Role: "system", Title: request.Title,
		Content: request.Command, Status: "pending", CreatedAt: m.now().UTC().Format(time.RFC3339Nano),
		Approval: &model.ToolApproval{
			Kind: request.Kind, Tool: request.Tool, Command: request.Command,
			Paths: append([]string(nil), request.Paths...), WorkingDirectory: request.WorkingDirectory, Reason: request.Reason,
		},
	})
	session.snapshot.Status = "waiting"
	if err := persistSession(session.workspace, session.snapshot); err != nil {
		delete(m.pending, approvalID)
		session.snapshot.Items = session.snapshot.Items[:len(session.snapshot.Items)-1]
		session.snapshot.Status = "running"
		m.mu.Unlock()
		return ApprovalDeny, err
	}
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()
	emit(snapshot)

	select {
	case value := <-decision:
		return value, nil
	case <-ctx.Done():
		m.cancelPendingApproval(approvalID)
		return ApprovalDeny, ctx.Err()
	}
}

func (m *Manager) ResolveApproval(workspace model.Workspace, approvalID, decision string) (model.SessionSnapshot, error) {
	approvalID = strings.TrimSpace(approvalID)
	resolvedDecision := ApprovalDecision(strings.TrimSpace(decision))
	if approvalID == "" || (resolvedDecision != ApprovalAllow && resolvedDecision != ApprovalDeny) {
		return model.SessionSnapshot{}, errors.New("approval decision is invalid")
	}

	m.mu.Lock()
	pending, ok := m.pending[approvalID]
	if !ok || pending.workspaceID != workspace.ID {
		m.mu.Unlock()
		return model.SessionSnapshot{}, errors.New("approval is no longer pending")
	}
	session := m.sessions[workspace.ID]
	if session == nil {
		m.mu.Unlock()
		return model.SessionSnapshot{}, errors.New("workspace session was not found")
	}
	index := approvalItemIndex(session.snapshot.Items, approvalID)
	if index < 0 || session.snapshot.Items[index].Status != "pending" {
		m.mu.Unlock()
		return model.SessionSnapshot{}, errors.New("approval is no longer pending")
	}
	if resolvedDecision == ApprovalAllow {
		session.snapshot.Items[index].Status = "approved"
	} else {
		session.snapshot.Items[index].Status = "denied"
	}
	if !m.hasOtherPendingApprovalLocked(workspace.ID, approvalID) {
		session.snapshot.Status = "running"
	}
	if err := persistSession(session.workspace, session.snapshot); err != nil {
		session.snapshot.Items[index].Status = "pending"
		session.snapshot.Status = "waiting"
		m.mu.Unlock()
		return model.SessionSnapshot{}, err
	}
	delete(m.pending, approvalID)
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()

	pending.decision <- resolvedDecision
	emit(snapshot)
	return snapshot, nil
}

func (m *Manager) cancelPendingApproval(approvalID string) {
	m.mu.Lock()
	pending, ok := m.pending[approvalID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.pending, approvalID)
	session := m.sessions[pending.workspaceID]
	if session == nil {
		m.mu.Unlock()
		return
	}
	if index := approvalItemIndex(session.snapshot.Items, pending.itemID); index >= 0 && session.snapshot.Items[index].Status == "pending" {
		session.snapshot.Items[index].Status = "cancelled"
	}
	_ = persistSession(session.workspace, session.snapshot)
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()
	emit(snapshot)
}

func (m *Manager) hasOtherPendingApprovalLocked(workspaceID, excludedApprovalID string) bool {
	for approvalID, pending := range m.pending {
		if approvalID != excludedApprovalID && pending.workspaceID == workspaceID {
			return true
		}
	}
	return false
}

func normalizeApprovalRequest(request ApprovalRequest) ApprovalRequest {
	request.Kind = truncateRunes(strings.TrimSpace(request.Kind), 40)
	request.Tool = truncateRunes(strings.TrimSpace(request.Tool), 120)
	request.Title = truncateRunes(strings.TrimSpace(request.Title), 240)
	request.Command = truncateRunes(strings.TrimSpace(request.Command), 8000)
	request.WorkingDirectory = truncateRunes(strings.TrimSpace(request.WorkingDirectory), 2000)
	request.Reason = truncateRunes(strings.TrimSpace(request.Reason), 2000)
	if request.Kind == "" {
		request.Kind = "tool"
	}
	if request.Title == "" {
		request.Title = "Approval required"
	}
	if len(request.Paths) > 20 {
		request.Paths = request.Paths[:20]
	}
	for index := range request.Paths {
		request.Paths[index] = truncateRunes(strings.TrimSpace(request.Paths[index]), 2000)
	}
	return request
}

func (m *Manager) applyEvent(workspaceID, turnID string, event Event) {
	m.mu.Lock()
	session := m.sessions[workspaceID]
	if session == nil {
		m.mu.Unlock()
		return
	}
	if event.ID == "" {
		event.ID, _ = randomID()
	}
	index := chatItemIndex(session.snapshot.Items, turnID, event.ID)
	if index >= 0 {
		item := &session.snapshot.Items[index]
		if item.TurnID == "" {
			item.TurnID = turnID
		}
		if event.Kind != "" {
			item.Kind = event.Kind
		}
		if event.Role != "" {
			item.Role = event.Role
		}
		if event.Title != "" {
			item.Title = event.Title
		}
		if event.Content != "" {
			item.Content = event.Content
		}
		if event.Status != "" {
			item.Status = event.Status
		}
	} else {
		session.snapshot.Items = append(session.snapshot.Items, model.ChatItem{
			ID: event.ID, TurnID: turnID, Kind: event.Kind, Role: event.Role, Title: event.Title,
			Content: event.Content, Status: event.Status, CreatedAt: m.now().UTC().Format(time.RFC3339Nano),
		})
	}
	_ = persistSession(session.workspace, session.snapshot)
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()
	emit(snapshot)
}

func (m *Manager) finishTurn(workspaceID string, result RunResult, runErr error) {
	m.mu.Lock()
	session := m.sessions[workspaceID]
	if session == nil {
		m.mu.Unlock()
		return
	}
	if result.ProviderSessionID != "" {
		session.snapshot.ProviderSessionID = result.ProviderSessionID
	}
	markRunningUserCompleted(&session.snapshot, runErr)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		id, _ := randomID()
		session.snapshot.Items = append(session.snapshot.Items, model.ChatItem{
			ID: id, Kind: "error", Role: "system", Content: runErr.Error(), Status: "failed",
			CreatedAt: m.now().UTC().Format(time.RFC3339Nano),
		})
		session.snapshot.Status = "error"
	} else if len(session.queue) > 0 {
		session.snapshot.Status = "queued"
	} else {
		// The queue worker still owns this session until its next loop pass.
		// Keep it running here; runQueue emits idle only after clearing running.
		session.snapshot.Status = "running"
	}
	session.snapshot.QueueDepth = len(session.queue)
	_ = persistSession(session.workspace, session.snapshot)
	snapshot := cloneSnapshot(session.snapshot)
	emit := m.emit
	m.mu.Unlock()
	emit(snapshot)
}

func (m *Manager) ensureSessionLocked(workspace model.Workspace) (*activeSession, error) {
	if existing := m.sessions[workspace.ID]; existing != nil {
		return existing, nil
	}
	resumeLatest := !hasStoredSession(workspace)
	snapshot, err := loadSession(workspace)
	if err != nil {
		return nil, err
	}
	session := &activeSession{
		snapshot: snapshot, workspace: workspace,
		resumeLatest: resumeLatest && snapshot.ProviderSessionID == "",
	}
	m.sessions[workspace.ID] = session
	return session, nil
}

func hasStoredSession(workspace model.Workspace) bool {
	for _, path := range []string{sessionPath(workspace), legacySessionPath(workspace)} {
		if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

func loadSession(workspace model.Workspace) (model.SessionSnapshot, error) {
	snapshot := model.SessionSnapshot{
		WorkspaceID: workspace.ID, ProviderID: workspace.ProviderID, Status: "idle", Items: []model.ChatItem{},
	}
	contents, err := os.ReadFile(sessionPath(workspace))
	legacy := false
	if errors.Is(err, os.ErrNotExist) {
		contents, err = os.ReadFile(legacySessionPath(workspace))
		legacy = err == nil
		if errors.Is(err, os.ErrNotExist) {
			return snapshot, nil
		}
	}
	if err != nil {
		return model.SessionSnapshot{}, fmt.Errorf("read workspace session: %w", err)
	}
	var stored sessionFile
	if err := json.Unmarshal(contents, &stored); err != nil {
		return model.SessionSnapshot{}, fmt.Errorf("decode workspace session: %w", err)
	}
	if stored.Version != sessionFileVersion {
		return model.SessionSnapshot{}, errors.New("workspace session is invalid or unsupported")
	}
	if stored.ProviderID != workspace.ProviderID {
		return snapshot, nil
	}
	workspaceMatches := stored.WorkspaceID == workspace.ID
	legacyMatches := legacy && workspace.ProjectID != "" && stored.WorkspaceID == workspace.ProjectID
	if !workspaceMatches && !legacyMatches {
		return model.SessionSnapshot{}, errors.New("workspace session is invalid or unsupported")
	}
	snapshot.ProviderSessionID = stored.ProviderSessionID
	snapshot.Items = stored.Items
	if snapshot.Items == nil {
		snapshot.Items = []model.ChatItem{}
	}
	staleApproval := false
	for index := range snapshot.Items {
		if snapshot.Items[index].Kind == "approval" && snapshot.Items[index].Status == "pending" {
			snapshot.Items[index].Status = "cancelled"
			staleApproval = true
		}
	}
	if legacy || staleApproval {
		if err := persistSession(workspace, snapshot); err != nil {
			return model.SessionSnapshot{}, fmt.Errorf("migrate provider session: %w", err)
		}
	}
	return snapshot, nil
}

func persistSession(workspace model.Workspace, snapshot model.SessionSnapshot) error {
	stored := sessionFile{
		Version: sessionFileVersion, WorkspaceID: workspace.ID, ProviderID: workspace.ProviderID,
		ProviderSessionID: snapshot.ProviderSessionID, Items: snapshot.Items,
	}
	contents, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace session: %w", err)
	}
	contents = append(contents, '\n')
	if err := fsx.WriteFileAtomically(sessionPath(workspace), contents); err != nil {
		return fmt.Errorf("save workspace session: %w", err)
	}
	return nil
}

func sessionPath(workspace model.Workspace) string {
	return filepath.Join(workspace.RootPath, ".agentx", "sessions", workspace.ProviderID+".json")
}

func legacySessionPath(workspace model.Workspace) string {
	return filepath.Join(workspace.RootPath, ".agentx", "session.json")
}

func cloneSnapshot(snapshot model.SessionSnapshot) model.SessionSnapshot {
	result := snapshot
	result.Items = append([]model.ChatItem(nil), snapshot.Items...)
	for index := range result.Items {
		result.Items[index].Screenshots = append([]model.Screenshot(nil), result.Items[index].Screenshots...)
		if result.Items[index].Approval != nil {
			approval := *result.Items[index].Approval
			approval.Paths = append([]string(nil), approval.Paths...)
			result.Items[index].Approval = &approval
		}
	}
	return result
}

func persistScreenshot(workspace model.Workspace, input model.ScreenshotInput) (model.Screenshot, string, error) {
	mediaType := strings.ToLower(strings.TrimSpace(input.MediaType))
	extension, ok := map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp",
	}[mediaType]
	if !ok {
		return model.Screenshot{}, "", errors.New("only PNG, JPEG, or WebP screenshots are supported")
	}
	contents, err := decodeScreenshotData(input.Data, maximumScreenshotBytes)
	if err != nil {
		return model.Screenshot{}, "", fmt.Errorf("invalid screenshot: %w", err)
	}
	if !screenshotSignatureMatches(mediaType, contents) {
		return model.Screenshot{}, "", errors.New("screenshot contents do not match its image type")
	}
	previewData := strings.TrimSpace(input.PreviewData)
	if previewData != "" {
		preview, previewErr := decodeScreenshotData(previewData, maximumPreviewBytes)
		if previewErr != nil || !screenshotSignatureMatches(mediaType, preview) {
			return model.Screenshot{}, "", errors.New("screenshot preview is invalid")
		}
	}
	id, err := randomID()
	if err != nil {
		return model.Screenshot{}, "", fmt.Errorf("create screenshot id: %w", err)
	}
	path := filepath.Join(workspace.RootPath, ".agentx", "screenshots", id+extension)
	if err := fsx.WriteFileAtomically(path, contents); err != nil {
		return model.Screenshot{}, "", fmt.Errorf("save screenshot: %w", err)
	}
	return model.Screenshot{ID: id, MediaType: mediaType, PreviewData: previewData}, path, nil
}

func decodeScreenshotData(value string, maximumBytes int) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("image data is required")
	}
	if len(value) > base64.StdEncoding.EncodedLen(maximumBytes)+4 {
		return nil, errors.New("image is too large")
	}
	contents, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("image data is not valid base64")
	}
	if len(contents) == 0 || len(contents) > maximumBytes {
		return nil, errors.New("image is too large")
	}
	return contents, nil
}

func screenshotSignatureMatches(mediaType string, contents []byte) bool {
	switch mediaType {
	case "image/png":
		return len(contents) >= 8 && string(contents[:8]) == "\x89PNG\r\n\x1a\n"
	case "image/jpeg":
		return len(contents) >= 3 && contents[0] == 0xff && contents[1] == 0xd8 && contents[2] == 0xff
	case "image/webp":
		return len(contents) >= 12 && string(contents[:4]) == "RIFF" && string(contents[8:12]) == "WEBP"
	default:
		return false
	}
}

func chatItemIndex(items []model.ChatItem, turnID, id string) int {
	for index := range items {
		if items[index].TurnID == turnID && items[index].ID == id {
			return index
		}
	}
	return -1
}

func approvalItemIndex(items []model.ChatItem, id string) int {
	for index := range items {
		if items[index].ID == id && items[index].Kind == "approval" {
			return index
		}
	}
	return -1
}

func markOldestQueuedUserRunning(snapshot *model.SessionSnapshot) {
	for index := range snapshot.Items {
		if snapshot.Items[index].Role == "user" && snapshot.Items[index].Status == "queued" {
			snapshot.Items[index].Status = "running"
			return
		}
	}
}

func markRunningUserCompleted(snapshot *model.SessionSnapshot, runErr error) {
	for index := len(snapshot.Items) - 1; index >= 0; index-- {
		if snapshot.Items[index].Role == "user" && snapshot.Items[index].Status == "running" {
			if runErr != nil {
				snapshot.Items[index].Status = "failed"
			} else {
				snapshot.Items[index].Status = "completed"
			}
			return
		}
	}
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
