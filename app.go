package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentx/internal/bootstrap"
	"agentx/internal/config"
	"agentx/internal/model"
	"agentx/internal/provider"
	"agentx/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx        context.Context
	bootstrap  *bootstrap.Service
	workspaces *workspace.Manager
	pickFolder func(context.Context, runtime.OpenDialogOptions) (string, error)
}

func NewApp() (*App, error) {
	configPath, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	store := config.New(configPath)
	return &App{
		bootstrap:  bootstrap.NewService(store, provider.NewDetector()),
		workspaces: workspace.NewManager(),
		pickFolder: runtime.OpenDirectoryDialog,
	}, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) Bootstrap() (model.BootstrapState, error) {
	return a.bootstrap.State(a.context())
}

func (a *App) CompleteOnboarding(request model.OnboardingRequest) (model.BootstrapState, error) {
	return a.bootstrap.CompleteOnboarding(a.context(), request)
}

func (a *App) RefreshProviders() (model.BootstrapState, error) {
	return a.bootstrap.State(a.context())
}

func (a *App) OpenWorkspace(request model.OpenWorkspaceRequest) (model.BootstrapState, error) {
	if a.ctx == nil {
		return model.BootstrapState{}, errors.New("application is still starting")
	}
	state, err := a.bootstrap.State(a.ctx)
	if err != nil {
		return model.BootstrapState{}, err
	}
	request.Name = strings.TrimSpace(request.Name)
	if err := validateWorkspaceRequest(state, request); err != nil {
		return model.BootstrapState{}, err
	}

	rootPath, err := a.pickFolder(a.ctx, runtime.OpenDialogOptions{
		Title:                fmt.Sprintf("Choose a folder for %s", request.Name),
		CanCreateDirectories: true,
	})
	if err != nil {
		return model.BootstrapState{}, fmt.Errorf("open workspace dialog: %w", err)
	}
	if rootPath == "" {
		return a.bootstrap.State(a.ctx)
	}

	opened, err := a.workspaces.Open(rootPath, request.Name, request.ProviderID)
	if err != nil {
		return model.BootstrapState{}, err
	}
	return a.bootstrap.AddWorkspace(a.ctx, opened)
}

func validateWorkspaceRequest(state model.BootstrapState, request model.OpenWorkspaceRequest) error {
	if request.Name == "" {
		return errors.New("project name is required")
	}
	if len([]rune(request.Name)) > 80 {
		return errors.New("project name must be 80 characters or fewer")
	}
	if request.DeviceID == "" || request.DeviceID != state.Device.ID {
		return errors.New("select this device to browse for a local workspace")
	}

	selected := false
	for _, id := range state.SelectedProviderIDs {
		if id == request.ProviderID {
			selected = true
			break
		}
	}
	if !selected {
		return errors.New("select a configured provider")
	}
	for _, item := range state.Providers {
		if item.ID == request.ProviderID && item.Installed && item.Supported && !item.ComingSoon {
			return nil
		}
	}
	return errors.New("the selected provider is no longer available on this device")
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
