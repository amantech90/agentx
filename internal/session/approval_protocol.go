package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"agentx/internal/childproc"
)

const (
	codexCommandApproval = "command"
	codexFileApproval    = "file-change"
)

type protocolWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *protocolWriter) write(value any) error {
	contents, err := json.Marshal(value)
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.writer.Write(contents)
	return err
}

func runClaudeCommand(ctx context.Context, request RunRequest, callbacks RunCallbacks) (RunResult, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	command := exec.CommandContext(runContext, request.ProviderPath, claudeArguments(request)...)
	childproc.Configure(command)
	command.Dir = request.Workspace.RootPath
	command.Env = environmentWithExecutableDir(os.Environ(), request.ProviderPath)
	stdin, err := command.StdinPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("open Claude input: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("open Claude output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start claude: %w", err)
	}

	writer := &protocolWriter{writer: stdin}
	initID, err := randomID()
	if err != nil {
		cancel()
		_ = command.Wait()
		return RunResult{}, fmt.Errorf("create Claude initialization id: %w", err)
	}
	if err := writer.write(map[string]any{
		"type": "control_request", "request_id": initID,
		"request": map[string]any{"subtype": "initialize"},
	}); err != nil {
		cancel()
		_ = command.Wait()
		return RunResult{}, fmt.Errorf("initialize Claude: %w", err)
	}

	parser := &claudeParser{workspaceRoot: request.Workspace.RootPath}
	providerSessionID := request.ProviderSessionID
	initialized := false
	finished := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxProtocolLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Error     string `json:"error"`
			} `json:"response"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			cancel()
			_ = command.Wait()
			return RunResult{}, fmt.Errorf("decode Claude event: %w", err)
		}

		if envelope.Type == "control_response" && envelope.Response.RequestID == initID {
			if envelope.Response.Subtype != "success" {
				cancel()
				_ = command.Wait()
				return RunResult{}, fmt.Errorf("initialize Claude: %s", strings.TrimSpace(envelope.Response.Error))
			}
			if !initialized {
				initialized = true
				if err := writer.write(claudeUserMessage(providerInput(request))); err != nil {
					cancel()
					_ = command.Wait()
					return RunResult{}, fmt.Errorf("send Claude message: %w", err)
				}
			}
			continue
		}

		if envelope.Type == "control_request" {
			requestID, input, approval, ok, parseErr := parseClaudeApprovalRequest(line, request.Workspace.RootPath)
			if parseErr != nil {
				cancel()
				_ = command.Wait()
				return RunResult{}, parseErr
			}
			if !ok {
				_ = writer.write(claudeControlError(envelope.RequestID, "Agent X does not support this interactive request."))
				continue
			}
			decision := ApprovalDeny
			var approvalErr error
			if callbacks.RequestApproval != nil {
				decision, approvalErr = callbacks.RequestApproval(runContext, approval)
			}
			if approvalErr != nil {
				decision = ApprovalDeny
			}
			response, responseErr := claudeApprovalResponse(requestID, decision, input)
			if responseErr != nil {
				cancel()
				_ = command.Wait()
				return RunResult{}, responseErr
			}
			if _, err := stdin.Write(response); err != nil {
				cancel()
				_ = command.Wait()
				if ctx.Err() != nil {
					return RunResult{ProviderSessionID: providerSessionID}, ctx.Err()
				}
				return RunResult{}, fmt.Errorf("answer Claude approval: %w", err)
			}
			continue
		}

		parsed, parseErr := parser.Parse(line)
		if parseErr != nil {
			cancel()
			_ = command.Wait()
			return RunResult{}, parseErr
		}
		if parsed.ProviderSessionID != "" {
			providerSessionID = parsed.ProviderSessionID
		}
		for _, event := range parsed.Events {
			callbacks.Emit(event)
		}
		if envelope.Type == "result" {
			finished = true
			_ = stdin.Close()
		}
	}
	if err := scanner.Err(); err != nil {
		cancel()
		_ = command.Wait()
		return RunResult{}, fmt.Errorf("read Claude output: %w", err)
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return RunResult{ProviderSessionID: providerSessionID}, ctx.Err()
		}
		return RunResult{ProviderSessionID: providerSessionID}, providerExitError("claude", err, stderr.String())
	}
	if !finished {
		return RunResult{ProviderSessionID: providerSessionID}, errors.New("claude ended before completing the turn")
	}
	return RunResult{ProviderSessionID: providerSessionID}, nil
}

func claudeUserMessage(prompt string) map[string]any {
	return map[string]any{
		"type": "user", "session_id": "", "parent_tool_use_id": nil,
		"message": map[string]any{
			"role":    "user",
			"content": []any{map[string]any{"type": "text", "text": prompt}},
		},
	}
}

func parseClaudeApprovalRequest(line []byte, workspaceRoot string) (string, map[string]any, ApprovalRequest, bool, error) {
	var envelope struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype                 string         `json:"subtype"`
			ToolName                string         `json:"tool_name"`
			Input                   map[string]any `json:"input"`
			DecisionReason          string         `json:"decision_reason"`
			Title                   string         `json:"title"`
			Description             string         `json:"description"`
			RequiresUserInteraction bool           `json:"requires_user_interaction"`
		} `json:"request"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return "", nil, ApprovalRequest{}, false, fmt.Errorf("decode Claude approval: %w", err)
	}
	if envelope.Type != "control_request" || envelope.Request.Subtype != "can_use_tool" || envelope.RequestID == "" {
		return envelope.RequestID, nil, ApprovalRequest{}, false, nil
	}
	if envelope.Request.RequiresUserInteraction {
		return envelope.RequestID, envelope.Request.Input, ApprovalRequest{}, false, nil
	}
	rawInput, _ := json.Marshal(envelope.Request.Input)
	command := mapString(envelope.Request.Input, "command")
	paths := claudeApprovalPaths(envelope.Request.Input)
	title := strings.TrimSpace(envelope.Request.Title)
	if title == "" {
		title = describeClaudeTool(envelope.Request.ToolName, rawInput, workspaceRoot)
	}
	reason := strings.TrimSpace(envelope.Request.DecisionReason)
	if reason == "" {
		reason = strings.TrimSpace(envelope.Request.Description)
	}
	return envelope.RequestID, envelope.Request.Input, ApprovalRequest{
		Kind: approvalKindForTool(envelope.Request.ToolName), Tool: envelope.Request.ToolName,
		Title: title, Command: command, Paths: paths, WorkingDirectory: workspaceRoot,
		Reason: stripANSI(reason),
	}, true, nil
}

func claudeApprovalResponse(requestID string, decision ApprovalDecision, input map[string]any) ([]byte, error) {
	result := map[string]any{"behavior": "deny", "message": "Denied in Agent X", "interrupt": false}
	if decision == ApprovalAllow {
		result = map[string]any{"behavior": "allow", "updatedInput": input}
	}
	contents, err := json.Marshal(map[string]any{
		"type":     "control_response",
		"response": map[string]any{"subtype": "success", "request_id": requestID, "response": result},
	})
	if err != nil {
		return nil, fmt.Errorf("encode Claude approval: %w", err)
	}
	return append(contents, '\n'), nil
}

func claudeControlError(requestID, message string) map[string]any {
	return map[string]any{
		"type":     "control_response",
		"response": map[string]any{"subtype": "error", "request_id": requestID, "error": message},
	}
}

func claudeApprovalPaths(input map[string]any) []string {
	paths := make([]string, 0, 2)
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		if value := mapString(input, key); value != "" {
			paths = append(paths, value)
		}
	}
	return paths
}

func approvalKindForTool(tool string) string {
	switch tool {
	case "Bash":
		return "command"
	case "Edit", "Write", "NotebookEdit":
		return "file-change"
	default:
		return "tool"
	}
}

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func stripANSI(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '[' {
			index += 2
			for index < len(value) {
				character := value[index]
				if character >= 0x40 && character <= 0x7e {
					break
				}
				index++
			}
			continue
		}
		result.WriteByte(value[index])
	}
	return result.String()
}

type codexRPCEnvelope struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func runCodexAppServer(ctx context.Context, request RunRequest, callbacks RunCallbacks) (RunResult, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(runContext, request.ProviderPath, "app-server")
	childproc.Configure(command)
	command.Dir = request.Workspace.RootPath
	command.Env = environmentWithExecutableDir(os.Environ(), request.ProviderPath)
	stdin, err := command.StdinPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("open Codex app-server input: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("open Codex app-server output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start codex app-server: %w", err)
	}
	writer := &protocolWriter{writer: stdin}
	if err := writer.write(map[string]any{
		"method": "initialize", "id": "agentx-init",
		"params": map[string]any{"clientInfo": map[string]any{"name": "agent_x", "title": "Agent X", "version": "0.1.0"}},
	}); err != nil {
		cancel()
		_ = command.Wait()
		return RunResult{}, fmt.Errorf("initialize Codex app-server: %w", err)
	}

	providerSessionID := request.ProviderSessionID
	messageText := make(map[string]string)
	commandOutput := make(map[string]string)
	finished := false
	var turnErr error
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxProtocolLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope codexRPCEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			cancel()
			_ = command.Wait()
			return RunResult{}, fmt.Errorf("decode Codex app-server event: %w", err)
		}

		if len(envelope.ID) > 0 && envelope.Method != "" {
			id, approval, kind, ok, parseErr := parseCodexApprovalRequest(line)
			if parseErr != nil {
				cancel()
				_ = command.Wait()
				return RunResult{}, parseErr
			}
			if !ok {
				_ = writer.write(codexRPCError(id, -32601, "Agent X does not support this server request."))
				continue
			}
			decision := ApprovalDeny
			var approvalErr error
			if callbacks.RequestApproval != nil {
				decision, approvalErr = callbacks.RequestApproval(runContext, approval)
			}
			if approvalErr != nil {
				decision = ApprovalDeny
			}
			response, responseErr := codexApprovalResponse(id, kind, decision)
			if responseErr != nil {
				cancel()
				_ = command.Wait()
				return RunResult{}, responseErr
			}
			if _, err := stdin.Write(response); err != nil {
				cancel()
				_ = command.Wait()
				if ctx.Err() != nil {
					return RunResult{ProviderSessionID: providerSessionID}, ctx.Err()
				}
				return RunResult{}, fmt.Errorf("answer Codex approval: %w", err)
			}
			continue
		}

		if len(envelope.ID) > 0 {
			id := rpcIDString(envelope.ID)
			if envelope.Error != nil {
				cancel()
				_ = command.Wait()
				return RunResult{ProviderSessionID: providerSessionID}, fmt.Errorf("Codex %s: %s", id, envelope.Error.Message)
			}
			switch id {
			case "agentx-init":
				if err := writer.write(map[string]any{"method": "initialized"}); err != nil {
					return RunResult{}, fmt.Errorf("finish Codex initialization: %w", err)
				}
				if request.ProviderSessionID != "" {
					err = writeCodexThreadResume(writer, request.ProviderSessionID, request.Workspace.RootPath)
				} else if request.ResumeLatest {
					err = writer.write(map[string]any{
						"method": "thread/list", "id": "agentx-list",
						"params": map[string]any{
							"cwd": request.Workspace.RootPath, "limit": 1, "sortKey": "updated_at", "sortDirection": "desc",
							"sourceKinds": []string{"cli", "vscode", "exec", "appServer"},
						},
					})
				} else {
					err = writeCodexThreadStart(writer, request.Workspace.RootPath)
				}
			case "agentx-list":
				var result struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if err = json.Unmarshal(envelope.Result, &result); err == nil {
					if len(result.Data) > 0 && result.Data[0].ID != "" {
						err = writeCodexThreadResume(writer, result.Data[0].ID, request.Workspace.RootPath)
					} else {
						err = writeCodexThreadStart(writer, request.Workspace.RootPath)
					}
				}
			case "agentx-thread":
				var result struct {
					Thread struct {
						ID string `json:"id"`
					} `json:"thread"`
				}
				if err = json.Unmarshal(envelope.Result, &result); err == nil {
					providerSessionID = result.Thread.ID
					if providerSessionID == "" {
						err = errors.New("Codex returned an empty thread id")
					} else {
						err = writeCodexTurnStart(writer, request, providerSessionID)
					}
				}
			}
			if err != nil {
				cancel()
				_ = command.Wait()
				return RunResult{ProviderSessionID: providerSessionID}, fmt.Errorf("continue Codex protocol: %w", err)
			}
			continue
		}

		events, completed, completedErr := parseCodexAppServerNotification(envelope, messageText, commandOutput)
		for _, event := range events {
			callbacks.Emit(event)
		}
		if completed {
			finished = true
			turnErr = completedErr
			break
		}
		if completedErr != nil {
			cancel()
			_ = command.Wait()
			return RunResult{ProviderSessionID: providerSessionID}, completedErr
		}
	}
	if err := scanner.Err(); err != nil {
		cancel()
		_ = command.Wait()
		return RunResult{ProviderSessionID: providerSessionID}, fmt.Errorf("read Codex app-server output: %w", err)
	}
	if finished {
		cancel()
		_ = command.Wait()
		return RunResult{ProviderSessionID: providerSessionID}, turnErr
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return RunResult{ProviderSessionID: providerSessionID}, ctx.Err()
		}
		return RunResult{ProviderSessionID: providerSessionID}, providerExitError("codex", err, stderr.String())
	}
	return RunResult{ProviderSessionID: providerSessionID}, errors.New("codex app-server ended before completing the turn")
}

func writeCodexThreadStart(writer *protocolWriter, cwd string) error {
	return writer.write(map[string]any{
		"method": "thread/start", "id": "agentx-thread",
		"params": map[string]any{
			"cwd": cwd, "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": "workspace-write",
		},
	})
}

func writeCodexThreadResume(writer *protocolWriter, threadID, cwd string) error {
	return writer.write(map[string]any{
		"method": "thread/resume", "id": "agentx-thread",
		"params": map[string]any{
			"threadId": threadID, "cwd": cwd, "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": "workspace-write",
		},
	})
}

func writeCodexTurnStart(writer *protocolWriter, request RunRequest, threadID string) error {
	input := []any{map[string]any{"type": "text", "text": providerInput(request)}}
	for _, path := range request.ScreenshotPaths {
		input = append(input, map[string]any{"type": "localImage", "path": path})
	}
	return writer.write(map[string]any{
		"method": "turn/start", "id": "agentx-turn",
		"params": map[string]any{
			"threadId": threadID, "cwd": request.Workspace.RootPath, "input": input,
			"approvalPolicy": "on-request", "approvalsReviewer": "user",
		},
	})
}

func parseCodexApprovalRequest(line []byte) (json.RawMessage, ApprovalRequest, string, bool, error) {
	var envelope codexRPCEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, ApprovalRequest{}, "", false, fmt.Errorf("decode Codex approval: %w", err)
	}
	if len(envelope.ID) == 0 {
		return nil, ApprovalRequest{}, "", false, nil
	}
	switch envelope.Method {
	case "item/commandExecution/requestApproval":
		var params struct {
			Command string `json:"command"`
			CWD     string `json:"cwd"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			return nil, ApprovalRequest{}, "", false, fmt.Errorf("decode Codex command approval: %w", err)
		}
		title := "Run command"
		if params.Command != "" {
			title = "Run " + truncateRunes(singleLine(params.Command), 160)
		}
		return envelope.ID, ApprovalRequest{
			Kind: "command", Tool: "Shell", Title: title, Command: params.Command,
			WorkingDirectory: params.CWD, Reason: params.Reason,
		}, codexCommandApproval, true, nil
	case "item/fileChange/requestApproval":
		var params struct {
			GrantRoot string `json:"grantRoot"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			return nil, ApprovalRequest{}, "", false, fmt.Errorf("decode Codex file approval: %w", err)
		}
		paths := []string{}
		if params.GrantRoot != "" {
			paths = append(paths, params.GrantRoot)
		}
		return envelope.ID, ApprovalRequest{
			Kind: "file-change", Tool: "Apply patch", Title: "Apply file changes", Paths: paths, Reason: params.Reason,
		}, codexFileApproval, true, nil
	default:
		return envelope.ID, ApprovalRequest{}, "", false, nil
	}
}

func codexApprovalResponse(id json.RawMessage, kind string, decision ApprovalDecision) ([]byte, error) {
	value := "decline"
	if decision == ApprovalAllow {
		value = "accept"
	}
	if kind != codexCommandApproval && kind != codexFileApproval {
		return nil, errors.New("unsupported Codex approval kind")
	}
	contents, err := json.Marshal(struct {
		ID     json.RawMessage `json:"id"`
		Result map[string]any  `json:"result"`
	}{ID: id, Result: map[string]any{"decision": value}})
	if err != nil {
		return nil, fmt.Errorf("encode Codex approval: %w", err)
	}
	return append(contents, '\n'), nil
}

func codexRPCError(id json.RawMessage, code int, message string) any {
	return struct {
		ID    json.RawMessage `json:"id"`
		Error map[string]any  `json:"error"`
	}{ID: id, Error: map[string]any{"code": code, "message": message}}
}

func rpcIDString(id json.RawMessage) string {
	var text string
	if json.Unmarshal(id, &text) == nil {
		return text
	}
	return strings.TrimSpace(string(id))
}

func parseCodexAppServerNotification(envelope codexRPCEnvelope, messageText, commandOutput map[string]string) ([]Event, bool, error) {
	switch envelope.Method {
	case "item/agentMessage/delta", "item/commandExecution/outputDelta", "item/started", "item/completed", "turn/completed", "error":
	default:
		return nil, false, nil
	}

	var params struct {
		Delta  string `json:"delta"`
		ItemID string `json:"itemId"`
		Item   struct {
			ID               string          `json:"id"`
			Type             string          `json:"type"`
			Text             string          `json:"text"`
			Status           string          `json:"status"`
			Command          string          `json:"command"`
			CWD              string          `json:"cwd"`
			AggregatedOutput string          `json:"aggregatedOutput"`
			Changes          json.RawMessage `json:"changes"`
			Server           string          `json:"server"`
			Tool             string          `json:"tool"`
		} `json:"item"`
		Turn struct {
			Status string      `json:"status"`
			Error  *codexError `json:"error"`
		} `json:"turn"`
		Error *codexError `json:"error"`
	}
	if len(envelope.Params) > 0 {
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			return nil, false, fmt.Errorf("decode Codex %s notification: %w", envelope.Method, err)
		}
	}
	switch envelope.Method {
	case "item/agentMessage/delta":
		messageText[params.ItemID] += params.Delta
		return []Event{{ID: params.ItemID, Kind: "message", Role: "assistant", Content: messageText[params.ItemID], Status: "running"}}, false, nil
	case "item/commandExecution/outputDelta":
		commandOutput[params.ItemID] += params.Delta
		return []Event{{ID: params.ItemID, Kind: "activity", Role: "system", Content: boundedPreview(commandOutput[params.ItemID], 6000), Status: "running"}}, false, nil
	case "item/started", "item/completed":
		item := params.Item
		status := "running"
		if envelope.Method == "item/completed" {
			status = normalizeCodexAppServerStatus(item.Status)
		}
		switch item.Type {
		case "agentMessage":
			if item.Text == "" {
				return nil, false, nil
			}
			messageText[item.ID] = item.Text
			return []Event{{ID: item.ID, Kind: "message", Role: "assistant", Content: item.Text, Status: status}}, false, nil
		case "commandExecution":
			content := item.AggregatedOutput
			if content == "" {
				content = commandOutput[item.ID]
			}
			return []Event{{ID: item.ID, Kind: "activity", Role: "system", Title: item.Command, Content: boundedPreview(content, 6000), Status: status}}, false, nil
		case "fileChange":
			return []Event{{ID: item.ID, Kind: "activity", Role: "system", Title: "File changes", Content: prettyJSON(item.Changes), Status: status}}, false, nil
		case "mcpToolCall":
			title := strings.TrimSpace(strings.Join([]string{item.Server, item.Tool}, " · "))
			return []Event{{ID: item.ID, Kind: "activity", Role: "system", Title: title, Status: status}}, false, nil
		}
	case "turn/completed":
		if params.Turn.Status == "failed" {
			message := "Codex could not complete the turn."
			if params.Turn.Error != nil && strings.TrimSpace(params.Turn.Error.Message) != "" {
				message = strings.TrimSpace(params.Turn.Error.Message)
			}
			return nil, true, errors.New(message)
		}
		if params.Turn.Status == "interrupted" {
			return nil, true, errors.New("Codex turn was interrupted")
		}
		return nil, true, nil
	case "error":
		message := "Codex reported an error."
		if params.Error != nil && strings.TrimSpace(params.Error.Message) != "" {
			message = strings.TrimSpace(params.Error.Message)
		}
		return nil, false, errors.New(message)
	}
	return nil, false, nil
}

func normalizeCodexAppServerStatus(status string) string {
	switch status {
	case "failed", "declined", "cancelled", "canceled":
		return "failed"
	case "inProgress":
		return "running"
	default:
		return "completed"
	}
}

func providerExitError(provider string, err error, stderr string) error {
	message := strings.TrimSpace(stderr)
	if len(message) > 4000 {
		message = message[len(message)-4000:]
	}
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("%s exited: %s", provider, message)
}

func displayApprovalPath(path, workspaceRoot string) string {
	if relative, err := filepath.Rel(workspaceRoot, path); err == nil && relative != "." && !strings.HasPrefix(relative, "..") {
		return relative
	}
	return path
}
