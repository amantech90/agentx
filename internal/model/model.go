package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const ConfigVersion = 1

type Device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Configured bool   `json:"configured"`
	Trusted    bool   `json:"trusted"`
}

type Provider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Installed   bool   `json:"installed"`
	Supported   bool   `json:"supported"`
	ComingSoon  bool   `json:"comingSoon"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description"`
}

type Workspace struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	Name       string `json:"name"`
	RootPath   string `json:"rootPath"`
	ProviderID string `json:"providerId"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type BootstrapState struct {
	Version             string              `json:"version"`
	NeedsOnboarding     bool                `json:"needsOnboarding"`
	Device              Device              `json:"device"`
	NearbyDevices       []Device            `json:"nearbyDevices"`
	PairedDevices       []Device            `json:"pairedDevices"`
	Providers           []Provider          `json:"providers"`
	SelectedProviderIDs []string            `json:"selectedProviderIds"`
	Workspaces          []Workspace         `json:"workspaces"`
	RemoteDevices       []RemoteDeviceState `json:"remoteDevices"`
}

type PairingRequest struct {
	ID        string `json:"id"`
	Device    Device `json:"device"`
	Direction string `json:"direction"`
	Code      string `json:"code"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expiresAt"`
}

type PairingSnapshot struct {
	Requests      []PairingRequest `json:"requests"`
	PairedDevices []Device         `json:"pairedDevices"`
}

type OnboardingRequest struct {
	DeviceName          string   `json:"deviceName"`
	SelectedProviderIDs []string `json:"selectedProviderIds"`
}

type OpenWorkspaceRequest struct {
	Name       string `json:"name"`
	ProviderID string `json:"providerId"`
	DeviceID   string `json:"deviceId"`
}

type SendMessageRequest struct {
	DeviceID       string           `json:"deviceId,omitempty"`
	WorkspaceID    string           `json:"workspaceId"`
	Content        string           `json:"content"`
	DisplayContent string           `json:"displayContent,omitempty"`
	PermissionMode string           `json:"permissionMode,omitempty"`
	Screenshot     *ScreenshotInput `json:"screenshot,omitempty"`
}

type ScreenshotInput struct {
	MediaType   string `json:"mediaType"`
	Data        string `json:"data"`
	PreviewData string `json:"previewData,omitempty"`
}

type Screenshot struct {
	ID          string `json:"id"`
	MediaType   string `json:"mediaType"`
	PreviewData string `json:"previewData,omitempty"`
}

type WorkspaceCommandRequest struct {
	DeviceID    string `json:"deviceId"`
	WorkspaceID string `json:"workspaceId"`
}

type ResolveApprovalRequest struct {
	DeviceID    string `json:"deviceId,omitempty"`
	WorkspaceID string `json:"workspaceId"`
	ApprovalID  string `json:"approvalId"`
	Decision    string `json:"decision"`
}

type ToolApproval struct {
	Kind             string   `json:"kind"`
	Tool             string   `json:"tool,omitempty"`
	Command          string   `json:"command,omitempty"`
	Paths            []string `json:"paths,omitempty"`
	WorkingDirectory string   `json:"workingDirectory,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

type ChatItem struct {
	ID          string        `json:"id"`
	TurnID      string        `json:"turnId,omitempty"`
	Kind        string        `json:"kind"`
	Role        string        `json:"role"`
	Title       string        `json:"title,omitempty"`
	Content     string        `json:"content"`
	Screenshots []Screenshot  `json:"screenshots,omitempty"`
	Approval    *ToolApproval `json:"approval,omitempty"`
	Status      string        `json:"status,omitempty"`
	CreatedAt   string        `json:"createdAt"`
}

type SessionSnapshot struct {
	WorkspaceID       string     `json:"workspaceId"`
	ProviderID        string     `json:"providerId"`
	ProviderSessionID string     `json:"providerSessionId,omitempty"`
	Status            string     `json:"status"`
	QueueDepth        int        `json:"queueDepth"`
	Items             []ChatItem `json:"items"`
}

type RemoteDeviceState struct {
	Device              Device            `json:"device"`
	Online              bool              `json:"online"`
	Providers           []Provider        `json:"providers"`
	SelectedProviderIDs []string          `json:"selectedProviderIds"`
	Workspaces          []Workspace       `json:"workspaces"`
	Sessions            []SessionSnapshot `json:"sessions"`
}

type BridgeSnapshot struct {
	Devices []RemoteDeviceState `json:"devices"`
}

type ProjectFile struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ProviderID string    `json:"providerId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func ProviderWorkspaceID(projectID, providerID string) string {
	identity := strings.TrimSpace(projectID) + "\x00" + strings.TrimSpace(providerID)
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:16])
}
