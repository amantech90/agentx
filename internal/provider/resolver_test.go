package provider

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCommandResolverFindsCLIOutsideGUIPath(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	command := "agentx-test-cli"
	if runtime.GOOS == "windows" {
		command += ".cmd"
	}
	commandPath := filepath.Join(binDir, command)
	if err := os.WriteFile(commandPath, []byte("test executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolver := commandResolver{
		lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		homeDir:  func() (string, error) { return home, nil },
		getenv:   func(string) string { return "" },
		goos:     runtime.GOOS,
	}

	got, err := resolver.Resolve("agentx-test-cli")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != commandPath {
		t.Fatalf("Resolve() = %q, want %q", got, commandPath)
	}
}

func TestCommandResolverUsesLookPathFirst(t *testing.T) {
	t.Parallel()

	resolver := commandResolver{
		lookPath: func(command string) (string, error) {
			return "/custom/path/" + command, nil
		},
		homeDir: func() (string, error) { return "", errors.New("must not be called") },
		getenv:  func(string) string { return "" },
		goos:    "darwin",
	}

	got, err := resolver.Resolve("codex")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "/custom/path/codex" {
		t.Fatalf("Resolve() = %q, want lookPath result", got)
	}
}

func TestDarwinCandidatesIncludeBothHomebrewPrefixes(t *testing.T) {
	t.Parallel()

	candidates := commandCandidates("claude", "darwin", "/Users/test", func(string) string { return "" })
	wants := []string{
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	}
	for _, want := range wants {
		if !containsPath(candidates, want) {
			t.Errorf("commandCandidates() does not contain %q", want)
		}
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
