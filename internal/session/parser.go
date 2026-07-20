package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// codexError decodes Codex's "error" field, which the app-server sends either
// as a bare string ("error": "boom") or as an object ("error": {"message":
// "boom"}). Both shapes populate Message so downstream handlers can read it
// uniformly instead of failing to unmarshal one form or the other.
type codexError struct {
	Message string
}

func (e *codexError) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &e.Message)
	}
	var object struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	e.Message = object.Message
	return nil
}

type ParseResult struct {
	ProviderSessionID string
	Events            []Event
}

type lineParser interface {
	Parse([]byte) (ParseResult, error)
}

type codexParser struct{}

func (codexParser) Parse(line []byte) (ParseResult, error) {
	var envelope struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
		Message  string     `json:"message"`
		Error    codexError `json:"error"`
		Item     struct {
			ID               string          `json:"id"`
			Type             string          `json:"type"`
			Text             string          `json:"text"`
			Status           string          `json:"status"`
			Command          json.RawMessage `json:"command"`
			AggregatedOutput string          `json:"aggregated_output"`
			Changes          json.RawMessage `json:"changes"`
			Name             string          `json:"name"`
		} `json:"item"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return ParseResult{}, fmt.Errorf("decode Codex event: %w", err)
	}

	result := ParseResult{ProviderSessionID: envelope.ThreadID}
	switch envelope.Type {
	case "item.started", "item.completed":
		status := normalizeCodexItemStatus(envelope.Type, envelope.Item.Status)
		switch envelope.Item.Type {
		case "agent_message":
			if text := strings.TrimSpace(envelope.Item.Text); text != "" && envelope.Type == "item.completed" {
				result.Events = append(result.Events, Event{
					ID: envelope.Item.ID, Kind: "message", Role: "assistant", Content: text, Status: status,
				})
			}
		case "command_execution":
			result.Events = append(result.Events, Event{
				ID:      envelope.Item.ID,
				Kind:    "activity",
				Role:    "system",
				Title:   rawText(envelope.Item.Command, "Running command"),
				Content: strings.TrimSpace(envelope.Item.AggregatedOutput),
				Status:  status,
			})
		case "file_change":
			result.Events = append(result.Events, Event{
				ID: envelope.Item.ID, Kind: "activity", Role: "system", Title: "File changes",
				Content: prettyJSON(envelope.Item.Changes), Status: status,
			})
		case "mcp_tool_call", "dynamic_tool_call":
			title := strings.TrimSpace(envelope.Item.Name)
			if title == "" {
				title = "Tool call"
			}
			result.Events = append(result.Events, Event{
				ID: envelope.Item.ID, Kind: "activity", Role: "system", Title: title, Status: status,
			})
		}
	case "error", "turn.failed":
		message := strings.TrimSpace(envelope.Error.Message)
		if message == "" {
			message = strings.TrimSpace(envelope.Message)
		}
		if message == "" {
			message = "Codex could not complete the turn."
		}
		result.Events = append(result.Events, Event{Kind: "error", Role: "system", Content: message, Status: "failed"})
	}
	return result, nil
}

func normalizeCodexItemStatus(eventType, status string) string {
	if eventType == "item.started" {
		return "running"
	}
	switch status {
	case "failed", "error", "cancelled", "canceled", "declined":
		return "failed"
	default:
		return "completed"
	}
}

type claudeParser struct {
	sawAssistantText bool
	workspaceRoot    string
	tools            map[string]string
}

func (p *claudeParser) Parse(line []byte) (ParseResult, error) {
	var envelope struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Result    string `json:"result"`
		IsError   bool   `json:"is_error"`
		Message   struct {
			ID      string `json:"id"`
			Content []struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
				IsError   bool            `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return ParseResult{}, fmt.Errorf("decode Claude event: %w", err)
	}

	result := ParseResult{ProviderSessionID: envelope.SessionID}
	switch envelope.Type {
	case "assistant":
		for index, content := range envelope.Message.Content {
			switch content.Type {
			case "text":
				if text := strings.TrimSpace(content.Text); text != "" {
					p.sawAssistantText = true
					result.Events = append(result.Events, Event{
						ID: fmt.Sprintf("%s-text-%d", envelope.Message.ID, index), Kind: "message", Role: "assistant", Content: text,
					})
				}
			case "tool_use":
				if p.tools == nil {
					p.tools = make(map[string]string)
				}
				p.tools[content.ID] = content.Name
				result.Events = append(result.Events, Event{
					ID: content.ID, Kind: "activity", Role: "system",
					Title: describeClaudeTool(content.Name, content.Input, p.workspaceRoot), Status: "running",
				})
			}
		}
	case "user":
		for _, content := range envelope.Message.Content {
			if content.Type != "tool_result" {
				continue
			}
			status := "completed"
			if content.IsError {
				status = "failed"
			}
			toolName := p.tools[content.ToolUseID]
			toolOutput := rawText(content.Content, "")
			if toolName == "Read" {
				toolOutput = ""
			} else {
				toolOutput = boundedPreview(toolOutput, 6000)
			}
			result.Events = append(result.Events, Event{
				ID: content.ToolUseID, Kind: "activity", Role: "system",
				Content: toolOutput, Status: status,
			})
		}
	case "result":
		if envelope.IsError {
			message := strings.TrimSpace(envelope.Result)
			if message == "" {
				message = "Claude could not complete the turn."
			}
			result.Events = append(result.Events, Event{Kind: "error", Role: "system", Content: message, Status: "failed"})
		} else if !p.sawAssistantText && strings.TrimSpace(envelope.Result) != "" {
			result.Events = append(result.Events, Event{Kind: "message", Role: "assistant", Content: strings.TrimSpace(envelope.Result)})
		}
	}
	return result, nil
}

func describeClaudeTool(name string, input json.RawMessage, workspaceRoot string) string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input, &fields)
	value := func(key string) string { return jsonString(fields[key]) }
	path := func(key string) string { return displayToolPath(value(key), workspaceRoot) }

	switch name {
	case "Bash":
		command := singleLine(value("command"))
		if command == "" {
			command = singleLine(value("description"))
		}
		if command != "" {
			return "Run " + truncateRunes(command, 160)
		}
	case "Read":
		if file := path("file_path"); file != "" {
			return "Read " + file
		}
	case "Edit":
		if file := path("file_path"); file != "" {
			return "Edit " + file
		}
	case "Write":
		if file := path("file_path"); file != "" {
			return "Write " + file
		}
	case "Glob":
		pattern := value("pattern")
		location := path("path")
		if pattern != "" && location != "" {
			return fmt.Sprintf("Find %s in %s", pattern, location)
		}
		if pattern != "" {
			return "Find " + pattern
		}
	case "Grep":
		pattern := value("pattern")
		location := path("path")
		if location == "" {
			location = "workspace"
		}
		if pattern != "" {
			return fmt.Sprintf("Search %q in %s", truncateRunes(singleLine(pattern), 90), location)
		}
	case "NotebookEdit":
		if file := path("notebook_path"); file != "" {
			return "Edit notebook " + file
		}
	case "WebFetch":
		if url := value("url"); url != "" {
			return "Fetch " + truncateRunes(url, 140)
		}
	case "WebSearch":
		if query := value("query"); query != "" {
			return fmt.Sprintf("Search web for %q", truncateRunes(singleLine(query), 120))
		}
	case "Task":
		if description := value("description"); description != "" {
			return truncateRunes(singleLine(description), 150)
		}
	case "Skill":
		if skill := value("skill"); skill != "" {
			return "Use " + skill
		}
	case "TodoWrite":
		return "Update task list"
	}

	if name == "" {
		return "Use tool"
	}
	return name
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func displayToolPath(path, workspaceRoot string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if workspaceRoot != "" && filepath.IsAbs(cleaned) {
		relative, err := filepath.Rel(filepath.Clean(workspaceRoot), cleaned)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			if relative == "." {
				return "workspace"
			}
			return filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(cleaned)
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum-1]) + "…"
}

func boundedPreview(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	tail := maximum / 5
	return string(runes[:maximum-tail]) + "\n… output truncated …\n" + string(runes[len(runes)-tail:])
}

func rawText(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return fallback
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if value := strings.TrimSpace(text); value != "" {
			return value
		}
		return fallback
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return strings.Join(values, " ")
	}
	return prettyJSON(raw)
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return compact.String()
}
