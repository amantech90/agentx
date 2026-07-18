package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentx/internal/model"
)

type lookPathFunc func(string) (string, error)
type versionFunc func(context.Context, string) string

type Detector struct {
	lookPath lookPathFunc
	version  versionFunc
}

func NewDetector() *Detector {
	resolver := newCommandResolver()
	return &Detector{
		lookPath: resolver.Resolve,
		version:  commandVersion,
	}
}

func NewDetectorForTest(lookPath lookPathFunc, version versionFunc) *Detector {
	return &Detector{lookPath: lookPath, version: version}
}

func (d *Detector) Detect(ctx context.Context) []model.Provider {
	manifests := []model.Provider{
		{ID: "claude", Name: "Claude", Command: "claude", Supported: true, Description: "Claude Code CLI"},
		{ID: "codex", Name: "Codex", Command: "codex", Supported: true, Description: "OpenAI Codex CLI"},
		{ID: "gemini", Name: "Gemini", Command: "gemini", ComingSoon: true, Description: "Gemini CLI support is planned"},
	}

	for index := range manifests {
		path, err := d.lookPath(manifests[index].Command)
		if err != nil {
			continue
		}
		manifests[index].Installed = true
		manifests[index].Path = path
		manifests[index].Version = d.version(ctx, path)
	}
	return manifests
}

func commandVersion(parent context.Context, path string) string {
	ctx, cancel := context.WithTimeout(parent, 1500*time.Millisecond)
	defer cancel()

	command := exec.CommandContext(ctx, path, "--version")
	command.Env = environmentWithExecutableDir(os.Environ(), filepath.Clean(path))
	output, err := command.CombinedOutput()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(output))
	if newline := strings.IndexByte(version, '\n'); newline >= 0 {
		version = version[:newline]
	}
	if len(version) > 120 {
		version = version[:120]
	}
	return version
}
