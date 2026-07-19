package bridge

import "encoding/json"

const (
	wireVersion = 1

	kindRequest  = "request"
	kindResponse = "response"
	kindEvent    = "event"

	methodWorkspaceSnapshot = "workspace.snapshot"
	methodSessionUpdated    = "session.updated"
	methodWorkspaceOpen     = "workspace.open"
	methodSessionGet        = "session.get"
	methodSessionSend       = "session.send"
	methodApprovalResolve   = "session.approval.resolve"
	methodSessionDelete     = "session.delete"
)

type envelope struct {
	Version int             `json:"version"`
	Kind    string          `json:"kind"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type workspaceRequest struct {
	WorkspaceID string `json:"workspaceId"`
}
