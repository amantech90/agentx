package session

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agentx/internal/childproc"
	"agentx/internal/model"
)

const maxProtocolLine = 16 * 1024 * 1024

const (
	ClaudePermissionDefault     = "default"
	ClaudePermissionAuto        = "auto"
	ClaudePermissionAcceptEdits = "acceptEdits"
	ClaudePermissionPlan        = "plan"
)

type Event struct {
	ID      string
	Kind    string
	Role    string
	Title   string
	Content string
	Status  string
}

type ApprovalDecision string

const (
	ApprovalAllow ApprovalDecision = "allow"
	ApprovalDeny  ApprovalDecision = "deny"
)

type ApprovalRequest struct {
	Kind             string
	Tool             string
	Title            string
	Command          string
	Paths            []string
	WorkingDirectory string
	Reason           string
}

type RunCallbacks struct {
	Emit            func(Event)
	RequestApproval func(context.Context, ApprovalRequest) (ApprovalDecision, error)
}

type RunRequest struct {
	Workspace         model.Workspace
	ProviderPath      string
	ProviderSessionID string
	ResumeLatest      bool
	Prompt            string
	PermissionMode    string
	ScreenshotPaths   []string
}

type RunResult struct {
	ProviderSessionID string
}

type Runner interface {
	Run(context.Context, RunRequest, RunCallbacks) (RunResult, error)
}

type codexRunner struct{}
type claudeRunner struct{}

func NewCodexRunner() Runner  { return codexRunner{} }
func NewClaudeRunner() Runner { return claudeRunner{} }

func (codexRunner) Run(ctx context.Context, request RunRequest, callbacks RunCallbacks) (RunResult, error) {
	return runCodexAppServer(ctx, request, callbacks)
}

func (claudeRunner) Run(ctx context.Context, request RunRequest, callbacks RunCallbacks) (RunResult, error) {
	result, err := runClaudeCommand(ctx, request, callbacks)
	if shouldStartFresh(request, err) {
		request.ResumeLatest = false
		return runClaudeCommand(ctx, request, callbacks)
	}
	return result, err
}

func codexArguments(request RunRequest) []string {
	imageArgs := make([]string, 0, len(request.ScreenshotPaths)*2)
	for _, path := range request.ScreenshotPaths {
		imageArgs = append(imageArgs, "--image", path)
	}
	if request.ProviderSessionID != "" {
		return append([]string{"exec", "resume", "--json", "--skip-git-repo-check"}, append(imageArgs, request.ProviderSessionID, "-")...)
	}
	if request.ResumeLatest {
		return append([]string{"exec", "resume", "--last", "--json", "--skip-git-repo-check"}, append(imageArgs, "-")...)
	}
	return append([]string{"exec", "--json", "--skip-git-repo-check"}, append(imageArgs, "-")...)
}

// NormalizePermissionMode keeps the app on the explicit, safety-aware Claude
// modes it exposes in the UI. In particular, bypassPermissions is never passed
// through even if a caller crafts its own frontend request.
func NormalizePermissionMode(providerID, requested string) string {
	if providerID != "claude" {
		return ""
	}
	switch strings.TrimSpace(requested) {
	case ClaudePermissionDefault:
		return ClaudePermissionDefault
	case ClaudePermissionAcceptEdits:
		return ClaudePermissionAcceptEdits
	case ClaudePermissionPlan:
		return ClaudePermissionPlan
	case ClaudePermissionAuto:
		return ClaudePermissionAuto
	default:
		return ClaudePermissionDefault
	}
}

func claudeArguments(request RunRequest) []string {
	args := []string{
		"-p", "--verbose", "--output-format", "stream-json",
		"--input-format", "stream-json", "--permission-prompt-tool", "stdio",
		"--permission-mode", NormalizePermissionMode("claude", request.PermissionMode),
	}
	if request.ProviderSessionID != "" {
		args = append(args, "--resume", request.ProviderSessionID)
	} else if request.ResumeLatest {
		args = append(args, "--continue")
	}
	return args
}

func shouldStartFresh(request RunRequest, err error) bool {
	if !request.ResumeLatest || request.ProviderSessionID != "" || err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no session found",
		"no sessions found",
		"no previous session",
		"no conversation found",
		"no conversations found",
		"no previous conversation",
		"no recent conversation",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func runStructuredCommand(ctx context.Context, request RunRequest, args []string, parser lineParser, emit func(Event)) (RunResult, error) {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	command := exec.CommandContext(runContext, request.ProviderPath, args...)
	childproc.Configure(command)
	command.Dir = request.Workspace.RootPath
	command.Stdin = strings.NewReader(providerInput(request))
	command.Env = environmentWithExecutableDir(os.Environ(), request.ProviderPath)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("open provider output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start %s: %w", request.Workspace.ProviderID, err)
	}

	providerSessionID := request.ProviderSessionID
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxProtocolLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
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
			emit(event)
		}
	}
	if err := scanner.Err(); err != nil {
		cancel()
		_ = command.Wait()
		return RunResult{}, fmt.Errorf("read %s output: %w", request.Workspace.ProviderID, err)
	}
	if err := command.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if len(message) > 4000 {
			message = message[len(message)-4000:]
		}
		if message == "" {
			message = err.Error()
		}
		return RunResult{ProviderSessionID: providerSessionID}, fmt.Errorf("%s exited: %s", request.Workspace.ProviderID, message)
	}
	return RunResult{ProviderSessionID: providerSessionID}, nil
}

func providerInput(request RunRequest) string {
	prompt := strings.TrimSpace(request.Prompt)
	if len(request.ScreenshotPaths) == 0 {
		return prompt
	}
	if prompt == "" {
		prompt = "Please inspect the attached screenshot."
	}
	if request.Workspace.ProviderID != "claude" {
		return prompt
	}
	paths := make([]string, 0, len(request.ScreenshotPaths))
	for _, path := range request.ScreenshotPaths {
		displayPath := path
		if relative, err := filepath.Rel(request.Workspace.RootPath, path); err == nil {
			displayPath = relative
		}
		paths = append(paths, displayPath)
	}
	return prompt + "\n\nA screenshot was attached through Agent X. Use the Read tool to inspect it before responding: " + strings.Join(paths, ", ")
}

func environmentWithExecutableDir(environ []string, executablePath string) []string {
	directory := filepath.Dir(executablePath)
	if directory == "." || directory == "" {
		return environ
	}
	result := append([]string(nil), environ...)
	for index, entry := range result {
		key, value, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(key, "PATH") {
			result[index] = key + "=" + directory + string(os.PathListSeparator) + value
			return result
		}
	}
	return append(result, "PATH="+directory)
}
