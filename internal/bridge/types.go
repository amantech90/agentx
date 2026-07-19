package bridge

import (
	"context"

	"agentx/internal/model"
)

const DefaultPort uint16 = 41938

type Target struct {
	Device    model.Device
	Endpoint  string
	PublicKey string
}

type Handlers struct {
	State              func(context.Context) (model.RemoteDeviceState, error)
	OpenWorkspace      func(context.Context, model.OpenWorkspaceRequest) (model.RemoteDeviceState, error)
	GetSession         func(context.Context, string) (model.SessionSnapshot, error)
	SendMessage        func(context.Context, model.SendMessageRequest) (model.SessionSnapshot, error)
	ResolveApproval    func(context.Context, model.ResolveApprovalRequest) (model.SessionSnapshot, error)
	DeleteConversation func(context.Context, string) (model.SessionSnapshot, error)
}
