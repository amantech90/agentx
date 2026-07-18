package model

import "time"

const ConfigVersion = 1

type Device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Configured bool   `json:"configured"`
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
	Name       string `json:"name"`
	RootPath   string `json:"rootPath"`
	ProviderID string `json:"providerId"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type BootstrapState struct {
	Version             string      `json:"version"`
	NeedsOnboarding     bool        `json:"needsOnboarding"`
	Device              Device      `json:"device"`
	NearbyDevices       []Device    `json:"nearbyDevices"`
	Providers           []Provider  `json:"providers"`
	SelectedProviderIDs []string    `json:"selectedProviderIds"`
	Workspaces          []Workspace `json:"workspaces"`
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

type ProjectFile struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ProviderID string    `json:"providerId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
