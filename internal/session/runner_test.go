package session

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"agentx/internal/model"
)

func TestClaudeArgumentsUseSelectedPermissionMode(t *testing.T) {
	t.Parallel()

	args := claudeArguments(RunRequest{
		Workspace:         model.Workspace{ProviderID: "claude"},
		ProviderSessionID: "session-123",
		PermissionMode:    ClaudePermissionAcceptEdits,
	})

	want := []string{
		"-p", "--verbose", "--output-format", "stream-json", "--input-format", "stream-json",
		"--permission-prompt-tool", "stdio",
		"--permission-mode", "acceptEdits",
		"--resume", "session-123",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("claudeArguments() = %#v, want %#v", args, want)
	}
}

func TestCodexArgumentsAttachScreenshotForNewAndResumedTurns(t *testing.T) {
	t.Parallel()

	imagePath := filepath.Join("workspace", ".agentx", "screenshots", "shot.png")
	for _, request := range []RunRequest{
		{Workspace: model.Workspace{ProviderID: "codex"}, ScreenshotPaths: []string{imagePath}},
		{Workspace: model.Workspace{ProviderID: "codex"}, ProviderSessionID: "session-123", ScreenshotPaths: []string{imagePath}},
	} {
		args := codexArguments(request)
		imageIndex := slices.Index(args, "--image")
		if imageIndex < 0 || imageIndex+1 >= len(args) || args[imageIndex+1] != imagePath {
			t.Fatalf("codexArguments() = %#v, want --image %q", args, imagePath)
		}
	}
}

func TestClaudeInputDirectsReadToolToPersistedScreenshot(t *testing.T) {
	t.Parallel()

	root := filepath.Join("workspace", "project")
	imagePath := filepath.Join(root, ".agentx", "screenshots", "shot.png")
	input := providerInput(RunRequest{
		Workspace:       model.Workspace{ProviderID: "claude", RootPath: root},
		Prompt:          "What is wrong here?",
		ScreenshotPaths: []string{imagePath},
	})
	if !strings.Contains(input, "What is wrong here?") || !strings.Contains(input, filepath.Join(".agentx", "screenshots", "shot.png")) {
		t.Fatalf("providerInput() = %q", input)
	}
}

func TestClaudeArgumentsContinueLatestConversationForUntrackedWorkspace(t *testing.T) {
	t.Parallel()

	args := claudeArguments(RunRequest{
		Workspace:      model.Workspace{ProviderID: "claude"},
		ResumeLatest:   true,
		PermissionMode: ClaudePermissionAuto,
	})
	if !slices.Contains(args, "--continue") {
		t.Fatalf("claudeArguments() = %#v, want --continue", args)
	}
}

func TestCodexArgumentsResumeLatestConversationForUntrackedWorkspace(t *testing.T) {
	t.Parallel()

	args := codexArguments(RunRequest{
		Workspace:    model.Workspace{ProviderID: "codex"},
		ResumeLatest: true,
	})
	want := []string{"exec", "resume", "--last", "--json", "--skip-git-repo-check", "-"}
	if !slices.Equal(args, want) {
		t.Fatalf("codexArguments() = %#v, want %#v", args, want)
	}
}

func TestProviderArgumentsStartFreshAfterConversationDeletion(t *testing.T) {
	t.Parallel()

	claudeArgs := claudeArguments(RunRequest{
		Workspace: model.Workspace{ProviderID: "claude"}, PermissionMode: ClaudePermissionAuto,
	})
	if slices.Contains(claudeArgs, "--continue") || slices.Contains(claudeArgs, "--resume") {
		t.Fatalf("claudeArguments() unexpectedly resumed: %#v", claudeArgs)
	}
	codexArgs := codexArguments(RunRequest{Workspace: model.Workspace{ProviderID: "codex"}})
	if len(codexArgs) < 2 || codexArgs[1] == "resume" {
		t.Fatalf("codexArguments() unexpectedly resumed: %#v", codexArgs)
	}
}

func TestAutomaticResumeFallsBackOnlyWhenProviderHasNoPreviousSession(t *testing.T) {
	t.Parallel()

	request := RunRequest{ResumeLatest: true}
	if !shouldStartFresh(request, errors.New("codex exited: no sessions found for this directory")) {
		t.Fatal("missing provider session did not trigger a fresh session")
	}
	if shouldStartFresh(request, errors.New("codex exited: authentication required")) {
		t.Fatal("authentication failure incorrectly triggered a second provider run")
	}
}

func TestClaudePermissionModeRejectsUnrestrictedBypass(t *testing.T) {
	t.Parallel()

	if got := NormalizePermissionMode("claude", "bypassPermissions"); got != ClaudePermissionDefault {
		t.Fatalf("NormalizePermissionMode() = %q, want %q", got, ClaudePermissionDefault)
	}
}

func TestClaudePermissionModeAllowsOnlyProductModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{ClaudePermissionDefault, ClaudePermissionAuto, ClaudePermissionAcceptEdits, ClaudePermissionPlan} {
		if got := NormalizePermissionMode("claude", mode); got != mode {
			t.Errorf("NormalizePermissionMode(%q) = %q", mode, got)
		}
	}
}

func TestClaudeControlResponseAllowsOnlyTheRequestedInput(t *testing.T) {
	t.Parallel()

	line := []byte(`{"type":"control_request","request_id":"request-1","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"tool-1","input":{"command":"go test ./..."},"decision_reason":"Runs project code"}}`)
	requestID, input, approval, ok, err := parseClaudeApprovalRequest(line, "/workspace")
	if err != nil || !ok {
		t.Fatalf("parseClaudeApprovalRequest() = ok %v, err %v", ok, err)
	}
	if requestID != "request-1" || approval.Tool != "Bash" || approval.Command != "go test ./..." {
		t.Fatalf("parsed approval = %#v, input = %#v", approval, input)
	}
	response, err := claudeApprovalResponse(requestID, ApprovalAllow, input)
	if err != nil {
		t.Fatalf("claudeApprovalResponse() error = %v", err)
	}
	text := string(response)
	if !strings.Contains(text, `"behavior":"allow"`) || !strings.Contains(text, `"updatedInput":{"command":"go test ./..."}`) {
		t.Fatalf("response = %s", response)
	}
}

func TestCodexApprovalResponseNeverGrantsSessionWideAccess(t *testing.T) {
	t.Parallel()

	line := []byte(`{"method":"item/commandExecution/requestApproval","id":42,"params":{"itemId":"item-1","threadId":"thread-1","turnId":"turn-1","startedAtMs":1,"command":"go test ./...","cwd":"/workspace","reason":"Runs project code"}}`)
	id, approval, kind, ok, err := parseCodexApprovalRequest(line)
	if err != nil || !ok {
		t.Fatalf("parseCodexApprovalRequest() = ok %v, err %v", ok, err)
	}
	if kind != codexCommandApproval || approval.Command != "go test ./..." {
		t.Fatalf("parsed approval = %#v, kind = %q", approval, kind)
	}
	response, err := codexApprovalResponse(id, kind, ApprovalAllow)
	if err != nil {
		t.Fatalf("codexApprovalResponse() error = %v", err)
	}
	if strings.Contains(string(response), "acceptForSession") || !strings.Contains(string(response), `"decision":"accept"`) {
		t.Fatalf("response = %s", response)
	}
}

func TestCodexAppServerDeltasKeepVisibleOutputCurrent(t *testing.T) {
	t.Parallel()

	messages := map[string]string{}
	commands := map[string]string{}
	for _, line := range []string{
		`{"method":"item/agentMessage/delta","params":{"itemId":"answer","delta":"Hello "}}`,
		`{"method":"item/agentMessage/delta","params":{"itemId":"answer","delta":"world"}}`,
	} {
		var envelope codexRPCEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		events, completed, err := parseCodexAppServerNotification(envelope, messages, commands)
		if err != nil || completed || len(events) != 1 {
			t.Fatalf("notification result = %#v, completed %v, err %v", events, completed, err)
		}
	}
	if messages["answer"] != "Hello world" {
		t.Fatalf("streamed message = %q", messages["answer"])
	}
}

func TestCodexAppServerIgnoresMCPStartupStatusNotifications(t *testing.T) {
	t.Parallel()

	line := []byte(`{"method":"mcpServer/startupStatus/updated","params":{"name":"github","status":"failed","error":"MCP server failed to start","failureReason":null,"threadId":null}}`)
	var envelope codexRPCEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	events, completed, err := parseCodexAppServerNotification(envelope, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("parseCodexAppServerNotification() error = %v", err)
	}
	if completed || len(events) != 0 {
		t.Fatalf("notification result = %#v, completed %v; want ignored notification", events, completed)
	}
}
