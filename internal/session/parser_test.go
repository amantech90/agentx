package session

import "testing"

func TestCodexParserTurnsJSONEventsIntoChatEvents(t *testing.T) {
	t.Parallel()

	parser := codexParser{}
	started, err := parser.Parse([]byte(`{"type":"thread.started","thread_id":"thread-123"}`))
	if err != nil {
		t.Fatalf("Parse(thread.started) error = %v", err)
	}
	if started.ProviderSessionID != "thread-123" {
		t.Fatalf("session id = %q", started.ProviderSessionID)
	}

	command, err := parser.Parse([]byte(`{"type":"item.completed","item":{"id":"item-1","type":"command_execution","command":"go test ./...","aggregated_output":"ok agentx","status":"completed","exit_code":0}}`))
	if err != nil {
		t.Fatalf("Parse(command) error = %v", err)
	}
	if len(command.Events) != 1 || command.Events[0].Kind != "activity" {
		t.Fatalf("command events = %#v", command.Events)
	}
	if command.Events[0].Title != "go test ./..." || command.Events[0].Content != "ok agentx" {
		t.Fatalf("command event = %#v", command.Events[0])
	}

	message, err := parser.Parse([]byte(`{"type":"item.completed","item":{"id":"item-2","type":"agent_message","text":"All tests pass."}}`))
	if err != nil {
		t.Fatalf("Parse(agent_message) error = %v", err)
	}
	if len(message.Events) != 1 || message.Events[0].Role != "assistant" || message.Events[0].Content != "All tests pass." {
		t.Fatalf("message events = %#v", message.Events)
	}
}

func TestCodexParserNormalizesInProgressCommandsToRunning(t *testing.T) {
	t.Parallel()

	parser := codexParser{}
	result, err := parser.Parse([]byte(`{"type":"item.started","item":{"id":"item-running","type":"command_execution","command":"sleep 5","aggregated_output":"","status":"in_progress"}}`))
	if err != nil {
		t.Fatalf("Parse(item.started) error = %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Status != "running" {
		t.Fatalf("events = %#v, want running command", result.Events)
	}
}

func TestClaudeParserTurnsStreamJSONIntoChatEvents(t *testing.T) {
	t.Parallel()

	parser := claudeParser{workspaceRoot: "/workspace"}
	initialized, err := parser.Parse([]byte(`{"type":"system","subtype":"init","session_id":"claude-123"}`))
	if err != nil {
		t.Fatalf("Parse(init) error = %v", err)
	}
	if initialized.ProviderSessionID != "claude-123" {
		t.Fatalf("session id = %q", initialized.ProviderSessionID)
	}

	tool, err := parser.Parse([]byte(`{"type":"assistant","session_id":"claude-123","message":{"id":"message-1","content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}`))
	if err != nil {
		t.Fatalf("Parse(tool_use) error = %v", err)
	}
	if len(tool.Events) != 1 || tool.Events[0].ID != "tool-1" || tool.Events[0].Title != "Run go test ./..." || tool.Events[0].Status != "running" {
		t.Fatalf("tool events = %#v", tool.Events)
	}

	result, err := parser.Parse([]byte(`{"type":"user","session_id":"claude-123","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok agentx","is_error":false}]}}`))
	if err != nil {
		t.Fatalf("Parse(tool_result) error = %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].ID != "tool-1" || result.Events[0].Status != "completed" || result.Events[0].Content != "ok agentx" {
		t.Fatalf("result events = %#v", result.Events)
	}

	message, err := parser.Parse([]byte(`{"type":"assistant","session_id":"claude-123","message":{"id":"message-2","content":[{"type":"text","text":"All tests pass."}]}}`))
	if err != nil {
		t.Fatalf("Parse(text) error = %v", err)
	}
	if len(message.Events) != 1 || message.Events[0].Role != "assistant" || message.Events[0].Content != "All tests pass." {
		t.Fatalf("message events = %#v", message.Events)
	}
}

func TestClaudeParserDescribesFileToolsWithWorkspaceRelativePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolName  string
		input     string
		wantTitle string
	}{
		{name: "read", toolName: "Read", input: `{"file_path":"/workspace/internal/session/parser.go"}`, wantTitle: "Read internal/session/parser.go"},
		{name: "edit", toolName: "Edit", input: `{"file_path":"/workspace/frontend/src/main.js","old_string":"before","new_string":"after"}`, wantTitle: "Edit frontend/src/main.js"},
		{name: "glob", toolName: "Glob", input: `{"pattern":"**/*.go","path":"/workspace/internal"}`, wantTitle: "Find **/*.go in internal"},
		{name: "grep", toolName: "Grep", input: `{"pattern":"tool_use","path":"/workspace"}`, wantTitle: `Search "tool_use" in workspace`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := claudeParser{workspaceRoot: "/workspace"}
			line := []byte(`{"type":"assistant","message":{"id":"message","content":[{"type":"tool_use","id":"tool","name":"` + test.toolName + `","input":` + test.input + `}]}}`)
			result, err := parser.Parse(line)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(result.Events) != 1 || result.Events[0].Title != test.wantTitle {
				t.Fatalf("events = %#v, want title %q", result.Events, test.wantTitle)
			}
		})
	}
}
